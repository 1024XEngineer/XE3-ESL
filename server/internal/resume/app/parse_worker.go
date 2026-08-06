// 本文件实现异步简历解析任务的领取、处理和优雅停止。
package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
)

const (
	ocrSignedURLLifetime = 2 * time.Minute
	maximumOCRPages      = 5
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
	expired, found, err := w.repository.ClaimExpiredTemporary(ctx, time.Now().UTC())
	if err != nil {
		return false, err
	}
	if found {
		if err := w.storage.Delete(ctx, expired.ObjectKey); err != nil {
			return true, RepositoryError(err)
		}
		if err := w.repository.MarkDeleted(ctx, expired.OwnerUserID, expired.ID); err != nil {
			return true, err
		}
		return true, nil
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
	startedAt := time.Now()
	reader, err := w.storage.Open(ctx, claimed.ObjectKey)
	if err != nil {
		return w.failClaim(ctx, claimed, "storage_read_failed")
	}
	content, parseErr := w.parser.Parse(ctx, reader)
	closeErr := reader.Close()
	parserVersion := w.parser.Version()
	if parseErr != nil {
		failureCode := parserFailureCode(parseErr)
		fallback, canFallback := w.parser.(URLFallbackParser)
		if failureCode != "pdf_text_unavailable" || !canFallback {
			return w.failClaim(ctx, claimed, failureCode)
		}
		if parserFailurePageCount(parseErr) > maximumOCRPages {
			return w.failClaim(ctx, claimed, "ocr_page_limit_exceeded")
		}
		slog.Info(
			"Resume parse entered OCR fallback",
			slog.String("resume_id", claimed.ID),
			slog.String("stage", "ocr"),
			slog.String("parser_version", fallback.OCRVersion()),
		)
		if closeErr != nil {
			return w.failClaim(ctx, claimed, "storage_read_failed")
		}
		sourceURL, _, signErr := w.storage.SignedReadURL(
			ctx,
			claimed.ObjectKey,
			ocrSignedURLLifetime,
		)
		if signErr != nil {
			return w.failClaim(ctx, claimed, "storage_read_failed")
		}
		content, parseErr = fallback.ParseURL(ctx, sourceURL)
		if parseErr != nil {
			return w.failClaim(ctx, claimed, parserFailureCode(parseErr))
		}
		parserVersion = fallback.OCRVersion()
	}
	if closeErr != nil {
		return w.failClaim(ctx, claimed, "storage_read_failed")
	}
	if !validContent(content) {
		return w.failClaim(ctx, claimed, "parser_output_invalid")
	}
	content = normalizeContent(content)
	err = w.repository.CompleteParse(ctx, claimed, resume.Revision{
		Source:        resume.RevisionParser,
		ParserVersion: parserVersion,
		Content:       content,
	})
	if err == nil {
		slog.Info(
			"Resume parse completed",
			slog.String("resume_id", claimed.ID),
			slog.String("stage", "complete"),
			slog.String("parser_version", parserVersion),
			slog.Duration("duration", time.Since(startedAt)),
		)
	}
	return err
}

// failClaim 持久化稳定失败码；成功落库后不再把单份坏简历升级为 Worker 故障。
func (w *ParseWorker) failClaim(ctx context.Context, claimed resume.Resume, code string) error {
	if err := w.repository.FailParse(ctx, claimed, code); err != nil {
		return err
	}
	slog.Warn(
		"Resume parse failed",
		slog.String("resume_id", claimed.ID),
		slog.String("stage", "failed"),
		slog.String("failure_code", code),
	)
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

func parserFailurePageCount(err error) int {
	type pageCountFailure interface {
		PageCount() int
	}
	var failure pageCountFailure
	if errors.As(err, &failure) {
		return failure.PageCount()
	}
	return 0
}
