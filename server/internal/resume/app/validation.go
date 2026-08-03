// 本文件集中校验 Resume 应用层输入和结构化内容边界。
package app

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
)

var (
	applicationUUIDPattern = regexp.MustCompile(`\A[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\z`)
	idempotencyKeyPattern  = regexp.MustCompile(`\A[A-Za-z0-9._~+/-]{8,128}\z`)
	workerFailurePattern   = regexp.MustCompile(`\A[a-z][a-z0-9_]{0,127}\z`)
)

// validServiceCall 校验应用服务依赖、上下文和可信 Actor。
func validServiceCall(service *Service, ctx interface{ Err() error }, actor requestcontext.Actor) bool {
	return service != nil && service.repository != nil && service.storage != nil &&
		service.ids != nil && ctx != nil && ctx.Err() == nil && actor.Valid() &&
		validUUID(actor.UserID)
}

// validUUID 校验公开资源标识是规范小写 UUID。
func validUUID(value string) bool {
	return applicationUUIDPattern.MatchString(value)
}

// validIdempotencyKey 校验幂等键符合公开 API 契约。
func validIdempotencyKey(value string) bool {
	return idempotencyKeyPattern.MatchString(value)
}

// validTitle 校验简历展示名称。
func validTitle(value string) bool {
	return validTrimmedText(value, 1, 120) && !strings.ContainsAny(value, "\r\n\x00")
}

// validFilename 校验上传文件名不包含路径或控制字符。
func validFilename(value string) bool {
	return validTrimmedText(value, 1, 255) &&
		!strings.ContainsAny(value, "\r\n\x00/\\") &&
		strings.EqualFold(value[max(0, len(value)-4):], ".pdf")
}

// validContent 校验结构化简历内容满足 OpenAPI 数量和长度限制。
func validContent(content resume.Content) bool {
	if !validOptionalText(content.TargetPosition, 200) ||
		!validOptionalText(content.ProfessionalSummary, 4000) ||
		len(content.WorkExperiences) > 30 || len(content.ProjectExperiences) > 50 ||
		len(content.EducationExperiences) > 20 || !validUniqueItems(content.Skills, 100, 100) {
		return false
	}
	for _, item := range content.WorkExperiences {
		if !validOptionalText(item.Company, 200) || !validOptionalText(item.Position, 200) ||
			!validOptionalText(item.StartDate, 32) || !validOptionalText(item.EndDate, 32) ||
			!validItems(item.Duties, 100, 1000) || !validItems(item.Achievements, 100, 1000) {
			return false
		}
	}
	for _, item := range content.ProjectExperiences {
		if !validOptionalText(item.ProjectName, 200) || !validOptionalText(item.Role, 200) ||
			!validOptionalText(item.Description, 4000) ||
			!validUniqueItems(item.Technologies, 100, 100) ||
			!validItems(item.Duties, 100, 1000) || !validItems(item.Achievements, 100, 1000) {
			return false
		}
	}
	for _, item := range content.EducationExperiences {
		if !validOptionalText(item.School, 200) || !validOptionalText(item.Major, 200) ||
			!validOptionalText(item.Degree, 100) || !validOptionalText(item.StartDate, 32) ||
			!validOptionalText(item.EndDate, 32) {
			return false
		}
	}
	return true
}

// normalizeContent 保证公开响应中的数组使用空数组而不是 null。
func normalizeContent(content resume.Content) resume.Content {
	if content.WorkExperiences == nil {
		content.WorkExperiences = []resume.WorkExperience{}
	}
	if content.ProjectExperiences == nil {
		content.ProjectExperiences = []resume.ProjectExperience{}
	}
	if content.EducationExperiences == nil {
		content.EducationExperiences = []resume.EducationExperience{}
	}
	if content.Skills == nil {
		content.Skills = []string{}
	}
	for index := range content.WorkExperiences {
		if content.WorkExperiences[index].Duties == nil {
			content.WorkExperiences[index].Duties = []string{}
		}
		if content.WorkExperiences[index].Achievements == nil {
			content.WorkExperiences[index].Achievements = []string{}
		}
	}
	for index := range content.ProjectExperiences {
		if content.ProjectExperiences[index].Technologies == nil {
			content.ProjectExperiences[index].Technologies = []string{}
		}
		if content.ProjectExperiences[index].Duties == nil {
			content.ProjectExperiences[index].Duties = []string{}
		}
		if content.ProjectExperiences[index].Achievements == nil {
			content.ProjectExperiences[index].Achievements = []string{}
		}
	}
	return content
}

// validItems 校验普通字符串数组的数量和单项长度。
func validItems(items []string, maximumItems int, maximumLength int) bool {
	if len(items) > maximumItems {
		return false
	}
	for _, item := range items {
		if !validTrimmedText(item, 1, maximumLength) {
			return false
		}
	}
	return true
}

// validUniqueItems 校验去重字符串数组。
func validUniqueItems(items []string, maximumItems int, maximumLength int) bool {
	if !validItems(items, maximumItems, maximumLength) {
		return false
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		key := strings.ToLower(item)
		if _, exists := seen[key]; exists {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

// validOptionalText 校验允许为空的结构化文本。
func validOptionalText(value string, maximum int) bool {
	return value == "" || validTrimmedText(value, 1, maximum)
}

// validTrimmedText 校验 UTF-8、首尾空白和字符数量。
func validTrimmedText(value string, minimum int, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && strings.TrimSpace(value) == value &&
		length >= minimum && length <= maximum && !strings.ContainsRune(value, '\x00')
}
