package preparation

import "encoding/json"

const maxResumeMaterialBytes = 512 * 1024

// ResumeMaterial is the immutable structured content extracted from the
// optional PDF owned by one InterviewPreparation.
type ResumeMaterial struct {
	TargetPosition       string                      `json:"target_position"`
	ProfessionalSummary  string                      `json:"professional_summary"`
	WorkExperiences      []ResumeWorkExperience      `json:"work_experiences"`
	ProjectExperiences   []ResumeProjectExperience   `json:"project_experiences"`
	EducationExperiences []ResumeEducationExperience `json:"education_experiences"`
	Skills               []string                    `json:"skills"`
	Awards               []string                    `json:"awards"`
}

type ResumeWorkExperience struct {
	Company      string   `json:"company"`
	Position     string   `json:"position"`
	StartDate    string   `json:"start_date,omitempty"`
	EndDate      string   `json:"end_date,omitempty"`
	Duties       []string `json:"duties"`
	Achievements []string `json:"achievements"`
}

type ResumeProjectExperience struct {
	ProjectName  string   `json:"project_name"`
	Role         string   `json:"role"`
	Description  string   `json:"description"`
	Technologies []string `json:"technologies"`
	Duties       []string `json:"duties"`
	Achievements []string `json:"achievements"`
}

type ResumeEducationExperience struct {
	School    string `json:"school"`
	Major     string `json:"major"`
	Degree    string `json:"degree"`
	GPA       string `json:"gpa,omitempty"`
	StartDate string `json:"start_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
}

func validResumeMaterial(material ResumeMaterial) bool {
	if material.WorkExperiences == nil ||
		material.ProjectExperiences == nil ||
		material.EducationExperiences == nil ||
		material.Skills == nil || material.Awards == nil {
		return false
	}
	for _, item := range material.WorkExperiences {
		if item.Duties == nil || item.Achievements == nil {
			return false
		}
	}
	for _, item := range material.ProjectExperiences {
		if item.Technologies == nil || item.Duties == nil ||
			item.Achievements == nil {
			return false
		}
	}
	encoded, err := json.Marshal(material)
	return err == nil && len(encoded) <= maxResumeMaterialBytes
}

func ValidResumeMaterial(material ResumeMaterial) bool {
	return validResumeMaterial(material)
}

func cloneResumeMaterial(material ResumeMaterial) ResumeMaterial {
	result := material
	result.Skills = append([]string{}, material.Skills...)
	result.Awards = append([]string{}, material.Awards...)
	result.WorkExperiences = append(
		[]ResumeWorkExperience{},
		material.WorkExperiences...,
	)
	for index := range result.WorkExperiences {
		result.WorkExperiences[index].Duties = append(
			[]string{},
			material.WorkExperiences[index].Duties...,
		)
		result.WorkExperiences[index].Achievements = append(
			[]string{},
			material.WorkExperiences[index].Achievements...,
		)
	}
	result.ProjectExperiences = append(
		[]ResumeProjectExperience{},
		material.ProjectExperiences...,
	)
	for index := range result.ProjectExperiences {
		result.ProjectExperiences[index].Technologies = append(
			[]string{},
			material.ProjectExperiences[index].Technologies...,
		)
		result.ProjectExperiences[index].Duties = append(
			[]string{},
			material.ProjectExperiences[index].Duties...,
		)
		result.ProjectExperiences[index].Achievements = append(
			[]string{},
			material.ProjectExperiences[index].Achievements...,
		)
	}
	result.EducationExperiences = append(
		[]ResumeEducationExperience{},
		material.EducationExperiences...,
	)
	return result
}

func CloneResumeMaterial(material ResumeMaterial) ResumeMaterial {
	return cloneResumeMaterial(material)
}
