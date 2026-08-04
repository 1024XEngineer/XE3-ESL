// 本文件实现 Resume CRUD 用例的应用编排、幂等处理和文件补偿。
package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
)

const (
	// MaxResumesPerUser 是单个用户允许保留的活动简历数量上限。
	MaxResumesPerUser = 3
	// DefaultMaximumFileBytes 是 Resume API 接受的 PDF 最大字节数。
	DefaultMaximumFileBytes int64 = 10 * 1024 * 1024
)

// ServiceConfiguration 保存应用服务的文件和授权地址边界。
type ServiceConfiguration struct {
	MaximumFileBytes int64
	ReadURLLifetime  time.Duration
}

// Service 编排简历持久化和原始文件存储，不直接依赖 GORM 或具体对象存储。
type Service struct {
	repository       Repository
	storage          FileStorage
	ids              IDGenerator
	maximumFileBytes int64
	readURLLifetime  time.Duration
	now              func() time.Time
}

// NewService 创建 Resume 应用服务。
func NewService(
	repository Repository,
	storage FileStorage,
	ids IDGenerator,
	configuration ServiceConfiguration,
) (*Service, error) {
	if repository == nil || storage == nil || ids == nil ||
		configuration.MaximumFileBytes < 1 ||
		configuration.MaximumFileBytes > DefaultMaximumFileBytes ||
		configuration.ReadURLLifetime <= 0 ||
		configuration.ReadURLLifetime > 2*time.Minute {
		return nil, errors.New("resume: invalid service configuration")
	}
	return &Service{
		repository:       repository,
		storage:          storage,
		ids:              ids,
		maximumFileBytes: configuration.MaximumFileBytes,
		readURLLifetime:  configuration.ReadURLLifetime,
		now:              func() time.Time { return time.Now().UTC() },
	}, nil
}

