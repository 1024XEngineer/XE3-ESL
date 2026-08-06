package service

import . "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"

func validCanonicalPath(value string) bool { return ValidCanonicalPath(value) }

func validIdempotencyKey(value string) bool { return ValidIdempotencyKey(value) }

func targetedPreparationSnapshot(snapshot Snapshot) bool {
	return TargetedPreparationSnapshot(snapshot)
}

func cloneSnapshotJobTargetInput(input *JobTargetInput) *JobTargetInput {
	return CloneSnapshotJobTargetInput(input)
}

func cloneSnapshotJobTargetCandidate(
	candidate *JobTargetCandidate,
) *JobTargetCandidate {
	return CloneSnapshotJobTargetCandidate(candidate)
}
