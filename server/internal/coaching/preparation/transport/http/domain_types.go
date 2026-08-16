package http

import (
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	preparationservice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/service"
)

type CreateInterviewPreparationRequest = preparation.CreateInterviewPreparationRequest
type PatchInterviewPreparationRequest = preparation.PatchInterviewPreparationRequest
type InterviewPreparation = preparation.InterviewPreparation
type InterviewPreparationService = preparation.InterviewPreparationService
type CreatePlanRequest = preparation.CreatePlanRequest
type ConfirmPlanRequest = preparation.ConfirmPlanRequest
type PracticePlan = preparation.PracticePlan
type PracticePlanSummary = preparation.PracticePlanSummary
type PlanService = preparationservice.PlanService

var (
	ErrInterviewPreparationInvalid      = preparation.ErrInterviewPreparationInvalid
	ErrInterviewPreparationNotFound     = preparation.ErrInterviewPreparationNotFound
	ErrInterviewPreparationConflict     = preparation.ErrInterviewPreparationConflict
	ErrInterviewPreparationRequestReuse = preparation.ErrInterviewPreparationRequestReuse
	ErrInterviewPreparationGeneration   = preparation.ErrInterviewPreparationGeneration
	ErrPlanInvalid                      = preparation.ErrPlanInvalid
	ErrPlanNotFound                     = preparation.ErrPlanNotFound
	ErrPlanConflict                     = preparation.ErrPlanConflict
	ErrPlanIdempotencyConflict          = preparation.ErrPlanIdempotencyConflict
)

func validCreatePlanRequest(request CreatePlanRequest) bool {
	return preparationservice.ValidCreatePlanRequest(request)
}
func validPlanResourceID(value string) bool     { return preparationservice.ValidPlanResourceID(value) }
func validResourceIdentifier(value string) bool { return preparation.ValidAggregateID(value) }
func newPreparationCorrelationID() string       { return preparation.NewCorrelationID() }
