package postgres

import (
	. "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	preparationservice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/service"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

func validResourceIdentifier(value string) bool {
	return ValidResourceIdentifier(value)
}

func validCanonicalPath(value string) bool { return ValidCanonicalPath(value) }

func validIdempotencyKey(value string) bool { return ValidIdempotencyKey(value) }

func validCreateProfileRequest(request CreateProfileRequest) bool {
	return ValidCreateProfileRequest(request)
}

func targetedPreparationSnapshot(snapshot Snapshot) bool {
	return TargetedPreparationSnapshot(snapshot)
}

func validResumeRevisionSnapshot(snapshot ResumeRevisionSnapshot) bool {
	return ValidResumeRevisionSnapshot(snapshot)
}

func validJobTargetInput(input JobTargetInput) bool {
	return ValidJobTargetInput(input)
}

func validJobTargetCandidateShape(
	candidate JobTargetCandidate,
	source JobTargetSource,
) bool {
	return ValidJobTargetCandidateShape(candidate, source)
}

func validPlanIELTSAssignment(
	selection scene.SelectionSnapshot,
	assignment *IELTSAssignmentSnapshot,
) bool {
	return preparationservice.ValidPlanIELTSAssignment(selection, assignment)
}

func validSelectedPlanOption(
	selection scene.SelectionSnapshot,
	roles []scene.RoleDefinition,
	option scene.PracticeOption,
) bool {
	return preparationservice.ValidSelectedPlanOption(selection, roles, option)
}

func validReturnedPlan(
	plan PracticePlan,
	actor requestcontext.Actor,
	expectedID string,
) bool {
	return preparationservice.ValidReturnedPlan(plan, actor, expectedID)
}

func validPracticeObjectives(values []PracticeObjective) bool {
	return preparationservice.ValidPracticeObjectives(values)
}

func validStoredSessionPolicy(policy SessionPolicy) bool {
	return preparationservice.ValidStoredSessionPolicy(policy)
}

func validPlanResourceID(value string) bool {
	return preparationservice.ValidPlanResourceID(value)
}

func validUniquePlanIDs(values []string) bool {
	return preparationservice.ValidUniquePlanIDs(values)
}

func validPlanText(value string) bool { return preparationservice.ValidPlanText(value) }

func cloneJobTargetCandidate(source JobTargetCandidate) JobTargetCandidate {
	return CloneJobTargetCandidate(source)
}

func cloneGoalSnapshot(source *GoalSnapshot) *GoalSnapshot {
	return preparationservice.CloneGoalSnapshot(source)
}

func clonePlanPreparationSnapshot(source Snapshot) Snapshot {
	return preparationservice.ClonePlanPreparationSnapshot(source)
}

func clonePlanObjectives(source []PracticeObjective) []PracticeObjective {
	return preparationservice.ClonePlanObjectives(source)
}

func cloneIELTSAssignment(
	source *IELTSAssignmentSnapshot,
) *IELTSAssignmentSnapshot {
	return preparationservice.CloneIELTSAssignment(source)
}

func clonePracticePlan(source PracticePlan) PracticePlan {
	return preparationservice.ClonePracticePlan(source)
}
