// Package providerobservability records bounded, content-free metrics for
// external provider calls.
package providerobservability

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Provider is a configured external service. Values are deliberately closed
// so user, request, model, bucket, and object identifiers cannot become labels.
type Provider string

const (
	ProviderQianwen   Provider = "qianwen"
	ProviderQiniu     Provider = "qiniu"
	ProviderPaddleOCR Provider = "paddleocr"
	ProviderAliyunOSS Provider = "aliyun_oss"
	ProviderQiniuKodo Provider = "qiniu_kodo"
)

// Capability is one bounded external operation family.
type Capability string

const (
	CapabilityTextGeneration    Capability = "text_generation"
	CapabilitySpeechRecognition Capability = "speech_recognition"
	CapabilitySpeechSynthesis   Capability = "speech_synthesis"
	CapabilityDocumentOCR       Capability = "document_ocr"
	CapabilityObjectPut         Capability = "object_put"
	CapabilityObjectSignedGet   Capability = "object_signed_get"
	CapabilityObjectOpen        Capability = "object_open"
	CapabilityObjectDelete      Capability = "object_delete"
)

// ErrorKind is a stable, sanitized failure class.
type ErrorKind string

const (
	ErrorNone                ErrorKind = "none"
	ErrorInvalidRequest      ErrorKind = "invalid_request"
	ErrorConfiguration       ErrorKind = "configuration"
	ErrorAuthentication      ErrorKind = "authentication"
	ErrorAuthorization       ErrorKind = "authorization"
	ErrorQuotaExhausted      ErrorKind = "quota_exhausted"
	ErrorRateLimited         ErrorKind = "rate_limited"
	ErrorTimeout             ErrorKind = "timeout"
	ErrorProviderUnavailable ErrorKind = "provider_unavailable"
	ErrorInvalidResponse     ErrorKind = "invalid_response"
	ErrorCancelled           ErrorKind = "cancelled"
	ErrorPageLimitExceeded   ErrorKind = "page_limit_exceeded"
	ErrorCredentials         ErrorKind = "credentials"
	ErrorInvalidObject       ErrorKind = "invalid_object"
	ErrorAlreadyExists       ErrorKind = "already_exists"
	ErrorOperationFailed     ErrorKind = "operation_failed"
)

const (
	outcomeSuccess   = "success"
	outcomeFailure   = "failure"
	outcomeTimeout   = "timeout"
	outcomeCancelled = "cancelled"
)

// Usage contains only fixed-unit billable quantities already available at the
// provider boundary. Zero means the provider did not report that quantity.
type Usage struct {
	Tokens       float64
	AudioSeconds float64
	Characters   float64
	Pages        float64
	Bytes        float64
}

// Observation is one completed logical provider call.
type Observation struct {
	Provider   Provider
	Capability Capability
	Duration   time.Duration
	ErrorKind  ErrorKind
	Usage      Usage
}

// Recorder is the narrow dependency used by provider adapters.
type Recorder interface {
	Record(Observation)
	RecordRetry(Provider, Capability)
}

// Observer owns all external-provider metrics for one service registry.
type Observer struct {
	calls        *prometheus.CounterVec
	duration     *prometheus.HistogramVec
	retries      *prometheus.CounterVec
	tokens       *prometheus.CounterVec
	audioSeconds *prometheus.CounterVec
	characters   *prometheus.CounterVec
	pages        *prometheus.CounterVec
	bytes        *prometheus.CounterVec
}

// New registers the complete provider metric family. Registration is
// transactional so startup cannot continue with a partially visible family.
func New(registerer prometheus.Registerer) (*Observer, error) {
	if registerer == nil {
		return nil, errors.New("provider observability registerer is required")
	}
	observer := &Observer{
		calls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "speakup", Subsystem: "provider", Name: "calls_total",
			Help: "Completed external provider calls by bounded outcome and error kind.",
		}, []string{"provider", "capability", "outcome", "error_kind"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "speakup", Subsystem: "provider", Name: "call_duration_seconds",
			Help:    "External provider call duration in seconds by bounded outcome.",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
		}, []string{"provider", "capability", "outcome"}),
		retries: providerCounter("retries_total", "External provider retries."),
		tokens:  providerCounter("usage_tokens_total", "Provider-reported tokens consumed."),
		audioSeconds: providerCounter(
			"usage_audio_seconds_total", "Provider-reported audio seconds consumed.",
		),
		characters: providerCounter(
			"usage_characters_total", "Provider-reported or submitted TTS characters consumed.",
		),
		pages: providerCounter("usage_pages_total", "Provider-reported document pages consumed."),
		bytes: providerCounter("usage_bytes_total", "Provider-reported object bytes stored."),
	}
	collectors := []prometheus.Collector{
		observer.calls, observer.duration, observer.retries, observer.tokens,
		observer.audioSeconds, observer.characters, observer.pages, observer.bytes,
	}
	registered := make([]prometheus.Collector, 0, len(collectors))
	for _, collector := range collectors {
		if err := registerer.Register(collector); err != nil {
			for _, current := range registered {
				registerer.Unregister(current)
			}
			return nil, fmt.Errorf("register provider metrics: %w", err)
		}
		registered = append(registered, collector)
	}
	return observer, nil
}

