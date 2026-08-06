// Package interview adapts existing Resume and JobTarget capabilities to the
// interview Preparation strategy without exposing their repositories to the
// strategy implementation.
package interview

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/model"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/service/port"
)

type Verifier struct {
	resumes preparation.ResumeRevisionReader
	targets preparation.JobTargetRepository
}

func New(
	resumes preparation.ResumeRevisionReader,
	targets preparation.JobTargetRepository,
) (*Verifier, error) {
	if resumes == nil || targets == nil {
		return nil, model.ErrInvalidContext
	}
	return &Verifier{resumes: resumes, targets: targets}, nil
}

func (verifier *Verifier) VerifyResumeRevision(
	ctx context.Context,
	command port.ResolveCommand,
	reference model.ResumeRevisionRef,
) error {
	snapshot, err := verifier.resumes.ReadOwnedRevision(
		ctx,
		command.Actor,
		reference.ResumeID,
		reference.Revision,
	)
	if err != nil {
		return err
	}
	if snapshot.ResumeID != reference.ResumeID ||
		snapshot.Revision != reference.Revision {
		return model.ErrInvalidContext
	}
	return nil
}

func (verifier *Verifier) VerifyConfirmedJobTarget(
	ctx context.Context,
	command port.ResolveCommand,
	reference model.ConfirmedJobTargetRef,
) error {
	target, err := verifier.targets.Get(
		ctx,
		command.Actor,
		reference.JobTargetID,
	)
	if err != nil {
		return err
	}
	if target.Stage != preparation.JobTargetStageConfirmed ||
		target.Confirmation == nil ||
		target.Confirmation.ConfirmationVersion != reference.ConfirmationVersion {
		return model.ErrInvalidContext
	}
	return nil
}
