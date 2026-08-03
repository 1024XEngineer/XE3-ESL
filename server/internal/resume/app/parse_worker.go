// 本文件实现异步简历解析任务的领取、处理和优雅停止。
package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
)

// ParseWorker 从持久化队列领取简历并调用解析器生成结构化修订。
type ParseWorker struct {
	repository   Repository
	storage      FileStorage
	parser       Parser
	pollInterval time.Duration
}

// NewParseWorker 创建简历解析 Worker。
func NewParseWorker(
	repository Repository,
	storage FileStorage,
	parser Parser,
	pollInterval time.Duration,
) (*ParseWorker, error) {
	if repository == nil || storage == nil || parser == nil ||
		pollInterval <= 0 || strings.TrimSpace(parser.Version()) == "" {
		return nil, errors.New("resume: invalid parse worker configuration")
	}
	return &ParseWorker{
		repository:   repository,
		storage:      storage,
		parser:       parser,
		pollInterval: pollInterval,
	}, nil
}

// Run 持续领取解析任务，直到上下文被取消。
func (w *ParseWorker) Run(ctx context.Context) {
	if w == nil || ctx == nil {
		return
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			processed, _ := w.ProcessNext(ctx)
			if processed {
				timer.Reset(0)
			} else {
				timer.Reset(w.pollInterval)
			}
		}
	}
}

// ProcessNext 尝试领取并处理下一份等待解析的简历。
func (w *ParseWorker) ProcessNext(ctx context.Context) (bool, error) {
	if w == nil || w.repository == nil || w.storage == nil || w.parser == nil ||
		ctx == nil || ctx.Err() != nil {
		return false, InvalidResumeError()
	}
	claimed, found, err := w.repository.ClaimNextQueuedParse(ctx)
	if err != nil || !found {
		return false, err
	}
	if err := w.ProcessResume(ctx, claimed); err != nil {
		return true, err
	}
	return true, nil
}

// ProcessResume 读取指定简历原文件并保存解析结果修订。
func (w *ParseWorker) ProcessResume(ctx context.Context, claimed resume.Resume) error {
	if w == nil || w.repository == nil || w.storage == nil || w.parser == nil ||
		ctx == nil || ctx.Err() != nil || claimed.ParseStatus != resume.ParseRunning ||
		claimed.FileStatus != resume.FileAvailable {
		return InvalidResumeError()
	}
	reader, err := w.storage.Open(ctx, claimed.ObjectKey)
	if err != nil {
		return w.failClaim(ctx, claimed, "storage_read_failed")
	}
	content, parseErr := w.parser.Parse(ctx, reader)
	closeErr := reader.Close()
	if parseErr != nil {
		return w.failClaim(ctx, claimed, parserFailureCode(parseErr))
	}
	if closeErr != nil {
		return w.failClaim(ctx, claimed, "storage_read_failed")
	}
	if !validContent(content) {
		return w.failClaim(ctx, claimed, "parser_output_invalid")
	}
	content = normalizeContent(content)
	return w.repository.CompleteParse(ctx, claimed, resume.Revision{
		Source:        resume.RevisionParser,
		ParserVersion: w.parser.Version(),
		Content:       content,
	})
}

// failClaim 持久化稳定失败码；成功落库后不再把单份坏简历升级为 Worker 故障。
func (w *ParseWorker) failClaim(ctx context.Context, claimed resume.Resume, code string) error {
	if err := w.repository.FailParse(ctx, claimed, code); err != nil {
		return err
	}
	return nil
}

// parserFailureCode 从解析器错误中提取稳定失败码。
func parserFailureCode(err error) string {
	type codedFailure interface {
		FailureCode() string
	}
	var failure codedFailure
	if errors.As(err, &failure) {
		code := failure.FailureCode()
		if workerFailurePattern.MatchString(code) {
			return code
		}
	}
	return "pdf_parse_failed"
}
