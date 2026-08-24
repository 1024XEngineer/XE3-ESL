package qianwen

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	platformconfig "github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
	"github.com/gorilla/websocket"
)

const (
	omniLatencyLiveTestFlag = "QIANWEN_OMNI_LATENCY_LIVE_TEST"
	omniLatencyRunsEnv      = "QIANWEN_OMNI_LATENCY_RUNS"
	omniLatencyAudioEnv     = "QIANWEN_OMNI_LATENCY_AUDIO"
	omniLatencyModel        = "qwen3.5-omni-flash-realtime"
	omniLatencyVoice        = "Tina"
	omniLatencyFrameBytes   = 640
	omniLatencyFramePeriod  = 20 * time.Millisecond
	omniLatencyInstructions = "You are SpeakUp, an English conversation coach. " +
		"Reply in one short English sentence of no more than 20 words."
)

type omniLatencyResult struct {
	Pipeline            string `json:"pipeline"`
	Run                 int    `json:"run"`
	Models              string `json:"models"`
	BenchmarkScope      string `json:"benchmark_scope"`
	SetupScope          string `json:"setup_scope"`
	SessionSetupMS      int64  `json:"session_setup_ms"`
	AudioToTranscriptMS int64  `json:"audio_to_transcript_ms"`
	AudioToFirstTextMS  int64  `json:"audio_to_first_text_ms"`
	AudioToFirstAudioMS int64  `json:"audio_to_first_audio_ms"`
	AudioToDoneMS       int64  `json:"audio_to_done_ms"`
	RunWallMS           int64  `json:"run_wall_ms"`
	TranscriptRunes     int    `json:"transcript_runes"`
	TranscriptDigest    string `json:"transcript_digest"`
	ResponseRunes       int    `json:"response_runes"`
	ResponseDigest      string `json:"response_digest"`
	AudioBytes          int    `json:"audio_bytes"`
	UsageScope          string `json:"usage_scope"`
	InputTokens         int    `json:"input_tokens"`
	OutputTokens        int    `json:"output_tokens"`
	TotalTokens         int    `json:"total_tokens"`
}

type omniLatencySummary struct {
	Pipeline             string `json:"pipeline"`
	SuccessfulRuns       int    `json:"successful_runs"`
	AudioToTranscriptP50 int64  `json:"audio_to_transcript_p50_ms"`
	AudioToTranscriptP95 int64  `json:"audio_to_transcript_p95_ms"`
	AudioToFirstTextP50  int64  `json:"audio_to_first_text_p50_ms"`
	AudioToFirstTextP95  int64  `json:"audio_to_first_text_p95_ms"`
	AudioToFirstAudioP50 int64  `json:"audio_to_first_audio_p50_ms"`
	AudioToFirstAudioP95 int64  `json:"audio_to_first_audio_p95_ms"`
	AudioToCompletionP50 int64  `json:"audio_to_completion_p50_ms"`
	AudioToCompletionP95 int64  `json:"audio_to_completion_p95_ms"`
}

