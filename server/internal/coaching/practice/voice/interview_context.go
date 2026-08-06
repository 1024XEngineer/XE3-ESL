package voice

import (
	"encoding/json"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

const maxInterviewConversationContextBytes = 96 * 1024

// InterviewConversationContext is the private, data-only projection consumed
// by question generation. It deliberately excludes Preparation resource IDs,
// Resume IDs, revisions, and catalog routing metadata.
type InterviewConversationContext struct {
	Background string                    `json:"background,omitempty"`
	JobTarget  InterviewJobTargetContext `json:"job_target"`
	Resume     *InterviewResumeContext   `json:"resume,omitempty"`
}

type InterviewJobTargetContext struct {
	JobTitle            string   `json:"job_title"`
	Company             string   `json:"company,omitempty"`
	Seniority           string   `json:"seniority,omitempty"`
	JobDescription      string   `json:"job_description,omitempty"`
	CandidateBackground string   `json:"candidate_background,omitempty"`
	PracticeFocus       string   `json:"practice_focus,omitempty"`
	GeneralAdviceOnly   bool     `json:"general_advice_only"`
	Responsibilities    []string `json:"responsibilities,omitempty"`
	CoreSkills          []string `json:"core_skills,omitempty"`
	CommunicationFocus  []string `json:"communication_focus,omitempty"`
	PracticeGoals       []string `json:"practice_goals,omitempty"`
	ScopeNotice         string   `json:"scope_notice,omitempty"`
}

type InterviewResumeContext struct {
	TargetPosition       string                               `json:"target_position,omitempty"`
	ProfessionalSummary  string                               `json:"professional_summary,omitempty"`
	WorkExperiences      []InterviewResumeWorkExperience      `json:"work_experiences,omitempty"`
	ProjectExperiences   []InterviewResumeProjectExperience   `json:"project_experiences,omitempty"`
	EducationExperiences []InterviewResumeEducationExperience `json:"education_experiences,omitempty"`
	Skills               []string                             `json:"skills,omitempty"`
	Awards               []string                             `json:"awards,omitempty"`
}

type InterviewResumeWorkExperience struct {
	Company      string   `json:"company,omitempty"`
	Position     string   `json:"position,omitempty"`
	StartDate    string   `json:"start_date,omitempty"`
	EndDate      string   `json:"end_date,omitempty"`
	Duties       []string `json:"duties,omitempty"`
	Achievements []string `json:"achievements,omitempty"`
}

type InterviewResumeProjectExperience struct {
	ProjectName  string   `json:"project_name,omitempty"`
	Role         string   `json:"role,omitempty"`
	Description  string   `json:"description,omitempty"`
	Technologies []string `json:"technologies,omitempty"`
	Duties       []string `json:"duties,omitempty"`
	Achievements []string `json:"achievements,omitempty"`
}

type InterviewResumeEducationExperience struct {
	School    string `json:"school,omitempty"`
	Major     string `json:"major,omitempty"`
	Degree    string `json:"degree,omitempty"`
	GPA       string `json:"gpa,omitempty"`
	StartDate string `json:"start_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
}

func projectInterviewConversationContext(
	snapshot practice.PreparationSnapshot,
) (*InterviewConversationContext, error) {
	input := snapshot.JobTargetInputSnapshot
	candidate := snapshot.JobTargetCandidateSnapshot
	if input == nil && candidate == nil && snapshot.ResumeSnapshot == nil {
		return nil, nil
	}
	if input == nil || candidate == nil {
		return nil, ErrInvalidContext
	}
	result := &InterviewConversationContext{
		Background: interviewText(snapshot.BackgroundSnapshot, 2000),
		JobTarget: InterviewJobTargetContext{
			JobTitle:            interviewText(candidate.JobTitle, 256),
			Company:             interviewText(input.Company, 256),
			Seniority:           interviewText(candidate.Seniority, 256),
			JobDescription:      interviewText(input.JobDescription, 6000),
			CandidateBackground: interviewText(input.CandidateBackground, 2000),
			PracticeFocus:       interviewText(input.PracticeFocus, 2000),
			GeneralAdviceOnly:   candidate.GeneralAdviceOnly,
			Responsibilities: interviewStrings(
				candidate.Responsibilities, 12, 512,
			),
			CoreSkills: interviewStrings(candidate.CoreSkills, 20, 256),
			CommunicationFocus: interviewStrings(
				candidate.CommunicationFocus, 12, 512,
			),
			PracticeGoals: interviewStrings(candidate.PracticeGoals, 12, 512),
			ScopeNotice:   interviewText(candidate.ScopeNotice, 1000),
		},
	}
	if result.JobTarget.JobTitle == "" {
		result.JobTarget.JobTitle = interviewText(input.JobTitle, 256)
	}
	if result.JobTarget.Seniority == "" {
		result.JobTarget.Seniority = interviewText(input.Seniority, 256)
	}
	if snapshot.ResumeSnapshot != nil {
		result.Resume = projectInterviewResume(
			snapshot.ResumeSnapshot.Material,
		)
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > maxInterviewConversationContextBytes ||
		result.JobTarget.JobTitle == "" {
		return nil, ErrInvalidContext
	}
	return result, nil
}

func projectInterviewResume(
	material practice.ResumeMaterial,
) *InterviewResumeContext {
	result := &InterviewResumeContext{
		TargetPosition:      interviewText(material.TargetPosition, 256),
		ProfessionalSummary: interviewText(material.ProfessionalSummary, 2000),
		Skills:              interviewStrings(material.Skills, 30, 256),
		Awards:              interviewStrings(material.Awards, 10, 512),
	}
	for _, source := range material.WorkExperiences[:min(
		len(material.WorkExperiences), 5,
	)] {
		result.WorkExperiences = append(
			result.WorkExperiences,
			InterviewResumeWorkExperience{
				Company:      interviewText(source.Company, 256),
				Position:     interviewText(source.Position, 256),
				StartDate:    interviewText(source.StartDate, 64),
				EndDate:      interviewText(source.EndDate, 64),
				Duties:       interviewStrings(source.Duties, 8, 512),
				Achievements: interviewStrings(source.Achievements, 8, 512),
			},
		)
	}
	for _, source := range material.ProjectExperiences[:min(
		len(material.ProjectExperiences), 5,
	)] {
		result.ProjectExperiences = append(
			result.ProjectExperiences,
			InterviewResumeProjectExperience{
				ProjectName:  interviewText(source.ProjectName, 256),
				Role:         interviewText(source.Role, 256),
				Description:  interviewText(source.Description, 1500),
				Technologies: interviewStrings(source.Technologies, 20, 256),
				Duties:       interviewStrings(source.Duties, 8, 512),
				Achievements: interviewStrings(source.Achievements, 8, 512),
			},
		)
	}
	for _, source := range material.EducationExperiences[:min(
		len(material.EducationExperiences), 3,
	)] {
		result.EducationExperiences = append(
			result.EducationExperiences,
			InterviewResumeEducationExperience{
				School:    interviewText(source.School, 256),
				Major:     interviewText(source.Major, 256),
				Degree:    interviewText(source.Degree, 128),
				GPA:       interviewText(source.GPA, 64),
				StartDate: interviewText(source.StartDate, 64),
				EndDate:   interviewText(source.EndDate, 64),
			},
		)
	}
	return result
}

func interviewText(value string, maximumRunes int) string {
	if maximumRunes < 1 {
		return ""
	}
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= maximumRunes {
		return value
	}
	if maximumRunes == 1 {
		return string(runes[:maximumRunes])
	}
	return string(runes[:maximumRunes-1]) + "…"
}

func interviewStrings(
	values []string,
	maximumItems int,
	maximumRunes int,
) []string {
	if maximumItems < 1 || maximumRunes < 1 {
		return nil
	}
	result := make([]string, 0, min(len(values), maximumItems))
	for _, value := range values {
		value = interviewText(value, maximumRunes)
		if value == "" {
			continue
		}
		result = append(result, value)
		if len(result) == maximumItems {
			break
		}
	}
	return result
}