// Create 创建一份属于当前认证用户的新简历。
func (s *Service) Create(
	ctx context.Context,
	actor requestcontext.Actor,
	command CreateCommand,
) (resume.Resume, error) {
	if !validServiceCall(s, ctx, actor) || !validTitle(command.Title) ||
		!validIdempotencyKey(command.IdempotencyKey) {
		return resume.Resume{}, InvalidResumeError()
	}
	body, err := readValidatedPDF(command.File, command.SizeBytes, command.ChecksumSHA256, s.maximumFileBytes)
	if err != nil {
		return resume.Resume{}, err
	}
	if !validFilename(command.Filename) || command.ContentType != "application/pdf" {
		return resume.Resume{}, UnsupportedResumeFormatError()
	}
	command.ChecksumSHA256 = strings.ToLower(command.ChecksumSHA256)

	resumeID := s.ids.NewResumeID(actor.UserID, command.IdempotencyKey)
	objectKey := s.ids.NewObjectKey(actor.UserID, resumeID)
	if !validUUID(resumeID) || strings.TrimSpace(objectKey) == "" {
		return resume.Resume{}, RepositoryError(errors.New("resume: invalid generated identifier"))
	}
	now := s.now().UTC()
	item := resume.Resume{
		ID:               resumeID,
		OwnerUserID:      actor.UserID,
		Title:            command.Title,
		OriginalFilename: command.Filename,
		ContentType:      command.ContentType,
		SizeBytes:        int64(len(body)),
		ChecksumSHA256:   command.ChecksumSHA256,
		ObjectKey:        objectKey,
		FileStatus:       resume.FileUploading,
		ParseStatus:      resume.ParseQueued,
		Version:          1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := s.repository.CreateWithinLimit(ctx, item, MaxResumesPerUser); err != nil {
		if existing, ok := s.replayedCreate(ctx, actor.UserID, item, err); ok {
			return existing, nil
		}
		return resume.Resume{}, err
	}
	if err := s.storage.Put(
		ctx,
		objectKey,
		bytes.NewReader(body),
		int64(len(body)),
		command.ChecksumSHA256,
	); err != nil {
		s.abortCreate(ctx, actor.UserID, resumeID, objectKey)
		return resume.Resume{}, RepositoryError(err)
	}
	if err := s.repository.MarkFileAvailableAndQueueParse(ctx, actor.UserID, resumeID); err != nil {
		s.abortCreate(ctx, actor.UserID, resumeID, objectKey)
		return resume.Resume{}, err
	}
	return s.repository.FindByOwnerAndID(ctx, actor.UserID, resumeID)
}

// List 列出当前认证用户拥有的活动简历。
func (s *Service) List(
	ctx context.Context,
	actor requestcontext.Actor,
	query ListQuery,
) (ListResult, error) {
	if !validServiceCall(s, ctx, actor) || query.Limit < 1 ||
		query.Limit > MaxResumesPerUser {
		return ListResult{}, InvalidResumeError()
	}
	items, err := s.repository.ListByOwner(ctx, actor.UserID, ListQuery{
		Cursor: query.Cursor,
		Limit:  MaxResumesPerUser,
	})
	if err != nil {
		return ListResult{}, err
	}
	result := ListResult{Items: items}
	if len(result.Items) > query.Limit {
		result.Items = result.Items[:query.Limit]
		result.NextCursor = result.Items[len(result.Items)-1].ID
	}
	return result, nil
}

// Get 获取当前认证用户指定简历及其当前内容修订。
func (s *Service) Get(
	ctx context.Context,
	actor requestcontext.Actor,
	resumeID string,
) (Detail, error) {
	if !validServiceCall(s, ctx, actor) || !validUUID(resumeID) {
		return Detail{}, ResumeNotFoundError()
	}
	detail, err := s.repository.FindDetailByOwnerAndID(ctx, actor.UserID, resumeID)
	if err != nil {
		return Detail{}, err
	}
	if detail.Revision != nil {
		detail.Revision.Content = normalizeContent(detail.Revision.Content)
	}
	return detail, nil
}

// UpdateMetadata 修改指定简历的展示名称。
func (s *Service) UpdateMetadata(
	ctx context.Context,
	actor requestcontext.Actor,
	command UpdateMetadataCommand,
) (resume.Resume, error) {
	if !validServiceCall(s, ctx, actor) || !validUUID(command.ResumeID) ||
		!validTitle(command.Title) || command.ExpectedVersion < 1 {
		return resume.Resume{}, InvalidResumeError()
	}
	return s.repository.UpdateMetadata(ctx, actor.UserID, command)
}

// UpdateContent 手动保存指定简历的一次结构化内容修订。
func (s *Service) UpdateContent(
	ctx context.Context,
	actor requestcontext.Actor,
	command UpdateContentCommand,
) (resume.Revision, error) {
	if !validServiceCall(s, ctx, actor) || !validUUID(command.ResumeID) ||
		command.ExpectedVersion < 1 || !validContent(command.Content) {
		return resume.Revision{}, InvalidResumeError()
	}
	command.Content = normalizeContent(command.Content)
	return s.repository.SaveManualRevision(ctx, actor.UserID, command)
}

// ReplaceFile 替换指定简历的原始 PDF 并重新进入解析队列。
func (s *Service) ReplaceFile(
	ctx context.Context,
	actor requestcontext.Actor,
	command ReplaceFileCommand,
) (resume.Resume, error) {
	if !validServiceCall(s, ctx, actor) || !validUUID(command.ResumeID) ||
		command.ExpectedVersion < 1 || !validFilename(command.Filename) ||
		command.ContentType != "application/pdf" {
		return resume.Resume{}, InvalidResumeError()
	}
	body, err := readValidatedPDF(command.File, command.SizeBytes, command.ChecksumSHA256, s.maximumFileBytes)
	if err != nil {
		return resume.Resume{}, err
	}
	command.ChecksumSHA256 = strings.ToLower(command.ChecksumSHA256)
	current, err := s.repository.FindByOwnerAndID(ctx, actor.UserID, command.ResumeID)
	if err != nil {
		return resume.Resume{}, err
	}
	if current.Version != command.ExpectedVersion {
		if current.ChecksumSHA256 == command.ChecksumSHA256 &&
			current.OriginalFilename == command.Filename {
			return current, nil
		}
		return resume.Resume{}, ResumeVersionConflictError()
	}
	objectKey := s.ids.NewObjectKey(actor.UserID, command.ResumeID+"-"+command.ChecksumSHA256)
	if strings.TrimSpace(objectKey) == "" {
		return resume.Resume{}, RepositoryError(errors.New("resume: invalid generated object key"))
	}
	if err := s.storage.Put(
		ctx,
		objectKey,
		bytes.NewReader(body),
		int64(len(body)),
		command.ChecksumSHA256,
	); err != nil {
		return resume.Resume{}, RepositoryError(err)
	}
	updated, err := s.repository.ReplaceFileRecord(ctx, actor.UserID, ReplaceFileRecordCommand{
		ResumeID:        command.ResumeID,
		Filename:        command.Filename,
		ContentType:     command.ContentType,
		SizeBytes:       int64(len(body)),
		ChecksumSHA256:  command.ChecksumSHA256,
		ObjectKey:       objectKey,
		ExpectedVersion: command.ExpectedVersion,
	})
	if err != nil {
		_ = s.storage.Delete(ctx, objectKey)
		return resume.Resume{}, err
	}
	if current.ObjectKey != objectKey {
		_ = s.storage.Delete(ctx, current.ObjectKey)
	}
	return updated, nil
}

// GetContentURL 获取指定简历原始 PDF 的短时授权读取地址。
func (s *Service) GetContentURL(
	ctx context.Context,
	actor requestcontext.Actor,
	resumeID string,
) (ContentURL, error) {
	if !validServiceCall(s, ctx, actor) || !validUUID(resumeID) {
		return ContentURL{}, ResumeNotFoundError()
	}
	item, err := s.repository.FindByOwnerAndID(ctx, actor.UserID, resumeID)
	if err != nil {
		return ContentURL{}, err
	}
	if item.FileStatus != resume.FileAvailable {
		return ContentURL{}, ResumeVersionConflictError()
	}
	url, expiresAt, err := s.storage.SignedReadURL(ctx, item.ObjectKey, s.readURLLifetime)
	if err != nil {
		return ContentURL{}, RepositoryError(err)
	}
	return ContentURL{URL: url, ExpiresAt: expiresAt.UTC()}, nil
}

// RetryParse 重新提交一份解析失败的简历并返回最新状态。
func (s *Service) RetryParse(
	ctx context.Context,
	actor requestcontext.Actor,
	resumeID string,
) (resume.Resume, error) {
	if !validServiceCall(s, ctx, actor) || !validUUID(resumeID) {
		return resume.Resume{}, ResumeNotFoundError()
	}
	item, err := s.repository.FindByOwnerAndID(ctx, actor.UserID, resumeID)
	if err != nil {
		return resume.Resume{}, err
	}
	if item.ParseStatus == resume.ParseQueued || item.ParseStatus == resume.ParseRunning {
		return item, nil
	}
	if item.ParseStatus != resume.ParseFailed {
		return resume.Resume{}, ResumeVersionConflictError()
	}
	if err := s.repository.QueueParse(ctx, actor.UserID, resumeID); err != nil {
		return resume.Resume{}, err
	}
	return s.repository.FindByOwnerAndID(ctx, actor.UserID, resumeID)
}

// Delete 删除指定简历并同步清理原始文件。
func (s *Service) Delete(
	ctx context.Context,
	actor requestcontext.Actor,
	command DeleteCommand,
) error {
	if !validServiceCall(s, ctx, actor) || !validUUID(command.ResumeID) ||
		command.ExpectedVersion < 1 {
		return InvalidResumeError()
	}
	item, err := s.repository.FindByOwnerAndID(ctx, actor.UserID, command.ResumeID)
	if err != nil {
		return err
	}
	if item.FileStatus != resume.FileDeleting {
		item, err = s.repository.MarkDeleting(ctx, actor.UserID, command)
		if err != nil {
			return err
		}
	} else if item.Version != command.ExpectedVersion {
		return ResumeVersionConflictError()
	}
	if err := s.storage.Delete(ctx, item.ObjectKey); err != nil {
		return RepositoryError(err)
	}
	return s.repository.MarkDeleted(ctx, actor.UserID, command.ResumeID)
}

// replayedCreate 判断数据库冲突是否是同一幂等请求的成功重放。
func (s *Service) replayedCreate(
	ctx context.Context,
	ownerUserID string,
	wanted resume.Resume,
	createErr error,
) (resume.Resume, bool) {
	applicationError, ok := apperror.From(createErr)
	if !ok || applicationError.Code() != "resume_version_conflict" {
		return resume.Resume{}, false
	}
	existing, err := s.repository.FindByOwnerAndID(ctx, ownerUserID, wanted.ID)
	if err != nil || existing.Title != wanted.Title ||
		existing.OriginalFilename != wanted.OriginalFilename ||
		existing.ChecksumSHA256 != wanted.ChecksumSHA256 {
		return resume.Resume{}, false
	}
	return existing, true
}

// abortCreate 尽最大努力删除对象和未完成数据库记录，不覆盖原始错误。
func (s *Service) abortCreate(ctx context.Context, ownerUserID string, resumeID string, objectKey string) {
	_ = s.storage.Delete(ctx, objectKey)
	_ = s.repository.AbortCreate(ctx, ownerUserID, resumeID)
}

// readValidatedPDF 双重校验上传大小、声明摘要和 PDF 文件魔数。
func readValidatedPDF(
	reader io.Reader,
	declaredSize int64,
	declaredChecksum string,
	maximum int64,
) ([]byte, error) {
	if reader == nil || declaredSize < 1 || maximum < 1 {
		return nil, UnsupportedResumeFormatError()
	}
	if declaredSize > maximum {
		return nil, ResumeFileTooLargeError()
	}
	body, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, RepositoryError(err)
	}
	if int64(len(body)) > maximum {
		return nil, ResumeFileTooLargeError()
	}
	if int64(len(body)) != declaredSize || len(body) < 5 ||
		!bytes.Equal(body[:5], []byte("%PDF-")) {
		return nil, UnsupportedResumeFormatError()
	}
	sum := sha256.Sum256(body)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), declaredChecksum) {
		return nil, UnsupportedResumeFormatError()
	}
	return body, nil
}
