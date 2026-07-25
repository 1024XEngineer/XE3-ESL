package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const temporaryAudioIDPrefix = "tmp_audio_"

var (
	ErrTemporaryAudioNotFound = errors.New("temporary audio not found")
	ErrTemporaryAudioCapacity = errors.New("temporary audio capacity exhausted")
	ErrTemporaryAudioClosed   = errors.New("temporary audio vault is closed")
)

// TemporaryAudioVaultConfig makes the operational retention and capacity
// limits explicit. The vault has no permissive production defaults.
type TemporaryAudioVaultConfig struct {
	ScratchDirectory              string
	Lifetime                      time.Duration
	MaxItems                      int
	MaxBytes                      int64
	MaxItemsPerActor              int
	MaxBytesPerActor              int64
	MaxConcurrentCaptures         int
	MaxConcurrentCapturesPerActor int
}

// TemporaryAudioMetadata is safe to return across the media capability
// boundary. It deliberately contains neither a filesystem path nor raw bytes.
type TemporaryAudioMetadata struct {
	ID         string
	MediaType  string
	Size       int64
	Duration   time.Duration
	SampleRate int
	ExpiresAt  time.Time
}

type temporaryAudioEntry struct {
	ownerID string
	data    []byte
	meta    TemporaryAudioMetadata
	readers int
	removed bool
}

type actorAudioUsage struct {
	items         int
	bytes         int64
	captures      int
	reservedBytes int64
	reservedItems int
}

// TemporaryAudioVault is an actor-bound, process-local holding area for an
// unconfirmed recording. It keeps bytes only in memory, removes validation
// scratch files immediately, and never exposes a storage path or public URL.
//
// Conversation remains the owner of AudioAsset business metadata. This vault
// only supplies the narrow temporary file-content capability needed before
// the accepted Conversation persistence contract is implemented.
type TemporaryAudioVault struct {
	mu       sync.Mutex
	captures sync.WaitGroup

	config         TemporaryAudioVaultConfig
	entries        map[string]*temporaryAudioEntry
	actorUsage     map[string]*actorAudioUsage
	totalItems     int
	totalBytes     int64
	activeCaptures int
	reservedItems  int
	reservedBytes  int64
	closed         bool
	wake           chan struct{}
	done           chan struct{}
	stopped        chan struct{}
}

func NewTemporaryAudioVault(
	config TemporaryAudioVaultConfig,
) (*TemporaryAudioVault, error) {
	// Preserve explicit legacy test/composition callers while production
	// supplies the stricter actor-aware values from platform configuration.
	if config.MaxItemsPerActor == 0 {
		config.MaxItemsPerActor = config.MaxItems
	}
	if config.MaxBytesPerActor == 0 {
		config.MaxBytesPerActor = config.MaxBytes
	}
	if config.MaxConcurrentCaptures == 0 {
		config.MaxConcurrentCaptures = 1
	}
	if config.MaxConcurrentCapturesPerActor == 0 {
		config.MaxConcurrentCapturesPerActor =
			config.MaxConcurrentCaptures
	}
	if config.Lifetime <= 0 ||
		config.MaxItems <= 0 ||
		config.MaxBytes < MaxAudioBytes ||
		config.MaxItemsPerActor <= 0 ||
		config.MaxItemsPerActor > config.MaxItems ||
		config.MaxBytesPerActor < MaxAudioBytes ||
		config.MaxBytesPerActor > config.MaxBytes ||
		config.MaxConcurrentCaptures <= 0 ||
		config.MaxConcurrentCapturesPerActor <= 0 ||
		config.MaxConcurrentCapturesPerActor >
			config.MaxConcurrentCaptures {
		return nil, errors.New("temporary audio vault configuration is invalid")
	}
	vault := &TemporaryAudioVault{
		config:     config,
		entries:    make(map[string]*temporaryAudioEntry),
		actorUsage: make(map[string]*actorAudioUsage),
		wake:       make(chan struct{}, 1),
		done:       make(chan struct{}),
		stopped:    make(chan struct{}),
	}
	go vault.reap()
	return vault, nil
}

