package postgres_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentsummary "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/summary"
	agenttitle "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/title"
	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/memory"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	objectfake "github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore/fake"
	"github.com/gin-gonic/gin"
)

func successfulVoiceTranscription() agentvoice.TranscriptionResult {
	return agentvoice.TranscriptionResult{
		ID:         "fake-asr-request-1",
		Provider:   "fake",
		Model:      "fake-asr-model",
		Transcript: "A faithful provider transcript.",
		Language:   "en",
	}
}

type fixedTextGenerator struct {
	result agentrun.TextResult
	err    error
}

type fixedSummaryGenerator struct {
	mu       sync.Mutex
	result   agentsummary.GenerationResult
	err      error
	requests []agentsummary.GenerationRequest
}

type fixedTitleGenerator struct {
	mu       sync.Mutex
	result   agenttitle.GenerationResult
	err      error
	requests []agenttitle.GenerationRequest
}

func (generator *fixedTitleGenerator) GenerateJSON(
	ctx context.Context,
	request agenttitle.GenerationRequest,
) (agenttitle.GenerationResult, error) {
	if err := ctx.Err(); err != nil {
		return agenttitle.GenerationResult{}, err
	}
	generator.mu.Lock()
	defer generator.mu.Unlock()
	generator.requests = append(generator.requests, request)
	if generator.err != nil {
		return agenttitle.GenerationResult{}, generator.err
	}
	return generator.result, nil
}

func (generator *fixedTitleGenerator) CallCount() int {
	generator.mu.Lock()
	defer generator.mu.Unlock()
	return len(generator.requests)
}

func newFixedSummaryGenerator(
	result agentsummary.GenerationResult,
) *fixedSummaryGenerator {
	return &fixedSummaryGenerator{result: result}
}

func (generator *fixedSummaryGenerator) GenerateJSON(
	ctx context.Context,
	request agentsummary.GenerationRequest,
) (agentsummary.GenerationResult, error) {
	if err := ctx.Err(); err != nil {
		return agentsummary.GenerationResult{}, err
	}
	generator.mu.Lock()
	defer generator.mu.Unlock()
	generator.requests = append(generator.requests, request)
	if generator.err != nil {
		return agentsummary.GenerationResult{}, generator.err
	}
	return generator.result, nil
}

func (generator *fixedSummaryGenerator) CallCount() int {
	generator.mu.Lock()
	defer generator.mu.Unlock()
	return len(generator.requests)
}

func newFixedTextGenerator(result agentrun.TextResult) *fixedTextGenerator {
	return &fixedTextGenerator{result: result}
}

func newFailingTextGenerator(err error) *fixedTextGenerator {
	return &fixedTextGenerator{err: err}
}

func (generator *fixedTextGenerator) Generate(
	ctx context.Context,
	request agentrun.TextRequest,
) (agentrun.TextResult, error) {
	if err := ctx.Err(); err != nil {
		kind := agentrun.ErrorCancelled
		if err == context.DeadlineExceeded {
			kind = agentrun.ErrorTimeout
		}
		return agentrun.TextResult{}, agentrun.NewGenerationError(
			kind,
			0,
			"",
			"",
			err,
		)
	}
	if err := agentrun.ValidateTextRequest(request); err != nil {
		return agentrun.TextResult{}, agentrun.NewGenerationError(
			agentrun.ErrorInvalidRequest,
			0,
			"",
			"",
			err,
		)
	}
	if generator.err != nil {
		return agentrun.TextResult{}, generator.err
	}
	return generator.result, nil
}

type fixedMemoryEmbedder struct {
	result memory.EmbeddingResult
	err    error
}

func (embedder *fixedMemoryEmbedder) Embed(
	ctx context.Context,
	request memory.EmbeddingRequest,
) (memory.EmbeddingResult, error) {
	if err := ctx.Err(); err != nil {
		return memory.EmbeddingResult{}, err
	}
	if err := memory.ValidateEmbeddingRequest(request); err != nil {
		return memory.EmbeddingResult{}, err
	}
	if embedder.err != nil {
		return memory.EmbeddingResult{}, embedder.err
	}
	return embedder.result, nil
}

type fixedSpeechRecognizer struct {
	result agentvoice.TranscriptionResult
	err    error
}

func newFixedSpeechRecognizer(
	result agentvoice.TranscriptionResult,
) *fixedSpeechRecognizer {
	return &fixedSpeechRecognizer{result: result}
}

func (recognizer *fixedSpeechRecognizer) Transcribe(
	ctx context.Context,
	request agentvoice.TranscriptionRequest,
) (agentvoice.TranscriptionResult, error) {
	if err := ctx.Err(); err != nil {
		return agentvoice.TranscriptionResult{}, err
	}
	if err := agentvoice.ValidateTranscriptionRequest(request); err != nil {
		return agentvoice.TranscriptionResult{}, err
	}
	if recognizer.err != nil {
		return agentvoice.TranscriptionResult{}, recognizer.err
	}
	return recognizer.result, nil
}

func (recognizer *fixedSpeechRecognizer) TranscribeStream(
	ctx context.Context,
	request agentvoice.TranscriptionRequest,
	observer agentvoice.TranscriptionObserver,
) (agentvoice.TranscriptionResult, error) {
	result, err := recognizer.Transcribe(ctx, request)
	if err != nil {
		return agentvoice.TranscriptionResult{}, err
	}
	if err := observer.OnTranscriptionUpdate(
		ctx,
		agentvoice.TranscriptionUpdate{Transcript: result.Transcript},
	); err != nil {
		return agentvoice.TranscriptionResult{}, err
	}
	if err := observer.OnTranscriptionUpdate(
		ctx,
		agentvoice.TranscriptionUpdate{Transcript: result.Transcript, Final: true},
	); err != nil {
		return agentvoice.TranscriptionResult{}, err
	}
	return result, nil
}

type fixedSpeechSynthesizer struct {
	result       agentvoice.SynthesisResult
	audioFactory func() platformmedia.ManagedAudioSource
	err          error
}

func newFixedSpeechSynthesizer(
	result agentvoice.SynthesisResult,
	audioFactory func() platformmedia.ManagedAudioSource,
) *fixedSpeechSynthesizer {
	result.Audio = nil
	return &fixedSpeechSynthesizer{result: result, audioFactory: audioFactory}
}

func (synthesizer *fixedSpeechSynthesizer) Synthesize(
	ctx context.Context,
	request agentvoice.SynthesisRequest,
) (agentvoice.SynthesisResult, error) {
	if err := ctx.Err(); err != nil {
		return agentvoice.SynthesisResult{}, err
	}
	if err := agentvoice.ValidateSynthesisRequest(request); err != nil {
		return agentvoice.SynthesisResult{}, err
	}
	if synthesizer.err != nil {
		return agentvoice.SynthesisResult{}, synthesizer.err
	}
	result := synthesizer.result
	if synthesizer.audioFactory != nil {
		result.Audio = synthesizer.audioFactory()
	}
	return result, nil
}

var (
	_ agentrun.TextGenerator               = (*fixedTextGenerator)(nil)
	_ agentsummary.Generator               = (*fixedSummaryGenerator)(nil)
	_ memory.Embedder                      = (*fixedMemoryEmbedder)(nil)
	_ agentvoice.StreamingSpeechRecognizer = (*fixedSpeechRecognizer)(nil)
	_ agentvoice.SpeechSynthesizer         = (*fixedSpeechSynthesizer)(nil)
)

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
