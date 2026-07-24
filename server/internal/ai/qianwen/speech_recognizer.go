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

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
)

const maxASRDataURLBytes = 10_000_000

type ASRConfig struct {
	BaseURL string
	Model   string
	Timeout time.Duration
}

type Recognizer struct {
	endpoint string
	model    string
	timeout  time.Duration
	apiKey   string
	client   httpDoer
}

func (recognizer *Recognizer) String() string {
	if recognizer == nil {
		return "FunASRRecognizer(<nil>)"
	}
	return fmt.Sprintf(
		"FunASRRecognizer(model=%q, timeout=%s, api_key=[REDACTED])",
		recognizer.model,
		recognizer.timeout,
	)
}

func (recognizer *Recognizer) GoString() string {
	return recognizer.String()
}

func NewRecognizer(config ASRConfig, apiKey string) (*Recognizer, error) {
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
) (*Recognizer, error) {
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
		return nil, fmt.Errorf("Fun-ASR timeout must be greater than zero and at most %s", maxTimeout)
	}
	if client == nil {
		return nil, errors.New("Fun-ASR HTTP client is required")
	}
	return &Recognizer{
		endpoint: baseURL + multimodalGenerationPath,
		model:    model,
		timeout:  config.Timeout,
		apiKey:   apiKey,
		client:   client,
	}, nil
}

