package postgres_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	objectfake "github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore/fake"
	"github.com/gin-gonic/gin"
)

func successfulVoiceTranscription() ai.TranscriptionResult {
	return ai.TranscriptionResult{
		ID:         "fake-asr-request-1",
		Provider:   "fake",
		Model:      "fake-asr-model",
		Transcript: "A faithful provider transcript.",
		Language:   "en",
	}
}

func voiceTestWAV(sample byte) []byte {
	const (
		sampleRate = 16_000
		samples    = 1_600
		dataSize   = samples * 2
	)
	result := make([]byte, 44+dataSize)
	copy(result[0:4], "RIFF")
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(result)-8))
	copy(result[8:12], "WAVE")
	copy(result[12:16], "fmt ")
	binary.LittleEndian.PutUint32(result[16:20], 16)
	binary.LittleEndian.PutUint16(result[20:22], 1)
	binary.LittleEndian.PutUint16(result[22:24], 1)
	binary.LittleEndian.PutUint32(result[24:28], sampleRate)
	binary.LittleEndian.PutUint32(result[28:32], sampleRate*2)
	binary.LittleEndian.PutUint16(result[32:34], 2)
	binary.LittleEndian.PutUint16(result[34:36], 16)
	copy(result[36:40], "data")
	binary.LittleEndian.PutUint32(result[40:44], dataSize)
	for index := 44; index < len(result); index++ {
		result[index] = sample
	}
	return result
}

type storedVoiceSourceLoader struct {
	store     *objectfake.Store
	directory string
}

func (loader *storedVoiceSourceLoader) LoadVoiceAudio(
	_ context.Context,
	candidate agentvoice.Candidate,
) (platformmedia.ManagedAudioSource, error) {
	body, found := loader.store.Bytes(candidate.ObjectKey)
	if !found {
		return nil, objectstore.ErrOperationFailed
	}
	return platformmedia.CaptureTemporaryAudio(
		loader.directory,
		candidate.ContentType,
		bytes.NewReader(body),
	)
}

type blockingVoiceStore struct {
	*objectfake.Store
	started chan struct{}
	release chan struct{}
	once    sync.Once
	puts    atomic.Int32
}

func newBlockingVoiceStore(store *objectfake.Store) *blockingVoiceStore {
	return &blockingVoiceStore{
		Store:   store,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (store *blockingVoiceStore) Put(
	ctx context.Context,
	request objectstore.PutRequest,
) (objectstore.PutResult, error) {
	store.puts.Add(1)
	store.once.Do(func() { close(store.started) })
	select {
	case <-ctx.Done():
		return objectstore.PutResult{}, ctx.Err()
	case <-store.release:
		return store.Store.Put(ctx, request)
	}
}

func messageResponse(message conversation.Message) gin.H {
	result := gin.H{
		"message_id": message.ID,
		"thread_id":  message.ThreadID,
		"sequence":   message.Sequence,
		"role":       message.Role,
		"content":    message.Content,
		"created_at": message.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if message.ClientMessageID != "" {
		result["client_message_id"] = message.ClientMessageID
	}
	if message.ProducedByRunID != "" {
		result["produced_by_run_id"] = message.ProducedByRunID
	}
	if message.Audio != nil {
		result["modality"] = conversation.MessageModalityVoice
		result["audio"] = agentMessageAudioResponse(*message.Audio)
	}
	return result
}

func agentMessageAudioResponse(audio conversation.MessageAudio) gin.H {
	result := gin.H{
		"audio_id":     audio.ID,
		"status":       audio.Status,
		"content_type": audio.ContentType,
		"size_bytes":   audio.Size,
		"duration_ms":  durationMilliseconds(audio.Duration),
	}
	if audio.Status == conversation.MessageAudioReadable {
		result["playback_path"] =
			"/v1/agent-message-audios/" + audio.ID + "/playback"
	}
	if !audio.DeletedAt.IsZero() {
		result["deleted_at"] = audio.DeletedAt.UTC().
			Format(time.RFC3339Nano)
	}
	return result
}

func durationMilliseconds(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return int64((duration + time.Millisecond - 1) / time.Millisecond)
}
