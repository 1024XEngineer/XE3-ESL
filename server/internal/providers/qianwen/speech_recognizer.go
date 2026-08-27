package qianwen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

const maxASRDataURLBytes = 10_000_000

const (
	realtimeASRModel = "qwen-audio-3.0-asr-flash-streaming"
	recordedASRModel = "qwen-audio-3.0-asr-flash"
)

type ASRConfig struct {
	BaseURL  string
	Model    string
	Timeout  time.Duration
	Observer providerobservability.Recorder
}

type speechRecognizer struct {
	endpoint   string
	wsEndpoint string
	model      string
	timeout    time.Duration
	apiKey     providerSecret
	client     httpDoer
	observer   providerobservability.Recorder
}

func (recognizer *speechRecognizer) String() string {
	if recognizer == nil {
		return "QianwenASRRecognizer(<nil>)"
	}
	return fmt.Sprintf(
		"QianwenASRRecognizer(model=%q, timeout=%s, api_key=[REDACTED])",
		recognizer.model,
		recognizer.timeout,
	)
}

func (recognizer *speechRecognizer) GoString() string {
	return recognizer.String()
}

func newSpeechRecognizer(config ASRConfig, apiKey string) (*speechRecognizer, error) {
	client := &http.Client{
		Timeout: config.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return newRecognizerWithClient(config, apiKey, client)
}

func newRecognizerWithClient(
	config ASRConfig,
	apiKey string,
	client httpDoer,
) (*speechRecognizer, error) {
	baseURL, err := normalizeDashScopeAPIBaseURL(config.BaseURL)
	if err != nil {
		return nil, err
	}
	model, err := normalizeASRModel(config.Model)
	if err != nil {
		return nil, err
	}
	apiKey, err = normalizeAPIKey(apiKey)
	if err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.Timeout > maxTimeout {
		return nil, fmt.Errorf("Qianwen ASR timeout must be greater than zero and at most %s", maxTimeout)
	}
	if client == nil {
		return nil, errors.New("Qianwen ASR HTTP client is required")
	}
	return &speechRecognizer{
		endpoint:   baseURL + multimodalGenerationPath,
		wsEndpoint: realtimeASREndpoint(baseURL),
		model:      model,
		timeout:    config.Timeout,
		apiKey:     newProviderSecret(apiKey),
		client:     client,
		observer:   config.Observer,
	}, nil
}

func (recognizer *speechRecognizer) Transcribe(
	ctx context.Context,
	request protocol.TranscriptionRequest,
) (protocol.TranscriptionResult, error) {
	if isRealtimeASRModel(recognizer.model) {
		return recognizer.transcribeRealtime(ctx, request, nil)
	}
	if ctx == nil {
		return protocol.TranscriptionResult{}, protocol.NewSpeechError(
			protocol.SpeechOperationTranscription,
			protocol.ErrorInvalidRequest,
			0,
			"",
			"",
			errors.New("speech transcription context is required"),
		)
	}
	if err := protocol.ValidateTranscriptionRequest(request); err != nil {
		return protocol.TranscriptionResult{}, protocol.NewSpeechError(
			protocol.SpeechOperationTranscription,
			protocol.ErrorInvalidRequest,
			0,
			"",
			"",
			err,
		)
	}
	audioBytes, err := readAudioSource(request.Audio)
	if err != nil {
		return protocol.TranscriptionResult{}, protocol.NewSpeechError(
			protocol.SpeechOperationTranscription,
			protocol.ErrorInvalidRequest,
			0,
			"",
			"",
			err,
		)
	}
	dataURL := "data:" + request.Audio.MediaType() + ";base64," +
		base64.StdEncoding.EncodeToString(audioBytes)
	if len(dataURL) > maxASRDataURLBytes {
		return protocol.TranscriptionResult{}, protocol.NewSpeechError(
			protocol.SpeechOperationTranscription,
			protocol.ErrorInvalidRequest,
			0,
			"",
			"",
			errors.New("encoded audio exceeds the Qianwen ASR input limit"),
		)
	}

	payload := asrRequest{
		Model: recognizer.model,
		Input: asrInput{
			Messages: []asrMessage{{
				Role: "user",
				Content: []asrContent{{
					Type: "input_audio",
					InputAudio: asrInputAudio{
						Data: dataURL,
					},
				}},
			}},
		},
		Parameters: asrParameters{
			Format:     "wav",
			SampleRate: strconv.Itoa(request.Audio.SampleRate()),
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return protocol.TranscriptionResult{}, protocol.NewSpeechError(
			protocol.SpeechOperationTranscription,
			protocol.ErrorInvalidRequest,
			0,
			"",
			"",
			errors.New("encode Qianwen ASR request"),
		)
	}

	callContext, cancel := context.WithTimeout(ctx, recognizer.timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(
		callContext,
		http.MethodPost,
		recognizer.endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return protocol.TranscriptionResult{}, protocol.NewSpeechError(
			protocol.SpeechOperationTranscription,
			protocol.ErrorConfiguration,
			0,
			"",
			"",
			errors.New("create Qianwen ASR request"),
		)
	}
	httpRequest.Header.Set(
		authorizationHeaderName,
		"Bearer "+recognizer.apiKey.reveal(),
	)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("X-DashScope-SSE", "disable")

	response, err := recognizer.client.Do(httpRequest)
	if err != nil {
		return protocol.TranscriptionResult{}, speechTransportError(
			protocol.SpeechOperationTranscription,
			callContext,
			err,
		)
	}
	if response == nil {
		return protocol.TranscriptionResult{}, invalidSpeechResponse(
			protocol.SpeechOperationTranscription,
			0,
			"",
			"Qianwen ASR returned a nil HTTP response",
		)
	}
	if response.Body == nil {
		return protocol.TranscriptionResult{}, invalidSpeechResponse(
			protocol.SpeechOperationTranscription,
			response.StatusCode,
			response.Header.Get("X-Request-Id"),
			"Qianwen ASR returned an HTTP response without a body",
		)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return protocol.TranscriptionResult{}, decodeSpeechStatusError(
			protocol.SpeechOperationTranscription,
			response,
		)
	}
	responseBody, err := readBounded(response.Body, maxResponseBytes)
	if err != nil {
		return protocol.TranscriptionResult{}, invalidSpeechResponse(
			protocol.SpeechOperationTranscription,
			response.StatusCode,
			response.Header.Get("X-Request-Id"),
			"Qianwen ASR response exceeds the accepted limit",
		)
	}
	var completion asrResponse
	if err := json.Unmarshal(responseBody, &completion); err != nil {
		return protocol.TranscriptionResult{}, invalidSpeechResponse(
			protocol.SpeechOperationTranscription,
			response.StatusCode,
			response.Header.Get("X-Request-Id"),
			"decode Qianwen ASR response",
		)
	}
	result, err := completion.result(recognizer.model)
	if err != nil {
		return protocol.TranscriptionResult{}, invalidSpeechResponse(
			protocol.SpeechOperationTranscription,
			response.StatusCode,
			response.Header.Get("X-Request-Id"),
			err.Error(),
		)
	}
	return result, nil
}

func (recognizer *speechRecognizer) TranscribeStream(
	ctx context.Context,
	request protocol.TranscriptionRequest,
	observer protocol.TranscriptionObserver,
) (protocol.TranscriptionResult, error) {
	if !isRealtimeASRModel(recognizer.model) || observer == nil {
		return recognizer.Transcribe(ctx, request)
	}
	return recognizer.transcribeRealtime(ctx, request, observer)
}

type asrRequest struct {
	Model      string        `json:"model"`
	Input      asrInput      `json:"input"`
	Parameters asrParameters `json:"parameters"`
}

type asrInput struct {
	Messages []asrMessage `json:"messages"`
}

type asrMessage struct {
	Role    string       `json:"role"`
	Content []asrContent `json:"content"`
}

type asrContent struct {
	Type       string        `json:"type"`
	InputAudio asrInputAudio `json:"input_audio"`
}

type asrInputAudio struct {
	Data string `json:"data"`
}

type asrParameters struct {
	Format     string `json:"format"`
	SampleRate string `json:"sample_rate"`
}

type asrResponse struct {
	RequestID string `json:"request_id"`
	Output    struct {
		Text   string `json:"text"`
		Output struct {
			Sentence struct {
				Text string `json:"text"`
			} `json:"sentence"`
		} `json:"output"`
		Sentence struct {
			Text string `json:"text"`
		} `json:"sentence"`
	} `json:"output"`
	Usage struct {
		Duration int `json:"duration"`
	} `json:"usage"`
}

func (response asrResponse) result(model string) (protocol.TranscriptionResult, error) {
	requestID := sanitizeIdentifier(response.RequestID)
	if requestID == "" {
		return protocol.TranscriptionResult{}, errors.New("Qianwen ASR response has no valid request ID")
	}
	if response.Usage.Duration < 0 {
		return protocol.TranscriptionResult{}, errors.New("Qianwen ASR response has invalid audio usage")
	}
	text := strings.TrimSpace(response.Output.Text)
	sentenceText := strings.TrimSpace(response.Output.Sentence.Text)
	// output.text is the cumulative full transcript. sentence.text is only
	// the current sentence, so differing non-empty values are expected.
	if text == "" {
		text = sentenceText
	}
	if text == "" {
		text = strings.TrimSpace(response.Output.Output.Sentence.Text)
	}
	if text == "" {
		return protocol.TranscriptionResult{}, errors.New("Qianwen ASR response has no transcript")
	}
	return protocol.TranscriptionResult{
		ID:         requestID,
		Provider:   providerName,
		Model:      model,
		Transcript: text,
		Usage: protocol.SpeechUsage{
			AudioSeconds: response.Usage.Duration,
		},
	}, nil
}

func readAudioSource(source platformmedia.AudioSource) ([]byte, error) {
	reader, err := source.Open()
	if err != nil {
		return nil, errors.New("open validated audio source")
	}
	data, readErr := io.ReadAll(
		io.LimitReader(reader, platformmedia.MaxAudioBytes+1),
	)
	closeErr := reader.Close()
	if readErr != nil {
		clear(data)
		return nil, errors.New("read validated audio source")
	}
	if closeErr != nil {
		clear(data)
		return nil, errors.New("close validated audio source")
	}
	if int64(len(data)) != source.Size() {
		clear(data)
		return nil, errors.New("audio source size changed after validation")
	}
	return data, nil
}

func normalizeASRModel(raw string) (string, error) {
	model := strings.ToLower(strings.TrimSpace(raw))
	if model != realtimeASRModel && model != recordedASRModel {
		return "", fmt.Errorf(
			"Qianwen ASR adapter only accepts %s or %s",
			realtimeASRModel,
			recordedASRModel,
		)
	}
	return model, nil
}

func isRealtimeASRModel(model string) bool {
	return model == realtimeASRModel
}

func invalidSpeechResponse(
	operation protocol.SpeechOperation,
	statusCode int,
	requestID string,
	cause string,
) error {
	return protocol.NewSpeechError(
		operation,
		protocol.ErrorInvalidResponse,
		statusCode,
		"",
		sanitizeIdentifier(requestID),
		errors.New(cause),
	)
}

var _ protocol.SpeechRecognizer = (*speechRecognizer)(nil)
var _ protocol.StreamingSpeechRecognizer = (*speechRecognizer)(nil)