// Capture validates an untrusted upload and binds it to the trusted Actor.
// Successful capture removes its mode-0600 validation scratch file before
// returning.
func (vault *TemporaryAudioVault) Capture(
	ctx context.Context,
	actor requestcontext.Actor,
	claimedContentType string,
	input io.Reader,
) (TemporaryAudioMetadata, error) {
	if vault == nil || ctx == nil || !actor.Valid() {
		return TemporaryAudioMetadata{}, errors.New(
			"temporary audio capture requires a trusted actor and context",
		)
	}
	if isNilReader(input) {
		return TemporaryAudioMetadata{}, errors.New(
			"temporary audio capture requires an input",
		)
	}
	if err := ctx.Err(); err != nil {
		return TemporaryAudioMetadata{}, err
	}

	if err := vault.reserveCapture(actor.UserID); err != nil {
		return TemporaryAudioMetadata{}, err
	}
	defer vault.releaseCapture(actor.UserID)
	audio, err := CaptureTemporaryAudio(
		vault.config.ScratchDirectory,
		claimedContentType,
		contextReader{ctx: ctx, reader: input},
	)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return TemporaryAudioMetadata{}, contextErr
		}
		return TemporaryAudioMetadata{}, err
	}
	defer audio.Close()

	reader, err := audio.Open()
	if err != nil {
		return TemporaryAudioMetadata{}, err
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, MaxAudioBytes+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || int64(len(data)) != audio.Size() {
		clear(data)
		return TemporaryAudioMetadata{}, errors.New(
			"read validated temporary audio",
		)
	}
	if err := ctx.Err(); err != nil {
		clear(data)
		return TemporaryAudioMetadata{}, err
	}
	if err := audio.Close(); err != nil {
		clear(data)
		return TemporaryAudioMetadata{}, err
	}

	vault.mu.Lock()
	defer vault.mu.Unlock()
	if vault.closed {
		clear(data)
		return TemporaryAudioMetadata{}, ErrTemporaryAudioClosed
	}
	vault.purgeExpiredLocked(time.Now())
	usage := vault.actorUsage[actor.UserID]
	if usage == nil ||
		vault.totalItems >= vault.config.MaxItems ||
		int64(len(data)) > vault.config.MaxBytes-vault.totalBytes ||
		usage.items >= vault.config.MaxItemsPerActor ||
		int64(len(data)) > vault.config.MaxBytesPerActor-usage.bytes {
		clear(data)
		return TemporaryAudioMetadata{}, ErrTemporaryAudioCapacity
	}

	id, err := vault.unusedIDLocked()
	if err != nil {
		clear(data)
		return TemporaryAudioMetadata{}, err
	}
	now := time.Now().UTC()
	metadata := TemporaryAudioMetadata{
		ID:         id,
		MediaType:  audio.MediaType(),
		Size:       audio.Size(),
		Duration:   audio.Duration(),
		SampleRate: audio.SampleRate(),
		ExpiresAt:  now.Add(vault.config.Lifetime),
	}
	vault.entries[id] = &temporaryAudioEntry{
		ownerID: actor.UserID,
		data:    data,
		meta:    metadata,
	}
	vault.totalItems++
	vault.totalBytes += int64(len(data))
	usage.items++
	usage.bytes += int64(len(data))
	vault.signalWake()
	return metadata, nil
}

// Source returns a fresh provider-facing source only when the trusted Actor
// owns the still-live recording. Missing, expired, and foreign IDs are
// intentionally indistinguishable.
func (vault *TemporaryAudioVault) Source(
	actor requestcontext.Actor,
	id string,
) (AudioSource, error) {
	if vault == nil || !actor.Valid() || id == "" {
		return nil, ErrTemporaryAudioNotFound
	}
	vault.mu.Lock()
	defer vault.mu.Unlock()
	if vault.closed {
		return nil, ErrTemporaryAudioClosed
	}
	vault.purgeExpiredLocked(time.Now())
	entry, found := vault.entries[id]
	if !found || entry.ownerID != actor.UserID {
		return nil, ErrTemporaryAudioNotFound
	}
	return &vaultAudioSource{
		vault:   vault,
		ownerID: actor.UserID,
		id:      id,
		meta:    entry.meta,
	}, nil
}

