package qiniu

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/gorilla/websocket"
)

const (
	qiniuASREndpoint     = "wss://api.qnaigc.com/v1/voice/asr"
	qiniuASRModel        = "asr"
	qiniuASRSampleRate   = 16_000
	qiniuASRChunkBytes   = 6_400
	qiniuASRMaxFrameSize = 1 << 20
	qiniuASRMaxTimeout   = 5 * time.Minute
	qiniuASRMaxPCMBytes  = int64(platformmedia.MaxAudioDuration/time.Second) *
		qiniuASRSampleRate * 2
)

type ASRConfig struct {
	BaseURL string
	Model   string
	Timeout time.Duration
}

type websocketDialer interface {
	DialContext(context.Context, string, http.Header) (
		*websocket.Conn,
		*http.Response,
		error,
	)
}

type asrClient struct {
	endpoint string
	model    string
	timeout  time.Duration
	apiKey   providerSecret
	dialer   websocketDialer
}

type asrResult struct {
	id           string
	transcript   string
	audioSeconds int
}

type asrUpdate struct {
	transcript string
	final      bool
}

type asrObserver func(context.Context, asrUpdate) error

type asrErrorKind string

const (
	asrErrorInvalidRequest  asrErrorKind = "invalid_request"
	asrErrorConfiguration   asrErrorKind = "configuration"
	asrErrorAuthentication  asrErrorKind = "authentication"
	asrErrorAuthorization   asrErrorKind = "authorization"
	asrErrorQuota           asrErrorKind = "quota_exhausted"
	asrErrorRateLimited     asrErrorKind = "rate_limited"
	asrErrorTimeout         asrErrorKind = "timeout"
	asrErrorUnavailable     asrErrorKind = "provider_unavailable"
	asrErrorInvalidResponse asrErrorKind = "invalid_response"
	asrErrorCancelled       asrErrorKind = "cancelled"
)

type asrError struct {
	kind       asrErrorKind
	statusCode int
	requestID  string
	cause      error
}

func (failure *asrError) Error() string {
	if failure == nil {
		return "Qiniu ASR failed"
	}
	return "Qiniu ASR failed: " + string(failure.kind)
}

func (failure *asrError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

func newASR(config ASRConfig, apiKey string) (*asrClient, error) {
	return newASRWithDialer(config, apiKey, websocket.DefaultDialer)
}

func newASRWithDialer(
	config ASRConfig,
	apiKey string,
	dialer websocketDialer,
) (*asrClient, error) {
	endpoint, err := normalizeASREndpoint(config.BaseURL)
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(config.Model)
	if model != qiniuASRModel {
		return nil, errors.New("Qiniu ASR model must be asr")
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" || strings.IndexFunc(apiKey, func(character rune) bool {
		return character < 0x21 || character == 0x7f
	}) >= 0 {
		return nil, errors.New("Qiniu AI API key is invalid")
	}
	if config.Timeout <= 0 || config.Timeout > qiniuASRMaxTimeout {
		return nil, fmt.Errorf(
			"Qiniu ASR timeout must be greater than zero and at most %s",
			qiniuASRMaxTimeout,
		)
	}
	if dialer == nil {
		return nil, errors.New("Qiniu ASR WebSocket dialer is required")
	}
	return &asrClient{
		endpoint: endpoint,
		model:    model,
		timeout:  config.Timeout,
		apiKey:   newProviderSecret(apiKey),
		dialer:   dialer,
	}, nil
}

func normalizeASREndpoint(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.String() != qiniuASREndpoint ||
		parsed.Scheme != "wss" || parsed.Host != "api.qnaigc.com" ||
		parsed.Path != "/v1/voice/asr" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New(
			"Qiniu ASR base URL must be wss://api.qnaigc.com/v1/voice/asr",
		)
	}
	return parsed.String(), nil
}

func (client *asrClient) String() string {
	if client == nil {
		return "QiniuASR(<nil>)"
	}
	return fmt.Sprintf(
		"QiniuASR(model=%q, timeout=%s, api_key=[REDACTED])",
		client.model,
		client.timeout,
	)
}

func (client *asrClient) GoString() string { return client.String() }

func (client *asrClient) transcribeWAV(
	ctx context.Context,
	source platformmedia.AudioSource,
	observer asrObserver,
) (asrResult, error) {
	if ctx == nil {
		return asrResult{}, invalidASRRequest("transcription context is required")
	}
	if err := platformmedia.ValidateAudioSource(source); err != nil {
		return asrResult{}, &asrError{
			kind:  asrErrorInvalidRequest,
			cause: errors.New("audio source is invalid"),
		}
	}
	if source.SampleRate() != qiniuASRSampleRate {
		return asrResult{}, invalidASRRequest(
			"Qiniu ASR requires 16000 Hz mono PCM audio",
		)
	}
	reader, err := source.Open()
	if err != nil {
		return asrResult{}, invalidASRRequest("open audio source")
	}
	defer reader.Close()
	data, err := readExactBounded(reader, source.Size(), platformmedia.MaxAudioBytes)
	if err != nil {
		return asrResult{}, invalidASRRequest("read audio source")
	}
	pcm, err := extractPCM16MonoWAV(data)
	if err != nil {
		return asrResult{}, invalidASRRequest("audio WAV is incompatible")
	}
	defer clearBytes(pcm)
	return client.transcribePCM(ctx, bytes.NewReader(pcm), qiniuASRSampleRate, observer)
}

