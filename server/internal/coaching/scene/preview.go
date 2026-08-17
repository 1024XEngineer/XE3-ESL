package scene

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

const MaxPreviewCatalogCandidates = 5

type PreviewCatalogCandidate struct {
	Scene          SceneDefinition
	DefaultRoleIDs []string
	DefaultOption  PracticeOption
}

type PreviewCatalogResolver interface {
	ResolvePreviewCatalog(
		context.Context,
		string,
	) ([]PreviewCatalogCandidate, error)
}

type CatalogPreviewResolver struct {
	catalog CatalogReader
}

func NewCatalogPreviewResolver(
	catalog CatalogReader,
) (*CatalogPreviewResolver, error) {
	if catalog == nil {
		return nil, errors.New("scene: catalog is required")
	}
	return &CatalogPreviewResolver{catalog: catalog}, nil
}

func (resolver *CatalogPreviewResolver) ResolvePreviewCatalog(
	ctx context.Context,
	query string,
) ([]PreviewCatalogCandidate, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if resolver == nil || resolver.catalog == nil || query == "" ||
		!utf8.ValidString(query) || utf8.RuneCountInString(query) > 500 {
		return nil, ErrCatalogSelectionInvalid
	}
	if previewUnsupportedIntent(query) {
		return []PreviewCatalogCandidate{}, nil
	}

	type scoredCandidate struct {
		candidate PreviewCatalogCandidate
		score     int
	}
	scored := make([]scoredCandidate, 0)
	definitions, err := resolver.catalog.ListActiveScenes(ctx)
	if err != nil {
		return nil, err
	}
	for _, definition := range definitions {
		score := previewCatalogScore(query, definition)
		if score == 0 {
			continue
		}
		defaultOption, found := defaultPracticeOption(definition.PracticeOptions)
		if !found || len(definition.Roles) == 0 {
			continue
		}
		candidate := PreviewCatalogCandidate{
			Scene:          definition,
			DefaultRoleIDs: []string{definition.Roles[0].ID},
			DefaultOption:  defaultOption,
		}
		if score >= 90 {
			return []PreviewCatalogCandidate{candidate}, nil
		}
		scored = append(scored, scoredCandidate{
			candidate: candidate,
			score:     score,
		})
	}
	sort.Slice(scored, func(left, right int) bool {
		if scored[left].score == scored[right].score {
			return scored[left].candidate.Scene.ID <
				scored[right].candidate.Scene.ID
		}
		return scored[left].score > scored[right].score
	})
	if len(scored) > MaxPreviewCatalogCandidates {
		scored = scored[:MaxPreviewCatalogCandidates]
	}
	if len(scored) > 1 && scored[0].score >= previewIntentScoreBase {
		bestScore := scored[0].score
		bestCount := 1
		for bestCount < len(scored) && scored[bestCount].score == bestScore {
			bestCount++
		}
		scored = scored[:bestCount]
	}
	result := make([]PreviewCatalogCandidate, len(scored))
	for index := range scored {
		result[index] = scored[index].candidate
	}
	return result, nil
}

func previewCatalogScore(
	query string,
	definition SceneDefinition,
) int {
	if query == strings.ToLower(definition.ID) {
		return 100
	}
	if query == strings.ToLower(definition.Name) {
		return 90
	}
	if score := previewSceneIntentScore(query, definition.ID); score > 0 {
		return score
	}
	fields := []string{
		definition.Name,
		string(definition.Experience),
		string(definition.Category),
		definition.Prompt.PublicSceneBrief,
		definition.Prompt.PracticeGoal,
		definition.Prompt.UserRole,
		definition.Prompt.AIRole,
	}
	for _, role := range definition.Roles {
		fields = append(fields, role.DisplayName, role.Type)
	}
	score := 0
	for _, field := range fields {
		normalized := strings.ToLower(strings.TrimSpace(field))
		if normalized != "" &&
			(strings.Contains(normalized, query) ||
				strings.Contains(query, normalized)) {
			score++
		}
	}
	return score
}

const previewIntentScoreBase = 60

type previewSceneIntent struct {
	aliases  []string
	excludes []string
}

var previewSceneIntents = map[string]previewSceneIntent{
	"scn_daily_rental_viewing": {
		aliases:  []string{"看房", "找房", "租房", "租赁", "房源", "租金", "租期", "入住要求"},
		excludes: []string{"报修", "维修", "漏水", "水管", "故障", "坏了", "物业"},
	},
	"scn_daily_rental_maintenance": {
		aliases: []string{"报修", "维修", "漏水", "水管", "故障", "坏了", "物业"},
	},
	"scn_daily_product_shopping": {
		aliases:  []string{"商品咨询", "购买", "买", "选购", "外套", "价格", "功能", "比较"},
		excludes: []string{"退款", "退货", "退掉", "换货", "售后"},
	},
	"scn_daily_return_refund": {
		aliases: []string{"退款", "退货", "退掉", "换货", "售后", "购买凭证"},
	},
	"scn_travel_airport_checkin": {
		aliases: []string{"机场值机", "值机", "航班", "登机口", "行李托运"},
	},
	"scn_workplace_client_delay": {
		aliases: []string{"客户延期", "延期", "延迟交付", "不能按时", "交付时间"},
	},
	"scn_workplace_requirement_clarification": {
		aliases: []string{"需求澄清", "澄清需求", "业务需求", "验收标准", "范围", "优先级"},
	},
	"scn_travel_hotel_checkin": {
		aliases: []string{"酒店入住", "办理入住", "酒店预订", "房型"},
	},
	"scn_workplace_feedback_conflict": {
		aliases:  []string{"提供反馈", "同事反馈", "反馈同事", "改进建议"},
		excludes: []string{"职场冲突", "冲突", "分歧", "矛盾", "争执"},
	},
	"scn_workplace_conflict_resolution": {
		aliases: []string{"职场冲突", "冲突", "分歧", "矛盾", "争执"},
	},
	"scn_daily_phone_call": {
		aliases: []string{"电话信息确认", "电话确认", "打电话", "来电", "电话沟通"},
	},
	"scn_daily_complaint_help": {
		aliases: []string{"投诉", "求助", "寻求帮助", "工作人员帮忙"},
	},
	"scn_workplace_solution_presentation": {
		aliases: []string{"方案介绍", "介绍方案", "方案汇报", "方案问答", "领导评审", "客户评审", "技术评审"},
	},
}

func previewSceneIntentScore(query, sceneID string) int {
	intent, found := previewSceneIntents[sceneID]
	if !found || containsAnyPreviewTerm(query, intent.excludes) {
		return 0
	}
	matches := 0
	for _, alias := range intent.aliases {
		if strings.Contains(query, alias) {
			matches++
		}
	}
	if matches == 0 {
		return 0
	}
	return previewIntentScoreBase + matches
}

func previewUnsupportedIntent(query string) bool {
	return containsAnyPreviewTerm(query, []string{
		"出租车",
		"打车",
		"坐公交",
		"公共交通",
		"拒绝朋友邀请",
		"拒绝邀请",
		"礼貌拒绝",
		"发出邀请",
	})
}

func containsAnyPreviewTerm(query string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(query, term) {
			return true
		}
	}
	return false
}

func defaultPracticeOption(
	options []PracticeOption,
) (PracticeOption, bool) {
	for _, option := range options {
		if option.Mode == PracticeModeFullSimulation ||
			option.Mode == PracticeModeFullMock {
			return option, true
		}
	}
	return PracticeOption{}, false
}