// Delete is owner-idempotent. A foreign or already absent ID is a no-op so
// callers cannot use deletion responses to enumerate another user's audio.
func (vault *TemporaryAudioVault) Delete(
	actor requestcontext.Actor,
	id string,
) error {
	if vault == nil || !actor.Valid() || id == "" {
		return nil
	}
	vault.mu.Lock()
	defer vault.mu.Unlock()
	if vault.closed {
		return ErrTemporaryAudioClosed
	}
	entry, found := vault.entries[id]
	if !found || entry.ownerID != actor.UserID {
		return nil
	}
	vault.removeLocked(id, entry)
	vault.signalWake()
	return nil
}

func (vault *TemporaryAudioVault) Close() error {
	if vault == nil {
		return nil
	}
	vault.mu.Lock()
	if !vault.closed {
		vault.closed = true
		for id, entry := range vault.entries {
			vault.removeLocked(id, entry)
		}
		close(vault.done)
	}
	vault.mu.Unlock()
	// The closed flag and capture admission share vault.mu, so no WaitGroup Add
	// can race with this Wait after the flag becomes visible.
	vault.captures.Wait()
	<-vault.stopped
	return nil
}

type vaultAudioSource struct {
	vault   *TemporaryAudioVault
	ownerID string
	id      string
	meta    TemporaryAudioMetadata
}

func (source *vaultAudioSource) Open() (io.ReadCloser, error) {
	if source == nil || source.vault == nil {
		return nil, ErrTemporaryAudioNotFound
	}
	return source.vault.open(source.ownerID, source.id)
}

func (source *vaultAudioSource) MediaType() string {
	if source == nil {
		return ""
	}
	return source.meta.MediaType
}

func (source *vaultAudioSource) Size() int64 {
	if source == nil {
		return 0
	}
	return source.meta.Size
}

func (source *vaultAudioSource) Duration() time.Duration {
	if source == nil {
		return 0
	}
	return source.meta.Duration
}

func (source *vaultAudioSource) SampleRate() int {
	if source == nil {
		return 0
	}
	return source.meta.SampleRate
}

func (vault *TemporaryAudioVault) open(
	ownerID string,
	id string,
) (io.ReadCloser, error) {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	if vault.closed {
		return nil, ErrTemporaryAudioClosed
	}
	vault.purgeExpiredLocked(time.Now())
	entry, found := vault.entries[id]
	if !found || entry.ownerID != ownerID {
		return nil, ErrTemporaryAudioNotFound
	}
	entry.readers++
	return &sharedAudioReadCloser{
		vault:  vault,
		entry:  entry,
		reader: bytes.NewReader(entry.data),
	}, nil
}

func (vault *TemporaryAudioVault) unusedIDLocked() (string, error) {
	for attempt := 0; attempt < 8; attempt++ {
		random := make([]byte, 24)
		if _, err := rand.Read(random); err != nil {
			return "", errors.New("generate temporary audio ID")
		}
		id := temporaryAudioIDPrefix +
			base64.RawURLEncoding.EncodeToString(random)
		if _, exists := vault.entries[id]; !exists {
			return id, nil
		}
	}
	return "", errors.New("generate unique temporary audio ID")
}

func (vault *TemporaryAudioVault) purgeExpiredLocked(now time.Time) {
	for id, entry := range vault.entries {
		if !now.Before(entry.meta.ExpiresAt) {
			vault.removeLocked(id, entry)
		}
	}
}

func (vault *TemporaryAudioVault) removeLocked(
	id string,
	entry *temporaryAudioEntry,
) {
	if entry.removed {
		return
	}
	delete(vault.entries, id)
	entry.removed = true
	if entry.readers == 0 {
		vault.releaseEntryLocked(entry)
	}
}

func (vault *TemporaryAudioVault) releaseReader(
	entry *temporaryAudioEntry,
) {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	if entry.readers <= 0 {
		return
	}
	entry.readers--
	if entry.removed && entry.readers == 0 {
		vault.releaseEntryLocked(entry)
	}
}

func (vault *TemporaryAudioVault) releaseEntryLocked(
	entry *temporaryAudioEntry,
) {
	vault.totalItems--
	vault.totalBytes -= int64(len(entry.data))
	if usage := vault.actorUsage[entry.ownerID]; usage != nil {
		usage.items--
		usage.bytes -= int64(len(entry.data))
		vault.removeActorUsageIfEmptyLocked(entry.ownerID, usage)
	}
	clear(entry.data)
	entry.data = nil
}