func (client *asrClient) transcribePCM(
	ctx context.Context,
	pcm io.Reader,
	sampleRate int,
	observer asrObserver,
) (asrResult, error) {
	if ctx == nil || pcm == nil || sampleRate != qiniuASRSampleRate {
		return asrResult{}, invalidASRRequest(
			"Qiniu ASR requires a context and 16000 Hz PCM audio",
		)
	}
	callContext, cancel := context.WithTimeout(ctx, client.timeout)
	defer cancel()
	header := http.Header{}
	header.Set("Authorization", "Bearer "+client.apiKey.reveal())
	connection, response, err := client.dialer.DialContext(
		callContext,
		client.endpoint,
		header,
	)
	if err != nil {
		return asrResult{}, dialASRError(callContext, response, err)
	}
	defer connection.Close()
	if deadline, ok := callContext.Deadline(); ok {
		_ = connection.SetReadDeadline(deadline)
		_ = connection.SetWriteDeadline(deadline)
	}
	connection.SetReadLimit(qiniuASRMaxFrameSize)
	requestID, err := newASRRequestID()
	if err != nil {
		return asrResult{}, &asrError{
			kind:  asrErrorConfiguration,
			cause: errors.New("create Qiniu ASR request identifier"),
		}
	}
	configuration, err := encodeASRConfiguration(requestID, client.model)
	if err != nil {
		return asrResult{}, &asrError{
			kind:  asrErrorConfiguration,
			cause: errors.New("encode Qiniu ASR configuration"),
		}
	}
	if err := connection.WriteMessage(websocket.BinaryMessage, configuration); err != nil {
		return asrResult{}, transportASRError(callContext, err)
	}

	type readResult struct {
		requestID  string
		transcript string
		err        error
	}
	readResults := make(chan readResult, 1)
	go func() {
		responseID, transcript, readErr := collectASRResponses(
			callContext,
			connection,
			observer,
		)
		readResults <- readResult{
			requestID:  responseID,
			transcript: transcript,
			err:        readErr,
		}
	}()

	audioBytes, err := streamPCMFrames(connection, pcm)
	if err != nil {
		_ = connection.Close()
		return asrResult{}, transportASRError(callContext, err)
	}
	var completed readResult
	select {
	case completed = <-readResults:
	case <-callContext.Done():
		return asrResult{}, transportASRError(callContext, callContext.Err())
	}
	if completed.err != nil {
		return asrResult{}, completed.err
	}
	if completed.requestID == "" {
		completed.requestID = requestID
	}
	return asrResult{
		id:           completed.requestID,
		transcript:   completed.transcript,
		audioSeconds: audioDurationSeconds(audioBytes),
	}, nil
}

func streamPCMFrames(
	connection *websocket.Conn,
	reader io.Reader,
) (int64, error) {
	limited := &io.LimitedReader{R: reader, N: qiniuASRMaxPCMBytes + 1}
	current := make([]byte, qiniuASRChunkBytes)
	next := make([]byte, qiniuASRChunkBytes)
	currentCount, err := readPCMChunk(limited, current)
	if err != nil {
		return 0, err
	}
	if currentCount == 0 {
		return 0, errors.New("Qiniu ASR PCM input is empty")
	}
	sequence := int32(2)
	var total int64
	for {
		if total+int64(currentCount) > qiniuASRMaxPCMBytes {
			return total, errors.New("Qiniu ASR PCM input exceeds the accepted duration")
		}
		nextCount, readErr := readPCMChunk(limited, next)
		if readErr != nil {
			return total, readErr
		}
		final := nextCount == 0
		if final && (total+int64(currentCount))%2 != 0 {
			return total, errors.New("Qiniu ASR PCM input must contain 16-bit samples")
		}
		frameSequence := sequence
		if final {
			frameSequence = -sequence
		}
		frame, encodeErr := encodeASRAudioFrame(
			frameSequence,
			current[:currentCount],
			final,
		)
		if encodeErr != nil {
			return total, encodeErr
		}
		if writeErr := connection.WriteMessage(websocket.BinaryMessage, frame); writeErr != nil {
			return total, writeErr
		}
		total += int64(currentCount)
		if final {
			return total, nil
		}
		current, next = next, current
		currentCount = nextCount
		sequence++
	}
}

func readPCMChunk(reader io.Reader, buffer []byte) (int, error) {
	read := 0
	for read < len(buffer) {
		count, err := reader.Read(buffer[read:])
		read += count
		if err == io.EOF {
			return read, nil
		}
		if err != nil {
			return read, err
		}
		if count == 0 {
			return read, io.ErrNoProgress
		}
	}
	return read, nil
}

