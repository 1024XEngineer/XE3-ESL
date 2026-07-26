// Package fake provides deterministic Preparation Provider adapters for tests
// and explicit offline composition roots.
package fake

import (
	"context"
	"sync"

	"github.com/1024XEngineer/XE3-ESL/server/internal/preparation"
)

type JobTargetParser struct {
	mu        sync.Mutex
	candidate preparation.JobTargetCandidate
	err       error
	inputs    []preparation.JobTargetInput
}

func NewJobTargetParser(
	candidate preparation.JobTargetCandidate,
) *JobTargetParser {
	return &JobTargetParser{candidate: cloneCandidate(candidate)}
}

func NewFailingJobTargetParser(err error) *JobTargetParser {
	return &JobTargetParser{err: err}
}

func (p *JobTargetParser) ParseJobTarget(
	ctx context.Context,
	input preparation.JobTargetInput,
) (preparation.JobTargetCandidate, error) {
	if err := ctx.Err(); err != nil {
		return preparation.JobTargetCandidate{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inputs = append(p.inputs, input)
	return cloneCandidate(p.candidate), p.err
}

func (p *JobTargetParser) Inputs() []preparation.JobTargetInput {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]preparation.JobTargetInput(nil), p.inputs...)
}

func cloneCandidate(
	source preparation.JobTargetCandidate,
) preparation.JobTargetCandidate {
	result := source
	result.Responsibilities = append(
		[]string(nil),
		source.Responsibilities...,
	)
	result.CoreSkills = append([]string(nil), source.CoreSkills...)
	result.CommunicationFocus = append(
		[]string(nil),
		source.CommunicationFocus...,
	)
	result.PracticeGoals = append(
		[]string(nil),
		source.PracticeGoals...,
	)
	result.CatalogRecommendation.SelectedRoleIDs = append(
		[]string(nil),
		source.CatalogRecommendation.SelectedRoleIDs...,
	)
	return result
}

var _ preparation.JobTargetParser = (*JobTargetParser)(nil)