func (recognizer *Recognizer) Transcribe(
	ctx context.Context,
	request ai.TranscriptionRequest,
) (ai.TranscriptionResult, error) {
	if ctx == nil {
		return ai.TranscriptionResult{}, ai.NewSpeechError(
			ai.SpeechOperationTranscription,
			ai.ErrorInvalidRequest,
			0,
			"",
			"",
			errors.New("speech transcription context is required"),
		)
	}
	if err := ai.ValidateTranscriptionRequest(request); err != nil {
		return ai.TranscriptionResult{}, ai.NewSpeechError(
			ai.SpeechOperationTranscription,
			ai.ErrorInvalidRequest,
			0,
			"",
			"",
			err,
		)
	}
	audioBytes, err := readAudioSource(request.Audio)
	if err != nil {
		return ai.TranscriptionResult{}, ai.NewSpeechError(
			ai.SpeechOperationTranscription,
			ai.ErrorInvalidRequest,
			0,
			"",
			"",
			err,
		)
	}
	dataURL := "data:" + request.Audio.MediaType() + ";base64," +
		base64.StdEncoding.EncodeToString(audioBytes)
	if len(dataURL) > maxASRDataURLBytes {
		return ai.TranscriptionResult{}, ai.NewSpeechError(
			ai.SpeechOperationTranscription,
			ai.ErrorInvalidRequest,
			0,
			"",
			"",
			errors.New("encoded audio exceeds the Fun-ASR input limit"),
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
		return ai.TranscriptionResult{}, ai.NewSpeechError(
			ai.SpeechOperationTranscription,
			ai.ErrorInvalidRequest,
			0,
			"",
			"",
			errors.New("encode Fun-ASR request"),
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
		return ai.TranscriptionResult{}, ai.NewSpeechError(
			ai.SpeechOperationTranscription,
			ai.ErrorConfiguration,
			0,
			"",
			"",
			errors.New("create Fun-ASR request"),
		)
	}
	httpRequest.Header.Set(authorizationHeaderName, "Bearer "+recognizer.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("X-DashScope-SSE", "disable")

	response, err := recognizer.client.Do(httpRequest)
	if err != nil {
		return ai.TranscriptionResult{}, speechTransportError(
			ai.SpeechOperationTranscription,
			callContext,
			err,
		)
	}
	if response == nil {
		return ai.TranscriptionResult{}, invalidSpeechResponse(
			ai.SpeechOperationTranscription,
			0,
			"",
			"Fun-ASR returned a nil HTTP response",
		)
	}
	if response.Body == nil {
		return ai.TranscriptionResult{}, invalidSpeechResponse(
			ai.SpeechOperationTranscription,
			response.StatusCode,
			response.Header.Get("X-Request-Id"),
			"Fun-ASR returned an HTTP response without a body",
		)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return ai.TranscriptionResult{}, decodeSpeechStatusError(
			ai.SpeechOperationTranscription,
			response,
		)
	}
	responseBody, err := readBounded(response.Body, maxResponseBytes)
	if err != nil {
		return ai.TranscriptionResult{}, invalidSpeechResponse(
			ai.SpeechOperationTranscription,
			response.StatusCode,
			response.Header.Get("X-Request-Id"),
			"Fun-ASR response exceeds the accepted limit",
		)
	}
	var completion asrResponse
	if err := json.Unmarshal(responseBody, &completion); err != nil {
		return ai.TranscriptionResult{}, invalidSpeechResponse(
			ai.SpeechOperationTranscription,
			response.StatusCode,
			response.Header.Get("X-Request-Id"),
			"decode Fun-ASR response",
		)
	}
	result, err := completion.result(recognizer.model)
	if err != nil {
		return ai.TranscriptionResult{}, invalidSpeechResponse(
			ai.SpeechOperationTranscription,
			response.StatusCode,
			response.Header.Get("X-Request-Id"),
			err.Error(),
		)
	}
	return result, nil
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
		Text     string `json:"text"`
		Sentence struct {
			Text string `json:"text"`
		} `json:"sentence"`
		Output struct {
			Sentence struct {
				Text string `json:"text"`
			} `json:"sentence"`
		} `json:"output"`
	} `json:"output"`
	Usage struct {
		Duration int `json:"duration"`
	} `json:"usage"`
}

func (response asrResponse) result(model string) (ai.TranscriptionResult, error) {
	requestID := sanitizeIdentifier(response.RequestID)
	if requestID == "" {
		return ai.TranscriptionResult{}, errors.New("Fun-ASR response has no valid request ID")
	}
	if response.Usage.Duration < 0 {
		return ai.TranscriptionResult{}, errors.New("Fun-ASR response has invalid audio usage")
	}
	text := strings.TrimSpace(response.Output.Text)
	sentenceText := strings.TrimSpace(response.Output.Sentence.Text)
	if sentenceText == "" {
		// Retain compatibility with the previously observed nested response
		// while preferring the shape in the current official API reference.
		sentenceText = strings.TrimSpace(response.Output.Output.Sentence.Text)
	}
	// output.text is the cumulative full transcript. sentence.text is only
	// the current sentence, so differing non-empty values are expected.
	if text == "" {
		text = sentenceText
	}
	if text == "" {
		return ai.TranscriptionResult{}, errors.New("Fun-ASR response has no transcript")
	}
	return ai.TranscriptionResult{
		ID:         requestID,
		Provider:   providerName,
		Model:      model,
		Transcript: text,
		Usage: ai.SpeechUsage{
			AudioSeconds: response.Usage.Duration,
		},
	}, nil
}

func readAudioSource(source platformmedia.AudioSource) ([]byte, error) {
	reader, err := source.Open()
	if err != nil {
		return nil, errors.New("open validated audio source")
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, platformmedia.MaxAudioBytes+1))
	if err != nil {
		return nil, errors.New("read validated audio source")
	}
	if int64(len(data)) != source.Size() {
		return nil, errors.New("audio source size changed after validation")
	}
	return data, nil
}

func normalizeASRModel(raw string) (string, error) {
	if !strings.EqualFold(strings.TrimSpace(raw), "fun-asr-flash-2026-06-15") {
		return "", errors.New("Fun-ASR adapter only accepts fun-asr-flash-2026-06-15")
	}
	return "fun-asr-flash-2026-06-15", nil
}

func invalidSpeechResponse(
	operation ai.SpeechOperation,
	statusCode int,
	requestID string,
	cause string,
) error {
	return ai.NewSpeechError(
		operation,
		ai.ErrorInvalidResponse,
		statusCode,
		"",
		sanitizeIdentifier(requestID),
		errors.New(cause),
	)
}

var _ ai.SpeechRecognizer = (*Recognizer)(nil)
