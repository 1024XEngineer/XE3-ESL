package providerobservability

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestObserverRecordsBoundedCallsAndFixedUsage(t *testing.T) {
	registry := prometheus.NewRegistry()
	observer, err := New(registry)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	observer.Record(Observation{
		Provider: ProviderQianwen, Capability: CapabilitySpeechRecognition,
		Duration: 250 * time.Millisecond, ErrorKind: ErrorNone,
		Usage: Usage{Tokens: 7, AudioSeconds: 3},
	})
	observer.Record(Observation{
		Provider: ProviderQianwen, Capability: CapabilitySpeechRecognition,
		Duration: time.Second, ErrorKind: ErrorTimeout,
	})
	observer.Record(Observation{
		Provider: ProviderQianwen, Capability: CapabilitySpeechRecognition,
		Duration: time.Millisecond, ErrorKind: ErrorCancelled,
	})
	observer.Record(Observation{
		Provider: ProviderXFYunISE, Capability: CapabilitySpeechEvaluation,
		Duration: time.Millisecond, ErrorKind: ErrorNone,
	})
	observer.Record(Observation{
		Provider: ProviderSpatius, Capability: CapabilityAvatarSessionToken,
		Duration: time.Millisecond, ErrorKind: ErrorAuthentication,
	})

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	want := map[string]float64{
		`speakup_provider_calls_total|capability=speech_recognition,error_kind=none,outcome=success,provider=qianwen`:             1,
		`speakup_provider_calls_total|capability=speech_recognition,error_kind=timeout,outcome=timeout,provider=qianwen`:          1,
		`speakup_provider_calls_total|capability=speech_recognition,error_kind=cancelled,outcome=cancelled,provider=qianwen`:      1,
		`speakup_provider_calls_total|capability=speech_evaluation,error_kind=none,outcome=success,provider=xfyun_ise`:            1,
		`speakup_provider_calls_total|capability=avatar_session_token,error_kind=authentication,outcome=failure,provider=spatius`: 1,
		`speakup_provider_usage_audio_seconds_total|capability=speech_recognition,provider=qianwen`:                               3,
		`speakup_provider_usage_tokens_total|capability=speech_recognition,provider=qianwen`:                                      7,
	}
	for _, family := range families {
		for _, metric := range family.Metric {
			labels := make([]string, 0, len(metric.Label))
			for _, label := range metric.Label {
				labels = append(labels, label.GetName()+"="+label.GetValue())
			}
			key := family.GetName() + "|" + strings.Join(labels, ",")
			if expected, exists := want[key]; exists {
				if metric.GetCounter().GetValue() != expected {
					t.Fatalf("%s = %v, want %v", key, metric.GetCounter().GetValue(), expected)
				}
				delete(want, key)
			}
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing metrics: %#v", want)
	}
}

func TestObserverNoUsageDoesNotInventUsageSeriesOrLeakInput(t *testing.T) {
	registry := prometheus.NewRegistry()
	observer, err := New(registry)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	observer.Record(Observation{
		Provider: ProviderPaddleOCR, Capability: CapabilityDocumentOCR,
		Duration: time.Millisecond, ErrorKind: ErrorProviderUnavailable,
	})
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, family := range families {
		name := family.GetName()
		if strings.Contains(name, "usage_") {
			t.Fatalf("zero usage created %q", name)
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if strings.Contains(label.GetValue(), "private-user@example.com") {
					t.Fatalf("metric leaked private input: %s", label.GetValue())
				}
			}
		}
	}
}

func TestObserverDuplicateRegistrationFailsWithoutPartialReplacement(t *testing.T) {
	registry := prometheus.NewRegistry()
	if _, err := New(registry); err != nil {
		t.Fatalf("first New: %v", err)
	}
	if observer, err := New(registry); err == nil || observer != nil {
		t.Fatalf("duplicate New = %#v, %v", observer, err)
	}
}

func TestObserverRejectsUnboundedLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	observer, err := New(registry)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("invalid provider did not panic")
		}
	}()
	observer.Record(Observation{
		Provider:   Provider("private-user@example.com"),
		Capability: CapabilityTextGeneration,
		ErrorKind:  ErrorNone,
	})
}
