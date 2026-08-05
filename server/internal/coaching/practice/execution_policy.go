package practice

import "errors"

const (
	GenericPracticeTurnPolicy             = "generic.practice.turn.v1"
	DailyHotelCheckinIssueTurnPolicy      = "daily.hotel_checkin_issue.turn.v1"
	WorkplaceProgressRiskUpdateTurnPolicy = "workplace.progress_risk_update.turn.v1"
	InterviewProjectDeepDiveTurnPolicy    = "interview.project_deep_dive.turn.v1"
	IELTSSpeakingPart1TurnPolicy          = "ielts.speaking_part1.turn.v1"
	IELTSSpeakingPart2TurnPolicy          = "ielts.speaking_part2.turn.v1"
	IELTSSpeakingPart3TurnPolicy          = "ielts.speaking_part3.turn.v1"
	IELTSSpeakingFullMockTurnPolicy       = "ielts.speaking_full_mock.turn.v1"

	GenericPracticeSessionPolicy             = "generic.practice.session.v1"
	DailyPracticeSessionPolicy               = "daily.practice.session.v1"
	WorkplacePracticeSessionPolicy           = "workplace.practice.session.v1"
	InterviewPracticeSessionPolicy           = "interview.practice.session.v1"
	ExamPracticeSessionPolicy                = "exam.practice.session.v1"
	DailyHotelCheckinIssueSessionPolicy      = "daily.hotel_checkin_issue.session.v1"
	WorkplaceProgressRiskUpdateSessionPolicy = "workplace.progress_risk_update.session.v1"
	InterviewProjectDeepDiveSessionPolicy    = "interview.project_deep_dive.session.v1"
	IELTSSpeakingPart1SessionPolicy          = "ielts.speaking_part1.session.v1"
	IELTSSpeakingPart2SessionPolicy          = "ielts.speaking_part2.session.v1"
	IELTSSpeakingPart3SessionPolicy          = "ielts.speaking_part3.session.v1"
	IELTSSpeakingFullMockSessionPolicy       = "ielts.speaking_full_mock.session.v1"
)

var ErrExecutionPolicyNotFound = errors.New(
	"practice: execution policy is not registered",
)

type TurnPolicyKind uint8

const (
	TurnPolicyGenerated TurnPolicyKind = iota + 1
	TurnPolicyInterview
	TurnPolicyFrozenIELTS
)

type TurnPolicy struct {
	Kind      TurnPolicyKind
	IELTSMode IELTSPracticeMode
}

type sessionPolicyRegistration struct {
	minEffectiveTurns          int
	maxEffectiveTurns          int
	coverageCheckpointTurn     int
	maxFollowUpsPerQuestion    int
	turnsFromBlueprints        bool
	retryAllowed               bool
	questionTranslationAllowed bool
}

func ResolveTurnPolicy(reference string) (TurnPolicy, error) {
	switch reference {
	case GenericPracticeTurnPolicy,
		DailyHotelCheckinIssueTurnPolicy,
		WorkplaceProgressRiskUpdateTurnPolicy:
		return TurnPolicy{Kind: TurnPolicyGenerated}, nil
	case InterviewProjectDeepDiveTurnPolicy:
		return TurnPolicy{Kind: TurnPolicyInterview}, nil
	case IELTSSpeakingPart1TurnPolicy:
		return TurnPolicy{
			Kind: TurnPolicyFrozenIELTS, IELTSMode: IELTSPracticeModePart1,
		}, nil
	case IELTSSpeakingPart2TurnPolicy:
		return TurnPolicy{
			Kind: TurnPolicyFrozenIELTS, IELTSMode: IELTSPracticeModePart2,
		}, nil
	case IELTSSpeakingPart3TurnPolicy:
		return TurnPolicy{
			Kind: TurnPolicyFrozenIELTS, IELTSMode: IELTSPracticeModePart3,
		}, nil
	case IELTSSpeakingFullMockTurnPolicy:
		return TurnPolicy{
			Kind: TurnPolicyFrozenIELTS, IELTSMode: IELTSPracticeModeFullMock,
		}, nil
	default:
		return TurnPolicy{}, ErrExecutionPolicyNotFound
	}
}

func ValidSessionPolicy(
	reference string,
	optionType PracticeOptionType,
	blueprintCount int,
	policy SessionPolicy,
) bool {
	if policy.SuggestedDurationSeconds < 1 || blueprintCount < 1 {
		return false
	}
	registration, found := resolveSessionPolicyRegistration(reference)
	if !found {
		return false
	}
	expected, ok := buildSessionPolicy(
		registration,
		optionType,
		blueprintCount,
		policy.SuggestedDurationSeconds,
		policy.MaxEffectiveTurns,
	)
	if !ok {
		return false
	}
	return policy.SuggestedDurationSeconds == expected.SuggestedDurationSeconds &&
		policy.MinEffectiveTurns == expected.MinEffectiveTurns &&
		policy.MaxEffectiveTurns == expected.MaxEffectiveTurns &&
		policy.CoverageCheckpointTurn == expected.CoverageCheckpointTurn &&
		policy.MaxFollowUpsPerQuestion == expected.MaxFollowUpsPerQuestion &&
		policy.EarlyCompletionRule == expected.EarlyCompletionRule &&
		policy.RetryAllowed == expected.RetryAllowed &&
		policy.QuestionTranslationAllowed == expected.QuestionTranslationAllowed
}

