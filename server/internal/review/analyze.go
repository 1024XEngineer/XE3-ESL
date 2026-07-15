package review

import (
	"context"
	"errors"
)

var (
	// ErrAnalysisNotFound 表示不存在指定的 Review Analysis。
	ErrAnalysisNotFound = errors.New("analysis not found")
	// ErrFeedbackNotFound 表示不存在指定的 Review Feedback。
	ErrFeedbackNotFound = errors.New("feedback not found")
)

// AnalyzeUseCase 定义 Turn 分析和证据反馈查询能力。
// 本接口只描述应用契约，不包含具体分析流程。
type AnalyzeUseCase interface {
	// AnalyzeTurn 请求分析一个已完成的来源 Turn。
	// 相同 TurnID 和评分版本的幂等处理由后续具体实现负责。
	AnalyzeTurn(ctx context.Context, command AnalyzeTurnCommand) (AnalyzeTurnResult, error)

	// GetTurnReview 根据稳定 AnalysisID 返回一次确定的复盘结果。
	GetTurnReview(ctx context.Context, query GetTurnReviewQuery) (TurnReviewDetail, error)

	// GetFeedback 返回一条句子级证据反馈。
	// Feedback 只保存音频时间范围；音频内容和播放能力仍归 Conversation 所有。
	GetFeedback(ctx context.Context, feedbackID string) (FeedbackItem, error)
}

// AnalyzeTurnCommand 标识需要分析的来源 Turn。
// Review 只引用稳定 ID，不修改来源 Turn、Transcript、音频或 PracticeSession。
type AnalyzeTurnCommand struct {
	TurnID string
}

// AnalyzeTurnResult 是 Turn 分析用例的返回结果。
type AnalyzeTurnResult struct {
	Analysis TurnAnalysis
	Feedback []FeedbackItem
	Reused   bool
}

// GetTurnReviewQuery 使用稳定 AnalysisID 查询一次确定的复盘结果。
type GetTurnReviewQuery struct {
	AnalysisID string
}

// TurnReviewDetail 聚合一次 Analysis 及其证据反馈。
type TurnReviewDetail struct {
	Analysis TurnAnalysis
	Feedback []FeedbackItem
}
