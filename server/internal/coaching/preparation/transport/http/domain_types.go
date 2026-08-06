package http

import (
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	preparationservice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/service"
)

type CreateJobTargetRequest = preparation.CreateJobTargetRequest
type UpdateJobTargetRequest = preparation.UpdateJobTargetRequest
type AnalyzeJobTargetRequest = preparation.AnalyzeJobTargetRequest
type ConfirmJobTargetRequest = preparation.ConfirmJobTargetRequest
type DiscardJobTargetRequest = preparation.DiscardJobTargetRequest
type JobTarget = preparation.JobTarget
type JobTargetInput = preparation.JobTargetInput
type JobTargetSource = preparation.JobTargetSource
type JobTargetService = preparationservice.JobTargetApplication

type CreatePlanRequest = preparation.CreatePlanRequest
type RevisePlanRequest = preparation.RevisePlanRequest
type PracticePlan = preparation.PracticePlan
type PlanService = preparationservice.PlanService

const (
	JobTargetSourceQuickStart = preparation.JobTargetSourceQuickStart
	JobTargetStageDraft       = preparation.JobTargetStageDraft
	JobTargetStageParsing     = preparation.JobTargetStageParsing
)

var (
	ErrJobTargetInvalid             = preparation.ErrJobTargetInvalid
	ErrJobTargetNotFound            = preparation.ErrJobTargetNotFound
	ErrJobTargetConflict            = preparation.ErrJobTargetConflict
	ErrJobTargetIdempotencyConflict = preparation.ErrJobTargetIdempotencyConflict
	ErrJobTargetAnalysisFailed      = preparation.ErrJobTargetAnalysisFailed
	ErrJobTargetAnalysisClaimLost   = preparation.ErrJobTargetAnalysisClaimLost
	ErrJobTargetRepository          = preparation.ErrJobTargetRepository

	ErrPlanInvalid             = preparation.ErrPlanInvalid
	ErrPlanNotFound            = preparation.ErrPlanNotFound
	ErrPlanConflict            = preparation.ErrPlanConflict
	ErrPlanIdempotencyConflict = preparation.ErrPlanIdempotencyConflict
	ErrPlanRepository          = preparation.ErrPlanRepository
)

func validCreatePlanRequest(request CreatePlanRequest) bool {
	return preparationservice.ValidCreatePlanRequest(request)
}

func validRevisePlanRequest(request RevisePlanRequest) bool {
	return preparationservice.ValidRevisePlanRequest(request)
}

func validPlanResourceID(value string) bool {
	return preparationservice.ValidPlanResourceID(value)
}

func validJobTargetCandidateJSONSize(
	candidate preparation.JobTargetCandidate,
) bool {
	return preparation.ValidJobTargetCandidateJSONSize(candidate)
}

func validResourceIdentifier(value string) bool {
	return preparation.ValidResourceIdentifier(value)
}

func newPreparationCorrelationID() string {
	return preparation.NewCorrelationID()
}
