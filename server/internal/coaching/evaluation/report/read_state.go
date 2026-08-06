package report

import (
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
)

var (
	ErrInterviewConfigurationConflict = errors.New(
		"evaluation report: Interview configuration conflict",
	)
	ErrIELTSSpeakingConfigurationConflict = errors.New(
		"evaluation report: IELTS Speaking configuration conflict",
	)
)

// InterviewReadState is the authoritative input for projecting an Interview
// report from one consistent read.
type InterviewReadState struct {
	Evaluation evaluation.Evaluation
	Runtime    scoring.InterviewShadowReadState
	Snapshot   *evidence.EvidenceSnapshot
}

// IELTSSpeakingReadState is the authoritative input for projecting an IELTS
// Speaking report from one consistent read.
type IELTSSpeakingReadState struct {
	Evaluation evaluation.Evaluation
	Runtime    scoring.IELTSSpeakingShadowReadState
	Snapshot   *evidence.EvidenceSnapshot
}