// ResolveSessionPolicy builds the complete frozen policy named by one Scene
// reference. Family and model are intentionally absent: changing display or
// evidence metadata cannot change runtime behavior.
func ResolveSessionPolicy(
	reference string,
	prompt ScenePrompt,
	option PracticeOption,
	requestedMaxEffectiveTurns int,
) (SessionPolicy, error) {
	if prompt.SuggestedDurationSeconds < 1 || len(prompt.TurnBlueprints) < 1 {
		return SessionPolicy{}, ErrConflict
	}
	registration, found := resolveSessionPolicyRegistration(reference)
	if !found {
		return SessionPolicy{}, ErrExecutionPolicyNotFound
	}
	switch option.Type {
	case PracticeOptionFullSimulation:
		if option.RoleDefinitionID != "" {
			return SessionPolicy{}, ErrInvalidArgument
		}
	case PracticeOptionFocus:
		if option.RoleDefinitionID == "" {
			return SessionPolicy{}, ErrInvalidArgument
		}
	default:
		return SessionPolicy{}, ErrInvalidArgument
	}
	policy, ok := buildSessionPolicy(
		registration,
		option.Type,
		len(prompt.TurnBlueprints),
		prompt.SuggestedDurationSeconds,
		requestedMaxEffectiveTurns,
	)
	if !ok {
		return SessionPolicy{}, ErrInvalidArgument
	}
	return policy, nil
}

func resolveSessionPolicyRegistration(
	reference string,
) (sessionPolicyRegistration, bool) {
	standard := sessionPolicyRegistration{
		minEffectiveTurns:       4,
		maxEffectiveTurns:       6,
		coverageCheckpointTurn:  4,
		maxFollowUpsPerQuestion: 1,
	}
	switch reference {
	case GenericPracticeSessionPolicy,
		ExamPracticeSessionPolicy:
		return standard, true
	case InterviewPracticeSessionPolicy:
		standard.questionTranslationAllowed = true
		return standard, true
	case DailyPracticeSessionPolicy,
		DailyHotelCheckinIssueSessionPolicy,
		WorkplacePracticeSessionPolicy,
		WorkplaceProgressRiskUpdateSessionPolicy:
		standard.retryAllowed = true
		return standard, true
	case InterviewProjectDeepDiveSessionPolicy:
		standard.maxFollowUpsPerQuestion = 3
		standard.questionTranslationAllowed = true
		return standard, true
	case IELTSSpeakingPart1SessionPolicy,
		IELTSSpeakingPart2SessionPolicy,
		IELTSSpeakingPart3SessionPolicy,
		IELTSSpeakingFullMockSessionPolicy:
		return sessionPolicyRegistration{turnsFromBlueprints: true}, true
	default:
		return sessionPolicyRegistration{}, false
	}
}

func buildSessionPolicy(
	registration sessionPolicyRegistration,
	optionType PracticeOptionType,
	blueprintCount int,
	suggestedDurationSeconds int,
	requestedMaxEffectiveTurns int,
) (SessionPolicy, bool) {
	if blueprintCount < 1 || suggestedDurationSeconds < 1 ||
		requestedMaxEffectiveTurns < 0 {
		return SessionPolicy{}, false
	}
	if registration.turnsFromBlueprints {
		if optionType != PracticeOptionFullSimulation || blueprintCount > 14 {
			return SessionPolicy{}, false
		}
		registration.minEffectiveTurns = blueprintCount
		registration.maxEffectiveTurns = blueprintCount
		registration.coverageCheckpointTurn = blueprintCount
	} else if optionType == PracticeOptionFocus {
		registration.minEffectiveTurns = 1
		registration.maxEffectiveTurns = 3
		registration.coverageCheckpointTurn = 1
	} else if optionType != PracticeOptionFullSimulation {
		return SessionPolicy{}, false
	}
	if requestedMaxEffectiveTurns > 0 {
		if requestedMaxEffectiveTurns < registration.minEffectiveTurns ||
			requestedMaxEffectiveTurns > registration.maxEffectiveTurns {
			return SessionPolicy{}, false
		}
		registration.maxEffectiveTurns = requestedMaxEffectiveTurns
	}
	return SessionPolicy{
		SuggestedDurationSeconds:   suggestedDurationSeconds,
		MinEffectiveTurns:          registration.minEffectiveTurns,
		MaxEffectiveTurns:          registration.maxEffectiveTurns,
		CoverageCheckpointTurn:     registration.coverageCheckpointTurn,
		MaxFollowUpsPerQuestion:    registration.maxFollowUpsPerQuestion,
		EarlyCompletionRule:        EarlyCompletionCoverageSatisfiedAfterCheckpoint,
		RetryAllowed:               registration.retryAllowed,
		QuestionTranslationAllowed: registration.questionTranslationAllowed,
	}, true
}
