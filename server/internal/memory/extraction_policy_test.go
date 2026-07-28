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
