package xfyun

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
)

const (
	DefaultISEEndpoint = "wss://ise-api.xfyun.cn/v2/open-ise"

	defaultFrameBytes    = 1280
	defaultFrameInterval = 40 * time.Millisecond
	maxAudioBytes        = 9_600_000
	maxTimeout           = 5 * time.Minute
)

type ISEConfig struct {
	Endpoint string
	Timeout  time.Duration
	Observer providerobservability.Recorder
}

type EvaluationRequest struct {
	Audio         []byte
	ReferenceText string
	TopicTitle    string
	Category      EvaluationCategory
}

type EvaluationCategory string

const (
	CategoryReadWord     EvaluationCategory = "read_word"
	CategoryReadSentence EvaluationCategory = "read_sentence"
	CategoryTopic        EvaluationCategory = "topic"
)

type EvaluationResult struct {
	SessionID       string
	RawXML          string
	AvailableFields []ResultField
	Summary         ScoreSummary
}

type ResultField struct {
	Path  string
	Name  string
	Value string
}

type ScoreSummary struct {
	TotalScore     *float64
	AccuracyScore  *float64
	FluencyScore   *float64
	IntegrityScore *float64
	StandardScore  *float64
	PhoneScore     *float64
	SpeakingSpeed  *float64
	Rejected       *bool
	ExceptionInfo  string
}

type Evaluator struct {
	endpoint      string
	timeout       time.Duration
	appID         secret
	apiKey        secret
	apiSecret     secret
	frameBytes    int
	frameInterval time.Duration
	dial          dialFunc
	now           func() time.Time
	observer      providerobservability.Recorder
}

type secret struct {
	revealValue func() string
}

func newSecret(value string) secret {
	return secret{revealValue: func() string { return value }}
}

func (value secret) reveal() string {
	if value.revealValue == nil {
		return ""
	}
	return value.revealValue()
}

func (secret) String() string   { return "[REDACTED]" }
func (secret) GoString() string { return "xfyun.secret([REDACTED])" }

type websocketConnection interface {
	WriteJSON(any) error
	ReadJSON(any) error
	SetWriteDeadline(time.Time) error
	SetReadDeadline(time.Time) error
	Close() error
}

type dialFunc func(
	context.Context,
	string,
) (websocketConnection, *http.Response, error)

func NewEvaluator(
	config ISEConfig,
	appID string,
	apiKey string,
	apiSecret string,
) (*Evaluator, error) {
	endpoint, err := normalizeEndpoint(config.Endpoint)
	if err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.Timeout > maxTimeout {
		return nil, fmt.Errorf(
			"iFlytek ISE timeout must be greater than zero and at most %s",
			maxTimeout,
		)
	}
	appID, err = normalizeCredential("iFlytek ISE APPID", appID)
	if err != nil {
		return nil, err
	}
	apiKey, err = normalizeCredential("iFlytek ISE APIKey", apiKey)
	if err != nil {
		return nil, err
	}
	apiSecret, err = normalizeCredential("iFlytek ISE APISecret", apiSecret)
	if err != nil {
		return nil, err
	}
	dialer := websocket.Dialer{}
	return &Evaluator{
		endpoint:      endpoint,
		timeout:       config.Timeout,
		appID:         newSecret(appID),
		apiKey:        newSecret(apiKey),
		apiSecret:     newSecret(apiSecret),
		frameBytes:    defaultFrameBytes,
		frameInterval: defaultFrameInterval,
		dial: func(
			ctx context.Context,
			target string,
		) (websocketConnection, *http.Response, error) {
			return dialer.DialContext(ctx, target, nil)
		},
		now:      time.Now,
		observer: config.Observer,
	}, nil
}

