package bootstrap

import (
	"context"
	"sync"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
)

type evaluationShadowProcessor interface {
	ProcessPending(
		context.Context,
		int,
	) (scoring.InterviewShadowSweepResult, error)
}

type combinedEvaluationShadowProcessor struct {
	mu         sync.Mutex
	intake     *scoring.CompletionIntake
	interview  *scoring.InterviewShadowWorker
	ielts      *scoring.IELTSSpeakingShadowWorker
	general    *scoring.GeneralSceneWorker
	nextWorker int
}

func newEvaluationShadowProcessor(
	intake *scoring.CompletionIntake,
	interview *scoring.InterviewShadowWorker,
	ielts *scoring.IELTSSpeakingShadowWorker,
	general *scoring.GeneralSceneWorker,
) (*combinedEvaluationShadowProcessor, error) {
	if intake == nil || interview == nil || ielts == nil || general == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	return &combinedEvaluationShadowProcessor{
		intake:    intake,
		interview: interview,
		ielts:     ielts,
		general:   general,
	}, nil
}

func (processor *combinedEvaluationShadowProcessor) ProcessPending(
	ctx context.Context,
	limit int,
) (scoring.InterviewShadowSweepResult, error) {
	if processor == nil ||
		processor.intake == nil ||
		processor.interview == nil ||
		processor.ielts == nil ||
		processor.general == nil ||
		ctx == nil ||
		limit < 1 ||
		limit > 20 {
		return scoring.InterviewShadowSweepResult{},
			evaluation.ErrInvalidRequest
	}
	processor.mu.Lock()
	defer processor.mu.Unlock()

	var total scoring.InterviewShadowSweepResult
	intakeSweep, err := processor.intake.ProcessPending(ctx, 1)
	if err != nil {
		return total, err
	}
	total.Claimed += intakeSweep.Claimed
	total.Completed += intakeSweep.Delivered
	total.Retried += intakeSweep.Retried
	total.Failed += intakeSweep.Failed
	for total.Claimed < limit {
		preferred := processor.nextWorker
		processor.nextWorker = (processor.nextWorker + 1) % 3
		var claimed scoring.InterviewShadowSweepResult
		for offset := range 3 {
			candidate := (preferred + offset) % 3
			claimed, err = processor.processOne(ctx, candidate)
			addInterviewSweep(&total, claimed)
			if err != nil {
				return total, err
			}
			if claimed.Claimed != 0 {
				break
			}
		}
		if claimed.Claimed == 0 {
			return total, nil
		}
	}
	return total, nil
}

func (processor *combinedEvaluationShadowProcessor) processOne(
	ctx context.Context,
	worker int,
) (scoring.InterviewShadowSweepResult, error) {
	switch worker {
	case 0:
		return processor.interview.ProcessPending(ctx, 1)
	case 1:
		result, err := processor.ielts.ProcessPending(ctx, 1)
		return scoring.InterviewShadowSweepResult{
			Claimed:   result.Claimed,
			Completed: result.Completed,
			Retried:   result.Retried,
			Failed:    result.Failed,
		}, err
	case 2:
		result, err := processor.general.ProcessPending(ctx, 1)
		return scoring.InterviewShadowSweepResult{
			Claimed:   result.Claimed,
			Completed: result.Completed,
			Retried:   result.Retried,
			Failed:    result.Failed,
		}, err
	default:
		return scoring.InterviewShadowSweepResult{},
			evaluation.ErrInvalidRequest
	}
}

func addInterviewSweep(
	total *scoring.InterviewShadowSweepResult,
	next scoring.InterviewShadowSweepResult,
) {
	total.Claimed += next.Claimed
	total.Completed += next.Completed
	total.Retried += next.Retried
	total.Failed += next.Failed
}

var _ evaluationShadowProcessor = (*combinedEvaluationShadowProcessor)(nil)
