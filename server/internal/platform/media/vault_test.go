package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func TestTemporaryAudioVaultProtectsActorAndRemovesScratchFile(t *testing.T) {
	t.Parallel()

	scratch := t.TempDir()
	vault := newTestVault(t, scratch, time.Minute, 2, MaxAudioBytes*2)
	owner := testActor("owner")
	foreign := testActor("foreign")
	payload := testWAV(t, time.Second)

	metadata, err := vault.Capture(
		context.Background(),
		owner,
		ContentTypeWAV,
		bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if !strings.HasPrefix(metadata.ID, temporaryAudioIDPrefix) ||
		metadata.MediaType != ContentTypeWAV ||
		metadata.Size != int64(len(payload)) ||
		metadata.Duration != time.Second ||
		metadata.SampleRate != 8_000 ||
		metadata.ExpiresAt.Before(time.Now()) {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
	entries, err := os.ReadDir(scratch)
	if err != nil {
		t.Fatalf("read scratch directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("successful capture retained validation files: %v", entries)
	}

	if _, err := vault.Source(foreign, metadata.ID); !errors.Is(
		err,
		ErrTemporaryAudioNotFound,
	) {
		t.Fatalf("foreign source error = %v", err)
	}
	if err := vault.Delete(foreign, metadata.ID); err != nil {
		t.Fatalf("foreign delete should hide ownership: %v", err)
	}

	source, err := vault.Source(owner, metadata.ID)
	if err != nil {
		t.Fatalf("owner source: %v", err)
	}
	if err := ValidateAudioSource(source); err != nil {
		t.Fatalf("validate source: %v", err)
	}
	reader, err := source.Open()
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close source: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("vault changed audio bytes")
	}

	inFlight, err := source.Open()
	if err != nil {
		t.Fatalf("open in-flight source: %v", err)
	}
	if err := vault.Delete(owner, metadata.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := vault.Delete(owner, metadata.ID); err != nil {
		t.Fatalf("second delete: %v", err)
	}
	if _, err := source.Open(); !errors.Is(err, ErrTemporaryAudioNotFound) {
		t.Fatalf("open deleted source error = %v", err)
	}
	inFlightBytes, err := io.ReadAll(inFlight)
	if err != nil {
		t.Fatalf("read authorized in-flight source: %v", err)
	}
	if err := inFlight.Close(); err != nil {
		t.Fatalf("close authorized in-flight source: %v", err)
	}
	if !bytes.Equal(inFlightBytes, payload) {
		t.Fatal("deletion corrupted an already authorized in-flight read")
	}
}

func TestTemporaryAudioVaultEnforcesCapacityAndExpiry(t *testing.T) {
	t.Parallel()

	payload := testWAV(t, time.Second)
	vault := newTestVault(
		t,
		t.TempDir(),
		25*time.Millisecond,
		1,
		MaxAudioBytes,
	)
	actor := testActor("owner")
	first, err := vault.Capture(
		context.Background(),
		actor,
		ContentTypeWAV,
		bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("capture first: %v", err)
	}
	source, err := vault.Source(actor, first.ID)
	if err != nil {
		t.Fatalf("source first: %v", err)
	}
	inFlight, err := source.Open()
	if err != nil {
		t.Fatalf("open first: %v", err)
	}
	defer inFlight.Close()
	if _, err := vault.Capture(
		context.Background(),
		actor,
		ContentTypeWAV,
		bytes.NewReader(payload),
	); !errors.Is(err, ErrTemporaryAudioCapacity) {
		t.Fatalf("capacity error = %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for {
		_, err := vault.Source(actor, first.ID)
		if errors.Is(err, ErrTemporaryAudioNotFound) {
			break
		}
		if err != nil {
			t.Fatalf("source before expiry: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("temporary audio was not reaped")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := vault.Capture(
		context.Background(),
		actor,
		ContentTypeWAV,
		bytes.NewReader(payload),
	); !errors.Is(err, ErrTemporaryAudioCapacity) {
		t.Fatalf("expired in-flight capacity error = %v", err)
	}
	inFlightBytes, err := io.ReadAll(inFlight)
	if err != nil {
		t.Fatalf("read expired in-flight audio: %v", err)
	}
	if !bytes.Equal(inFlightBytes, payload) {
		t.Fatal("expiry corrupted an authorized in-flight reader")
	}
	if err := inFlight.Close(); err != nil {
		t.Fatalf("close expired in-flight audio: %v", err)
	}
	if _, err := vault.Capture(
		context.Background(),
		actor,
		ContentTypeWAV,
		bytes.NewReader(payload),
	); err != nil {
		t.Fatalf("capture after expiry: %v", err)
	}
}

func TestTemporaryAudioVaultSharesLargeAudioWithoutOpenAmplification(
	t *testing.T,
) {
	t.Parallel()

	payload := largeVaultTestWAV(t)
	vault := newTestVault(
		t,
		t.TempDir(),
		time.Minute,
		2,
		MaxAudioBytes,
	)
	actor := testActor("owner")
	metadata, err := vault.Capture(
		context.Background(),
		actor,
		ContentTypeWAV,
		bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("capture large audio: %v", err)
	}
	source, err := vault.Source(actor, metadata.ID)
	if err != nil {
		t.Fatalf("source large audio: %v", err)
	}

	const readerCount = 32
	start := make(chan struct{})
	readers := make(chan io.ReadCloser, readerCount)
	failures := make(chan error, readerCount)
	var wait sync.WaitGroup
	for index := 0; index < readerCount; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			reader, openErr := source.Open()
			if openErr != nil {
				failures <- openErr
				return
			}
			readers <- reader
		}()
	}
	close(start)
	wait.Wait()
	close(readers)
	close(failures)
	opened := make([]io.ReadCloser, 0, readerCount)
	for reader := range readers {
		opened = append(opened, reader)
	}
	var openFailure error
	for failure := range failures {
		openFailure = errors.Join(openFailure, failure)
	}
	if openFailure != nil {
		for _, reader := range opened {
			_ = reader.Close()
		}
		t.Fatalf("concurrent open: %v", openFailure)
	}
	if len(opened) != readerCount {
		t.Fatalf("opened readers = %d, want %d", len(opened), readerCount)
	}
	defer func() {
		for _, reader := range opened {
			_ = reader.Close()
		}
	}()
	for index, reader := range opened {
		firstByte := make([]byte, 1)
		if _, err := io.ReadFull(reader, firstByte); err != nil {
			t.Fatalf("read shared reader %d: %v", index, err)
		}
		if firstByte[0] != payload[0] {
			t.Fatalf("shared reader %d returned changed audio", index)
		}
	}

	vault.mu.Lock()
	if vault.totalItems != 1 || vault.totalBytes != int64(len(payload)) {
		t.Fatalf(
			"concurrent opens amplified retained audio: items=%d bytes=%d",
			vault.totalItems,
			vault.totalBytes,
		)
	}
	vault.mu.Unlock()

	if err := vault.Delete(actor, metadata.ID); err != nil {
		t.Fatalf("delete large audio: %v", err)
	}
	small := testWAV(t, time.Second)
	if _, err := vault.Capture(
		context.Background(),
		actor,
		ContentTypeWAV,
		bytes.NewReader(small),
	); !errors.Is(err, ErrTemporaryAudioCapacity) {
		t.Fatalf("retained-reader capacity error = %v", err)
	}

	for _, reader := range opened {
		if err := reader.Close(); err != nil {
			t.Fatalf("close shared reader: %v", err)
		}
	}
	vault.mu.Lock()
	if vault.totalItems != 0 || vault.totalBytes != 0 {
		t.Fatalf(
			"reader close did not release capacity: items=%d bytes=%d",
			vault.totalItems,
			vault.totalBytes,
		)
	}
	vault.mu.Unlock()
	if _, err := vault.Capture(
		context.Background(),
		actor,
		ContentTypeWAV,
		bytes.NewReader(small),
	); err != nil {
		t.Fatalf("capture after reader close: %v", err)
	}
}

func TestTemporaryAudioVaultRejectsInvalidBoundaryInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config TemporaryAudioVaultConfig
	}{
		{
			name: "missing lifetime",
			config: TemporaryAudioVaultConfig{
				MaxItems: 1,
				MaxBytes: MaxAudioBytes,
			},
		},
		{
			name: "missing item limit",
			config: TemporaryAudioVaultConfig{
				Lifetime: time.Minute,
				MaxBytes: MaxAudioBytes,
			},
		},
		{
			name: "cannot hold maximum upload",
			config: TemporaryAudioVaultConfig{
				Lifetime: time.Minute,
				MaxItems: 1,
				MaxBytes: MaxAudioBytes - 1,
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewTemporaryAudioVault(test.config); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}

	vault := newTestVault(
		t,
		t.TempDir(),
		time.Minute,
		1,
		MaxAudioBytes,
	)
	payload := testWAV(t, time.Second)
	if _, err := vault.Capture(
		nil,
		testActor("owner"),
		ContentTypeWAV,
		bytes.NewReader(payload),
	); err == nil {
		t.Fatal("expected nil context error")
	}
	if _, err := vault.Capture(
		context.Background(),
		requestcontext.Actor{},
		ContentTypeWAV,
		bytes.NewReader(payload),
	); err == nil {
		t.Fatal("expected untrusted actor error")
	}
	if _, err := vault.Capture(
		context.Background(),
		testActor("owner"),
		ContentTypeWAV,
		nil,
	); err == nil {
		t.Fatal("expected nil input error")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := vault.Capture(
		cancelled,
		testActor("owner"),
		ContentTypeWAV,
		bytes.NewReader(payload),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled capture error = %v", err)
	}
}

func TestTemporaryAudioVaultCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	vault := newTestVault(
		t,
		t.TempDir(),
		time.Minute,
		1,
		MaxAudioBytes,
	)
	actor := testActor("owner")
	payload := testWAV(t, time.Second)
	metadata, err := vault.Capture(
		context.Background(),
		actor,
		ContentTypeWAV,
		bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	source, err := vault.Source(actor, metadata.ID)
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	inFlight, err := source.Open()
	if err != nil {
		t.Fatalf("open in-flight source: %v", err)
	}
	defer inFlight.Close()
	if err := vault.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	vault.mu.Lock()
	if vault.totalItems != 1 || vault.totalBytes != int64(len(payload)) {
		t.Fatalf(
			"vault close released in-flight audio: items=%d bytes=%d",
			vault.totalItems,
			vault.totalBytes,
		)
	}
	vault.mu.Unlock()
	got, err := io.ReadAll(inFlight)
	if err != nil {
		t.Fatalf("read in-flight source after vault close: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("vault close corrupted an authorized in-flight reader")
	}
	if err := inFlight.Close(); err != nil {
		t.Fatalf("close in-flight source: %v", err)
	}
	if err := inFlight.Close(); err != nil {
		t.Fatalf("second in-flight close: %v", err)
	}
	vault.mu.Lock()
	if vault.totalItems != 0 || vault.totalBytes != 0 {
		t.Fatalf(
			"reader close did not release closed-vault capacity: items=%d bytes=%d",
			vault.totalItems,
			vault.totalBytes,
		)
	}
	vault.mu.Unlock()
	if err := vault.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := source.Open(); !errors.Is(err, ErrTemporaryAudioClosed) {
		t.Fatalf("open after close error = %v", err)
	}
	if _, err := vault.Capture(
		context.Background(),
		actor,
		ContentTypeWAV,
		bytes.NewReader(testWAV(t, time.Second)),
	); !errors.Is(err, ErrTemporaryAudioClosed) {
		t.Fatalf("capture after close error = %v", err)
	}
}

func TestTemporaryAudioVaultConcurrentReadAndDelete(t *testing.T) {
	t.Parallel()

	vault := newTestVault(
		t,
		t.TempDir(),
		time.Minute,
		1,
		MaxAudioBytes,
	)
	actor := testActor("owner")
	payload := testWAV(t, time.Second)
	metadata, err := vault.Capture(
		context.Background(),
		actor,
		ContentTypeWAV,
		bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	source, err := vault.Source(actor, metadata.ID)
	if err != nil {
		t.Fatalf("source: %v", err)
	}

	var wait sync.WaitGroup
	failures := make(chan error, 64)
	for index := 0; index < 32; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			reader, openErr := source.Open()
			if errors.Is(openErr, ErrTemporaryAudioNotFound) {
				return
			}
			if openErr != nil {
				failures <- openErr
				return
			}
			data, readErr := io.ReadAll(reader)
			closeErr := reader.Close()
			if readErr != nil {
				failures <- readErr
			} else if closeErr != nil {
				failures <- closeErr
			} else if !bytes.Equal(data, payload) {
				failures <- errors.New("audio bytes changed")
			}
		}()
	}
	wait.Add(1)
	go func() {
		defer wait.Done()
		if deleteErr := vault.Delete(actor, metadata.ID); deleteErr != nil {
			failures <- deleteErr
		}
	}()
	wait.Wait()
	close(failures)
	for failure := range failures {
		t.Errorf("concurrent operation: %v", failure)
	}
}

func newTestVault(
	t *testing.T,
	scratch string,
	lifetime time.Duration,
	maxItems int,
	maxBytes int64,
) *TemporaryAudioVault {
	t.Helper()
	vault, err := NewTemporaryAudioVault(TemporaryAudioVaultConfig{
		ScratchDirectory: scratch,
		Lifetime:         lifetime,
		MaxItems:         maxItems,
		MaxBytes:         maxBytes,
	})
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	t.Cleanup(func() {
		if err := vault.Close(); err != nil {
			t.Errorf("close vault: %v", err)
		}
	})
	return vault
}

func testActor(seed string) requestcontext.Actor {
	return requestcontext.Actor{
		UserID:    seed + "-user",
		SessionID: seed + "-session",
	}
}

func largeVaultTestWAV(t *testing.T) []byte {
	t.Helper()
	const (
		channels      = 2
		sampleRate    = 48_000
		bitsPerSample = 16
		duration      = 38*time.Second + 500*time.Millisecond
	)
	blockAlign := channels * bitsPerSample / 8
	byteRate := sampleRate * blockAlign
	dataSize := int64(duration) * int64(byteRate) / int64(time.Second)
	payload := make([]byte, 44+dataSize)
	copy(payload[0:4], "RIFF")
	binary.LittleEndian.PutUint32(payload[4:8], uint32(len(payload)-8))
	copy(payload[8:12], "WAVE")
	copy(payload[12:16], "fmt ")
	binary.LittleEndian.PutUint32(payload[16:20], 16)
	binary.LittleEndian.PutUint16(payload[20:22], 1)
	binary.LittleEndian.PutUint16(payload[22:24], channels)
	binary.LittleEndian.PutUint32(payload[24:28], sampleRate)
	binary.LittleEndian.PutUint32(payload[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(payload[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(payload[34:36], bitsPerSample)
	copy(payload[36:40], "data")
	binary.LittleEndian.PutUint32(payload[40:44], uint32(dataSize))
	if len(payload) > int(MaxAudioBytes) {
		t.Fatalf("large test WAV exceeds limit: %d", len(payload))
	}
	return payload
}