func TestLiveOmniLatencyAgainstCurrentPipeline(t *testing.T) {
	if os.Getenv(omniLatencyLiveTestFlag) != "1" {
		t.Skip(
			"set " + omniLatencyLiveTestFlag +
				"=1 to compare paid Qianwen ASR/LLM/TTS and Omni calls",
		)
	}
	runs := omniLatencyRunCount(t)
	audioPath := strings.TrimSpace(os.Getenv(omniLatencyAudioEnv))
	if audioPath == "" {
		audioPath = defaultASRLiveFixture
	}
	audioBytes, err := os.ReadFile(audioPath)
	if err != nil {
		t.Fatalf("read latency fixture: %v", err)
	}
	audio := captureLiveASRFixture(t, audioPath)
	defer audio.Close()
	pcm, err := omniLatencyPCMWAV(audioBytes)
	if err != nil || audio.SampleRate() != 16_000 {
		t.Fatalf("latency fixture must be mono 16-bit 16 kHz PCM WAV: %v", err)
	}

	asrConfig, err := platformconfig.LoadSpeechRecognition()
	if err != nil {
		t.Fatalf("load ASR config: %v", err)
	}
	textConfig, err := platformconfig.LoadTextGeneration()
	if err != nil {
		t.Fatalf("load text config: %v", err)
	}
	ttsConfig, err := platformconfig.LoadSpeechSynthesis()
	if err != nil {
		t.Fatalf("load TTS config: %v", err)
	}
	if asrConfig.APIKey.Reveal() != textConfig.APIKey.Reveal() ||
		asrConfig.APIKey.Reveal() != ttsConfig.APIKey.Reveal() {
		t.Fatal("latency benchmark requires one Qianwen workspace credential")
	}

	recognizer, err := newSpeechRecognizer(ASRConfig{
		BaseURL: asrConfig.BaseURL,
		Model:   asrConfig.Model,
		Timeout: asrConfig.Timeout,
	}, asrConfig.APIKey.Reveal())
	if err != nil {
		t.Fatalf("create current ASR: %v", err)
	}
	generator, err := newTextClient(TextConfig{
		BaseURL:         textConfig.BaseURL,
		Model:           textConfig.Model,
		Timeout:         textConfig.Timeout,
		MaxOutputTokens: textConfig.MaxOutputTokens,
	}, textConfig.APIKey.Reveal())
	if err != nil {
		t.Fatalf("create current LLM: %v", err)
	}
	synthesizer, err := newSpeechSynthesizer(TTSConfig{
		BaseURL:       ttsConfig.BaseURL,
		Model:         ttsConfig.Model,
		Voice:         ttsConfig.Voice,
		LanguageHint:  ttsConfig.LanguageHint,
		Timeout:       ttsConfig.Timeout,
		TempDirectory: ttsConfig.TempDirectory,
	}, ttsConfig.APIKey.Reveal())
	if err != nil {
		t.Fatalf("create current TTS: %v", err)
	}
	for _, endpoint := range []struct {
		name    string
		baseURL string
	}{
		{name: "ASR", baseURL: asrConfig.BaseURL},
		{name: "LLM", baseURL: textConfig.BaseURL},
		{name: "TTS", baseURL: ttsConfig.BaseURL},
	} {
		if _, err := parseOmniLatencySingaporeBaseURL(endpoint.baseURL); err != nil {
			t.Fatalf("%s endpoint must use a Singapore Qianwen workspace: %v", endpoint.name, err)
		}
	}
	omniEndpoint, err := omniLatencyEndpoint(textConfig.BaseURL)
	if err != nil {
		t.Fatalf("create Singapore Omni endpoint: %v", err)
	}

	results := make([]omniLatencyResult, 0, runs*2)
	for run := 1; run <= runs; run++ {
		current := func() {
			result, runErr := benchmarkCurrentVoicePipeline(
				context.Background(), run, pcm, audio.SampleRate(),
				recognizer, generator, synthesizer,
			)
			if runErr != nil {
				t.Logf("benchmark_failure pipeline=current run=%d kind=%s", run, benchmarkErrorKind(runErr))
				return
			}
			results = append(results, result)
			logOmniLatencyResult(t, result)
		}
		omni := func() {
			result, runErr := benchmarkOmniVoicePipeline(
				context.Background(), run, omniEndpoint,
				textConfig.APIKey.Reveal(), pcm,
			)
			if runErr != nil {
				t.Logf("benchmark_failure pipeline=omni run=%d kind=%s", run, benchmarkErrorKind(runErr))
				return
			}
			results = append(results, result)
			logOmniLatencyResult(t, result)
		}
		if run%2 == 1 {
			current()
			omni()
		} else {
			omni()
			current()
		}
	}

	for _, pipeline := range []string{"current", "omni"} {
		summary, ok := summarizeOmniLatency(results, pipeline)
		if !ok || summary.SuccessfulRuns != runs {
			t.Fatalf(
				"pipeline %s produced %d/%d successful benchmark runs",
				pipeline,
				summary.SuccessfulRuns,
				runs,
			)
		}
		payload, err := json.Marshal(summary)
		if err != nil {
			t.Fatalf("encode benchmark summary: %v", err)
		}
		t.Logf("benchmark_summary=%s", payload)
	}
}