func (vault *TemporaryAudioVault) reserveCapture(ownerID string) error {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	if vault.closed {
		return ErrTemporaryAudioClosed
	}
	vault.purgeExpiredLocked(time.Now())
	usage := vault.actorUsage[ownerID]
	if usage == nil {
		usage = &actorAudioUsage{}
		vault.actorUsage[ownerID] = usage
	}
	if vault.activeCaptures >= vault.config.MaxConcurrentCaptures ||
		usage.captures >= vault.config.MaxConcurrentCapturesPerActor ||
		vault.totalItems+vault.reservedItems >= vault.config.MaxItems ||
		MaxAudioBytes > vault.config.MaxBytes-
			vault.totalBytes-vault.reservedBytes ||
		usage.items+usage.reservedItems >=
			vault.config.MaxItemsPerActor ||
		MaxAudioBytes > vault.config.MaxBytesPerActor-
			usage.bytes-usage.reservedBytes {
		vault.removeActorUsageIfEmptyLocked(ownerID, usage)
		return ErrTemporaryAudioCapacity
	}
	vault.activeCaptures++
	vault.reservedItems++
	vault.reservedBytes += MaxAudioBytes
	usage.captures++
	usage.reservedItems++
	usage.reservedBytes += MaxAudioBytes
	vault.captures.Add(1)
	return nil
}

func (vault *TemporaryAudioVault) releaseCapture(ownerID string) {
	vault.mu.Lock()
	vault.activeCaptures--
	vault.reservedItems--
	vault.reservedBytes -= MaxAudioBytes
	if usage := vault.actorUsage[ownerID]; usage != nil {
		usage.captures--
		usage.reservedItems--
		usage.reservedBytes -= MaxAudioBytes
		vault.removeActorUsageIfEmptyLocked(ownerID, usage)
	}
	vault.mu.Unlock()
	vault.captures.Done()
}

func (vault *TemporaryAudioVault) removeActorUsageIfEmptyLocked(
	ownerID string,
	usage *actorAudioUsage,
) {
	if usage.items == 0 && usage.bytes == 0 &&
		usage.captures == 0 && usage.reservedItems == 0 &&
		usage.reservedBytes == 0 {
		delete(vault.actorUsage, ownerID)
	}
}

func (vault *TemporaryAudioVault) signalWake() {
	select {
	case vault.wake <- struct{}{}:
	default:
	}
}

func (vault *TemporaryAudioVault) reap() {
	defer close(vault.stopped)
	for {
		vault.mu.Lock()
		if vault.closed {
			vault.mu.Unlock()
			return
		}
		var next time.Time
		for _, entry := range vault.entries {
			if next.IsZero() || entry.meta.ExpiresAt.Before(next) {
				next = entry.meta.ExpiresAt
			}
		}
		vault.mu.Unlock()

		if next.IsZero() {
			select {
			case <-vault.done:
				return
			case <-vault.wake:
			}
			continue
		}

		delay := time.Until(next)
		if delay < 0 {
			delay = 0
		}
		timer := time.NewTimer(delay)
		select {
		case <-vault.done:
			stopTimer(timer)
			return
		case <-vault.wake:
			stopTimer(timer)
		case now := <-timer.C:
			vault.mu.Lock()
			if !vault.closed {
				vault.purgeExpiredLocked(now)
			}
			vault.mu.Unlock()
		}
	}
}

func stopTimer(timer *time.Timer) {
	if timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}

type sharedAudioReadCloser struct {
	mu     sync.Mutex
	vault  *TemporaryAudioVault
	entry  *temporaryAudioEntry
	reader *bytes.Reader
	closed bool
}

func (reader *sharedAudioReadCloser) Read(buffer []byte) (int, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.closed {
		return 0, ErrAudioClosed
	}
	return reader.reader.Read(buffer)
}

func (reader *sharedAudioReadCloser) Close() error {
	if reader == nil {
		return nil
	}
	reader.mu.Lock()
	if reader.closed {
		reader.mu.Unlock()
		return nil
	}
	reader.reader = bytes.NewReader(nil)
	reader.closed = true
	vault := reader.vault
	entry := reader.entry
	reader.vault = nil
	reader.entry = nil
	reader.mu.Unlock()
	vault.releaseReader(entry)
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

var _ AudioSource = (*vaultAudioSource)(nil)
