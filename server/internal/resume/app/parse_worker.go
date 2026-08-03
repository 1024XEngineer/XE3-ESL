// 本文件定义异步简历解析任务的应用编排骨架。
package app

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
)

// ParseWorker 从持久化队列领取简历并调用解析器生成结构化修订。
type ParseWorker struct {
	repository Repository
	storage    FileStorage
	parser     Parser
}

// NewParseWorker 创建简历解析 Worker。
func NewParseWorker(repository Repository, storage FileStorage, parser Parser) (*ParseWorker, error) {
	// TODO(issue-320): 后续补充租约、重试次数、轮询间隔和时钟依赖。
	if repository == nil || storage == nil || parser == nil {
		return nil, errors.New("resume: parse worker dependencies are required")
	}
	return &ParseWorker{repository: repository, storage: storage, parser: parser}, nil
}

// Run 持续领取解析任务，直到上下文被取消。
func (w *ParseWorker) Run(ctx context.Context) {
	// TODO(issue-320): 实现带退避和关闭语义的解析轮询循环。
	_ = ctx
}

// ProcessNext 尝试领取并处理下一份等待解析的简历。
func (w *ParseWorker) ProcessNext(context.Context) (bool, error) {
	// TODO(issue-320): 实现解析任务租约领取和一次处理。
	return false, NotImplementedError()
}

// ProcessResume 读取指定简历原文件并保存解析结果修订。
func (w *ParseWorker) ProcessResume(context.Context, resume.Resume) error {
	// TODO(issue-320): 实现文件读取、解析、成功提交和失败状态记录。
	return NotImplementedError()
}
