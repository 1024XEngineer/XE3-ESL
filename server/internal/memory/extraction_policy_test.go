package memory

import (
	"testing"
	"time"
)

func TestExtractionPolicyAcceptsOnlyExplicitSupportedFacts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 28, 8, 0, 0, 0, time.UTC)
	policy, err := NewExtractionPolicy(
		"memory-policy-v1",
		30*24*time.Hour,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewExtractionPolicy: %v", err)
	}
	source := validCompletedRunSource()
	source.UserText = "Call me Alex. I like running. Let's discuss salary negotiation."
	output := ExtractionOutput{Candidates: []ExtractedCandidate{
		{
			Action:       CandidateUpsert,
			Type:         TypeIdentity,
			CanonicalKey: "identity.preferred_name",
			Content:      "Alex",
			Scope:        ScopeUser,
			Evidence:     "Call me Alex",
		},
		{
			Action:       CandidateUpsert,
			Type:         TypeInterest,
			CanonicalKey: "interest.running",
			Content:      "Enjoys running",
			Scope:        ScopeUser,
			Evidence:     "I like running",
		},
		{
			Action:       CandidateUpsert,
			Type:         TypeTopic,
			CanonicalKey: "topic.salary_negotiation",
			Content:      "Discussing salary negotiation",
			Scope:        ScopeUser,
			Evidence:     "salary negotiation",
		},
		{
			Action:       CandidateUpsert,
			Type:         TypeWeakness,
			CanonicalKey: "weakness.metrics",
			Content:      "Lacks metrics",
			Scope:        ScopeUser,
			Evidence:     "salary negotiation",
		},
		{
			Action:       CandidateUpsert,
			Type:         TypeProfile,
			CanonicalKey: "profile.assistant_claim",
			Content:      "Inferred by assistant",
			Scope:        ScopeUser,
			Evidence:     "not in user text",
		},
	}}
	batch, err := policy.Decide(source, output)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if batch.CandidateCount != 5 || len(batch.Decisions) != 3 {
		t.Fatalf("batch = %#v", batch)
	}
	wantRejections := []CandidateRejection{
		{CandidateIndex: 3, Reason: RejectionUnsupportedType},
		{CandidateIndex: 4, Reason: RejectionEvidenceMismatch},
	}
	if !equalCandidateRejections(batch.Rejections, wantRejections) {
		t.Fatalf(
			"rejections = %#v, want %#v",
			batch.Rejections,
			wantRejections,
		)
	}
	topic := batch.Decisions[2]
	if topic.Type != TypeTopic ||
		topic.ExpiresAt == nil ||
		!topic.ExpiresAt.Equal(now.Add(30*24*time.Hour)) {
		t.Fatalf("topic decision = %#v", topic)
	}
	if batch.Source.SourceID != source.RunID ||
		batch.Source.Type != SourceAgentRun {
		t.Fatalf("source evidence = %#v", batch.Source)
	}
}

func TestExtractionPolicyRequiresMatterAndGenderUse(t *testing.T) {
	t.Parallel()

	policy, err := NewExtractionPolicy(
		"memory-policy-v1",
		30*24*time.Hour,
		time.Now,
	)
	if err != nil {
		t.Fatalf("NewExtractionPolicy: %v", err)
	}
	source := validCompletedRunSource()
	source.MatterID = ""
	source.UserText = "I am a woman preparing for a PM interview."
	output := ExtractionOutput{Candidates: []ExtractedCandidate{
		{
			Action:       CandidateUpsert,
			Type:         TypeIdentity,
			CanonicalKey: "identity.gender",
			Content:      "woman",
			Scope:        ScopeUser,
			Evidence:     "I am a woman",
		},
		{
			Action:         CandidateUpsert,
			Type:           TypeIdentity,
			CanonicalKey:   "identity.gender",
			Content:        "woman",
			Scope:          ScopeUser,
			Evidence:       "I am a woman",
			InteractionUse: true,
		},
		{
			Action:       CandidateUpsert,
			Type:         TypeGoal,
			CanonicalKey: "goal.current",
			Content:      "Prepare for a PM interview",
			Scope:        ScopeMatter,
			Evidence:     "preparing for a PM interview",
		},
	}}
	batch, err := policy.Decide(source, output)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if len(batch.Decisions) != 1 ||
		batch.Decisions[0].CanonicalKey != "identity.gender" {
		t.Fatalf("decisions = %#v", batch.Decisions)
	}
	wantRejections := []CandidateRejection{
		{
			CandidateIndex: 0,
			Reason:         RejectionGenderInteractionUseRequired,
		},
		{CandidateIndex: 2, Reason: RejectionMissingMatter},
	}
	if !equalCandidateRejections(batch.Rejections, wantRejections) {
		t.Fatalf(
			"rejections = %#v, want %#v",
			batch.Rejections,
			wantRejections,
		)
	}
}

func TestExtractionPolicyRejectsSensitiveEvidence(t *testing.T) {
	t.Parallel()

	policy, err := NewExtractionPolicy(
		"memory-policy-v1",
		30*24*time.Hour,
		time.Now,
	)
	if err != nil {
		t.Fatalf("NewExtractionPolicy: %v", err)
	}
	source := validCompletedRunSource()
	source.UserText = "My password is hunter2."
	output := ExtractionOutput{Candidates: []ExtractedCandidate{{
		Action:       CandidateUpsert,
		Type:         TypeProfile,
		CanonicalKey: "profile.login_note",
		Content:      "hunter2",
		Scope:        ScopeUser,
		Evidence:     "My password is hunter2",
	}}}

	batch, err := policy.Decide(source, output)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if len(batch.Decisions) != 0 {
		t.Fatalf("sensitive evidence decisions = %#v", batch.Decisions)
	}
	if !equalCandidateRejections(
		batch.Rejections,
		[]CandidateRejection{{
			CandidateIndex: 0,
			Reason:         RejectionSensitiveCandidate,
		}},
	) {
		t.Fatalf("sensitive evidence rejections = %#v", batch.Rejections)
	}
}

