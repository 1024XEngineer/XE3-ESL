package voice

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

func TestMapPracticeSessionProjectsFrozenInterviewContext(t *testing.T) {
	bootstrap := turnPolicySessionBootstrap(practice.GenericPracticeTurnPolicy)
	bootstrap.Snapshot.Preparation = interviewPreparationSnapshotFixture(true)

	mapped, err := mapPracticeSession(bootstrap, "user-1")
	if err != nil {
		t.Fatalf("mapPracticeSession: %v", err)
	}
	if mapped.InterviewContext == nil ||
		mapped.InterviewContext.JobTarget.JobTitle != "Senior Go Engineer" ||
		mapped.InterviewContext.Resume == nil ||
		mapped.InterviewContext.Resume.WorkExperiences[0].Company != "Example Co" {
		t.Fatalf("InterviewContext = %#v", mapped.InterviewContext)
	}

	bootstrap.Snapshot.Preparation.JobTargetCandidateSnapshot.JobTitle = "changed"
	bootstrap.Snapshot.Preparation.ResumeSnapshot.Material.
		WorkExperiences[0].Company = "changed"
	if mapped.InterviewContext.JobTarget.JobTitle != "Senior Go Engineer" ||
		mapped.InterviewContext.Resume.WorkExperiences[0].Company != "Example Co" {
		t.Fatalf("InterviewContext was not frozen: %#v", mapped.InterviewContext)
	}

	encoded, err := json.Marshal(mapped.InterviewContext)
	if err != nil {
		t.Fatalf("marshal InterviewContext: %v", err)
	}
	for _, privateValue := range []string{
		"job-target-private-id",
		"resume-private-id",
		"catalog-private-scene",
	} {
		if strings.Contains(string(encoded), privateValue) {
			t.Fatalf("private value %q leaked in %s", privateValue, encoded)
		}
	}
}

func TestMapPracticeSessionProjectsInterviewContextWithoutResume(t *testing.T) {
	bootstrap := turnPolicySessionBootstrap(practice.GenericPracticeTurnPolicy)
	bootstrap.Snapshot.Preparation = interviewPreparationSnapshotFixture(false)

	mapped, err := mapPracticeSession(bootstrap, "user-1")
	if err != nil || mapped.InterviewContext == nil ||
		mapped.InterviewContext.Resume != nil {
		t.Fatalf("mapped = %#v, error = %v", mapped.InterviewContext, err)
	}
}

func TestInterviewPreparationFeedsInitialAndFollowUpQuestions(t *testing.T) {
	for _, test := range []struct {
		name      string
		policy    string
		sequence  int
		response  string
		configure func(*Session, *turnPolicyQuestionRepository)
	}{
		{
			name:      "generic interview initial question",
			policy:    practice.GenericPracticeTurnPolicy,
			sequence:  1,
			response:  "How would you apply Go concurrency to this role?",
			configure: func(_ *Session, _ *turnPolicyQuestionRepository) {},
		},
		{
			name:     "project deep dive follow-up",
			policy:   practice.InterviewProjectDeepDiveTurnPolicy,
			sequence: 2,
			response: `{"question_type":"FOLLOW_UP","content":"How did that launch prepare you for this role?"}`,
			configure: func(
				session *Session,
				repository *turnPolicyQuestionRepository,
			) {
				session.EffectiveTurns = 1
				session.PreviousQuestion = "Tell me about the launch."
				session.PreviousUserResponse = "I led it end to end."
				repository.history = []practice.Question{{
					ID:   "primary-1",
					Type: "PRIMARY",
				}}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := newTurnPolicyQuestionRepository()
			generator := &turnPolicyQuestionGenerator{response: test.response}
			session := sessionFixture()
			session.TurnPolicyRef = test.policy
			session.InterviewContext = interviewConversationContextFixture()
			test.configure(&session, repository)

			_, err := (&questionAdapter{
				repository: repository,
				generator:  generator,
			}).EnsureQuestion(
				context.Background(),
				persistenceRequestActor(),
				session,
				test.sequence,
			)
			if err != nil {
				t.Fatalf("EnsureQuestion: %v", err)
			}
			if !strings.Contains(
				generator.request.SystemPrompt,
				"authoritative, frozen interview facts",
			) || !strings.Contains(
				generator.request.UserPrompt,
				`"job_title":"Senior Go Engineer"`,
			) || !strings.Contains(
				generator.request.UserPrompt,
				`"professional_summary":"Backend engineer"`,
			) || strings.Contains(
				generator.request.UserPrompt,
				"resume-private-id",
			) {
				t.Fatalf("generation request = %#v", generator.request)
			}
		})
	}
}

func TestInterviewConversationContextIsBounded(t *testing.T) {
	snapshot := interviewPreparationSnapshotFixture(true)
	longText := strings.Repeat("长", 20000)
	snapshot.JobTargetInputSnapshot.JobDescription = longText
	snapshot.ResumeSnapshot.Material.ProjectExperiences = make(
		[]practice.ResumeProjectExperience,
		20,
	)
	for index := range snapshot.ResumeSnapshot.Material.ProjectExperiences {
		snapshot.ResumeSnapshot.Material.ProjectExperiences[index] =
			practice.ResumeProjectExperience{
				ProjectName:  longText,
				Description:  longText,
				Technologies: []string{longText},
				Duties:       []string{longText},
				Achievements: []string{longText},
			}
	}

	projected, err := projectInterviewConversationContext(snapshot)
	if err != nil {
		t.Fatalf("projectInterviewConversationContext: %v", err)
	}
	encoded, err := json.Marshal(projected)
	if err != nil || len(encoded) > maxInterviewConversationContextBytes {
		t.Fatalf("encoded bytes = %d, error = %v", len(encoded), err)
	}
	if len(projected.Resume.ProjectExperiences) != 5 ||
		len([]rune(projected.JobTarget.JobDescription)) != 6000 {
		t.Fatalf("bounded context = %#v", projected)
	}
}

