package qianwen

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
)

const (
	dashScopeAPIBasePath     = "/api/v1"
	multimodalGenerationPath = "/services/aigc/multimodal-generation/generation"
	ttsSpeechSynthesizerPath = "/services/audio/tts/SpeechSynthesizer"
	ttsOutputSampleRate      = 24_000
	providerRIFFSizeMarker   = 0x7fffffbf
	providerDataSizeMarker   = 0x7fffff9b
)

type TTSConfig struct {
	BaseURL       string
	Model         string
	Voice         string
	LanguageHint  string
	Timeout       time.Duration
	TempDirectory string
}

type Synthesizer struct {
	endpoint      string
	model         string
	voice         string
	languageHint  string
	timeout       time.Duration
	tempDirectory string
	apiKey        providerSecret
	client        httpDoer
	now           func() time.Time
}

func (synthesizer *Synthesizer) String() string {
	if synthesizer == nil {
		return "QianwenSynthesizer(<nil>)"
	}
	return fmt.Sprintf(
		"QianwenSynthesizer(model=%q, voice=%q, language=%q, timeout=%s, api_key=[REDACTED])",
		synthesizer.model,
		synthesizer.voice,
		synthesizer.languageHint,
		synthesizer.timeout,
	)
}

func (synthesizer *Synthesizer) GoString() string {
	return synthesizer.String()
}

