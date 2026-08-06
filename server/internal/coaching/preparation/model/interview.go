package model

type ResumeRevisionRef struct {
	ResumeID string `json:"resume_id"`
	Revision int64  `json:"revision"`
}

type ConfirmedJobTargetRef struct {
	JobTargetID         string `json:"job_target_id"`
	ConfirmationVersion int    `json:"confirmation_version"`
}

// InterviewContextInput contains references selected by the actor. Resume is
// optional under the accepted interview preparation contract.
type InterviewContextInput struct {
	Resume    *ResumeRevisionRef
	JobTarget ConfirmedJobTargetRef
}

// InterviewContextSnapshot records the exact external identities accepted by
// the strategy. Existing Profile snapshot fields continue to own the resolved
// Resume and JobTarget material during the staged migration.
type InterviewContextSnapshot struct {
	Resume    *ResumeRevisionRef    `json:"resume,omitempty"`
	JobTarget ConfirmedJobTargetRef `json:"job_target"`
}
