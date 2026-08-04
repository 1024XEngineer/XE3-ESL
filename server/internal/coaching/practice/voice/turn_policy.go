package voice

// turnPolicy identifies one Practice-owned question behavior. Scene versions
// select a policy by reference; they do not define or execute that behavior.
type turnPolicy uint8

const (
	turnPolicyGenerated turnPolicy = iota + 1
	turnPolicyInterview
	turnPolicyFrozenIELTS
)

const (
	genericPracticeTurnPolicy             = "generic.practice.turn.v1"
	dailyHotelCheckinIssueTurnPolicy      = "daily.hotel_checkin_issue.turn.v1"
	workplaceProgressRiskUpdateTurnPolicy = "workplace.progress_risk_update.turn.v1"
	interviewProjectDeepDiveTurnPolicy    = "interview.project_deep_dive.turn.v1"
	ieltsSpeakingPart1TurnPolicy          = "ielts.speaking_part1.turn.v1"
	ieltsSpeakingPart2TurnPolicy          = "ielts.speaking_part2.turn.v1"
	ieltsSpeakingPart3TurnPolicy          = "ielts.speaking_part3.turn.v1"
	ieltsSpeakingFullMockTurnPolicy       = "ielts.speaking_full_mock.turn.v1"
)

func resolveTurnPolicy(reference string) (turnPolicy, error) {
	switch reference {
	case genericPracticeTurnPolicy,
		dailyHotelCheckinIssueTurnPolicy,
		workplaceProgressRiskUpdateTurnPolicy:
		return turnPolicyGenerated, nil
	case interviewProjectDeepDiveTurnPolicy:
		return turnPolicyInterview, nil
	case ieltsSpeakingPart1TurnPolicy,
		ieltsSpeakingPart2TurnPolicy,
		ieltsSpeakingPart3TurnPolicy,
		ieltsSpeakingFullMockTurnPolicy:
		return turnPolicyFrozenIELTS, nil
	default:
		return 0, ErrInvalidContext
	}
}
