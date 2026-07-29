package bootstrap

import (
	"context"
	"sync"

	"github.com/1024XEngineer/XE3-ESL/server/internal/evaluation"
)

type evaluationShadowProcessor interface {
	ProcessPending(
		context.Context,
		int,
	) (evaluation.InterviewShadowSweepResult, error)
}

type combinedEvaluationShadowProcessor struct {
	mu        sync.Mutex
	interview *evaluation.InterviewShadowWorker
	ielts     *evaluation.IELTSSpeakingShadowWorker
	nextIELTS bool
}

func newEvaluationShadowProcessor(
	interview *evaluation.InterviewShadowWorker,
	ielts *evaluation.IELTSSpeakingShadowWorker,
) (*combinedEvaluationShadowProcessor, error) {
	if interview == nil || ielts == nil {
		return nil, evaluation.ErrInvalidRequest
	}
	return &combinedEvaluationShadowProcessor{
		interview: interview,
		ielts:     ielts,
	}, nil
}

func (processor *combinedEvaluationShadowProcessor) ProcessPending(
	ctx context.Context,
	limit int,
) (evaluation.InterviewShadowSweepResult, error) {
	if processor == nil ||
		processor.interview == nil ||
		processor.ielts == nil ||
		ctx == nil ||
		limit < 1 ||
		limit > 20 {
		return evaluation.InterviewShadowSweepResult{},
			evaluation.ErrInvalidRequest
	}
	processor.mu.Lock()
	defer processor.mu.Unlock()

	var total evaluation.InterviewShadowSweepResult
	for total.Claimed < limit {
		preferIELTS := processor.nextIELTS
		processor.nextIELTS = !processor.nextIELTS
		claimed, err := processor.processOne(ctx, preferIELTS)
		addInterviewSweep(&total, claimed)
		if err != nil {
			return total, err
		}
		if claimed.Claimed == 0 {
			claimed, err = processor.processOne(ctx, !preferIELTS)
			addInterviewSweep(&total, claimed)
			if err != nil {
				return total, err
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
	ielts bool,
) (evaluation.InterviewShadowSweepResult, error) {
	if !ielts {
		return processor.interview.ProcessPending(ctx, 1)
	}
	result, err := processor.ielts.ProcessPending(ctx, 1)
	return evaluation.InterviewShadowSweepResult{
		Claimed:   result.Claimed,
		Completed: result.Completed,
		Retried:   result.Retried,
		Failed:    result.Failed,
	}, err
}

func addInterviewSweep(
	total *evaluation.InterviewShadowSweepResult,
	next evaluation.InterviewShadowSweepResult,
) {
	total.Claimed += next.Claimed
	total.Completed += next.Completed
	total.Retried += next.Retried
	total.Failed += next.Failed
}

var _ evaluationShadowProcessor = (*combinedEvaluationShadowProcessor)(nil)
