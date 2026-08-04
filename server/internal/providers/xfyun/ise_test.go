package xfyun

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const testResultXML = `<?xml version="1.0" encoding="UTF-8"?>
<xml_result>
  <read_sentence>
    <rec_paper>
      <read_sentence total_score="87.5" accuracy_score="86.0" fluency_score="82.5" integrity_score="100" standard_score="0" is_rejected="false" except_info="0">
        <sentence total_score="87.5" accuracy_score="86.0" fluency_score="82.5">
          <word content="hello" total_score="91.2" beg_pos="0" end_pos="42">
            <syll content="həˈləʊ" syll_score="90.1" syll_accent="1"/>
            <phone content="h" dp_message="0"/>
          </word>
        </sentence>
      </read_sentence>
    </rec_paper>
  </read_sentence>
</xml_result>`

const testTopicResultXML = `<?xml version="1.0" encoding="UTF-8"?>
<xml_result>
  <topic>
    <rec_paper total_score="88.4" accuracy_score="84.2" phone_score="91.5" speeking_speed="156.0" except_info="0" content="I use artificial intelligence at work.">
      <sentence content="I use artificial intelligence at work." index="0"/>
    </rec_paper>
  </topic>
</xml_result>`

func TestEvaluatorSendsDocumentedFramesAndParsesFinalResult(t *testing.T) {
	connection := &fakeConnection{
		responses: []responseMessage{{
			Code:      0,
			Message:   "success",
			SessionID: "ise-session",
			Data: &responseData{
				Status: 2,
				Data: base64.StdEncoding.EncodeToString(
					[]byte(testResultXML),
				),
			},
		}},
	}
	evaluator := newTestEvaluator(t, connection)
	evaluator.frameBytes = 2
	evaluator.frameInterval = 0

	result, err := evaluator.Evaluate(context.Background(), EvaluationRequest{
		Audio:         []byte{1, 2, 3},
		ReferenceText: "Hello.",
		Category:      CategoryReadSentence,
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if result.SessionID != "ise-session" ||
		result.RawXML != testResultXML ||
		result.Summary.TotalScore == nil ||
		*result.Summary.TotalScore != 87.5 ||
		result.Summary.AccuracyScore == nil ||
		*result.Summary.AccuracyScore != 86 ||
		result.Summary.FluencyScore == nil ||
		*result.Summary.FluencyScore != 82.5 ||
		result.Summary.IntegrityScore == nil ||
		*result.Summary.IntegrityScore != 100 ||
		result.Summary.Rejected == nil ||
		*result.Summary.Rejected {
		t.Fatalf("unexpected result: %#v", result)
	}
	if !hasField(result.AvailableFields, "syll", "syll_score") ||
		!hasField(result.AvailableFields, "syll", "syll_accent") ||
		!hasField(result.AvailableFields, "phone", "dp_message") {
		t.Fatalf("missing detailed fields: %#v", result.AvailableFields)
	}

	if len(connection.writes) != 4 {
		t.Fatalf("writes = %d, want 4", len(connection.writes))
	}
	initial := decodeObject(t, connection.writes[0])
	business := objectValue(t, initial, "business")
	if business["category"] != "read_sentence" ||
		business["ent"] != "en_vip" ||
		business["plev"] != "0" ||
		business["extra_ability"] !=
			"multi_dimension;pitch;syll_phone_err_msg" ||
		business["text"] != "\ufeff[content]\nHello." {
		t.Fatalf("unexpected initial business: %#v", business)
	}
	firstAudio := objectValue(t, decodeObject(t, connection.writes[1]), "business")
	secondAudio := objectValue(t, decodeObject(t, connection.writes[2]), "business")
	lastAudio := objectValue(t, decodeObject(t, connection.writes[3]), "business")
	if firstAudio["aus"] != float64(1) ||
		secondAudio["aus"] != float64(2) ||
		lastAudio["aus"] != float64(4) {
		t.Fatalf(
			"unexpected audio states: first=%v middle=%v last=%v",
			firstAudio["aus"],
			secondAudio["aus"],
			lastAudio["aus"],
		)
	}
}

func TestEvaluatorReportsProviderFailureWithoutFallback(t *testing.T) {
	connection := &fakeConnection{
		responses: []responseMessage{{
			Code:      10105,
			Message:   "illegal access",
			SessionID: "ise-failed",
		}},
	}
	evaluator := newTestEvaluator(t, connection)
	evaluator.frameInterval = 0

	_, err := evaluator.Evaluate(context.Background(), EvaluationRequest{
		Audio:         []byte{1},
		ReferenceText: "Hello.",
		Category:      CategoryReadSentence,
	})
	if err == nil ||
		!strings.Contains(err.Error(), "code=10105") ||
		!strings.Contains(err.Error(), "ise-failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestSignedURLUsesHMACAndNeverFormatsCredentials(t *testing.T) {
	evaluator := newTestEvaluator(t, &fakeConnection{})
	target, err := evaluator.signedURL(
		time.Date(2026, time.July, 31, 4, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("signed URL: %v", err)
	}
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	authorization, err := base64.StdEncoding.DecodeString(
		parsed.Query().Get("authorization"),
	)
	if err != nil {
		t.Fatalf("decode authorization: %v", err)
	}
	if !strings.Contains(string(authorization), `api_key="test-api-key"`) ||
		!strings.Contains(string(authorization), `algorithm="hmac-sha256"`) ||
		parsed.Query().Get("host") != "ise-api.xfyun.cn" ||
		parsed.Query().Get("date") != "Fri, 31 Jul 2026 04:00:00 GMT" {
		t.Fatalf("unexpected signed URL: %s", target)
	}
	if evaluator.appID.String() != "[REDACTED]" ||
		evaluator.apiKey.GoString() != "xfyun.secret([REDACTED])" ||
		strings.Contains(evaluator.apiSecret.String(), "test-api-secret") {
		t.Fatal("credential formatting is not redacted")
	}
}

func TestEvaluatorRejectsInvalidInputBeforeDial(t *testing.T) {
	evaluator := newTestEvaluator(t, &fakeConnection{})
	tests := []EvaluationRequest{
		{},
		{Audio: []byte{1}},
		{ReferenceText: "Hello."},
		{
			Audio:         []byte{1},
			ReferenceText: "Hello.",
			Category:      "read_chapter",
		},
	}
	for _, request := range tests {
		if _, err := evaluator.Evaluate(context.Background(), request); err == nil {
			t.Fatalf("request %#v should fail", request)
		}
	}
}

func TestEvaluatorUsesReadWordCategory(t *testing.T) {
	connection := &fakeConnection{
		responses: []responseMessage{{
			Code:      0,
			Message:   "success",
			SessionID: "ise-word",
			Data: &responseData{
				Status: 2,
				Data: base64.StdEncoding.EncodeToString(
					[]byte(testResultXML),
				),
			},
		}},
	}
	evaluator := newTestEvaluator(t, connection)
	evaluator.frameInterval = 0

	if _, err := evaluator.Evaluate(
		context.Background(),
		EvaluationRequest{
			Audio:         []byte{1},
			ReferenceText: "Hello",
			Category:      CategoryReadWord,
		},
	); err != nil {
		t.Fatalf("evaluate word: %v", err)
	}
	initial := decodeObject(t, connection.writes[0])
	business := objectValue(t, initial, "business")
	if business["category"] != "read_word" {
		t.Fatalf("category = %v, want read_word", business["category"])
	}
}

func TestEvaluatorUsesDocumentedTopicPaperAndParsesTopicScores(t *testing.T) {
	connection := &fakeConnection{
		responses: []responseMessage{{
			Code:      0,
			Message:   "success",
			SessionID: "ise-topic",
			Data: &responseData{
				Status: 2,
				Data: base64.StdEncoding.EncodeToString(
					[]byte(testTopicResultXML),
				),
			},
		}},
	}
	evaluator := newTestEvaluator(t, connection)
	evaluator.frameInterval = 0

	result, err := evaluator.Evaluate(
		context.Background(),
		EvaluationRequest{
			Audio:         []byte{1},
			ReferenceText: "How do you use artificial intelligence at work?",
			TopicTitle:    "Artificial intelligence at work",
			Category:      CategoryTopic,
		},
	)
	if err != nil {
		t.Fatalf("evaluate topic: %v", err)
	}
	if result.Summary.PhoneScore == nil ||
		*result.Summary.PhoneScore != 91.5 ||
		result.Summary.SpeakingSpeed == nil ||
		*result.Summary.SpeakingSpeed != 156 ||
		result.Summary.AccuracyScore == nil ||
		*result.Summary.AccuracyScore != 84.2 ||
		result.Summary.Rejected != nil {
		t.Fatalf("unexpected topic summary: %#v", result.Summary)
	}
	initial := decodeObject(t, connection.writes[0])
	business := objectValue(t, initial, "business")
	if business["category"] != "topic" ||
		business["text"] != "\ufeff[topic]\n"+
			"1. Artificial intelligence at work\n"+
			"1.1. How do you use artificial intelligence at work?" {
		t.Fatalf("unexpected topic business: %#v", business)
	}
	if business["extra_ability"] != "multi_dimension" {
		t.Fatalf("unexpected topic extra_ability: %#v", business)
	}
}

func TestEvaluatorRejectsInvalidTopicPaperBeforeDial(t *testing.T) {
	evaluator := newTestEvaluator(t, &fakeConnection{})
	for _, request := range []EvaluationRequest{
		{
			Audio:         []byte{1},
			ReferenceText: "How do you use AI?",
			Category:      CategoryTopic,
		},
		{
			Audio:         []byte{1},
			ReferenceText: "How do you\nuse AI?",
			TopicTitle:    "Artificial intelligence",
			Category:      CategoryTopic,
		},
		{
			Audio:         []byte{1},
			ReferenceText: "How do you use AI?",
			TopicTitle:    "人工智能",
			Category:      CategoryTopic,
		},
	} {
		if _, err := evaluator.Evaluate(
			context.Background(),
			request,
		); err == nil {
			t.Fatalf("request %#v should fail", request)
		}
	}
}

func TestEvaluatorNormalizesTypographicPunctuationInTopicPaper(t *testing.T) {
	connection := &fakeConnection{
		responses: []responseMessage{{
			Code:      0,
			Message:   "success",
			SessionID: "ise-topic-normalized",
			Data: &responseData{
				Status: 2,
				Data: base64.StdEncoding.EncodeToString(
					[]byte(testTopicResultXML),
				),
			},
		}},
	}
	evaluator := newTestEvaluator(t, connection)
	evaluator.frameInterval = 0

	if _, err := evaluator.Evaluate(
		context.Background(),
		EvaluationRequest{
			Audio:         []byte{1},
			ReferenceText: "How would you describe the role you’re applying for?",
			TopicTitle:    "Interview question",
			Category:      CategoryTopic,
		},
	); err != nil {
		t.Fatalf("evaluate topic with typographic punctuation: %v", err)
	}
	initial := decodeObject(t, connection.writes[0])
	business := objectValue(t, initial, "business")
	if business["text"] != "\ufeff[topic]\n"+
		"1. Interview question\n"+
		"1.1. How would you describe the role you're applying for?" {
		t.Fatalf("unexpected normalized topic business: %#v", business)
	}
}

func newTestEvaluator(
	t *testing.T,
	connection websocketConnection,
) *Evaluator {
	t.Helper()
	evaluator, err := NewEvaluator(
		ISEConfig{
			Endpoint: DefaultISEEndpoint,
			Timeout:  time.Second,
		},
		"test-app-id",
		"test-api-key",
		"test-api-secret",
	)
	if err != nil {
		t.Fatalf("new evaluator: %v", err)
	}
	evaluator.dial = func(
		context.Context,
		string,
	) (websocketConnection, *http.Response, error) {
		return connection, nil, nil
	}
	evaluator.now = func() time.Time {
		return time.Date(2026, time.July, 31, 4, 0, 0, 0, time.UTC)
	}
	return evaluator
}

type fakeConnection struct {
	writes    [][]byte
	responses []responseMessage
}

func (connection *fakeConnection) WriteJSON(value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	connection.writes = append(connection.writes, payload)
	return nil
}

func (connection *fakeConnection) ReadJSON(target any) error {
	if len(connection.responses) == 0 {
		return errors.New("no response")
	}
	response := connection.responses[0]
	connection.responses = connection.responses[1:]
	typed, ok := target.(*responseMessage)
	if !ok {
		return errors.New("unexpected response target")
	}
	*typed = response
	return nil
}

func (*fakeConnection) SetWriteDeadline(time.Time) error { return nil }
func (*fakeConnection) SetReadDeadline(time.Time) error  { return nil }
func (*fakeConnection) Close() error                     { return nil }

func decodeObject(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal(payload, &object); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	return object
}

func objectValue(
	t *testing.T,
	object map[string]any,
	key string,
) map[string]any {
	t.Helper()
	value, ok := object[key].(map[string]any)
	if !ok {
		t.Fatalf("%s is not an object: %#v", key, object[key])
	}
	return value
}

func hasField(fields []ResultField, pathSuffix string, name string) bool {
	for _, field := range fields {
		if strings.HasSuffix(field.Path, "/"+pathSuffix) &&
			field.Name == name {
			return true
		}
	}
	return false
}