func benchmarkCurrentVoicePipeline(
	ctx context.Context,
	run int,
	pcm []byte,
	sampleRate int,
	recognizer *speechRecognizer,
	generator *textClient,
	synthesizer *speechSynthesizer,
) (omniLatencyResult, error) {
	startedAt := time.Now()
	audioEnded := &omniLatencyMark{}
	transcriptReady := &omniLatencyMark{}
	firstText := &omniLatencyMark{}
	firstAudio := &omniLatencyMark{}
	observedPCM := &omniLatencyPacedReader{
		reader:  bytes.NewReader(pcm),
		markEOF: audioEnded.Mark,
	}
	transcription, err := recognizer.transcribeRealtimePCM(
		ctx,
		observedPCM,
		sampleRate,
		omniLatencyTranscriptionObserverFunc(func(
			_ context.Context,
			update protocol.TranscriptionUpdate,
		) error {
			if update.Final {
				transcriptReady.Mark()
			}
			return nil
		}),
	)
	if err != nil {
		return omniLatencyResult{}, err
	}
	if audioEnded.At().IsZero() || transcriptReady.At().IsZero() ||
		strings.TrimSpace(transcription.Transcript) == "" {
		return omniLatencyResult{}, errors.New("current ASR timing result is incomplete")
	}

	var audioMu sync.Mutex
	audioBytes := 0
	ttsStartedAt := time.Now()
	ttsReady := &omniLatencyMark{}
	ready := make(chan struct{})
	var speech *speechRealtimeSession
	var speechErr error
	go func() {
		speech, speechErr = synthesizer.openRealtimeSpeech(
			ctx,
			func(chunk []byte) error {
				if len(chunk) == 0 {
					return errors.New("current TTS returned an empty PCM chunk")
				}
				firstAudio.Mark()
				audioMu.Lock()
				audioBytes += len(chunk)
				audioMu.Unlock()
				return nil
			},
		)
		ttsReady.Mark()
		close(ready)
	}()

	completion, generationErr := generator.GenerateStream(
		ctx,
		protocol.TextRequest{Messages: []protocol.TextMessage{
			{Role: protocol.TextRoleSystem, Content: omniLatencyInstructions},
			{Role: protocol.TextRoleUser, Content: transcription.Transcript},
		}},
		protocol.TextDeltaObserverFunc(func(_ context.Context, delta string) error {
			if delta == "" {
				return nil
			}
			firstText.Mark()
			<-ready
			if speechErr != nil {
				return speechErr
			}
			return speech.AppendText(delta)
		}),
	)
	<-ready
	if generationErr != nil {
		if speech != nil {
			_ = speech.Close()
		}
		return omniLatencyResult{}, generationErr
	}
	if speechErr != nil {
		return omniLatencyResult{}, speechErr
	}
	if err := speech.Finish(); err != nil {
		_ = speech.Close()
		return omniLatencyResult{}, err
	}
	completedAt := time.Now()
	_ = speech.Close()
	audioMu.Lock()
	totalAudioBytes := audioBytes
	audioMu.Unlock()
	if firstText.At().IsZero() || firstAudio.At().IsZero() ||
		strings.TrimSpace(completion.Content) == "" || totalAudioBytes == 0 {
		return omniLatencyResult{}, errors.New("current pipeline result is incomplete")
	}

	return omniLatencyResult{
		Pipeline:            "current",
		Run:                 run,
		Models:              transcription.Model + "+" + completion.Model + "+" + synthesizer.model,
		BenchmarkScope:      "provider_direct_short_reply",
		SetupScope:          "tts_concurrent_after_asr",
		SessionSetupMS:      millisecondsBetween(ttsStartedAt, ttsReady.At()),
		AudioToTranscriptMS: millisecondsBetween(audioEnded.At(), transcriptReady.At()),
		AudioToFirstTextMS:  millisecondsBetween(audioEnded.At(), firstText.At()),
		AudioToFirstAudioMS: millisecondsBetween(audioEnded.At(), firstAudio.At()),
		AudioToDoneMS:       millisecondsBetween(audioEnded.At(), completedAt),
		RunWallMS:           millisecondsBetween(startedAt, completedAt),
		TranscriptRunes:     utf8.RuneCountInString(transcription.Transcript),
		TranscriptDigest:    omniLatencyDigest(transcription.Transcript),
		ResponseRunes:       utf8.RuneCountInString(completion.Content),
		ResponseDigest:      omniLatencyDigest(completion.Content),
		AudioBytes:          totalAudioBytes,
		UsageScope:          "text_generation_only",
		InputTokens:         completion.Usage.InputTokens,
		OutputTokens:        completion.Usage.OutputTokens,
		TotalTokens:         completion.Usage.TotalTokens,
	}, nil
}

