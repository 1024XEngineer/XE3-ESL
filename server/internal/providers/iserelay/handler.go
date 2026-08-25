package iserelay

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
)

const maxMultipartOverhead = 64 * 1024

type HandlerConfig struct {
	ProviderTimeout time.Duration
	Retention       time.Duration
	MaxJobs         int
	MaxInFlight     int
	Logger          *slog.Logger
}

type acousticEvaluator interface {
	Evaluate(
		context.Context,
		speechfeedback.AcousticAssessmentRequest,
	) (speechfeedback.AcousticAssessmentResult, error)
}

type Handler struct {
	evaluator  acousticEvaluator
	config     HandlerConfig
	submission chan struct{}
	sem        chan struct{}
	mu         sync.Mutex
	jobs       map[string]*relayJob
}

type relayJob struct {
	digest      [sha256.Size]byte
	response    StatusResponse
	completedAt time.Time
}

func NewHandler(
	evaluator acousticEvaluator,
	configuration HandlerConfig,
) (*Handler, error) {
	if evaluator == nil || configuration.ProviderTimeout <= 0 ||
		configuration.ProviderTimeout > 5*time.Minute ||
		configuration.Retention < time.Minute ||
		configuration.Retention > time.Hour ||
		configuration.MaxJobs < 1 || configuration.MaxJobs > 64 ||
		configuration.MaxInFlight < 1 ||
		configuration.MaxInFlight > configuration.MaxJobs ||
		configuration.Logger == nil {
		return nil, errors.New("ISE relay handler configuration is invalid")
	}
	return &Handler{
		evaluator:  evaluator,
		config:     configuration,
		submission: make(chan struct{}, configuration.MaxJobs),
		sem:        make(chan struct{}, configuration.MaxInFlight),
		jobs:       make(map[string]*relayJob),
	}, nil
}

func (handler *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handler.health)
	mux.HandleFunc("POST /v1/evaluations", handler.create)
	mux.HandleFunc("GET /v1/evaluations/{request_id}", handler.get)
	return mux
}

func (handler *Handler) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (handler *Handler) create(writer http.ResponseWriter, request *http.Request) {
	select {
	case handler.submission <- struct{}{}:
		defer func() { <-handler.submission }()
	default:
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": FailureOverloaded})
		return
	}
	assessment, err := decodeMultipartRequest(writer, request)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "INVALID_REQUEST"})
		return
	}
	digest, err := requestDigest(assessment)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "INVALID_REQUEST"})
		return
	}

	handler.mu.Lock()
	handler.pruneCompleted(time.Now().UTC())
	if existing, exists := handler.jobs[assessment.RequestID]; exists {
		if existing.digest != digest {
			handler.mu.Unlock()
			writeJSON(writer, http.StatusConflict, map[string]string{"error": "IDEMPOTENCY_CONFLICT"})
			return
		}
		response := existing.response
		handler.mu.Unlock()
		writeStatus(writer, response)
		return
	}
	if handler.processingJobCount() >= handler.config.MaxJobs {
		handler.mu.Unlock()
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": FailureOverloaded})
		return
	}
	handler.evictOldestCompleted(handler.config.MaxJobs * 8)
	job := &relayJob{
		digest:   digest,
		response: processingResponse(assessment.RequestID),
	}
	response := job.response
	handler.jobs[assessment.RequestID] = job
	handler.mu.Unlock()

	go handler.evaluate(assessment)
	writeJSON(writer, http.StatusAccepted, response)
}

func (handler *Handler) evictOldestCompleted(limit int) {
	for len(handler.jobs) >= limit {
		var oldestID string
		var oldestTime time.Time
		for requestID, job := range handler.jobs {
			if job.completedAt.IsZero() ||
				(!oldestTime.IsZero() && !job.completedAt.Before(oldestTime)) {
				continue
			}
			oldestID = requestID
			oldestTime = job.completedAt
		}
		if oldestID == "" {
			return
		}
		delete(handler.jobs, oldestID)
	}
}

func (handler *Handler) get(writer http.ResponseWriter, request *http.Request) {
	requestID := request.PathValue("request_id")
	if !validRequestID(requestID) {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "INVALID_REQUEST"})
		return
	}
	handler.mu.Lock()
	handler.pruneCompleted(time.Now().UTC())
	job, exists := handler.jobs[requestID]
	if !exists {
		handler.mu.Unlock()
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "NOT_FOUND"})
		return
	}
	response := job.response
	handler.mu.Unlock()
	writeStatus(writer, response)
}

