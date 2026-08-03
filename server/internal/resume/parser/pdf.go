// Package parser 从文本型 PDF 提取文本并生成受限的结构化简历内容。
package parser

import (
	"bytes"
	"context"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	pdf "github.com/ledongthuc/pdf"

	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
)

const (
	parserVersion    = "pdf-text/v1"
	maximumPDFBytes  = 10 * 1024 * 1024
	maximumTextBytes = 512 * 1024
)

var targetPositionPattern = regexp.MustCompile(
	`(?i)^(?:target\s+position|position\s+applied|desired\s+position|求职意向|目标岗位|应聘岗位)\s*[:：]\s*(.+)$`,
)

// Failure 表示 Worker 可以安全持久化的稳定解析失败类别。
type Failure struct {
	code string
}

// Error 返回不包含原始简历文本的安全错误描述。
func (failure *Failure) Error() string {
	if failure == nil {
		return "resume PDF parsing failed"
	}
	return "resume PDF parsing failed: " + failure.code
}

// FailureCode 返回可持久化的稳定失败码。
func (failure *Failure) FailureCode() string {
	if failure == nil || failure.code == "" {
		return "pdf_parse_failed"
	}
	return failure.code
}

// PDFParser 解析不需要 OCR 的文本型 PDF。
type PDFParser struct{}

// NewPDFParser 创建文本型 PDF 解析器。
func NewPDFParser() *PDFParser {
	return &PDFParser{}
}

// Version 返回写入 PARSER revision 的解析器版本。
func (*PDFParser) Version() string {
	return parserVersion
}

// Parse 提取 PDF 文本并按常见章节标题生成结构化字段。
func (parser *PDFParser) Parse(
	ctx context.Context,
	reader io.Reader,
) (content resume.Content, err error) {
	defer func() {
		if recover() != nil {
			content = resume.Content{}
			err = &Failure{code: "pdf_invalid"}
		}
	}()
	if parser == nil || ctx == nil || reader == nil || ctx.Err() != nil {
		return resume.Content{}, &Failure{code: "pdf_parse_failed"}
	}
	body, err := io.ReadAll(io.LimitReader(reader, maximumPDFBytes+1))
	if err != nil {
		return resume.Content{}, &Failure{code: "pdf_read_failed"}
	}
	if len(body) < 5 || len(body) > maximumPDFBytes || !bytes.Equal(body[:5], []byte("%PDF-")) {
		return resume.Content{}, &Failure{code: "pdf_invalid"}
	}
	document, err := pdf.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return resume.Content{}, &Failure{code: "pdf_invalid"}
	}
	plainReader, err := document.GetPlainText()
	if err != nil {
		return resume.Content{}, &Failure{code: "pdf_text_unavailable"}
	}
	plain, err := io.ReadAll(io.LimitReader(plainReader, maximumTextBytes+1))
	if err != nil || len(plain) > maximumTextBytes {
		return resume.Content{}, &Failure{code: "pdf_text_unavailable"}
	}
	if ctx.Err() != nil {
		return resume.Content{}, &Failure{code: "pdf_parse_failed"}
	}
	text := normalizeExtractedText(string(plain))
	if visibleRuneCount(text) < 20 {
		return resume.Content{}, &Failure{code: "pdf_text_unavailable"}
	}
	return structureText(text), nil
}