type omniLatencyEvent struct {
	Type       string `json:"type"`
	Delta      string `json:"delta"`
	Transcript string `json:"transcript"`
	Response   struct {
		Status string           `json:"status"`
		Usage  omniLatencyUsage `json:"usage"`
	} `json:"response"`
	Error struct {
		Type  string `json:"type"`
		Code  string `json:"code"`
		Param string `json:"param"`
	} `json:"error"`
}

type omniLatencyUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type omniLatencyState struct {
	transcriptReady  *omniLatencyMark
	firstText        *omniLatencyMark
	firstAudio       *omniLatencyMark
	transcript       string
	responseText     strings.Builder
	responseDoneText string
	audioBytes       int
	usage            omniLatencyUsage
}

func benchmarkOmniVoicePipeline(
	ctx context.Context,
	run int,
	endpoint string,
	apiKey string,
	audio []byte,
) (omniLatencyResult, error) {
	startedAt := time.Now()
	callContext, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	header := http.Header{}
	header.Set(authorizationHeaderName, "Bearer "+apiKey)
	connection, response, err := websocket.DefaultDialer.DialContext(
		callContext,
		endpoint,
		header,
	)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err != nil {
		return omniLatencyResult{}, errors.New("Omni WebSocket connection failed")
	}
	defer connection.Close()
	eventIDPrefix, err := newRealtimeSpeechTaskID()
	if err != nil {
		return omniLatencyResult{}, errors.New("create Omni event ID")
	}
	eventIDPrefix = "event_" + strings.ReplaceAll(eventIDPrefix, "-", "")
	eventIDSequence := 0
	nextEventID := func() string {
		eventIDSequence++
		return eventIDPrefix + "_" + strconv.Itoa(eventIDSequence)
	}
	if deadline, ok := callContext.Deadline(); ok {
		_ = connection.SetReadDeadline(deadline)
		_ = connection.SetWriteDeadline(deadline)
	}
	created, err := readOmniLatencyEvent(connection)
	if err != nil {
		return omniLatencyResult{}, err
	}
	if created.Type != "session.created" {
		return omniLatencyResult{}, errors.New("Omni session.created was not received")
	}
	if err := connection.WriteJSON(map[string]any{
		"event_id": nextEventID(),
		"type":     "session.update",
		"session": map[string]any{
			"model":        omniLatencyModel,
			"modalities":   []string{"text", "audio"},
			"voice":        omniLatencyVoice,
			"instructions": omniLatencyInstructions,
			"audio": map[string]any{
				"input": map[string]any{"format": map[string]any{
					"type": "pcm", "sample_rate": 16_000,
				}},
				"output": map[string]any{"format": map[string]any{
					"type": "pcm", "sample_rate": 24_000,
				}},
			},
			"input_audio_transcription": map[string]any{
				"model": "qwen3-asr-flash-realtime",
			},
			"turn_detection": nil,
		},
	}); err != nil {
		return omniLatencyResult{}, errors.New("send Omni session.update")
	}
	for {
		event, eventErr := readOmniLatencyEvent(connection)
		if eventErr != nil {
			return omniLatencyResult{}, eventErr
		}
		if event.Type == "session.updated" {
			break
		}
	}
	sessionReadyAt := time.Now()
	for offset := 0; offset < len(audio); offset += omniLatencyFrameBytes {
		end := min(offset+omniLatencyFrameBytes, len(audio))
		if err := connection.WriteJSON(map[string]any{
			"event_id": nextEventID(),
			"type":     "input_audio_buffer.append",
			"audio":    base64.StdEncoding.EncodeToString(audio[offset:end]),
		}); err != nil {
			return omniLatencyResult{}, errors.New("append Omni audio")
		}
		time.Sleep(omniLatencyFramePeriod)
	}
	audioEndedAt := time.Now()
	if err := connection.WriteJSON(map[string]any{
		"event_id": nextEventID(),
		"type":     "input_audio_buffer.commit",
	}); err != nil {
		return omniLatencyResult{}, errors.New("commit Omni audio")
	}
	state := omniLatencyState{
		transcriptReady: &omniLatencyMark{},
		firstText:       &omniLatencyMark{},
		firstAudio:      &omniLatencyMark{},
	}
	for {
		event, eventErr := readOmniLatencyEvent(connection)
		if eventErr != nil {
			return omniLatencyResult{}, eventErr
		}
		if err := state.Observe(event); err != nil {
			return omniLatencyResult{}, err
		}
		if event.Type == "input_audio_buffer.committed" {
			break
		}
	}
	if err := connection.WriteJSON(map[string]any{
		"event_id": nextEventID(),
		"type":     "response.create",
	}); err != nil {
		return omniLatencyResult{}, errors.New("create Omni response")
	}
	completedAt := time.Time{}
	for completedAt.IsZero() {
		event, eventErr := readOmniLatencyEvent(connection)
		if eventErr != nil {
			return omniLatencyResult{}, eventErr
		}
		if err := state.Observe(event); err != nil {
			return omniLatencyResult{}, err
		}
		if event.Type == "response.done" {
			completedAt = time.Now()
		}
	}
	responseText := strings.TrimSpace(state.responseDoneText)
	if responseText == "" {
		responseText = strings.TrimSpace(state.responseText.String())
	}
	if state.transcriptReady.At().IsZero() || state.firstText.At().IsZero() ||
		state.firstAudio.At().IsZero() || strings.TrimSpace(state.transcript) == "" ||
		responseText == "" || state.audioBytes == 0 || state.audioBytes%2 != 0 {
		return omniLatencyResult{}, errors.New("Omni benchmark result is incomplete")
	}
	return omniLatencyResult{
		Pipeline:            "omni",
		Run:                 run,
		Models:              omniLatencyModel,
		BenchmarkScope:      "provider_direct_short_reply",
		SetupScope:          "omni_before_input",
		SessionSetupMS:      millisecondsBetween(startedAt, sessionReadyAt),
		AudioToTranscriptMS: millisecondsBetween(audioEndedAt, state.transcriptReady.At()),
		AudioToFirstTextMS:  millisecondsBetween(audioEndedAt, state.firstText.At()),
		AudioToFirstAudioMS: millisecondsBetween(audioEndedAt, state.firstAudio.At()),
		AudioToDoneMS:       millisecondsBetween(audioEndedAt, completedAt),
		RunWallMS:           millisecondsBetween(startedAt, completedAt),
		TranscriptRunes:     utf8.RuneCountInString(state.transcript),
		TranscriptDigest:    omniLatencyDigest(state.transcript),
		ResponseRunes:       utf8.RuneCountInString(responseText),
		ResponseDigest:      omniLatencyDigest(responseText),
		AudioBytes:          state.audioBytes,
		UsageScope:          "omni_text_and_audio",
		InputTokens:         state.usage.InputTokens,
		OutputTokens:        state.usage.OutputTokens,
		TotalTokens:         state.usage.TotalTokens,
	}, nil
}

