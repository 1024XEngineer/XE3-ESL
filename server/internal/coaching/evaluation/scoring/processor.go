package scoring

import (
	"context"
	"sync"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

type Processor interface {
	ProcessPending(
		context.Context,
		int,
	) (InterviewShadowSweepResult, error)
}

type combinedProcessor struct {
	mu         sync.Mutex
	intake     *CompletionIntake
	interview  *InterviewShadowWorker
	ielts      *IELTSSpeakingShadowWorker
	general    *GeneralSceneWorker
	nextWorker int
}

func NewProcessor(
	intake *CompletionIntake,
	interview *InterviewShadowWorker,
	ielts *IELTSSpeakingShadowWorker,
	general *GeneralSceneWorker,
) (Processor, error) {
	if intake == nil || interview == nil || ielts == nil || general == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	return &combinedProcessor{
		intake:    intake,
		interview: interview,
		ielts:     ielts,
		general:   general,
	}, nil
}

func (processor *combinedProcessor) ProcessPending(
	ctx context.Context,
	limit int,
) (InterviewShadowSweepResult, error) {
	if processor == nil ||
		processor.intake == nil ||
		processor.interview == nil ||
		processor.ielts == nil ||
		processor.general == nil ||
		ctx == nil ||
		limit < 1 ||
		limit > 20 {
		return InterviewShadowSweepResult{}, evaluation.ErrInvalidRequest
	}
	processor.mu.Lock()
	defer processor.mu.Unlock()

	var total InterviewShadowSweepResult
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
		var claimed InterviewShadowSweepResult
		for offset := range 3 {
			candidate := (preferred + offset) % 3
			claimed, err = processor.processOne(ctx, candidate)
			addSweep(&total, claimed)
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

func (processor *combinedProcessor) processOne(
	ctx context.Context,
	worker int,
) (InterviewShadowSweepResult, error) {
	switch worker {
	case 0:
		return processor.interview.ProcessPending(ctx, 1)
	case 1:
		result, err := processor.ielts.ProcessPending(ctx, 1)
		return InterviewShadowSweepResult{
			Claimed:   result.Claimed,
			Completed: result.Completed,
			Retried:   result.Retried,
			Failed:    result.Failed,
		}, err
	case 2:
		result, err := processor.general.ProcessPending(ctx, 1)
		return InterviewShadowSweepResult{
			Claimed:   result.Claimed,
			Completed: result.Completed,
			Retried:   result.Retried,
			Failed:    result.Failed,
		}, err
	default:
		return InterviewShadowSweepResult{}, evaluation.ErrInvalidRequest
	}
}

func addSweep(total *InterviewShadowSweepResult, next InterviewShadowSweepResult) {
	total.Claimed += next.Claimed
	total.Completed += next.Completed
	total.Retried += next.Retried
	total.Failed += next.Failed
}

var _ Processor = (*combinedProcessor)(nil)