func readExactBounded(reader io.Reader, expected int64, maximum int64) ([]byte, error) {
	if expected <= 0 || expected > maximum {
		return nil, errors.New("audio size is outside the accepted range")
	}
	data, err := io.ReadAll(io.LimitReader(reader, expected+1))
	if err != nil || int64(len(data)) != expected {
		return nil, errors.New("audio size does not match its metadata")
	}
	return data, nil
}

func extractPCM16MonoWAV(wav []byte) ([]byte, error) {
	if len(wav) < 44 || string(wav[:4]) != "RIFF" ||
		string(wav[8:12]) != "WAVE" ||
		int(binary.LittleEndian.Uint32(wav[4:8]))+8 != len(wav) {
		return nil, errors.New("invalid WAV container")
	}
	var formatValid bool
	var pcm []byte
	for offset := 12; offset+8 <= len(wav); {
		chunkSize := int(binary.LittleEndian.Uint32(wav[offset+4 : offset+8]))
		start := offset + 8
		end := start + chunkSize
		if chunkSize < 0 || end < start || end > len(wav) {
			return nil, errors.New("invalid WAV chunk")
		}
		switch string(wav[offset : offset+4]) {
		case "fmt ":
			if chunkSize < 16 || binary.LittleEndian.Uint16(wav[start:start+2]) != 1 ||
				binary.LittleEndian.Uint16(wav[start+2:start+4]) != 1 ||
				binary.LittleEndian.Uint32(wav[start+4:start+8]) != qiniuASRSampleRate ||
				binary.LittleEndian.Uint16(wav[start+14:start+16]) != 16 {
				return nil, errors.New("WAV must be 16000 Hz mono 16-bit PCM")
			}
			formatValid = true
		case "data":
			if pcm != nil {
				return nil, errors.New("WAV contains multiple data chunks")
			}
			pcm = wav[start:end]
		}
		offset = end + chunkSize%2
	}
	if !formatValid || len(pcm) == 0 || len(pcm)%2 != 0 {
		return nil, errors.New("WAV PCM data is missing or invalid")
	}
	return pcm, nil
}

func audioDurationSeconds(bytes int64) int {
	bytesPerSecond := int64(qiniuASRSampleRate * 2)
	return int((bytes + bytesPerSecond - 1) / bytesPerSecond)
}

func newASRRequestID() (string, error) {
	identifier := make([]byte, 16)
	if _, err := rand.Read(identifier); err != nil {
		return "", err
	}
	return hex.EncodeToString(identifier), nil
}

func invalidASRRequest(message string) *asrError {
	return &asrError{
		kind:  asrErrorInvalidRequest,
		cause: errors.New(message),
	}
}

func dialASRError(
	ctx context.Context,
	response *http.Response,
	cause error,
) *asrError {
	statusCode := 0
	requestID := ""
	if response != nil {
		statusCode = response.StatusCode
		requestID = sanitizeASRIdentifier(response.Header.Get("X-Request-Id"))
	}
	return &asrError{
		kind:       classifyASRError(ctx, statusCode, cause),
		statusCode: statusCode,
		requestID:  requestID,
		cause:      safeTransportCause(ctx, cause),
	}
}

func transportASRError(ctx context.Context, cause error) *asrError {
	return &asrError{
		kind:  classifyASRError(ctx, 0, cause),
		cause: safeTransportCause(ctx, cause),
	}
}

func classifyASRError(
	ctx context.Context,
	statusCode int,
	cause error,
) asrErrorKind {
	if ctx != nil {
		switch ctx.Err() {
		case context.Canceled:
			return asrErrorCancelled
		case context.DeadlineExceeded:
			return asrErrorTimeout
		}
	}
	if errors.Is(cause, context.Canceled) {
		return asrErrorCancelled
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		return asrErrorTimeout
	}
	var networkError net.Error
	if errors.As(cause, &networkError) && networkError.Timeout() {
		return asrErrorTimeout
	}
	switch statusCode {
	case http.StatusUnauthorized:
		return asrErrorAuthentication
	case http.StatusPaymentRequired:
		return asrErrorQuota
	case http.StatusForbidden:
		return asrErrorAuthorization
	case http.StatusTooManyRequests:
		return asrErrorRateLimited
	default:
		return asrErrorUnavailable
	}
}

func safeTransportCause(ctx context.Context, cause error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(cause, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return errors.New("Qiniu ASR transport failed")
}

func sanitizeASRIdentifier(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > 128 || strings.IndexFunc(value, func(character rune) bool {
		return !(character >= 'a' && character <= 'z') &&
			!(character >= 'A' && character <= 'Z') &&
			!(character >= '0' && character <= '9') &&
			character != '-' && character != '_'
	}) >= 0 {
		return ""
	}
	return value
}

func clearBytes(data []byte) {
	for index := range data {
		data[index] = 0
	}
}