func (evaluator *Evaluator) Evaluate(
	ctx context.Context,
	request EvaluationRequest,
) (EvaluationResult, error) {
	if ctx == nil {
		return EvaluationResult{}, errors.New("iFlytek ISE context is required")
	}
	if len(request.Audio) == 0 {
		return EvaluationResult{}, errors.New("iFlytek ISE audio is required")
	}
	if len(request.Audio) > maxAudioBytes {
		return EvaluationResult{}, errors.New("iFlytek ISE audio exceeds five minutes")
	}
	evaluationText, err := buildEvaluationText(request)
	if err != nil {
		return EvaluationResult{}, err
	}
	if request.Category != CategoryReadWord &&
		request.Category != CategoryReadSentence &&
		request.Category != CategoryTopic {
		return EvaluationResult{}, errors.New(
			"iFlytek ISE category must be read_word, read_sentence, or topic",
		)
	}

	callContext, cancel := context.WithTimeout(ctx, evaluator.timeout)
	defer cancel()
	target, err := evaluator.signedURL(evaluator.now().UTC())
	if err != nil {
		return EvaluationResult{}, err
	}
	startedAt := time.Now()
	metricKind := providerobservability.ErrorProviderUnavailable
	defer func() {
		recordISECall(evaluator.observer, startedAt, metricKind)
	}()
	connection, response, err := evaluator.dial(callContext, target)
	if err != nil {
		metricKind = iseHandshakeErrorKind(callContext, response, err)
		if response != nil {
			return EvaluationResult{}, fmt.Errorf(
				"iFlytek ISE handshake failed with HTTP %d: %w",
				response.StatusCode,
				err,
			)
		}
		return EvaluationResult{}, fmt.Errorf("iFlytek ISE handshake failed: %w", err)
	}
	defer connection.Close()
	if deadline, ok := callContext.Deadline(); ok {
		if err := connection.SetWriteDeadline(deadline); err != nil {
			metricKind = iseTransportErrorKind(callContext, err)
			return EvaluationResult{}, fmt.Errorf(
				"set iFlytek ISE write deadline: %w",
				err,
			)
		}
		if err := connection.SetReadDeadline(deadline); err != nil {
			metricKind = iseTransportErrorKind(callContext, err)
			return EvaluationResult{}, fmt.Errorf(
				"set iFlytek ISE read deadline: %w",
				err,
			)
		}
	}
	extraAbility := "multi_dimension"
	if request.Category == CategoryReadWord ||
		request.Category == CategoryReadSentence {
		extraAbility += ";pitch;syll_phone_err_msg"
	}
	if err := connection.WriteJSON(initialRequest{
		Common: initialCommon{AppID: evaluator.appID.reveal()},
		Business: initialBusiness{
			AudioEncoding:  "raw",
			AudioFormat:    "audio/L16;rate=16000",
			Category:       string(request.Category),
			Command:        "ssb",
			Language:       "en_vip",
			Service:        "ise",
			Text:           evaluationText,
			TextEncoding:   "utf-8",
			SkipTextUpload: true,
			ResultEncoding: "utf8",
			ResultLevel:    "entirety",
			UnifiedResult:  "1",
			DetailLevel:    "0",
			ExtraAbility:   extraAbility,
		},
		Data: requestData{Status: 0, Data: ""},
	}); err != nil {
		metricKind = iseTransportErrorKind(callContext, err)
		return EvaluationResult{}, fmt.Errorf(
			"send iFlytek ISE parameters: %w",
			err,
		)
	}
	if err := evaluator.sendAudio(callContext, connection, request.Audio); err != nil {
		metricKind = iseTransportErrorKind(callContext, err)
		return EvaluationResult{}, err
	}
	for {
		var message responseMessage
		if err := connection.ReadJSON(&message); err != nil {
			metricKind = iseTransportErrorKind(callContext, err)
			return EvaluationResult{}, fmt.Errorf(
				"read iFlytek ISE response: %w",
				err,
			)
		}
		if message.Code != 0 {
			metricKind = iseProviderCodeKind(message.Code)
			return EvaluationResult{}, fmt.Errorf(
				"iFlytek ISE rejected request: code=%d message=%s sid=%s",
				message.Code,
				strings.TrimSpace(message.Message),
				strings.TrimSpace(message.SessionID),
			)
		}
		if message.Data == nil || message.Data.Status != 2 {
			continue
		}
		rawXML, err := base64.StdEncoding.DecodeString(message.Data.Data)
		if err != nil {
			metricKind = providerobservability.ErrorInvalidResponse
			return EvaluationResult{}, errors.New(
				"decode final iFlytek ISE result",
			)
		}
		result, err := parseResult(rawXML)
		if err != nil {
			metricKind = providerobservability.ErrorInvalidResponse
			return EvaluationResult{}, err
		}
		result.SessionID = strings.TrimSpace(message.SessionID)
		metricKind = providerobservability.ErrorNone
		return result, nil
	}
}

