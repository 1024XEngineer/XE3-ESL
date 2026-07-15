package review

// ReviewService 组合 Review 模块对内公开的应用用例契约。
// 具体业务流程由后续实现提供。
type ReviewService interface {
	AnalyzeUseCase
	RetryUseCase
	HistoryQueryUseCase
}
