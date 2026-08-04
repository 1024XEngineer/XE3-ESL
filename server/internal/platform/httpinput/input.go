// Package httpinput contains strict, business-neutral HTTP request decoders.
package httpinput

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

const (
	DefaultJSONBodyLimit = int64(64 * 1024)
	DefaultReadTimeout   = 5 * time.Second
)

// DecodeObject accepts exactly one JSON object with only the declared fields.
func DecodeObject(
	c *gin.Context,
	allowed []string,
	required []string,
	maxBytes int64,
	readTimeout time.Duration,
) (map[string]json.RawMessage, bool) {
	result := make(map[string]json.RawMessage)
	if c == nil || c.Request == nil ||
		!ValidJSONContentType(c.GetHeader("Content-Type")) {
		return result, false
	}
	if maxBytes <= 0 {
		maxBytes = DefaultJSONBodyLimit
	}
	if readTimeout <= 0 {
		readTimeout = DefaultReadTimeout
	}
	controller := http.NewResponseController(c.Writer)
	if err := controller.SetReadDeadline(time.Now().Add(readTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return result, false
	}
	body := http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
	raw, err := io.ReadAll(body)
	if err != nil {
		return result, false
	}
	if err := controller.SetReadDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return result, false
	}
	if !utf8.Valid(raw) || !validJSONSurrogates(raw) {
		return result, false
	}

	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return result, false
	}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return result, false
		}
		if _, duplicate := result[key]; duplicate {
			return result, false
		}
		if _, accepted := allowedSet[key]; !accepted {
			return result, false
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return result, false
		}
		result[key] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return result, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return result, false
	}
	for _, key := range required {
		if _, exists := result[key]; !exists {
			return result, false
		}
	}
	return result, true
}

func String(raw json.RawMessage) (string, bool) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func Int64(raw json.RawMessage) (int64, bool) {
	var value int64
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, false
	}
	return value, true
}

func StringArray(
	raw json.RawMessage,
	valid func(string) bool,
) ([]string, bool) {
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil || values == nil || valid == nil {
		return nil, false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !valid(value) {
			return nil, false
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, false
		}
		seen[value] = struct{}{}
	}
	return values, true
}

func IdempotencyKey(request *http.Request) (string, bool) {
	if request == nil {
		return "", false
	}
	values := request.Header.Values("Idempotency-Key")
	if len(values) != 1 {
		return "", false
	}
	key := strings.TrimSpace(values[0])
	return key, len(key) >= 8 && len(key) <= 128 &&
		!strings.ContainsAny(key, "\r\n\x00")
}

func ValidJSONContentType(value string) bool {
	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return false
	}
	for name, value := range parameters {
		if !strings.EqualFold(name, "charset") ||
			!strings.EqualFold(value, "utf-8") {
			return false
		}
	}
	return true
}

func DecimalDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func validJSONSurrogates(raw []byte) bool {
	inString := false
	for index := 0; index < len(raw); index++ {
		switch raw[index] {
		case '"':
			inString = !inString
		case '\\':
			if !inString {
				continue
			}
			if index+1 >= len(raw) {
				return false
			}
			if raw[index+1] != 'u' {
				index++
				continue
			}
			codeUnit, ok := parseHexCodeUnit(raw, index+2)
			if !ok {
				return false
			}
			switch {
			case codeUnit >= 0xd800 && codeUnit <= 0xdbff:
				if index+12 > len(raw) || raw[index+6] != '\\' || raw[index+7] != 'u' {
					return false
				}
				low, ok := parseHexCodeUnit(raw, index+8)
				if !ok || low < 0xdc00 || low > 0xdfff {
					return false
				}
				index += 11
			case codeUnit >= 0xdc00 && codeUnit <= 0xdfff:
				return false
			default:
				index += 5
			}
		}
	}
	return true
}

func parseHexCodeUnit(raw []byte, start int) (uint16, bool) {
	if start+4 > len(raw) {
		return 0, false
	}
	var value uint16
	for _, character := range raw[start : start+4] {
		value <<= 4
		switch {
		case character >= '0' && character <= '9':
			value |= uint16(character - '0')
		case character >= 'a' && character <= 'f':
			value |= uint16(character-'a') + 10
		case character >= 'A' && character <= 'F':
			value |= uint16(character-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}