func buildEvaluationText(request EvaluationRequest) (string, error) {
	reference := strings.TrimSpace(request.ReferenceText)
	if reference == "" {
		return "", errors.New("iFlytek ISE reference text is required")
	}
	if len([]byte(reference)) > 10_000 {
		return "", errors.New("iFlytek ISE reference text is too large")
	}
	switch request.Category {
	case CategoryReadWord, CategoryReadSentence:
		if strings.TrimSpace(request.TopicTitle) != "" {
			return "", errors.New(
				"iFlytek ISE topic title is only valid for topic evaluation",
			)
		}
		return "\ufeff[content]\n" + reference, nil
	case CategoryTopic:
		title := normalizeTopicPaperLine(request.TopicTitle)
		reference = normalizeTopicPaperLine(reference)
		if !validTopicPaperLine(title) ||
			!validTopicPaperLine(reference) {
			return "", errors.New("iFlytek ISE topic paper is invalid")
		}
		return "\ufeff[topic]\n1. " + title + "\n1.1. " + reference, nil
	default:
		return "", errors.New(
			"iFlytek ISE category must be read_word, read_sentence, or topic",
		)
	}
}

// ISE topic papers accept a narrow ASCII line format. User-facing questions
// may contain typographic punctuation, so normalize only the common variants
// before validation; unknown non-ASCII content remains fail-closed.
func normalizeTopicPaperLine(value string) string {
	return strings.NewReplacer(
		"\u00a0", " ",
		"\u2018", "'",
		"\u2019", "'",
		"\u201a", "'",
		"\u201b", "'",
		"\u201c", "\"",
		"\u201d", "\"",
		"\u201e", "\"",
		"\u201f", "\"",
		"\u2013", "-",
		"\u2014", "-",
		"\u2026", "...",
	).Replace(strings.TrimSpace(value))
}

func validTopicPaperLine(value string) bool {
	if value == "" || strings.ContainsAny(value, "\r\n\t[]（）") {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character > 0x7e {
			return false
		}
	}
	return true
}

func (evaluator *Evaluator) sendAudio(
	ctx context.Context,
	connection websocketConnection,
	audio []byte,
) error {
	first := true
	for offset := 0; offset < len(audio); offset += evaluator.frameBytes {
		end := min(offset+evaluator.frameBytes, len(audio))
		aus := 2
		if first {
			aus = 1
			first = false
		}
		if err := connection.WriteJSON(audioRequest{
			Business: audioBusiness{
				Command:       "auw",
				AudioStatus:   aus,
				AudioEncoding: "raw",
			},
			Data: requestData{
				Status:   1,
				Data:     base64.StdEncoding.EncodeToString(audio[offset:end]),
				DataType: 1,
				Encoding: "raw",
			},
		}); err != nil {
			return fmt.Errorf("send iFlytek ISE audio: %w", err)
		}
		if evaluator.frameInterval > 0 {
			timer := time.NewTimer(evaluator.frameInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return fmt.Errorf("send iFlytek ISE audio: %w", ctx.Err())
			case <-timer.C:
			}
		}
	}
	if err := connection.WriteJSON(audioRequest{
		Business: audioBusiness{
			Command:       "auw",
			AudioStatus:   4,
			AudioEncoding: "raw",
		},
		Data: requestData{
			Status:   2,
			Data:     "",
			DataType: 1,
			Encoding: "raw",
		},
	}); err != nil {
		return fmt.Errorf("finish iFlytek ISE audio: %w", err)
	}
	return nil
}

func (evaluator *Evaluator) signedURL(at time.Time) (string, error) {
	endpoint, err := url.Parse(evaluator.endpoint)
	if err != nil {
		return "", errors.New("parse iFlytek ISE endpoint")
	}
	date := at.UTC().Format(http.TimeFormat)
	signatureOrigin := "host: " + endpoint.Host + "\n" +
		"date: " + date + "\n" +
		"GET " + endpoint.EscapedPath() + " HTTP/1.1"
	mac := hmac.New(sha256.New, []byte(evaluator.apiSecret.reveal()))
	_, _ = mac.Write([]byte(signatureOrigin))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	authorizationOrigin := fmt.Sprintf(
		`api_key="%s", algorithm="hmac-sha256", headers="host date request-line", signature="%s"`,
		evaluator.apiKey.reveal(),
		signature,
	)
	query := endpoint.Query()
	query.Set(
		"authorization",
		base64.StdEncoding.EncodeToString([]byte(authorizationOrigin)),
	)
	query.Set("date", date)
	query.Set("host", endpoint.Host)
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func normalizeEndpoint(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "wss" || parsed.Host == "" ||
		parsed.Path == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("invalid iFlytek ISE endpoint")
	}
	return parsed.String(), nil
}

