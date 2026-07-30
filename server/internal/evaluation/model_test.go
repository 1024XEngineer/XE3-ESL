package evaluation

import (
	"strings"
	"testing"
)

func TestOpaqueIdentifierAcceptsDigitLeadingPracticeUUID(t *testing.T) {
	t.Parallel()
	if !validIdentifier("20000000-0000-4000-8000-000000000001") {
		t.Fatal("digit-leading Practice UUID was rejected")
	}
	for _, invalid := range []string{
		"",
		"-unsafe",
		"contains/slash",
		"contains space",
		"control\x00inside",
		strings.Repeat("a", 129),
	} {
		if validIdentifier(invalid) {
			t.Errorf("invalid opaque identifier %q was accepted", invalid)
		}
	}
}

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
