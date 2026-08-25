package iserelay

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"regexp"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
)

const (
	SchemaVersion = "ise-relay/v1"

	StatusProcessing = "PROCESSING"
	StatusSucceeded  = "SUCCEEDED"
	StatusFailed     = "FAILED"

	FailureInvalidRequest      = "INVALID_REQUEST"
	FailureProviderUnavailable = "PROVIDER_UNAVAILABLE"
	FailureProcessingTimeout   = "PROCESSING_TIMEOUT"
	FailureOverloaded          = "OVERLOADED"

	maxAudioBytes      = 9_600_000
	maxReferenceBytes  = 16 * 1024
	maxTopicTitleBytes = 1024
)

var requestIDPattern = regexp.MustCompile(
	`^[a-f0-9]{8}-[a-f0-9]{4}-[1-5][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`,
)

type StatusResponse struct {
	SchemaVersion string   `json:"schema_version"`
	RequestID     string   `json:"request_id"`
	Status        string   `json:"status"`
	Result        *Result  `json:"result,omitempty"`
	Failure       *Failure `json:"failure,omitempty"`
}

type Result struct {
	Provider  string  `json:"provider"`
	SessionID string  `json:"session_id"`
	Summary   Summary `json:"summary"`
}

type Summary struct {
	AccuracyScore  *float64 `json:"accuracy_score,omitempty"`
	FluencyScore   *float64 `json:"fluency_score,omitempty"`
	IntegrityScore *float64 `json:"integrity_score,omitempty"`
	PhoneScore     *float64 `json:"phone_score,omitempty"`
	SpeakingSpeed  *float64 `json:"speaking_speed,omitempty"`
	Rejected       *bool    `json:"rejected,omitempty"`
	ExceptionInfo  string   `json:"exception_info,omitempty"`
}

type Failure struct {
	Code      string `json:"code"`
	Retryable bool   `json:"retryable"`
}

func processingResponse(requestID string) StatusResponse {
	return StatusResponse{
		SchemaVersion: SchemaVersion,
		RequestID:     requestID,
		Status:        StatusProcessing,
	}
}

func resultResponse(
	requestID string,
	assessment speechfeedback.AcousticAssessmentResult,
) StatusResponse {
	return StatusResponse{
		SchemaVersion: SchemaVersion,
		RequestID:     requestID,
		Status:        StatusSucceeded,
		Result: &Result{
			Provider:  assessment.Provider,
			SessionID: assessment.SessionID,
			Summary: Summary{
				AccuracyScore:  assessment.Summary.AccuracyScore,
				FluencyScore:   assessment.Summary.FluencyScore,
				IntegrityScore: assessment.Summary.IntegrityScore,
				PhoneScore:     assessment.Summary.PhoneScore,
				SpeakingSpeed:  assessment.Summary.SpeakingSpeed,
				Rejected:       assessment.Summary.Rejected,
				ExceptionInfo:  assessment.Summary.ExceptionInfo,
			},
		},
	}
}

func failureResponse(requestID string, code string, retryable bool) StatusResponse {
	return StatusResponse{
		SchemaVersion: SchemaVersion,
		RequestID:     requestID,
		Status:        StatusFailed,
		Failure:       &Failure{Code: code, Retryable: retryable},
	}
}

func (response StatusResponse) valid() bool {
	if response.SchemaVersion != SchemaVersion ||
		!validRequestID(response.RequestID) {
		return false
	}
	switch response.Status {
	case StatusProcessing:
		return response.Result == nil && response.Failure == nil
	case StatusSucceeded:
		return response.Result != nil && response.Result.valid() &&
			response.Failure == nil
	case StatusFailed:
		return response.Result == nil && response.Failure != nil &&
			response.Failure.valid()
	default:
		return false
	}
}

func (result Result) valid() bool {
	return validIdentifier(result.Provider) &&
		validIdentifier(result.SessionID) &&
		validSummary(result.Summary)
}

func validSummary(summary Summary) bool {
	if len(summary.ExceptionInfo) > 1024 ||
		strings.ContainsRune(summary.ExceptionInfo, '\x00') {
		return false
	}
	for _, value := range []*float64{
		summary.AccuracyScore,
		summary.FluencyScore,
		summary.IntegrityScore,
		summary.PhoneScore,
	} {
		if value != nil && (!finite(*value) || *value < 0 || *value > 100) {
			return false
		}
	}
	return summary.SpeakingSpeed == nil ||
		(finite(*summary.SpeakingSpeed) && *summary.SpeakingSpeed >= 0)
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (failure Failure) valid() bool {
	switch failure.Code {
	case FailureInvalidRequest, FailureProviderUnavailable,
		FailureProcessingTimeout, FailureOverloaded:
		return true
	default:
		return false
	}
}

func validRequestID(value string) bool {
	return requestIDPattern.MatchString(value)
}

func validIdentifier(value string) bool {
	if value == "" || len(value) > 256 || value != strings.TrimSpace(value) {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character == 0x7f {
			return false
		}
	}
	return true
}

func validRequest(request speechfeedback.AcousticAssessmentRequest) bool {
	if !validRequestID(request.RequestID) || len(request.Audio) == 0 ||
		len(request.Audio) > maxAudioBytes || len(request.Audio)%2 != 0 ||
		strings.TrimSpace(request.ReferenceText) == "" ||
		request.ReferenceText != strings.TrimSpace(request.ReferenceText) ||
		len(request.ReferenceText) > maxReferenceBytes ||
		len(request.TopicTitle) > maxTopicTitleBytes {
		return false
	}
	switch request.Category {
	case speechfeedback.AcousticCategoryReadWord,
		speechfeedback.AcousticCategoryReadSentence:
		return request.TopicTitle == ""
	case speechfeedback.AcousticCategoryTopic:
		return strings.TrimSpace(request.TopicTitle) != "" &&
			request.TopicTitle == strings.TrimSpace(request.TopicTitle)
	default:
		return false
	}
}

func requestDigest(request speechfeedback.AcousticAssessmentRequest) ([sha256.Size]byte, error) {
	if !validRequest(request) {
		return [sha256.Size]byte{}, errors.New("ISE relay request is invalid")
	}
	hash := sha256.New()
	for _, value := range [][]byte{
		request.Audio,
		[]byte(request.ReferenceText),
		[]byte(request.TopicTitle),
		[]byte(request.Category),
	} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write(value)
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}
