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
	ScratchDirectory string
	Lifetime         time.Duration
	MaxItems         int
	MaxBytes         int64
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
}

// TemporaryAudioVault is an actor-bound, process-local holding area for an
// unconfirmed recording. It keeps bytes only in memory, removes validation
// scratch files immediately, and never exposes a storage path or public URL.
//
// Conversation remains the owner of AudioAsset business metadata. This vault
// only supplies the narrow temporary file-content capability needed before
// the accepted Conversation persistence contract is implemented.
type TemporaryAudioVault struct {
	mu        sync.Mutex
	captureMu sync.Mutex

	config     TemporaryAudioVaultConfig
	entries    map[string]*temporaryAudioEntry
	totalBytes int64
	closed     bool
	wake       chan struct{}
	done       chan struct{}
	stopped    chan struct{}
}

func NewTemporaryAudioVault(
	config TemporaryAudioVaultConfig,
) (*TemporaryAudioVault, error) {
	if config.Lifetime <= 0 ||
		config.MaxItems <= 0 ||
		config.MaxBytes < MaxAudioBytes {
		return nil, errors.New("temporary audio vault configuration is invalid")
	}
	vault := &TemporaryAudioVault{
		config:  config,
		entries: make(map[string]*temporaryAudioEntry),
		wake:    make(chan struct{}, 1),
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
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

	// Serialize validation so concurrent callers cannot create an unbounded
	// number of maximum-size scratch files before capacity is checked.
	vault.captureMu.Lock()
	defer vault.captureMu.Unlock()

	if vault.isClosed() {
		return TemporaryAudioMetadata{}, ErrTemporaryAudioClosed
	}
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
	if len(vault.entries) >= vault.config.MaxItems ||
		int64(len(data)) > vault.config.MaxBytes-vault.totalBytes {
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
	vault.totalBytes += int64(len(data))
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
	// Wait for any in-flight validation so Close does not return while an
	// upload can still create or retain a scratch file.
	vault.captureMu.Lock()
	defer vault.captureMu.Unlock()

	vault.mu.Lock()
	if !vault.closed {
		vault.closed = true
		for id, entry := range vault.entries {
			vault.removeLocked(id, entry)
		}
		close(vault.done)
	}
	vault.mu.Unlock()
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
	data := bytes.Clone(entry.data)
	return &clearingReadCloser{
		reader: bytes.NewReader(data),
		data:   data,
	}, nil
}

func (vault *TemporaryAudioVault) isClosed() bool {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	return vault.closed
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
	delete(vault.entries, id)
	vault.totalBytes -= int64(len(entry.data))
	clear(entry.data)
	entry.data = nil
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

type clearingReadCloser struct {
	mu     sync.Mutex
	reader *bytes.Reader
	data   []byte
	closed bool
}

func (reader *clearingReadCloser) Read(buffer []byte) (int, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.closed {
		return 0, ErrAudioClosed
	}
	return reader.reader.Read(buffer)
}

func (reader *clearingReadCloser) Close() error {
	if reader == nil {
		return nil
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.closed {
		return nil
	}
	clear(reader.data)
	reader.data = nil
	reader.reader = bytes.NewReader(nil)
	reader.closed = true
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
