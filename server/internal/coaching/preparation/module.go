// Package preparation owns user-provided background and immutable snapshots.
package preparation

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return "preparation" }

// CreateProfileRequest is the application input for one preparation profile.
type CreateProfileRequest struct {
	ResumeID                     string `json:"resume_id,omitempty"`
	ResumeRevision               int64  `json:"resume_revision,omitempty"`
	JobDescriptionRef            string `json:"job_description_ref,omitempty"`
	BackgroundSummary            string `json:"background_summary"`
	JobTargetID                  string `json:"job_target_id,omitempty"`
	JobTargetConfirmationVersion int    `json:"job_target_confirmation_version,omitempty"`
}

// CreateSnapshotRequest pins one immutable version of a preparation profile.
type CreateSnapshotRequest struct {
	SourceVersion int `json:"source_version"`
}