func (state *omniLatencyState) Observe(event omniLatencyEvent) error {
	switch event.Type {
	case "conversation.item.input_audio_transcription.completed":
		state.transcript = strings.TrimSpace(event.Transcript)
		state.transcriptReady.Mark()
	case "conversation.item.input_audio_transcription.failed":
		return errors.New("Omni input transcription failed")
	case "response.audio_transcript.delta":
		if event.Delta != "" {
			state.firstText.Mark()
			_, _ = state.responseText.WriteString(event.Delta)
		}
	case "response.audio_transcript.done":
		state.responseDoneText = strings.TrimSpace(event.Transcript)
	case "response.audio.delta":
		decoded, err := base64.StdEncoding.DecodeString(event.Delta)
		if err != nil || len(decoded) == 0 {
			return errors.New("Omni returned invalid PCM")
		}
		state.firstAudio.Mark()
		state.audioBytes += len(decoded)
	case "response.done":
		if event.Response.Status != "completed" {
			return errors.New("Omni response did not complete")
		}
		state.usage = event.Response.Usage
	}
	return nil
}

func readOmniLatencyEvent(connection *websocket.Conn) (omniLatencyEvent, error) {
	messageType, payload, err := connection.ReadMessage()
	if err != nil {
		return omniLatencyEvent{}, errors.New("read Omni event")
	}
	if messageType != websocket.TextMessage {
		return omniLatencyEvent{}, errors.New("Omni returned a non-JSON event")
	}
	var event omniLatencyEvent
	if err := json.Unmarshal(payload, &event); err != nil || event.Type == "" {
		return omniLatencyEvent{}, errors.New("decode Omni event")
	}
	if event.Type == "error" {
		return omniLatencyEvent{}, fmt.Errorf(
			"Omni request failed: type=%s code=%s param=%s",
			sanitizeIdentifier(event.Error.Type),
			sanitizeIdentifier(event.Error.Code),
			sanitizeIdentifier(event.Error.Param),
		)
	}
	return event, nil
}