func TestExtractionPolicyClassifiesEveryCandidateRejection(t *testing.T) {
	t.Parallel()

	policy, err := NewExtractionPolicy(
		"memory-policy-v1",
		30*24*time.Hour,
		time.Now,
	)
	if err != nil {
		t.Fatalf("NewExtractionPolicy: %v", err)
	}
	baseSource := validCompletedRunSource()
	baseSource.MatterID = ""
	baseSource.UserText = "Call me Alex."
	baseCandidate := ExtractedCandidate{
		Action:       CandidateUpsert,
		Type:         TypeIdentity,
		CanonicalKey: "identity.preferred_name",
		Content:      "Alex",
		Scope:        ScopeUser,
		Evidence:     "Call me Alex",
	}
	tests := map[string]struct {
		candidate ExtractedCandidate
		want      CandidateRejectionReason
	}{
		"invalid action": {
			candidate: func() ExtractedCandidate {
				item := baseCandidate
				item.Action = CandidateAction("replace")
				return item
			}(),
			want: RejectionInvalidAction,
		},
		"unsupported type": {
			candidate: func() ExtractedCandidate {
				item := baseCandidate
				item.Type = TypeWeakness
				item.CanonicalKey = "weakness.name"
				return item
			}(),
			want: RejectionUnsupportedType,
		},
		"invalid canonical key": {
			candidate: func() ExtractedCandidate {
				item := baseCandidate
				item.CanonicalKey = "Identity.Name"
				return item
			}(),
			want: RejectionInvalidCanonicalKey,
		},
		"incompatible key": {
			candidate: func() ExtractedCandidate {
				item := baseCandidate
				item.Type = TypeProfile
				return item
			}(),
			want: RejectionIncompatibleKey,
		},
		"evidence mismatch": {
			candidate: func() ExtractedCandidate {
				item := baseCandidate
				item.Evidence = "My name is Alex"
				return item
			}(),
			want: RejectionEvidenceMismatch,
		},
		"sensitive candidate": {
			candidate: func() ExtractedCandidate {
				item := baseCandidate
				item.Content = "secret"
				return item
			}(),
			want: RejectionSensitiveCandidate,
		},
		"gender requires interaction use": {
			candidate: func() ExtractedCandidate {
				item := baseCandidate
				item.CanonicalKey = "identity.gender"
				return item
			}(),
			want: RejectionGenderInteractionUseRequired,
		},
		"missing Matter": {
			candidate: func() ExtractedCandidate {
				item := baseCandidate
				item.Scope = ScopeMatter
				return item
			}(),
			want: RejectionMissingMatter,
		},
		"invalid scope": {
			candidate: func() ExtractedCandidate {
				item := baseCandidate
				item.Scope = ScopeType("thread")
				return item
			}(),
			want: RejectionInvalidScope,
		},
		"invalid content": {
			candidate: func() ExtractedCandidate {
				item := baseCandidate
				item.Content = " Alex "
				return item
			}(),
			want: RejectionInvalidContent,
		},
		"inactivate content not empty": {
			candidate: func() ExtractedCandidate {
				item := baseCandidate
				item.Action = CandidateInactivate
				return item
			}(),
			want: RejectionInactivateContentNotEmpty,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			batch, decideErr := policy.Decide(
				baseSource,
				ExtractionOutput{
					Candidates: []ExtractedCandidate{test.candidate},
				},
			)
			if decideErr != nil {
				t.Fatalf("Decide: %v", decideErr)
			}
			want := []CandidateRejection{{
				CandidateIndex: 0,
				Reason:         test.want,
			}}
			if !equalCandidateRejections(batch.Rejections, want) {
				t.Fatalf(
					"rejections = %#v, want %#v",
					batch.Rejections,
					want,
				)
			}
		})
	}

	duplicates, err := policy.Decide(baseSource, ExtractionOutput{
		Candidates: []ExtractedCandidate{baseCandidate, baseCandidate},
	})
	if err != nil {
		t.Fatalf("Decide duplicates: %v", err)
	}
	if len(duplicates.Decisions) != 1 ||
		!equalCandidateRejections(
			duplicates.Rejections,
			[]CandidateRejection{{
				CandidateIndex: 1,
				Reason:         RejectionDuplicateCandidate,
			}},
		) {
		t.Fatalf("duplicate batch = %#v", duplicates)
	}
}

func equalCandidateRejections(
	got []CandidateRejection,
	want []CandidateRejection,
) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range want {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func validCompletedRunSource() CompletedRunSource {
	return CompletedRunSource{
		OwnerID:            integrationUserA,
		RunID:              "a3000000-0000-4000-8000-000000000001",
		ThreadID:           "a4000000-0000-4000-8000-000000000001",
		InputMessageID:     "a5000000-0000-4000-8000-000000000001",
		AssistantMessageID: "a6000000-0000-4000-8000-000000000001",
		MatterID:           integrationMatterA,
		UserText:           "I am a Java backend engineer.",
		AssistantText:      "Thanks for sharing.",
		Attempt:            1,
		CompletedAt:        time.Now().UTC(),
	}
}
