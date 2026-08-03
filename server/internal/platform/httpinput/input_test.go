package httpinput

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestDecodeObjectEnforcesStrictJSONContract(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		contentType string
		body        string
		maxBytes    int64
		wantOK      bool
	}{
		{
			name:        "accepted",
			contentType: "application/json; charset=utf-8",
			body:        `{"name":"A","count":2}`,
			wantOK:      true,
		},
		{
			name:        "duplicate field",
			contentType: "application/json",
			body:        `{"name":"A","name":"B","count":2}`,
		},
		{
			name:        "unknown field",
			contentType: "application/json",
			body:        `{"name":"A","count":2,"extra":true}`,
		},
		{
			name:        "missing required field",
			contentType: "application/json",
			body:        `{"name":"A"}`,
		},
		{
			name:        "trailing value",
			contentType: "application/json",
			body:        `{"name":"A","count":2}{}`,
		},
		{
			name:        "wrong content type",
			contentType: "text/plain",
			body:        `{"name":"A","count":2}`,
		},
		{
			name:        "unpaired surrogate",
			contentType: "application/json",
			body:        `{"name":"\ud800","count":2}`,
		},
		{
			name:        "body limit",
			contentType: "application/json",
			body:        `{"name":"A","count":2}`,
			maxBytes:    8,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest(
				"POST", "/", strings.NewReader(test.body),
			)
			context.Request.Header.Set("Content-Type", test.contentType)
			values, ok := DecodeObject(
				context,
				[]string{"name", "count"},
				[]string{"name", "count"},
				test.maxBytes,
				time.Second,
			)
			if ok != test.wantOK {
				t.Fatalf("DecodeObject() ok = %t, want %t", ok, test.wantOK)
			}
			if test.wantOK && len(values) != 2 {
				t.Fatalf("DecodeObject() values = %#v", values)
			}
		})
	}
}

func TestScalarDecoders(t *testing.T) {
	t.Parallel()
	if value, ok := String(json.RawMessage(`"hello"`)); !ok || value != "hello" {
		t.Fatalf("String() = %q, %t", value, ok)
	}
	if _, ok := String(json.RawMessage(`3`)); ok {
		t.Fatal("String() accepted a number")
	}
	if value, ok := Int64(json.RawMessage(`42`)); !ok || value != 42 {
		t.Fatalf("Int64() = %d, %t", value, ok)
	}
	if _, ok := Int64(json.RawMessage(`42.5`)); ok {
		t.Fatal("Int64() accepted a fraction")
	}
	valid := func(value string) bool { return strings.HasPrefix(value, "id-") }
	values, ok := StringArray(json.RawMessage(`["id-1","id-2"]`), valid)
	if !ok || len(values) != 2 {
		t.Fatalf("StringArray() = %#v, %t", values, ok)
	}
	if _, ok := StringArray(json.RawMessage(`["id-1","id-1"]`), valid); ok {
		t.Fatal("StringArray() accepted a duplicate")
	}
	if _, ok := StringArray(json.RawMessage(`[]`), nil); ok {
		t.Fatal("StringArray() accepted a nil validator")
	}
}

func TestHeaderAndTextValidation(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest("POST", "/", nil)
	request.Header.Set("Idempotency-Key", "  request-123  ")
	if key, ok := IdempotencyKey(request); !ok || key != "request-123" {
		t.Fatalf("IdempotencyKey() = %q, %t", key, ok)
	}
	request.Header.Add("Idempotency-Key", "request-456")
	if _, ok := IdempotencyKey(request); ok {
		t.Fatal("IdempotencyKey() accepted repeated headers")
	}

	for _, value := range []string{
		"application/json",
		"Application/JSON; Charset=UTF-8",
	} {
		if !ValidJSONContentType(value) {
			t.Fatalf("ValidJSONContentType(%q) = false", value)
		}
	}
	for _, value := range []string{
		"text/json",
		"application/json; charset=iso-8859-1",
		"application/json; profile=test",
	} {
		if ValidJSONContentType(value) {
			t.Fatalf("ValidJSONContentType(%q) = true", value)
		}
	}

	if !DecimalDigits("01234") || DecimalDigits("") || DecimalDigits("12a") {
		t.Fatal("DecimalDigits() contract mismatch")
	}
}