func omniLatencyEndpoint(baseURL string) (string, error) {
	parsed, err := parseOmniLatencySingaporeBaseURL(baseURL)
	if err != nil {
		return "", err
	}
	parsed.Scheme = "wss"
	parsed.Path = "/api-ws/v1/realtime"
	parsed.RawPath = ""
	query := url.Values{}
	query.Set("model", omniLatencyModel)
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String(), nil
}

func parseOmniLatencySingaporeBaseURL(baseURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, errors.New("Qianwen base URL is invalid")
	}
	if !strings.HasSuffix(parsed.Hostname(), ".ap-southeast-1.maas.aliyuncs.com") {
		return nil, errors.New("latency benchmark requires a Singapore Qianwen workspace")
	}
	return parsed, nil
}

func TestOmniLatencyEndpointRequiresSingaporeWorkspace(t *testing.T) {
	endpoint, err := omniLatencyEndpoint(
		"https://workspace.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1",
	)
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	want := "wss://workspace.ap-southeast-1.maas.aliyuncs.com/api-ws/v1/realtime" +
		"?model=qwen3.5-omni-flash-realtime"
	if endpoint != want {
		t.Fatalf("endpoint = %q, want %q", endpoint, want)
	}
	for _, baseURL := range []string{
		"https://dashscope.aliyuncs.com/compatible-mode/v1",
		"http://workspace.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1",
	} {
		if _, err := omniLatencyEndpoint(baseURL); err == nil {
			t.Fatalf("expected endpoint %q to fail closed", baseURL)
		}
	}
}

type omniLatencyPacedReader struct {
	reader  io.Reader
	markEOF func()
	once    sync.Once
	nextAt  time.Time
}

func (reader *omniLatencyPacedReader) Read(payload []byte) (int, error) {
	if !reader.nextAt.IsZero() {
		if wait := time.Until(reader.nextAt); wait > 0 {
			time.Sleep(wait)
		}
	}
	if len(payload) > omniLatencyFrameBytes {
		payload = payload[:omniLatencyFrameBytes]
	}
	read, err := reader.reader.Read(payload)
	if read > 0 {
		reader.nextAt = time.Now().Add(omniLatencyFramePeriod)
	}
	if err == io.EOF {
		reader.once.Do(reader.markEOF)
	}
	return read, err
}

func omniLatencyPCMWAV(wav []byte) ([]byte, error) {
	if len(wav) < 44 || string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return nil, errors.New("invalid WAV header")
	}
	var validFormat bool
	var pcm []byte
	for offset := 12; offset+8 <= len(wav); {
		chunkSize := int(binary.LittleEndian.Uint32(wav[offset+4 : offset+8]))
		chunkStart := offset + 8
		chunkEnd := chunkStart + chunkSize
		if chunkSize < 0 || chunkEnd < chunkStart || chunkEnd > len(wav) {
			return nil, errors.New("invalid WAV chunk")
		}
		switch string(wav[offset : offset+4]) {
		case "fmt ":
			if chunkSize < 16 ||
				binary.LittleEndian.Uint16(wav[chunkStart:chunkStart+2]) != 1 ||
				binary.LittleEndian.Uint16(wav[chunkStart+2:chunkStart+4]) != 1 ||
				binary.LittleEndian.Uint32(wav[chunkStart+4:chunkStart+8]) != 16_000 ||
				binary.LittleEndian.Uint16(wav[chunkStart+14:chunkStart+16]) != 16 {
				return nil, errors.New("unsupported WAV format")
			}
			validFormat = true
		case "data":
			if chunkSize == 0 || chunkSize%2 != 0 {
				return nil, errors.New("invalid PCM payload")
			}
			pcm = wav[chunkStart:chunkEnd]
		}
		offset = chunkEnd + chunkSize%2
	}
	if !validFormat || len(pcm) == 0 {
		return nil, errors.New("WAV format or PCM payload is missing")
	}
	return pcm, nil
}

