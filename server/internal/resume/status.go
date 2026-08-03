// 本文件定义简历文件、解析任务和内容修订的稳定状态枚举。
package resume

// FileStatus 表示简历原始文件的生命周期状态。
type FileStatus string

const (
	// FileUploading 表示文件记录已创建但文件尚未完成保存。
	FileUploading FileStatus = "UPLOADING"
	// FileAvailable 表示文件已经可以读取。
	FileAvailable FileStatus = "AVAILABLE"
	// FileDeleting 表示文件正在执行删除流程。
	FileDeleting FileStatus = "DELETING"
	// FileDeleted 表示文件和活动记录已经完成删除。
	FileDeleted FileStatus = "DELETED"
)

// ParseStatus 表示结构化简历解析任务的状态。
type ParseStatus string

const (
	// ParseQueued 表示解析任务等待处理。
	ParseQueued ParseStatus = "QUEUED"
	// ParseRunning 表示解析任务正在处理。
	ParseRunning ParseStatus = "PARSING"
	// ParseReady 表示解析结果已经生成。
	ParseReady ParseStatus = "READY"
	// ParseFailed 表示解析任务已失败但原始文件仍可查看。
	ParseFailed ParseStatus = "FAILED"
)

// RevisionSource 表示结构化内容修订的来源。
type RevisionSource string

const (
	// RevisionParser 表示内容由简历解析器生成。
	RevisionParser RevisionSource = "PARSER"
	// RevisionManual 表示内容由用户手动保存。
	RevisionManual RevisionSource = "MANUAL"
)
