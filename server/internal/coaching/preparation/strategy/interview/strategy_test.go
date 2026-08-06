package interview

import (
	"context"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/model"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/service/port"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type verifier struct {
	resumeCalls int
	targetCalls int
}

func (verifier *verifier) VerifyResumeRevision(
	context.Context,
	port.ResolveCommand,
	model.ResumeRevisionRef,
) error {
	verifier.resumeCalls++
	return nil
}

func (verifier *verifier) VerifyConfirmedJobTarget(
	context.Context,
	port.ResolveCommand,
	model.ConfirmedJobTargetRef,
) error {
	verifier.targetCalls++
	return nil
}

func TestStrategyResolvesConfirmedTargetAndOptionalResume(t *testing.T) {
	refs := &verifier{}
	strategy, err := New(refs)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resolved, err := strategy.Resolve(context.Background(), port.ResolveCommand{
		Actor: requestcontext.Actor{UserID: "user-1", SessionID: "session-1"},
		Input: model.ContextInput{
			Kind: model.PreparationKindInterview,
			Interview: &model.InterviewContextInput{
				Resume: &model.ResumeRevisionRef{
					ResumeID: "resume-1",
					Revision: 2,
				},
				JobTarget: model.ConfirmedJobTargetRef{
					JobTargetID:         "target-1",
					ConfirmationVersion: 3,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if refs.resumeCalls != 1 || refs.targetCalls != 1 ||
		resolved.Interview.Resume.Revision != 2 {
		t.Fatalf("resolved = %#v, verifier = %#v", resolved, refs)
	}
}

func TestStrategyAllowsNoResume(t *testing.T) {
	refs := &verifier{}
	strategy, _ := New(refs)
	resolved, err := strategy.Resolve(context.Background(), port.ResolveCommand{
		Actor: requestcontext.Actor{UserID: "user-1", SessionID: "session-1"},
		Input: model.ContextInput{
			Kind: model.PreparationKindInterview,
			Interview: &model.InterviewContextInput{
				JobTarget: model.ConfirmedJobTargetRef{
					JobTargetID:         "target-1",
					ConfirmationVersion: 1,
				},
			},
		},
	})
	if err != nil || resolved.Interview.Resume != nil || refs.resumeCalls != 0 {
		t.Fatalf("resolved = %#v, err = %v", resolved, err)
	}
}