func TestOmniLatencyPCMWAVExtractsLiveFixture(t *testing.T) {
	wav, err := os.ReadFile(defaultASRLiveFixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	pcm, err := omniLatencyPCMWAV(wav)
	if err != nil {
		t.Fatalf("extract PCM: %v", err)
	}
	if len(pcm) == 0 || len(pcm)%2 != 0 || len(pcm) >= len(wav) {
		t.Fatalf("unexpected PCM length %d for WAV length %d", len(pcm), len(wav))
	}
}

type omniLatencyTranscriptionObserverFunc func(
	context.Context,
	protocol.TranscriptionUpdate,
) error

func (observe omniLatencyTranscriptionObserverFunc) OnTranscriptionUpdate(
	ctx context.Context,
	update protocol.TranscriptionUpdate,
) error {
	return observe(ctx, update)
}

type omniLatencyMark struct {
	mu sync.Mutex
	at time.Time
}

func (mark *omniLatencyMark) Mark() {
	mark.mu.Lock()
	defer mark.mu.Unlock()
	if mark.at.IsZero() {
		mark.at = time.Now()
	}
}

func (mark *omniLatencyMark) At() time.Time {
	mark.mu.Lock()
	defer mark.mu.Unlock()
	return mark.at
}

func omniLatencyRunCount(t *testing.T) int {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(omniLatencyRunsEnv))
	if raw == "" {
		return 3
	}
	runs, err := strconv.Atoi(raw)
	if err != nil || runs < 1 || runs > 10 {
		t.Fatalf("%s must be an integer from 1 to 10", omniLatencyRunsEnv)
	}
	return runs
}

func millisecondsBetween(start time.Time, end time.Time) int64 {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return -1
	}
	return end.Sub(start).Milliseconds()
}

func omniLatencyDigest(value string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return fmt.Sprintf("%x", digest[:6])
}

func logOmniLatencyResult(t *testing.T, result omniLatencyResult) {
	t.Helper()
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("encode benchmark result: %v", err)
	}
	t.Logf("benchmark_result=%s", payload)
}

func summarizeOmniLatency(
	results []omniLatencyResult,
	pipeline string,
) (omniLatencySummary, bool) {
	selected := make([]omniLatencyResult, 0, len(results))
	for _, result := range results {
		if result.Pipeline == pipeline {
			selected = append(selected, result)
		}
	}
	if len(selected) == 0 {
		return omniLatencySummary{}, false
	}
	metric := func(pick func(omniLatencyResult) int64, percentile int) int64 {
		values := make([]int64, 0, len(selected))
		for _, result := range selected {
			values = append(values, pick(result))
		}
		sort.Slice(values, func(left int, right int) bool { return values[left] < values[right] })
		index := (percentile*len(values)+99)/100 - 1
		return values[index]
	}
	return omniLatencySummary{
		Pipeline:             pipeline,
		SuccessfulRuns:       len(selected),
		AudioToTranscriptP50: metric(func(value omniLatencyResult) int64 { return value.AudioToTranscriptMS }, 50),
		AudioToTranscriptP95: metric(func(value omniLatencyResult) int64 { return value.AudioToTranscriptMS }, 95),
		AudioToFirstTextP50:  metric(func(value omniLatencyResult) int64 { return value.AudioToFirstTextMS }, 50),
		AudioToFirstTextP95:  metric(func(value omniLatencyResult) int64 { return value.AudioToFirstTextMS }, 95),
		AudioToFirstAudioP50: metric(func(value omniLatencyResult) int64 { return value.AudioToFirstAudioMS }, 50),
		AudioToFirstAudioP95: metric(func(value omniLatencyResult) int64 { return value.AudioToFirstAudioMS }, 95),
		AudioToCompletionP50: metric(func(value omniLatencyResult) int64 { return value.AudioToDoneMS }, 50),
		AudioToCompletionP95: metric(func(value omniLatencyResult) int64 { return value.AudioToDoneMS }, 95),
	}, true
}

func benchmarkErrorKind(err error) string {
	if err == nil {
		return "none"
	}
	var speechError *protocol.SpeechError
	if errors.As(err, &speechError) {
		return "speech_" + string(speechError.Kind)
	}
	var generationError *protocol.GenerationError
	if errors.As(err, &generationError) {
		return "generation_" + string(generationError.Kind)
	}
	if strings.Contains(strings.ToLower(err.Error()), "omni") {
		var kind strings.Builder
		lastUnderscore := false
		for _, character := range strings.ToLower(err.Error()) {
			if (character >= 'a' && character <= 'z') ||
				(character >= '0' && character <= '9') {
				_, _ = kind.WriteRune(character)
				lastUnderscore = false
			} else if !lastUnderscore {
				_, _ = kind.WriteRune('_')
				lastUnderscore = true
			}
		}
		return strings.Trim(kind.String(), "_")
	}
	return "benchmark_error"
}
