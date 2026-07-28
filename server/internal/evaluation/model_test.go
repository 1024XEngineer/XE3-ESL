package evaluation

import "testing"

func TestEvaluationValidityRequiresImmutableRevisionLinkage(t *testing.T) {
	evaluation := validEvaluation()
	if !evaluation.Valid() {
		t.Fatal("valid fixture was rejected")
	}
	evaluation.Revision.EvaluationID = testOwnerB
	if evaluation.Valid() {
		t.Fatal("cross-Evaluation revision linkage was accepted")
	}
}

func TestRevisionValidityRejectsNonFirstRevisionWithoutSupersedes(t *testing.T) {
	revision := validEvaluation().Revision
	revision.Number = 2
	if revision.Valid() {
		t.Fatal("revision 2 without supersedes was accepted")
	}
	revision.SupersedesRevisionID =
		"60000000-0000-4000-8000-000000000006"
	if !revision.Valid() {
		t.Fatal("revision 2 with supersedes was rejected")
	}
}

func TestRevisionValidityAllowsCompletedShadowResult(t *testing.T) {
	revision := validEvaluation().Revision
	completedAt := revision.UpdatedAt
	revision.Status = StatusReady
	revision.IsFinal = false
	revision.CompletedAt = &completedAt
	if !revision.Valid() {
		t.Fatal("completed non-final Shadow revision was rejected")
	}
}
