package evaluation

// PolicyRegistry is the publication-time authority for the Evaluation policy
// references embedded in the versioned Scene catalog. Runtime evaluator
// selection remains explicit in CompletionScheduler.
type PolicyRegistry struct{}

func NewPolicyRegistry() PolicyRegistry { return PolicyRegistry{} }

func (PolicyRegistry) ValidateEvaluationPolicyReference(reference string) error {
	switch reference {
	case InterviewEvaluationPolicyRef,
		IELTSSpeakingPracticeEvaluationPolicyRef,
		IELTSSpeakingFullMockEvaluationPolicyRef,
		WorkplaceEvaluationPolicyRef,
		DailyEvaluationPolicyRef:
		return nil
	default:
		return ErrInvalidRequest
	}
}
