// Package interview implements Preparation resolution for interview practice.
package interview

import (
	"context"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/model"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/service/port"
)

type ReferenceVerifier interface {
	VerifyResumeRevision(context.Context, port.ResolveCommand, model.ResumeRevisionRef) error
	VerifyConfirmedJobTarget(context.Context, port.ResolveCommand, model.ConfirmedJobTargetRef) error
}

type Strategy struct {
	verifier ReferenceVerifier
}

func New(verifier ReferenceVerifier) (*Strategy, error) {
	if verifier == nil {
		return nil, model.ErrInvalidContext
	}
	return &Strategy{verifier: verifier}, nil
}

func (*Strategy) Kind() model.PreparationKind {
	return model.PreparationKindInterview
}

func (strategy *Strategy) Resolve(
	ctx context.Context,
	command port.ResolveCommand,
) (model.ResolvedContext, error) {
	if strategy == nil || strategy.verifier == nil || ctx == nil ||
		!command.Actor.Valid() || !command.Input.ValidShape() ||
		command.Input.Kind != model.PreparationKindInterview {
		return model.ResolvedContext{}, model.ErrInvalidContext
	}
	input := command.Input.Interview
	if !validID(input.JobTarget.JobTargetID) ||
		input.JobTarget.ConfirmationVersion < 1 {
		return model.ResolvedContext{}, model.ErrInvalidContext
	}
	if err := strategy.verifier.VerifyConfirmedJobTarget(
		ctx,
		command,
		input.JobTarget,
	); err != nil {
		return model.ResolvedContext{}, err
	}
	var resume *model.ResumeRevisionRef
	if input.Resume != nil {
		if !validID(input.Resume.ResumeID) || input.Resume.Revision < 1 {
			return model.ResolvedContext{}, model.ErrInvalidContext
		}
		if err := strategy.verifier.VerifyResumeRevision(
			ctx,
			command,
			*input.Resume,
		); err != nil {
			return model.ResolvedContext{}, err
		}
		copy := *input.Resume
		resume = &copy
	}
	return model.ResolvedContext{
		Kind: model.PreparationKindInterview,
		Interview: &model.InterviewContextSnapshot{
			Resume:    resume,
			JobTarget: input.JobTarget,
		},
	}, nil
}

func validID(value string) bool {
	return value != "" && len(value) <= 128 &&
		strings.TrimSpace(value) == value &&
		!strings.ContainsRune(value, '\x00')
}

var _ port.PreparationStrategy = (*Strategy)(nil)
