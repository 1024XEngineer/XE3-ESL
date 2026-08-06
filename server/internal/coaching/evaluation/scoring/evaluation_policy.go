package scoring

import "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"

const (
	InterviewEvaluationPolicyRef             = "interview.shadow.evaluation.v1"
	IELTSSpeakingPracticeEvaluationPolicyRef = "ielts.speaking_practice.evaluation.v1"
	IELTSSpeakingFullMockEvaluationPolicyRef = "ielts.speaking_full_mock.evaluation.v1"
	WorkplaceEvaluationPolicyRef             = "workplace.general.evaluation.v1"
	DailyEvaluationPolicyRef                 = "daily.general.evaluation.v1"
)

type evaluationPolicySpec struct {
	SceneType       evaluation.SceneType
	StrategyRef     string
	PipelineVersion string
	Enabled         bool
}

var registeredEvaluationPolicies = map[string]evaluationPolicySpec{
	InterviewEvaluationPolicyRef: {
		SceneType:       evaluation.SceneInterview,
		StrategyRef:     InterviewShadowStrategyRef,
		PipelineVersion: InterviewShadowPipelineVersion,
		Enabled:         true,
	},
	IELTSSpeakingPracticeEvaluationPolicyRef: {
		SceneType:       evaluation.SceneIELTSSpeaking,
		StrategyRef:     GeneralSceneStrategyRef,
		PipelineVersion: GeneralScenePipelineVersion,
		Enabled:         true,
	},
	IELTSSpeakingFullMockEvaluationPolicyRef: {
		SceneType:       evaluation.SceneIELTSSpeaking,
		StrategyRef:     IELTSSpeakingShadowStrategyRef,
		PipelineVersion: IELTSSpeakingShadowPipelineVersion,
		Enabled:         true,
	},
	WorkplaceEvaluationPolicyRef: {
		SceneType:       evaluation.SceneOverseasWorkplace,
		StrategyRef:     GeneralSceneStrategyRef,
		PipelineVersion: GeneralScenePipelineVersion,
		Enabled:         true,
	},
	DailyEvaluationPolicyRef: {
		SceneType:       evaluation.SceneOverseasDaily,
		StrategyRef:     GeneralSceneStrategyRef,
		PipelineVersion: GeneralScenePipelineVersion,
		Enabled:         true,
	},
}

// EvaluationPolicyRegistry is the single authority that maps Scene policy
// references to enabled Evaluation pipelines.
type EvaluationPolicyRegistry struct {
	policies map[string]evaluationPolicySpec
}

func NewEvaluationPolicyRegistry() *EvaluationPolicyRegistry {
	policies := make(
		map[string]evaluationPolicySpec,
		len(registeredEvaluationPolicies),
	)
	for reference, policy := range registeredEvaluationPolicies {
		policies[reference] = policy
	}
	return &EvaluationPolicyRegistry{policies: policies}
}

// ValidateEvaluationPolicyReference is Scene's narrow publication-time port.
func (registry *EvaluationPolicyRegistry) ValidateEvaluationPolicyReference(
	reference string,
) error {
	_, err := registry.resolve(reference)
	return err
}

func (registry *EvaluationPolicyRegistry) resolve(
	reference string,
) (evaluationPolicySpec, error) {
	if registry == nil {
		return evaluationPolicySpec{}, ErrStrategyNotAvailable
	}
	policy, exists := registry.policies[reference]
	if !exists || !policy.Enabled {
		return evaluationPolicySpec{}, ErrStrategyNotAvailable
	}
	return policy, nil
}