func TestInterviewConversationContextShrinksRichResumeToBudget(t *testing.T) {
	snapshot := interviewPreparationSnapshotFixture(true)
	longText := strings.Repeat("detail", 100)
	snapshot.ResumeSnapshot.Material.WorkExperiences = make(
		[]practice.ResumeWorkExperience,
		5,
	)
	for index := range snapshot.ResumeSnapshot.Material.WorkExperiences {
		snapshot.ResumeSnapshot.Material.WorkExperiences[index] =
			practice.ResumeWorkExperience{
				Company:      longText,
				Position:     longText,
				Duties:       repeatedInterviewStrings(longText, 8),
				Achievements: repeatedInterviewStrings(longText, 8),
			}
	}
	snapshot.ResumeSnapshot.Material.ProjectExperiences = make(
		[]practice.ResumeProjectExperience,
		5,
	)
	for index := range snapshot.ResumeSnapshot.Material.ProjectExperiences {
		snapshot.ResumeSnapshot.Material.ProjectExperiences[index] =
			practice.ResumeProjectExperience{
				ProjectName:  longText,
				Role:         longText,
				Description:  strings.Repeat("description", 200),
				Technologies: repeatedInterviewStrings(longText, 20),
				Duties:       repeatedInterviewStrings(longText, 8),
				Achievements: repeatedInterviewStrings(longText, 8),
			}
	}

	projected, err := projectInterviewConversationContext(snapshot)
	if err != nil {
		t.Fatalf("projectInterviewConversationContext: %v", err)
	}
	encoded, err := json.Marshal(projected)
	if err != nil || len(encoded) > maxInterviewConversationContextBytes {
		t.Fatalf("encoded bytes = %d, error = %v", len(encoded), err)
	}
	if projected.JobTarget.JobTitle != "Senior Go Engineer" ||
		projected.Resume == nil ||
		len(projected.Resume.WorkExperiences) != 5 ||
		len(projected.Resume.ProjectExperiences) != 5 {
		t.Fatalf("essential context was lost: %#v", projected)
	}
}

func repeatedInterviewStrings(value string, count int) []string {
	result := make([]string, count)
	for index := range result {
		result[index] = value
	}
	return result
}

func interviewPreparationSnapshotFixture(
	withResume bool,
) practice.PreparationSnapshot {
	result := practice.PreparationSnapshot{
		ID:                 "preparation-snapshot-1",
		SourceProfileID:    "preparation-profile-1",
		SourceVersion:      1,
		Kind:               "interview",
		SourceJobTargetID:  "job-target-private-id",
		BackgroundSnapshot: "Prepare for a backend interview.",
		JobTargetInputSnapshot: &practice.JobTargetInput{
			JobTitle:            "Senior Go Engineer",
			JobDescription:      "Build reliable distributed services.",
			Company:             "Target Co",
			Seniority:           "Senior",
			CandidateBackground: "Backend engineer",
			PracticeFocus:       "System ownership and communication",
		},
		JobTargetCandidateSnapshot: &practice.JobTargetCandidate{
			JobTitle:         "Senior Go Engineer",
			Seniority:        "Senior",
			Responsibilities: []string{"Own distributed Go services"},
			CoreSkills:       []string{"Go", "PostgreSQL"},
			CommunicationFocus: []string{
				"Explain technical tradeoffs",
			},
			PracticeGoals: []string{"Demonstrate end-to-end ownership"},
			CatalogRecommendation: practice.JobTargetCatalogRecommendation{
				SceneID: "catalog-private-scene",
			},
		},
	}
	if withResume {
		result.ResumeSnapshot = &practice.ResumeRevisionSnapshot{
			ResumeID: "resume-private-id",
			Revision: 3,
			Material: practice.ResumeMaterial{
				TargetPosition:      "Senior Go Engineer",
				ProfessionalSummary: "Backend engineer",
				WorkExperiences: []practice.ResumeWorkExperience{{
					Company:      "Example Co",
					Position:     "Backend Engineer",
					Achievements: []string{"Led a zero-downtime launch"},
				}},
				ProjectExperiences: []practice.ResumeProjectExperience{{
					ProjectName: "Payments platform",
					Role:        "Technical lead",
				}},
				EducationExperiences: []practice.ResumeEducationExperience{{
					School: "Example University",
					Degree: "Bachelor",
				}},
				Skills: []string{"Go", "PostgreSQL"},
			},
		}
	}
	return result
}

func interviewConversationContextFixture() *InterviewConversationContext {
	projected, err := projectInterviewConversationContext(
		interviewPreparationSnapshotFixture(true),
	)
	if err != nil {
		panic(err)
	}
	return projected
}
