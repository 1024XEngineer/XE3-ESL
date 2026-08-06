package practice

import "errors"

const (
	GenericPracticeTurnPolicy             = "generic.practice.turn.v1"
	InterviewPracticeTurnPolicy           = "interview.practice.turn.v1"
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
	Kind TurnPolicyKind
	Mode PracticeMode
}

type sessionPolicyRegistration struct {
	minEffectiveTurns          int
	maxEffectiveTurns          int
	coverageCheckpointTurn     int
	maxFollowUpsPerQuestion    int
	turnsFromBlueprints        bool
	retryAllowed               bool
	questionTranslationAllowed bool
	questionTipsAllowed        bool
	avatarAllowed              bool
	speechFeedbackAllowed      bool
}

func ResolveTurnPolicy(reference string) (TurnPolicy, error) {
	switch reference {
	case GenericPracticeTurnPolicy,
		DailyHotelCheckinIssueTurnPolicy,
		WorkplaceProgressRiskUpdateTurnPolicy:
		return TurnPolicy{Kind: TurnPolicyGenerated}, nil
	case InterviewPracticeTurnPolicy,
		InterviewProjectDeepDiveTurnPolicy:
		return TurnPolicy{Kind: TurnPolicyInterview}, nil
	case IELTSSpeakingPart1TurnPolicy:
		return TurnPolicy{
			Kind: TurnPolicyFrozenIELTS, Mode: PracticeModePart1,
		}, nil
	case IELTSSpeakingPart2TurnPolicy:
		return TurnPolicy{
			Kind: TurnPolicyFrozenIELTS, Mode: PracticeModePart2,
		}, nil
	case IELTSSpeakingPart3TurnPolicy:
		return TurnPolicy{
			Kind: TurnPolicyFrozenIELTS, Mode: PracticeModePart3,
		}, nil
	case IELTSSpeakingFullMockTurnPolicy:
		return TurnPolicy{
			Kind: TurnPolicyFrozenIELTS, Mode: PracticeModeFullMock,
		}, nil
	default:
		return TurnPolicy{}, ErrExecutionPolicyNotFound
	}
}

func ValidSessionPolicy(
	reference string,
	mode PracticeMode,
	blueprintCount int,
	suggestedDurationSeconds int,
	policy SessionPolicy,
) bool {
	if suggestedDurationSeconds < 1 || blueprintCount < 1 {
		return false
	}
	registration, found := resolveSessionPolicyRegistration(reference)
	if !found {
		return false
	}
	expected, ok := buildSessionPolicy(
		registration,
		mode,
		blueprintCount,
		suggestedDurationSeconds,
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
		policy.QuestionTranslationAllowed == expected.QuestionTranslationAllowed &&
		policy.QuestionTipsAllowed == expected.QuestionTipsAllowed &&
		policy.AvatarAllowed == expected.AvatarAllowed &&
		policy.SpeechFeedbackAllowed == expected.SpeechFeedbackAllowed
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
	if option.SuggestedDurationSeconds < 1 || len(prompt.TurnBlueprints) < 1 {
		return SessionPolicy{}, ErrConflict
	}
	registration, found := resolveSessionPolicyRegistration(reference)
	if !found {
		return SessionPolicy{}, ErrExecutionPolicyNotFound
	}
	switch option.Mode {
	case PracticeModeFullSimulation, PracticeModeFullMock,
		PracticeModePart1, PracticeModePart2, PracticeModePart3:
		if option.RoleDefinitionID != "" {
			return SessionPolicy{}, ErrInvalidArgument
		}
	case PracticeModeFocus:
		if option.RoleDefinitionID == "" {
			return SessionPolicy{}, ErrInvalidArgument
		}
	default:
		return SessionPolicy{}, ErrInvalidArgument
	}
	policy, ok := buildSessionPolicy(
		registration,
		option.Mode,
		len(prompt.TurnBlueprints),
		option.SuggestedDurationSeconds,
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
		standard.maxFollowUpsPerQuestion = 3
		standard.questionTranslationAllowed = true
		standard.questionTipsAllowed = true
		standard.avatarAllowed = true
		standard.speechFeedbackAllowed = true
		return standard, true
	case DailyPracticeSessionPolicy,
		DailyHotelCheckinIssueSessionPolicy,
		WorkplacePracticeSessionPolicy,
		WorkplaceProgressRiskUpdateSessionPolicy:
		standard.retryAllowed = true
		standard.avatarAllowed = true
		standard.speechFeedbackAllowed = true
		return standard, true
	case InterviewProjectDeepDiveSessionPolicy:
		standard.maxFollowUpsPerQuestion = 3
		standard.questionTranslationAllowed = true
		standard.questionTipsAllowed = true
		standard.avatarAllowed = true
		standard.speechFeedbackAllowed = true
		return standard, true
	case IELTSSpeakingPart1SessionPolicy,
		IELTSSpeakingPart2SessionPolicy,
		IELTSSpeakingPart3SessionPolicy,
		IELTSSpeakingFullMockSessionPolicy:
		return sessionPolicyRegistration{
			turnsFromBlueprints:   true,
			speechFeedbackAllowed: true,
		}, true
	default:
		return sessionPolicyRegistration{}, false
	}
}

func buildSessionPolicy(
	registration sessionPolicyRegistration,
	mode PracticeMode,
	blueprintCount int,
	suggestedDurationSeconds int,
	requestedMaxEffectiveTurns int,
) (SessionPolicy, bool) {
	if blueprintCount < 1 || suggestedDurationSeconds < 1 ||
		requestedMaxEffectiveTurns < 0 {
		return SessionPolicy{}, false
	}
	if registration.turnsFromBlueprints {
		if (mode != PracticeModeFullMock && mode != PracticeModePart1 &&
			mode != PracticeModePart2 && mode != PracticeModePart3) ||
			blueprintCount > MaxPracticeTurns {
			return SessionPolicy{}, false
		}
		registration.minEffectiveTurns = blueprintCount
		registration.maxEffectiveTurns = blueprintCount
		registration.coverageCheckpointTurn = blueprintCount
	} else if mode == PracticeModeFocus {
		registration.minEffectiveTurns = 1
		registration.maxEffectiveTurns = 3
		registration.coverageCheckpointTurn = 1
	} else if mode != PracticeModeFullSimulation {
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
		QuestionTipsAllowed:        registration.questionTipsAllowed,
		AvatarAllowed:              registration.avatarAllowed,
		SpeechFeedbackAllowed:      registration.speechFeedbackAllowed,
	}, true
}
