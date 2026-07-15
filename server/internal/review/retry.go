package review

import "context"

// RetryUseCase 定义根据可重答 Feedback 发起同题重答的应用契约。
// 新 Turn 仍由 Conversation 创建和拥有。
type RetryUseCase interface {
	// RequestRetry 请求为一条可重答 Feedback 创建同题重答。
	// Feedback 不存在或不可重答时分别返回 ErrFeedbackNotFound 或
	// ErrFeedbackNotRetryable；具体查询、保存和跨模块调用由后续实现负责。
	RequestRetry(ctx context.Context, command RequestRetryCommand) (RequestRetryResult, error)
}

// RequestRetryCommand 使用 FeedbackID 标识需要再次练习的反馈。
// 原 Turn 由未来 Service 根据 Review 内部关联确定，调用方不重复提交 TurnID。
type RequestRetryCommand struct {
	FeedbackID string
}

// RequestRetryResult 返回 Review 所拥有的重答请求。
// NewTurnID 通过 Request.NewTurnID 表达；为空表示 Conversation 尚未创建新 Turn。
type RequestRetryResult struct {
	Request RetryRequest
}