func providerCounter(name string, help string) *prometheus.CounterVec {
	return prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "speakup", Subsystem: "provider", Name: name, Help: help,
	}, []string{"provider", "capability"})
}

// Record records one completed provider call. Invalid enum or numeric values
// are programmer errors and panic instead of silently dropping production
// observability.
func (observer *Observer) Record(observation Observation) {
	if observer == nil {
		panic("providerobservability: observer is required")
	}
	validateObservation(observation)
	outcome := outcomeFor(observation.ErrorKind)
	provider := string(observation.Provider)
	capability := string(observation.Capability)
	observer.calls.WithLabelValues(
		provider, capability, outcome, string(observation.ErrorKind),
	).Inc()
	observer.duration.WithLabelValues(provider, capability, outcome).
		Observe(observation.Duration.Seconds())
	addPositive(observer.tokens, provider, capability, observation.Usage.Tokens)
	addPositive(observer.audioSeconds, provider, capability, observation.Usage.AudioSeconds)
	addPositive(observer.characters, provider, capability, observation.Usage.Characters)
	addPositive(observer.pages, provider, capability, observation.Usage.Pages)
	addPositive(observer.bytes, provider, capability, observation.Usage.Bytes)
}

// RecordRetry records a real additional provider attempt, not merely a
// retryable error classification.
func (observer *Observer) RecordRetry(provider Provider, capability Capability) {
	if observer == nil {
		panic("providerobservability: observer is required")
	}
	validateProvider(provider)
	validateCapability(capability)
	observer.retries.WithLabelValues(string(provider), string(capability)).Inc()
}

func addPositive(counter *prometheus.CounterVec, provider string, capability string, value float64) {
	if value > 0 {
		counter.WithLabelValues(provider, capability).Add(value)
	}
}

func validateObservation(observation Observation) {
	validateProvider(observation.Provider)
	validateCapability(observation.Capability)
	validateErrorKind(observation.ErrorKind)
	if observation.Duration < 0 {
		panic("providerobservability: duration must not be negative")
	}
	for _, value := range []float64{
		observation.Usage.Tokens,
		observation.Usage.AudioSeconds,
		observation.Usage.Characters,
		observation.Usage.Pages,
		observation.Usage.Bytes,
	} {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			panic("providerobservability: usage must be finite and non-negative")
		}
	}
}

func validateProvider(provider Provider) {
	switch provider {
	case ProviderQianwen, ProviderQiniu, ProviderPaddleOCR,
		ProviderAliyunOSS, ProviderQiniuKodo:
		return
	default:
		panic("providerobservability: invalid provider")
	}
}

func validateCapability(capability Capability) {
	switch capability {
	case CapabilityTextGeneration, CapabilitySpeechRecognition,
		CapabilitySpeechSynthesis, CapabilityDocumentOCR, CapabilityObjectPut,
		CapabilityObjectSignedGet, CapabilityObjectOpen, CapabilityObjectDelete:
		return
	default:
		panic("providerobservability: invalid capability")
	}
}

func validateErrorKind(kind ErrorKind) {
	switch kind {
	case ErrorNone, ErrorInvalidRequest, ErrorConfiguration, ErrorAuthentication,
		ErrorAuthorization, ErrorQuotaExhausted, ErrorRateLimited, ErrorTimeout,
		ErrorProviderUnavailable, ErrorInvalidResponse, ErrorCancelled,
		ErrorPageLimitExceeded, ErrorCredentials, ErrorInvalidObject,
		ErrorAlreadyExists, ErrorOperationFailed:
		return
	default:
		panic("providerobservability: invalid error kind")
	}
}

func outcomeFor(kind ErrorKind) string {
	switch kind {
	case ErrorNone:
		return outcomeSuccess
	case ErrorTimeout:
		return outcomeTimeout
	case ErrorCancelled:
		return outcomeCancelled
	default:
		return outcomeFailure
	}
}

var _ Recorder = (*Observer)(nil)