func normalizeCredential(name string, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return r < 0x21 || r == 0x7f
	}) >= 0 {
		return "", fmt.Errorf("%s contains whitespace or control characters", name)
	}
	return value, nil
}

func parseResult(payload []byte) (EvaluationResult, error) {
	decoder := xml.NewDecoder(strings.NewReader(string(payload)))
	result := EvaluationResult{
		RawXML:          string(payload),
		AvailableFields: make([]ResultField, 0),
	}
	path := make([]string, 0, 8)
	bestScoreCount := -1
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return EvaluationResult{}, fmt.Errorf(
				"parse iFlytek ISE XML: %w",
				err,
			)
		}
		switch value := token.(type) {
		case xml.StartElement:
			path = append(path, value.Name.Local)
			currentPath := strings.Join(path, "/")
			summary, scoreCount, err := parseAttributes(
				currentPath,
				value.Attr,
				&result.AvailableFields,
			)
			if err != nil {
				return EvaluationResult{}, err
			}
			if scoreCount > bestScoreCount {
				result.Summary = summary
				bestScoreCount = scoreCount
			}
		case xml.EndElement:
			if len(path) > 0 {
				path = path[:len(path)-1]
			}
		}
	}
	if len(result.AvailableFields) == 0 {
		return EvaluationResult{}, errors.New(
			"iFlytek ISE XML contains no result fields",
		)
	}
	return result, nil
}

func parseAttributes(
	path string,
	attributes []xml.Attr,
	fields *[]ResultField,
) (ScoreSummary, int, error) {
	summary := ScoreSummary{}
	scoreCount := 0
	for _, attribute := range attributes {
		name := attribute.Name.Local
		value := attribute.Value
		*fields = append(*fields, ResultField{
			Path:  path,
			Name:  name,
			Value: value,
		})
		var target **float64
		switch name {
		case "total_score":
			target = &summary.TotalScore
		case "accuracy_score":
			target = &summary.AccuracyScore
		case "fluency_score":
			target = &summary.FluencyScore
		case "integrity_score":
			target = &summary.IntegrityScore
		case "standard_score":
			target = &summary.StandardScore
		case "phone_score":
			target = &summary.PhoneScore
		case "speeking_speed":
			target = &summary.SpeakingSpeed
		case "is_rejected":
			rejected, err := strconv.ParseBool(value)
			if err != nil {
				return ScoreSummary{}, 0, fmt.Errorf(
					"parse iFlytek ISE is_rejected: %w",
					err,
				)
			}
			summary.Rejected = &rejected
		case "except_info":
			summary.ExceptionInfo = value
		}
		if target != nil {
			score, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return ScoreSummary{}, 0, fmt.Errorf(
					"parse iFlytek ISE %s: %w",
					name,
					err,
				)
			}
			*target = &score
			scoreCount++
		}
	}
	return summary, scoreCount, nil
}

type initialRequest struct {
	Common   initialCommon   `json:"common"`
	Business initialBusiness `json:"business"`
	Data     requestData     `json:"data"`
}

type initialCommon struct {
	AppID string `json:"app_id"`
}

type initialBusiness struct {
	AudioEncoding  string `json:"aue"`
	AudioFormat    string `json:"auf"`
	Category       string `json:"category"`
	Command        string `json:"cmd"`
	Language       string `json:"ent"`
	Service        string `json:"sub"`
	Text           string `json:"text"`
	TextEncoding   string `json:"tte"`
	SkipTextUpload bool   `json:"ttp_skip"`
	ResultEncoding string `json:"rstcd"`
	ResultLevel    string `json:"rst"`
	UnifiedResult  string `json:"ise_unite"`
	DetailLevel    string `json:"plev"`
	ExtraAbility   string `json:"extra_ability,omitempty"`
}

type audioRequest struct {
	Business audioBusiness `json:"business"`
	Data     requestData   `json:"data"`
}

type audioBusiness struct {
	Command       string `json:"cmd"`
	AudioStatus   int    `json:"aus"`
	AudioEncoding string `json:"aue"`
}

type requestData struct {
	Status   int    `json:"status"`
	Data     string `json:"data"`
	DataType int    `json:"data_type,omitempty"`
	Encoding string `json:"encoding,omitempty"`
}

type responseMessage struct {
	Code      int           `json:"code"`
	Message   string        `json:"message"`
	SessionID string        `json:"sid"`
	Data      *responseData `json:"data"`
}

type responseData struct {
	Status int    `json:"status"`
	Data   string `json:"data"`
}