func NewSynthesizer(config TTSConfig, apiKey string) (*Synthesizer, error) {
	client := &http.Client{
		Timeout: config.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return newSynthesizerWithClient(config, apiKey, client)
}

func newSynthesizerWithClient(
	config TTSConfig,
	apiKey string,
	client httpDoer,
) (*Synthesizer, error) {
	baseURL, err := normalizeDashScopeAPIBaseURL(config.BaseURL)
	if err != nil {
		return nil, err
	}
	if !isBeijingDashScopeAPIBaseURL(baseURL) {
		return nil, errors.New(
			"Qwen-Audio-TTS is available only through a China (Beijing) endpoint",
		)
	}
	model, err := normalizeTTSModel(config.Model)
	if err != nil {
		return nil, err
	}
	voice, err := normalizeTTSVoice(config.Voice)
	if err != nil {
		return nil, err
	}
	language, err := normalizeTTSLanguage(config.LanguageHint)
	if err != nil {
		return nil, err
	}
	apiKey, err = normalizeAPIKey(apiKey)
	if err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.Timeout > maxTimeout {
		return nil, fmt.Errorf("Qianwen TTS timeout must be greater than zero and at most %s", maxTimeout)
	}
	if client == nil {
		return nil, errors.New("Qianwen TTS HTTP client is required")
	}
	return &Synthesizer{
		endpoint:      baseURL + ttsSpeechSynthesizerPath,
		model:         model,
		voice:         voice,
		languageHint:  language,
		timeout:       config.Timeout,
		tempDirectory: strings.TrimSpace(config.TempDirectory),
		apiKey:        newProviderSecret(apiKey),
		client:        client,
		now:           time.Now,
	}, nil
}

func (synthesizer *Synthesizer) Synthesize(
	ctx context.Context,
	request ai.SynthesisRequest,
) (ai.SynthesisResult, error) {
	if ctx == nil {
		return ai.SynthesisResult{}, ai.NewSpeechError(
			ai.SpeechOperationSynthesis,
			ai.ErrorInvalidRequest,
			0,
			"",
			"",
			errors.New("speech synthesis context is required"),
		)
	}
	if err := ai.ValidateSynthesisRequest(request); err != nil {
		return ai.SynthesisResult{}, ai.NewSpeechError(
			ai.SpeechOperationSynthesis,
			ai.ErrorInvalidRequest,
			0,
			"",
			"",
			err,
		)
	}
	payload := ttsRequest{
		Model: synthesizer.model,
		Input: ttsInput{
			Text:          strings.TrimSpace(request.Text),
			Voice:         synthesizer.voice,
			Format:        "wav",
			SampleRate:    ttsOutputSampleRate,
			LanguageHints: []string{synthesizer.languageHint},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ai.SynthesisResult{}, ai.NewSpeechError(
			ai.SpeechOperationSynthesis,
			ai.ErrorInvalidRequest,
			0,
			"",
			"",
			errors.New("encode Qianwen TTS request"),
		)
	}

	callContext, cancel := context.WithTimeout(ctx, synthesizer.timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(
		callContext,
		http.MethodPost,
		synthesizer.endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return ai.SynthesisResult{}, ai.NewSpeechError(
			ai.SpeechOperationSynthesis,
			ai.ErrorConfiguration,
			0,
			"",
			"",
			errors.New("create Qianwen TTS request"),
		)
	}
	httpRequest.Header.Set(
		authorizationHeaderName,
		"Bearer "+synthesizer.apiKey.reveal(),
	)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")

	response, err := synthesizer.client.Do(httpRequest)
	if err != nil {
		return ai.SynthesisResult{}, speechTransportError(
			ai.SpeechOperationSynthesis,
			callContext,
			err,
		)
	}
	if response == nil {
		return ai.SynthesisResult{}, invalidSpeechResponse(
			ai.SpeechOperationSynthesis,
			0,
			"",
			"Qianwen TTS returned a nil HTTP response",
		)
	}
	if response.Body == nil {
		return ai.SynthesisResult{}, invalidSpeechResponse(
			ai.SpeechOperationSynthesis,
			response.StatusCode,
			response.Header.Get("X-Request-Id"),
			"Qianwen TTS returned an HTTP response without a body",
		)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		defer response.Body.Close()
		return ai.SynthesisResult{}, decodeSpeechStatusError(
			ai.SpeechOperationSynthesis,
			response,
		)
	}
	responseBody, readErr := readBounded(response.Body, maxResponseBytes)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		return ai.SynthesisResult{}, invalidSpeechResponse(
			ai.SpeechOperationSynthesis,
			response.StatusCode,
			response.Header.Get("X-Request-Id"),
			"read Qianwen TTS response",
		)
	}
	var completion ttsResponse
	if err := json.Unmarshal(responseBody, &completion); err != nil {
		return ai.SynthesisResult{}, invalidSpeechResponse(
			ai.SpeechOperationSynthesis,
			response.StatusCode,
			response.Header.Get("X-Request-Id"),
			"decode Qianwen TTS response",
		)
	}
	metadata, err := completion.metadata(synthesizer.model, synthesizer.now())
	if err != nil {
		var speechError *ai.SpeechError
		if errors.As(err, &speechError) {
			return ai.SynthesisResult{}, err
		}
		return ai.SynthesisResult{}, invalidSpeechResponse(
			ai.SpeechOperationSynthesis,
			response.StatusCode,
			response.Header.Get("X-Request-Id"),
			err.Error(),
		)
	}
	audio, err := synthesizer.downloadAudio(callContext, metadata.audioURL)
	if err != nil {
		return ai.SynthesisResult{}, err
	}
	return ai.SynthesisResult{
		RequestID: metadata.requestID,
		Provider:  providerName,
		Model:     synthesizer.model,
		AudioID:   metadata.audioID,
		Audio:     audio,
		Usage:     metadata.usage,
	}, nil
}

type ttsRequest struct {
	Model string   `json:"model"`
	Input ttsInput `json:"input"`
}

type ttsInput struct {
	Text          string   `json:"text"`
	Voice         string   `json:"voice"`
	Format        string   `json:"format"`
	SampleRate    int      `json:"sample_rate"`
	LanguageHints []string `json:"language_hints"`
}

type ttsResponse struct {
	RequestID string `json:"request_id"`
	Output    struct {
		FinishReason string `json:"finish_reason"`
		Audio        struct {
			URL       string `json:"url"`
			ID        string `json:"id"`
			ExpiresAt int64  `json:"expires_at"`
		} `json:"audio"`
	} `json:"output"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
		Characters   int `json:"characters"`
	} `json:"usage"`
}

type ttsMetadata struct {
	requestID string
	audioID   string
	audioURL  string
	usage     ai.SpeechUsage
}

func (response ttsResponse) metadata(
	model string,
	now time.Time,
) (ttsMetadata, error) {
	requestID := sanitizeIdentifier(response.RequestID)
	if requestID == "" {
		return ttsMetadata{}, errors.New("Qianwen TTS response has no valid request ID")
	}
	if response.Output.FinishReason != "stop" {
		return ttsMetadata{}, errors.New("Qianwen TTS response did not finish normally")
	}
	audioID := sanitizeIdentifier(response.Output.Audio.ID)
	if audioID == "" {
		return ttsMetadata{}, errors.New("Qianwen TTS response has no valid audio ID")
	}
	if response.Output.Audio.ExpiresAt <= now.Unix() {
		return ttsMetadata{}, errors.New("Qianwen TTS response audio reference is expired")
	}
	if response.Usage.InputTokens < 0 ||
		response.Usage.OutputTokens < 0 ||
		response.Usage.TotalTokens < 0 ||
		response.Usage.Characters < 0 {
		return ttsMetadata{}, errors.New("Qianwen TTS response has invalid usage")
	}
	if _, err := normalizeTTSModel(model); err != nil {
		return ttsMetadata{}, errors.New("Qianwen TTS configured model is invalid")
	}
	return ttsMetadata{
		requestID: requestID,
		audioID:   audioID,
		audioURL:  response.Output.Audio.URL,
		usage: ai.SpeechUsage{
			InputTokens:  response.Usage.InputTokens,
			OutputTokens: response.Usage.OutputTokens,
			TotalTokens:  response.Usage.TotalTokens,
			Characters:   response.Usage.Characters,
		},
	}, nil
}

func (synthesizer *Synthesizer) downloadAudio(
	ctx context.Context,
	rawURL string,
) (*platformmedia.TemporaryAudio, error) {
	audioURL, err := validateProviderAudioURL(rawURL)
	if err != nil {
		return nil, invalidSpeechResponse(
			ai.SpeechOperationSynthesis,
			http.StatusOK,
			"",
			"Qianwen TTS returned an unsafe audio reference",
		)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, audioURL.String(), nil)
	if err != nil {
		return nil, invalidSpeechResponse(
			ai.SpeechOperationSynthesis,
			http.StatusOK,
			"",
			"create Qianwen TTS audio request",
		)
	}
	request.Header.Set("Accept", "audio/wav, application/octet-stream")
	response, err := synthesizer.client.Do(request)
	if err != nil {
		return nil, speechTransportError(ai.SpeechOperationSynthesis, ctx, err)
	}
	if response == nil || response.Body == nil {
		return nil, invalidSpeechResponse(
			ai.SpeechOperationSynthesis,
			0,
			"",
			"Qianwen TTS audio download returned no response",
		)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, invalidSpeechResponse(
			ai.SpeechOperationSynthesis,
			response.StatusCode,
			"",
			"Qianwen TTS audio download failed",
		)
	}
	contentEncoding := strings.TrimSpace(
		response.Header.Get("Content-Encoding"),
	)
	if response.Uncompressed ||
		(contentEncoding != "" &&
			!strings.EqualFold(contentEncoding, "identity")) {
		return nil, invalidSpeechResponse(
			ai.SpeechOperationSynthesis,
			response.StatusCode,
			"",
			"Qianwen TTS audio download uses unsupported content encoding",
		)
	}
	if response.ContentLength > platformmedia.MaxAudioBytes {
		return nil, invalidSpeechResponse(
			ai.SpeechOperationSynthesis,
			response.StatusCode,
			"",
			"Qianwen TTS audio download exceeds the accepted limit",
		)
	}
	contentType, _, parseErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if parseErr != nil {
		return nil, invalidSpeechResponse(
			ai.SpeechOperationSynthesis,
			response.StatusCode,
			"",
			"Qianwen TTS audio download has an invalid content type",
		)
	}
	switch strings.ToLower(contentType) {
	case platformmedia.ContentTypeWAV, "audio/x-wav":
		contentType = platformmedia.ContentTypeWAV
	case "application/octet-stream":
		if strings.ToLower(path.Ext(audioURL.Path)) != ".wav" {
			return nil, invalidSpeechResponse(
				ai.SpeechOperationSynthesis,
				response.StatusCode,
				"",
				"Qianwen TTS binary download is not a WAV resource",
			)
		}
		contentType = platformmedia.ContentTypeWAV
	default:
		return nil, invalidSpeechResponse(
			ai.SpeechOperationSynthesis,
			response.StatusCode,
			"",
			"Qianwen TTS audio download has an unsupported content type",
		)
	}
	normalizedBody, err := normalizeProviderWAVSizeMarkers(
		response.Body,
		response.ContentLength,
	)
	if err != nil {
		return nil, invalidSpeechResponse(
			ai.SpeechOperationSynthesis,
			response.StatusCode,
			"",
			"Qianwen TTS audio download has an incomplete WAV header",
		)
	}
	observedBody := &riffSizeObserver{reader: normalizedBody}
	audio, err := platformmedia.CaptureTemporaryAudio(
		synthesizer.tempDirectory,
		contentType,
		observedBody,
	)
	if err != nil {
		if ctx.Err() != nil {
			return nil, speechTransportError(ai.SpeechOperationSynthesis, ctx, ctx.Err())
		}
		validationCause := err.Error()
		if validationCause == "WAV size declaration does not match the upload" {
			if declared, actual, ok := observedBody.sizes(); ok {
				validationCause += fmt.Sprintf(
					" (declared_bytes=%d actual_bytes=%d)",
					declared,
					actual,
				)
			}
		}
		return nil, invalidSpeechResponse(
			ai.SpeechOperationSynthesis,
			response.StatusCode,
			"",
			"Qianwen TTS audio download failed validation: "+validationCause,
		)
	}
	if audio.SampleRate() != ttsOutputSampleRate {
		if err := audio.Close(); err != nil {
			return nil, invalidSpeechResponse(
				ai.SpeechOperationSynthesis,
				response.StatusCode,
				"",
				"Qianwen TTS audio cleanup failed after sample-rate validation",
			)
		}
		return nil, invalidSpeechResponse(
			ai.SpeechOperationSynthesis,
			response.StatusCode,
			"",
			"Qianwen TTS audio has an unexpected sample rate",
		)
	}
	return audio, nil
}

func normalizeProviderWAVSizeMarkers(
	body io.Reader,
	contentLength int64,
) (io.Reader, error) {
	if contentLength < 44 ||
		contentLength > platformmedia.MaxAudioBytes ||
		contentLength > int64(^uint32(0)) {
		return body, nil
	}
	header := make([]byte, 44)
	if _, err := io.ReadFull(body, header); err != nil {
		return nil, err
	}
	if string(header[0:4]) == "RIFF" &&
		string(header[8:12]) == "WAVE" &&
		string(header[12:16]) == "fmt " &&
		binary.LittleEndian.Uint32(header[16:20]) == 16 &&
		binary.LittleEndian.Uint16(header[20:22]) == 1 &&
		binary.LittleEndian.Uint16(header[22:24]) == 1 &&
		binary.LittleEndian.Uint32(header[24:28]) == ttsOutputSampleRate &&
		binary.LittleEndian.Uint32(header[28:32]) == 48_000 &&
		binary.LittleEndian.Uint16(header[32:34]) == 2 &&
		binary.LittleEndian.Uint16(header[34:36]) == 16 &&
		string(header[36:40]) == "data" &&
		binary.LittleEndian.Uint32(header[4:8]) == providerRIFFSizeMarker &&
		binary.LittleEndian.Uint32(header[40:44]) == providerDataSizeMarker {
		binary.LittleEndian.PutUint32(
			header[4:8],
			uint32(contentLength-8),
		)
		binary.LittleEndian.PutUint32(
			header[40:44],
			uint32(contentLength-44),
		)
	}
	return io.MultiReader(bytes.NewReader(header), body), nil
}

type riffSizeObserver struct {
	reader io.Reader
	header [8]byte
	count  int64
}

func (observer *riffSizeObserver) Read(target []byte) (int, error) {
	read, err := observer.reader.Read(target)
	if observer.count < int64(len(observer.header)) && read > 0 {
		headerOffset := int(observer.count)
		copy(observer.header[headerOffset:], target[:read])
	}
	observer.count += int64(read)
	return read, err
}

func (observer *riffSizeObserver) sizes() (int64, int64, bool) {
	if observer.count < int64(len(observer.header)) ||
		string(observer.header[:4]) != "RIFF" {
		return 0, observer.count, false
	}
	declared := int64(binary.LittleEndian.Uint32(observer.header[4:8])) + 8
	return declared, observer.count, true
}

func normalizeDashScopeAPIBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", errors.New("DashScope API base URL is invalid")
	}
	if parsed.Scheme != "https" ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		parsed.Port() != "" {
		return "", errors.New("DashScope API base URL must be a credential-free HTTPS URL")
	}
	if !isOfficialHost(strings.ToLower(parsed.Hostname())) {
		return "", errors.New("DashScope API base URL must use an official Alibaba Cloud endpoint")
	}
	if strings.TrimRight(parsed.EscapedPath(), "/") != dashScopeAPIBasePath {
		return "", fmt.Errorf("DashScope API base URL path must be %s", dashScopeAPIBasePath)
	}
	parsed.Path = dashScopeAPIBasePath
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func normalizeTTSModel(raw string) (string, error) {
	model := strings.TrimSpace(raw)
	if strings.ToLower(model) != "qwen-audio-3.0-tts-flash" {
		return "", errors.New("Qianwen TTS adapter only accepts qwen-audio-3.0-tts-flash")
	}
	return "qwen-audio-3.0-tts-flash", nil
}

func isBeijingDashScopeAPIBaseURL(baseURL string) bool {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "dashscope.aliyuncs.com" {
		return true
	}
	const suffix = ".cn-beijing.maas.aliyuncs.com"
	workspaceID := strings.TrimSuffix(host, suffix)
	return workspaceID != host &&
		validDNSLabel(workspaceID)
}

func validDNSLabel(value string) bool {
	if value == "" ||
		len(value) > 63 ||
		value[0] == '-' ||
		value[len(value)-1] == '-' {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') &&
			character != '-' {
			return false
		}
	}
	return true
}

func normalizeTTSVoice(raw string) (string, error) {
	voice := strings.TrimSpace(raw)
	if voice == "" || len(voice) > maxProviderIdentifier {
		return "", errors.New("Qianwen TTS voice is required and must not exceed 128 characters")
	}
	for _, character := range voice {
		if !((character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			strings.ContainsRune("._-", character)) {
			return "", errors.New("Qianwen TTS voice contains unsupported characters")
		}
	}
	return voice, nil
}

func normalizeTTSLanguage(raw string) (string, error) {
	if !strings.EqualFold(strings.TrimSpace(raw), "en") {
		return "", errors.New("Qianwen TTS adapter currently accepts only English")
	}
	return "en", nil
}

func validateProviderAudioURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, errors.New("provider audio URL is invalid")
	}
	host := strings.ToLower(parsed.Hostname())
	if (parsed.Scheme != "https" && parsed.Scheme != "http") ||
		parsed.User != nil ||
		parsed.Port() != "" ||
		parsed.Fragment != "" ||
		!strings.HasPrefix(host, "dashscope-result-") ||
		!strings.Contains(host, ".oss-") ||
		!strings.HasSuffix(host, ".aliyuncs.com") ||
		!strings.EqualFold(path.Ext(parsed.Path), ".wav") {
		return nil, errors.New("provider audio URL is not an approved HTTPS OSS reference")
	}
	// The provider's documented non-streaming response currently contains an
	// HTTP OSS URL. Treat it only as a signed reference and always upgrade the
	// actual download to HTTPS; an HTTP request is never issued.
	parsed.Scheme = "https"
	return parsed, nil
}

var _ ai.SpeechSynthesizer = (*Synthesizer)(nil)