// normalizeExtractedText 清理提取器产生的空白和不可见控制字符。
func normalizeExtractedText(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	lines := strings.Split(raw, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Map(func(value rune) rune {
			if value == '\t' {
				return ' '
			}
			if unicode.IsControl(value) {
				return -1
			}
			return value
		}, line)
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return strings.Join(cleaned, "\n")
}

// visibleRuneCount 返回非空白字符数量，用于识别扫描件或空 PDF。
func visibleRuneCount(value string) int {
	count := 0
	for _, character := range value {
		if !unicode.IsSpace(character) {
			count++
		}
	}
	return count
}

// structureText 按章节标题和项目符号生成稳定、边界受限的结构化内容。
func structureText(text string) resume.Content {
	sections := splitSections(strings.Split(text, "\n"))
	content := resume.Content{
		WorkExperiences:      []resume.WorkExperience{},
		ProjectExperiences:   []resume.ProjectExperience{},
		EducationExperiences: []resume.EducationExperience{},
		Skills:               []string{},
	}
	for _, line := range strings.Split(text, "\n") {
		matches := targetPositionPattern.FindStringSubmatch(line)
		if len(matches) == 2 {
			content.TargetPosition = truncate(matches[1], 200)
			break
		}
	}
	content.ProfessionalSummary = truncate(strings.Join(sections["summary"], " "), 4000)
	content.Skills = parseSkills(sections["skills"])
	if work := parseWork(sections["work"]); work != nil {
		content.WorkExperiences = append(content.WorkExperiences, *work)
	}
	if project := parseProject(sections["project"]); project != nil {
		content.ProjectExperiences = append(content.ProjectExperiences, *project)
	}
	if education := parseEducation(sections["education"]); education != nil {
		content.EducationExperiences = append(content.EducationExperiences, *education)
	}
	if content.ProfessionalSummary == "" {
		content.ProfessionalSummary = fallbackSummary(sections["other"])
	}
	return content
}

// splitSections 把常见中英文标题后的行归入结构化章节。
func splitSections(lines []string) map[string][]string {
	sections := map[string][]string{
		"other": {}, "summary": {}, "skills": {}, "work": {}, "project": {}, "education": {},
	}
	current := "other"
	for _, line := range lines {
		if section := sectionName(line); section != "" {
			current = section
			continue
		}
		if targetPositionPattern.MatchString(line) {
			continue
		}
		if len(sections[current]) < 150 {
			sections[current] = append(sections[current], truncate(line, 1000))
		}
	}
	return sections
}

// sectionName 识别常见中英文简历章节标题。
func sectionName(line string) string {
	normalized := strings.ToLower(strings.Trim(strings.TrimSpace(line), ":：-—"))
	switch normalized {
	case "summary", "profile", "professional summary", "个人简介", "自我介绍", "职业概述":
		return "summary"
	case "skills", "technical skills", "core skills", "专业技能", "技能", "核心技能":
		return "skills"
	case "experience", "work experience", "employment history", "工作经历", "工作经验":
		return "work"
	case "projects", "project experience", "selected projects", "项目经历", "项目经验":
		return "project"
	case "education", "education experience", "academic background", "教育经历", "教育背景":
		return "education"
	default:
		return ""
	}
}

// parseSkills 从技能章节按分隔符提取去重技能。
func parseSkills(lines []string) []string {
	seen := make(map[string]string)
	for _, line := range lines {
		line = strings.TrimLeft(line, "•·-* ")
		for _, skill := range regexp.MustCompile(`[,，;；|/]`).Split(line, -1) {
			skill = truncate(strings.TrimSpace(skill), 100)
			if skill == "" {
				continue
			}
			key := strings.ToLower(skill)
			if _, exists := seen[key]; !exists && len(seen) < 100 {
				seen[key] = skill
			}
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, seen[key])
	}
	return result
}

// parseWork 把工作章节投影为一条可继续手动编辑的工作经历。
func parseWork(lines []string) *resume.WorkExperience {
	if len(lines) == 0 {
		return nil
	}
	heading, bullets := headingAndBullets(lines)
	company, position := splitHeading(heading)
	return &resume.WorkExperience{
		Company:      truncate(company, 200),
		Position:     truncate(position, 200),
		Duties:       bullets,
		Achievements: []string{},
	}
}

// parseProject 把项目章节投影为一条可继续手动编辑的项目经历。
func parseProject(lines []string) *resume.ProjectExperience {
	if len(lines) == 0 {
		return nil
	}
	heading, bullets := headingAndBullets(lines)
	name, role := splitHeading(heading)
	description := ""
	if len(bullets) > 0 {
		description = truncate(bullets[0], 4000)
	}
	return &resume.ProjectExperience{
		ProjectName:  truncate(name, 200),
		Role:         truncate(role, 200),
		Description:  description,
		Technologies: []string{},
		Duties:       bullets,
		Achievements: []string{},
	}
}

// parseEducation 把教育章节投影为一条教育经历。
func parseEducation(lines []string) *resume.EducationExperience {
	if len(lines) == 0 {
		return nil
	}
	parts := splitParts(lines[0])
	item := &resume.EducationExperience{School: truncate(parts[0], 200)}
	if len(parts) > 1 {
		item.Major = truncate(parts[1], 200)
	}
	if len(parts) > 2 {
		item.Degree = truncate(parts[2], 100)
	}
	return item
}

// headingAndBullets 把章节首行作为标题，其余行作为职责条目。
func headingAndBullets(lines []string) (string, []string) {
	heading := strings.TrimLeft(lines[0], "•·-* ")
	bullets := make([]string, 0, len(lines)-1)
	for _, line := range lines[1:] {
		line = truncate(strings.TrimSpace(strings.TrimLeft(line, "•·-* ")), 1000)
		if line != "" && len(bullets) < 100 {
			bullets = append(bullets, line)
		}
	}
	return heading, bullets
}

// splitHeading 按简历常用分隔符拆分标题两端。
func splitHeading(value string) (string, string) {
	parts := splitParts(value)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

// splitParts 返回清理后的标题片段。
func splitParts(value string) []string {
	parts := regexp.MustCompile(`\s+(?:\||-|—|–)\s+|[|｜]`).Split(value, -1)
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	if len(cleaned) == 0 {
		return []string{""}
	}
	return cleaned
}

// fallbackSummary 从未分类文本中选择少量非联系方式行作为简介。
func fallbackSummary(lines []string) string {
	selected := make([]string, 0, 3)
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "@") || strings.Contains(lower, "phone") ||
			strings.Contains(lower, "电话") || visibleRuneCount(line) < 8 {
			continue
		}
		selected = append(selected, line)
		if len(selected) == 3 {
			break
		}
	}
	return truncate(strings.Join(selected, " "), 4000)
}

// truncate 按 Unicode 字符边界裁剪结构化字段。
func truncate(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if maximum < 1 || utf8.RuneCountInString(value) <= maximum {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:maximum]))
}

var _ error = (*Failure)(nil)
var _ interface{ FailureCode() string } = (*Failure)(nil)
