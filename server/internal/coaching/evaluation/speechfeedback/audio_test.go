package speechfeedback

import (
	"errors"
	"net/http"
	"testing"
)

func TestSpeechFeedbackAudioHTTPClientOwnsReadPolicy(t *testing.T) {
	client := NewSpeechFeedbackAudioHTTPClient()
	if client.Timeout != SpeechFeedbackAudioReadTimeout ||
		client.CheckRedirect == nil {
		t.Fatalf("NewSpeechFeedbackAudioHTTPClient() = %#v", client)
	}
	if err := client.CheckRedirect(
		&http.Request{},
		[]*http.Request{{}},
	); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect() error = %v", err)
	}
}