func (handler *Handler) evaluate(request speechfeedback.AcousticAssessmentRequest) {
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), handler.config.ProviderTimeout)
	select {
	case handler.sem <- struct{}{}:
		defer func() { <-handler.sem }()
		if ctx.Err() != nil {
			cancel()
			handler.complete(
				request.RequestID,
				failureResponse(request.RequestID, FailureProcessingTimeout, true),
				startedAt,
			)
			return
		}
	case <-ctx.Done():
		cancel()
		handler.complete(
			request.RequestID,
			failureResponse(request.RequestID, FailureProcessingTimeout, true),
			startedAt,
		)
		return
	}
	result, err := handler.evaluator.Evaluate(ctx, request)
	timedOut := errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(ctx.Err(), context.DeadlineExceeded)
	cancel()

	response := resultResponse(request.RequestID, result)
	if err != nil {
		code := FailureProviderUnavailable
		if timedOut {
			code = FailureProcessingTimeout
		}
		response = failureResponse(request.RequestID, code, true)
	} else if !response.valid() {
		response = failureResponse(request.RequestID, FailureProviderUnavailable, true)
	}
	handler.complete(request.RequestID, response, startedAt)
}

func (handler *Handler) complete(
	requestID string,
	response StatusResponse,
	startedAt time.Time,
) {
	handler.mu.Lock()
	if job, exists := handler.jobs[requestID]; exists {
		job.response = response
		job.completedAt = time.Now().UTC()
	}
	handler.mu.Unlock()
	handler.config.Logger.Info(
		"ISE relay evaluation completed",
		slog.String("request_id", requestID),
		slog.String("status", response.Status),
		slog.Duration("duration", time.Since(startedAt)),
	)
}

func (handler *Handler) processingJobCount() int {
	count := 0
	for _, job := range handler.jobs {
		if job.response.Status == StatusProcessing {
			count++
		}
	}
	return count
}

func (handler *Handler) pruneCompleted(now time.Time) {
	for requestID, job := range handler.jobs {
		if !job.completedAt.IsZero() &&
			!now.Before(job.completedAt.Add(handler.config.Retention)) {
			delete(handler.jobs, requestID)
		}
	}
}

func decodeMultipartRequest(
	writer http.ResponseWriter,
	request *http.Request,
) (speechfeedback.AcousticAssessmentRequest, error) {
	request.Body = http.MaxBytesReader(
		writer,
		request.Body,
		maxAudioBytes+maxReferenceBytes+maxTopicTitleBytes+maxMultipartOverhead,
	)
	reader, err := request.MultipartReader()
	if err != nil {
		return speechfeedback.AcousticAssessmentRequest{}, err
	}
	values := make(map[string][]byte, 5)
	for {
		part, partErr := reader.NextPart()
		if errors.Is(partErr, io.EOF) {
			break
		}
		if partErr != nil {
			return speechfeedback.AcousticAssessmentRequest{}, partErr
		}
		name := part.FormName()
		limit, accepted := multipartPartLimit(name)
		if !accepted || part.FileName() != "" && name != "audio" {
			part.Close()
			return speechfeedback.AcousticAssessmentRequest{}, errors.New("invalid multipart field")
		}
		if _, duplicate := values[name]; duplicate {
			part.Close()
			return speechfeedback.AcousticAssessmentRequest{}, errors.New("duplicate multipart field")
		}
		value, readErr := io.ReadAll(io.LimitReader(part, limit+1))
		part.Close()
		if readErr != nil || int64(len(value)) > limit {
			return speechfeedback.AcousticAssessmentRequest{}, errors.New("multipart field is too large")
		}
		values[name] = value
	}
	assessment := speechfeedback.AcousticAssessmentRequest{
		RequestID:     string(values["request_id"]),
		Audio:         values["audio"],
		ReferenceText: string(values["reference_text"]),
		TopicTitle:    string(values["topic_title"]),
		Category: speechfeedback.AcousticAssessmentCategory(
			string(values["category"]),
		),
	}
	if !validRequest(assessment) {
		return speechfeedback.AcousticAssessmentRequest{}, errors.New("invalid assessment")
	}
	return assessment, nil
}

func multipartPartLimit(name string) (int64, bool) {
	switch name {
	case "request_id":
		return 36, true
	case "audio":
		return maxAudioBytes, true
	case "reference_text":
		return maxReferenceBytes, true
	case "topic_title":
		return maxTopicTitleBytes, true
	case "category":
		return 32, true
	default:
		return 0, false
	}
}

func writeStatus(writer http.ResponseWriter, response StatusResponse) {
	status := http.StatusOK
	if response.Status == StatusProcessing {
		status = http.StatusAccepted
	}
	writeJSON(writer, status, response)
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("content-type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
