package review

import (
	"context"
	"errors"
)

// ErrInvalidHistoryQuery 表示 Review 历史查询条件无效。
var ErrInvalidHistoryQuery = errors.New("invalid review history query")

// HistoryQueryUseCase 定义 Review 复盘历史的只读查询契约。
// 它不表示 PracticeSession 进度或 Conversation 对话历史。
type HistoryQueryUseCase interface {
	// ListReviewHistory 返回 Review 自己维护的只读历史投影。
	// SessionID 为空表示不按 Session 过滤；Limit 和 Offset 必须为非负数。
	// 默认排序为 ReviewedAt 倒序，具体校验和查询由后续实现负责。
	ListReviewHistory(ctx context.Context, query ListReviewHistoryQuery) (ListReviewHistoryResult, error)
}

// ListReviewHistoryQuery 描述 Review 历史的过滤和分页条件。
// 它不包含 PracticeSession 状态或 Conversation 对话筛选条件。
type ListReviewHistoryQuery struct {
	SessionID string
	Limit     int
	Offset    int
}

// ListReviewHistoryResult 返回 Review 自己维护的最小历史投影。
type ListReviewHistoryResult struct {
	Records []HistoryRecord
}
