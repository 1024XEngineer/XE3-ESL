// Package persistence 使用 GORM 实现 Resume 模块的 PostgreSQL 持久化边界。
package persistence

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume/app"
)

var (
	resumeUUIDPattern     = regexp.MustCompile(`\A[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\z`)
	resumeChecksumPattern = regexp.MustCompile(`\A[0-9a-f]{64}\z`)
	parseFailurePattern   = regexp.MustCompile(`\A[a-z][a-z0-9_]{0,127}\z`)
)

// GormRepository 使用进程已有数据库连接池实现 Resume Repository。
type GormRepository struct {
	database *gorm.DB
}

// NewGormRepository 使用已有 GORM 数据库句柄创建 Repository。
func NewGormRepository(database *gorm.DB) (*GormRepository, error) {
	if database == nil {
		return nil, errors.New("resume: GORM database is required")
	}
	return &GormRepository{database: database}, nil
}

// NewGormRepositoryFromPool 复用进程已有 pgx 连接池创建 GORM Repository。
func NewGormRepositoryFromPool(pool *pgxpool.Pool) (*GormRepository, error) {
	if pool == nil {
		return nil, errors.New("resume: pgx pool is required")
	}
	database, err := gorm.Open(
		postgresdriver.New(postgresdriver.Config{Conn: stdlib.OpenDBFromPool(pool)}),
		&gorm.Config{
			DisableAutomaticPing: true,
			Logger:               logger.Default.LogMode(logger.Silent),
		},
	)
	if err != nil {
		return nil, errors.New("resume: open GORM database")
	}
	return NewGormRepository(database)
}

// CreateWithinLimit 在原子事务中校验三份上限并创建简历记录。
func (r *GormRepository) CreateWithinLimit(
	ctx context.Context,
	item resume.Resume,
	maximum int,
) error {
	if r == nil || r.database == nil || ctx == nil ||
		!validNewResume(item) || maximum < 1 || maximum > app.MaxResumesPerUser {
		return app.InvalidResumeError()
	}
	record := resumeToRecord(item)
	if record.Version == 0 {
		record.Version = 1
	}
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var owner struct {
			ID string `gorm:"column:id"`
		}
		if err := tx.Table("identity_users").
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id").
			Where("id = ? AND account_status = ?", item.OwnerUserID, "active").
			Take(&owner).Error; err != nil {
			return mapGormError(err)
		}
		if !item.Temporary {
			var count int64
			if err := tx.Model(&resumeRecord{}).
				Where("owner_user_id = ? AND is_temporary = FALSE", item.OwnerUserID).
				Count(&count).Error; err != nil {
				return mapGormError(err)
			}
			if count >= int64(maximum) {
				return app.ResumeLimitExceededError()
			}
		}
		if err := tx.Create(&record).Error; err != nil {
			return mapGormError(err)
		}
		return nil
	})
	return mapTransactionError(err)
}

// AbortCreate 硬删除仍处于上传中的记录，用于文件保存失败后的安全补偿。
func (r *GormRepository) AbortCreate(
	ctx context.Context,
	ownerUserID string,
	resumeID string,
) error {
	if r == nil || r.database == nil || ctx == nil ||
		!validUUID(ownerUserID) || !validUUID(resumeID) {
		return app.InvalidResumeError()
	}
	result := r.database.WithContext(ctx).
		Unscoped().
		Where("owner_user_id = ? AND resume_id = ?", ownerUserID, resumeID).
		Where("file_status = ?", string(resume.FileUploading)).
		Delete(&resumeRecord{})
	if result.Error != nil {
		return mapGormError(result.Error)
	}
	if result.RowsAffected == 0 {
		return r.writeMiss(ctx, ownerUserID, resumeID)
	}
	return nil
}

// ListByOwner 按稳定顺序分页查询当前用户的活动简历。
func (r *GormRepository) ListByOwner(
	ctx context.Context,
	ownerUserID string,
	query app.ListQuery,
) ([]resume.Resume, error) {
	if r == nil || r.database == nil || ctx == nil || !validUUID(ownerUserID) ||
		query.Limit < 1 || query.Limit > app.MaxResumesPerUser ||
		(query.Cursor != "" && !validUUID(query.Cursor)) {
		return nil, app.InvalidResumeError()
	}
	database := r.database.WithContext(ctx).
		Where("owner_user_id = ? AND is_temporary = FALSE", ownerUserID)
	if query.Cursor != "" {
		database = database.Where("resume_id < ?", query.Cursor)
	}
	var records []resumeRecord
	if err := database.Order("resume_id DESC").Limit(query.Limit).Find(&records).Error; err != nil {
		return nil, mapGormError(err)
	}
	items := make([]resume.Resume, 0, len(records))
	for _, record := range records {
		item, err := resumeFromRecord(record)
		if err != nil {
			return nil, app.RepositoryError(err)
		}
		items = append(items, item)
	}
	return items, nil
}

// FindByOwnerAndID 查询当前用户拥有的指定简历元数据。
func (r *GormRepository) FindByOwnerAndID(
	ctx context.Context,
	ownerUserID string,
	resumeID string,
) (resume.Resume, error) {
	if r == nil || r.database == nil || ctx == nil ||
		!validUUID(ownerUserID) || !validUUID(resumeID) {
		return resume.Resume{}, app.ResumeNotFoundError()
	}
	var record resumeRecord
	if err := r.database.WithContext(ctx).
		Where("owner_user_id = ? AND resume_id = ?", ownerUserID, resumeID).
		Take(&record).Error; err != nil {
		return resume.Resume{}, mapGormError(err)
	}
	item, err := resumeFromRecord(record)
	if err != nil {
		return resume.Resume{}, app.RepositoryError(err)
	}
	return item, nil
}

// FindDetailByOwnerAndID 查询指定简历和当前内容修订。
func (r *GormRepository) FindDetailByOwnerAndID(
	ctx context.Context,
	ownerUserID string,
	resumeID string,
) (app.Detail, error) {
	item, err := r.FindByOwnerAndID(ctx, ownerUserID, resumeID)
	if err != nil {
		return app.Detail{}, err
	}
	detail := app.Detail{Resume: item}
	if item.CurrentRevision == 0 {
		return detail, nil
	}
	var record revisionRecord
	if err := r.database.WithContext(ctx).
		Where(
			"owner_user_id = ? AND resume_id = ? AND revision = ?",
			ownerUserID,
			resumeID,
			item.CurrentRevision,
		).
		Take(&record).Error; err != nil {
		return app.Detail{}, mapGormError(err)
	}
	revision, err := revisionFromRecord(record)
	if err != nil {
		return app.Detail{}, app.RepositoryError(err)
	}
	detail.Revision = &revision
	return detail, nil
}

// UpdateMetadata 使用期望版本更新简历展示名称。
func (r *GormRepository) UpdateMetadata(
	ctx context.Context,
	ownerUserID string,
	command app.UpdateMetadataCommand,
) (resume.Resume, error) {
	if !validWriteInput(r, ctx, ownerUserID, command.ResumeID, command.ExpectedVersion) ||
		!validTitle(command.Title) {
		return resume.Resume{}, app.InvalidResumeError()
	}
	var record resumeRecord
	result := r.database.WithContext(ctx).
		Model(&record).
		Clauses(clause.Returning{}).
		Where(
			"owner_user_id = ? AND resume_id = ? AND version = ?",
			ownerUserID,
			command.ResumeID,
			command.ExpectedVersion,
		).
		Where("file_status NOT IN ?", []string{string(resume.FileDeleting), string(resume.FileDeleted)}).
		Updates(map[string]any{
			"title":      command.Title,
			"version":    gorm.Expr("version + 1"),
			"updated_at": nextResumeTimestamp(),
		})
	if result.Error != nil {
		return resume.Resume{}, mapGormError(result.Error)
	}
	if result.RowsAffected == 0 {
		return resume.Resume{}, r.writeMiss(ctx, ownerUserID, command.ResumeID)
	}
	return mappedResume(record)
}

// ReplaceFileRecord 原子替换文件元数据并重新排队解析任务。
func (r *GormRepository) ReplaceFileRecord(
	ctx context.Context,
	ownerUserID string,
	command app.ReplaceFileRecordCommand,
) (resume.Resume, error) {
	if !validWriteInput(r, ctx, ownerUserID, command.ResumeID, command.ExpectedVersion) ||
		!validFileMetadata(
			command.Filename,
			command.ContentType,
			command.SizeBytes,
			command.ChecksumSHA256,
			command.ObjectKey,
		) {
		return resume.Resume{}, app.InvalidResumeError()
	}
	var record resumeRecord
	result := r.database.WithContext(ctx).
		Model(&record).
		Clauses(clause.Returning{}).
		Where(
			"owner_user_id = ? AND resume_id = ? AND version = ?",
			ownerUserID,
			command.ResumeID,
			command.ExpectedVersion,
		).
		Where("file_status NOT IN ?", []string{string(resume.FileDeleting), string(resume.FileDeleted)}).
		Updates(map[string]any{
			"original_filename":  command.Filename,
			"content_type":       command.ContentType,
			"size_bytes":         command.SizeBytes,
			"checksum_sha256":    command.ChecksumSHA256,
			"object_key":         command.ObjectKey,
			"file_status":        string(resume.FileAvailable),
			"parse_status":       string(resume.ParseQueued),
			"parse_failure_code": nil,
			"current_revision":   nil,
			"version":            gorm.Expr("version + 1"),
			"updated_at":         nextResumeTimestamp(),
		})
	if result.Error != nil {
		return resume.Resume{}, mapGormError(result.Error)
	}
	if result.RowsAffected == 0 {
		return resume.Resume{}, r.writeMiss(ctx, ownerUserID, command.ResumeID)
	}
	return mappedResume(record)
}

// MarkFileAvailableAndQueueParse 把已保存文件标记为可用并进入解析队列。
func (r *GormRepository) MarkFileAvailableAndQueueParse(
	ctx context.Context,
	ownerUserID string,
	resumeID string,
) error {
	if r == nil || r.database == nil || ctx == nil ||
		!validUUID(ownerUserID) || !validUUID(resumeID) {
		return app.InvalidResumeError()
	}
	result := r.database.WithContext(ctx).
		Model(&resumeRecord{}).
		Where("owner_user_id = ? AND resume_id = ?", ownerUserID, resumeID).
		Where("file_status = ?", string(resume.FileUploading)).
		Updates(map[string]any{
			"file_status":  string(resume.FileAvailable),
			"parse_status": string(resume.ParseQueued),
			"version":      gorm.Expr("version + 1"),
			"updated_at":   nextResumeTimestamp(),
		})
	if result.Error != nil {
		return mapGormError(result.Error)
	}
	if result.RowsAffected == 0 {
		return r.writeMiss(ctx, ownerUserID, resumeID)
	}
	return nil
}

// SaveManualRevision 创建用户手动内容修订并更新当前修订指针。
func (r *GormRepository) SaveManualRevision(
	ctx context.Context,
	ownerUserID string,
	command app.UpdateContentCommand,
) (resume.Revision, error) {
	if !validWriteInput(r, ctx, ownerUserID, command.ResumeID, command.ExpectedVersion) {
		return resume.Revision{}, app.InvalidResumeError()
	}
	var saved resume.Revision
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		record, err := lockOwnedResume(tx, ownerUserID, command.ResumeID)
		if err != nil {
			return err
		}
		if record.Version != command.ExpectedVersion ||
			record.FileStatus == string(resume.FileDeleting) ||
			record.FileStatus == string(resume.FileDeleted) {
			return app.ResumeVersionConflictError()
		}
		nextRevision, err := nextRevision(tx, command.ResumeID)
		if err != nil {
			return err
		}
		saved = resume.Revision{
			ResumeID:  command.ResumeID,
			Revision:  nextRevision,
			Source:    resume.RevisionManual,
			Content:   command.Content,
			CreatedAt: time.Now().UTC(),
		}
		revisionRecord, err := revisionToRecord(saved)
		if err != nil {
			return app.InvalidResumeError()
		}
		revisionRecord.OwnerUserID = ownerUserID
		if err := tx.Create(&revisionRecord).Error; err != nil {
			return mapGormError(err)
		}
		result := tx.Model(&resumeRecord{}).
			Where("owner_user_id = ? AND resume_id = ?", ownerUserID, command.ResumeID).
			Updates(map[string]any{
				"current_revision": nextRevision,
				"version":          gorm.Expr("version + 1"),
				"updated_at":       nextResumeTimestamp(),
			})
		if result.Error != nil {
			return mapGormError(result.Error)
		}
		return nil
	})
	if err != nil {
		return resume.Revision{}, mapTransactionError(err)
	}
	return saved, nil
}

// MarkDeleting 使用期望版本把简历标记为删除中。
func (r *GormRepository) MarkDeleting(
	ctx context.Context,
	ownerUserID string,
	command app.DeleteCommand,
) (resume.Resume, error) {
	if !validWriteInput(r, ctx, ownerUserID, command.ResumeID, command.ExpectedVersion) {
		return resume.Resume{}, app.InvalidResumeError()
	}
	var record resumeRecord
	result := r.database.WithContext(ctx).
		Model(&record).
		Clauses(clause.Returning{}).
		Where(
			"owner_user_id = ? AND resume_id = ? AND version = ?",
			ownerUserID,
			command.ResumeID,
			command.ExpectedVersion,
		).
		Where("file_status NOT IN ?", []string{string(resume.FileDeleting), string(resume.FileDeleted)}).
		Updates(map[string]any{
			"file_status": string(resume.FileDeleting),
			"version":     gorm.Expr("version + 1"),
			"updated_at":  nextResumeTimestamp(),
		})
	if result.Error != nil {
		return resume.Resume{}, mapGormError(result.Error)
	}
	if result.RowsAffected == 0 {
		return resume.Resume{}, r.writeMiss(ctx, ownerUserID, command.ResumeID)
	}
	return mappedResume(record)
}

// MarkDeleted 完成简历软删除状态写入。
func (r *GormRepository) MarkDeleted(
	ctx context.Context,
	ownerUserID string,
	resumeID string,
) error {
	if r == nil || r.database == nil || ctx == nil ||
		!validUUID(ownerUserID) || !validUUID(resumeID) {
		return app.ResumeNotFoundError()
	}
	now := time.Now().UTC()
	result := r.database.WithContext(ctx).
		Model(&resumeRecord{}).
		Where("owner_user_id = ? AND resume_id = ?", ownerUserID, resumeID).
		Where("file_status = ?", string(resume.FileDeleting)).
		Updates(map[string]any{
			"file_status": string(resume.FileDeleted),
			"deleted_at":  now,
			"version":     gorm.Expr("version + 1"),
			"updated_at":  nextResumeTimestamp(),
		})
	if result.Error != nil {
		return mapGormError(result.Error)
	}
	if result.RowsAffected == 0 {
		return r.writeMiss(ctx, ownerUserID, resumeID)
	}
	return nil
}

// QueueParse 把当前用户指定的失败简历重新放入解析队列。
func (r *GormRepository) QueueParse(
	ctx context.Context,
	ownerUserID string,
	resumeID string,
) error {
	if r == nil || r.database == nil || ctx == nil ||
		!validUUID(ownerUserID) || !validUUID(resumeID) {
		return app.InvalidResumeError()
	}
	result := r.database.WithContext(ctx).
		Model(&resumeRecord{}).
		Where("owner_user_id = ? AND resume_id = ?", ownerUserID, resumeID).
		Where("file_status = ? AND parse_status = ?", string(resume.FileAvailable), string(resume.ParseFailed)).
		Updates(map[string]any{
			"parse_status":       string(resume.ParseQueued),
			"parse_failure_code": nil,
			"version":            gorm.Expr("version + 1"),
			"updated_at":         nextResumeTimestamp(),
		})
	if result.Error != nil {
		return mapGormError(result.Error)
	}
	if result.RowsAffected == 0 {
		return r.writeMiss(ctx, ownerUserID, resumeID)
	}
	return nil
}

// ClaimNextQueuedParse 领取下一份等待解析的简历任务。
func (r *GormRepository) ClaimNextQueuedParse(
	ctx context.Context,
) (resume.Resume, bool, error) {
	if r == nil || r.database == nil || ctx == nil {
		return resume.Resume{}, false, app.InvalidResumeError()
	}
	var claimed resume.Resume
	found := false
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record resumeRecord
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("file_status = ? AND parse_status = ?", string(resume.FileAvailable), string(resume.ParseQueued)).
			Order("updated_at, resume_id").
			Take(&record).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return mapGormError(err)
		}
		if err := tx.Model(&record).Clauses(clause.Returning{}).Updates(map[string]any{
			"parse_status": string(resume.ParseRunning),
			"version":      gorm.Expr("version + 1"),
			"updated_at":   nextResumeTimestamp(),
		}).Error; err != nil {
			return mapGormError(err)
		}
		mapped, err := resumeFromRecord(record)
		if err != nil {
			return app.RepositoryError(err)
		}
		claimed = mapped
		found = true
		return nil
	})
	if err != nil {
		return resume.Resume{}, false, mapTransactionError(err)
	}
	return claimed, found, nil
}

// ClaimExpiredTemporary locks one expired temporary resume and marks its file for deletion.
func (r *GormRepository) ClaimExpiredTemporary(
	ctx context.Context,
	now time.Time,
) (resume.Resume, bool, error) {
	if r == nil || r.database == nil || ctx == nil || now.IsZero() {
		return resume.Resume{}, false, app.InvalidResumeError()
	}
	var claimed resume.Resume
	found := false
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record resumeRecord
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("is_temporary = TRUE AND expires_at <= ?", now.UTC()).
			Where("file_status IN ?", []string{string(resume.FileAvailable), string(resume.FileDeleting)}).
			Order("expires_at, resume_id").
			Take(&record).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return mapGormError(err)
		}
		if record.FileStatus != string(resume.FileDeleting) {
			if err := tx.Model(&record).Clauses(clause.Returning{}).Updates(map[string]any{
				"file_status": string(resume.FileDeleting),
				"version":     gorm.Expr("version + 1"),
				"updated_at":  nextResumeTimestamp(),
			}).Error; err != nil {
				return mapGormError(err)
			}
		}
		mapped, err := resumeFromRecord(record)
		if err != nil {
			return app.RepositoryError(err)
		}
		claimed = mapped
		found = true
		return nil
	})
	if err != nil {
		return resume.Resume{}, false, mapTransactionError(err)
	}
	return claimed, found, nil
}

// CompleteParse 保存解析修订并把简历标记为解析完成。
func (r *GormRepository) CompleteParse(
	ctx context.Context,
	claimed resume.Resume,
	revision resume.Revision,
) error {
	if r == nil || r.database == nil || ctx == nil || !validUUID(claimed.OwnerUserID) ||
		!validUUID(claimed.ID) || claimed.Version < 1 || revision.Source != resume.RevisionParser ||
		strings.TrimSpace(revision.ParserVersion) == "" {
		return app.InvalidResumeError()
	}
	return mapTransactionError(r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		record, err := lockOwnedResume(tx, claimed.OwnerUserID, claimed.ID)
		if err != nil {
			return err
		}
		if record.FileStatus != string(resume.FileAvailable) ||
			record.ParseStatus != string(resume.ParseRunning) ||
			record.ObjectKey != claimed.ObjectKey ||
			record.ChecksumSHA256 != claimed.ChecksumSHA256 {
			return app.ResumeVersionConflictError()
		}
		number, err := nextRevision(tx, claimed.ID)
		if err != nil {
			return err
		}
		revision.ResumeID = claimed.ID
		revision.Revision = number
		if revision.CreatedAt.IsZero() {
			revision.CreatedAt = time.Now().UTC()
		}
		revisionRecord, err := revisionToRecord(revision)
		if err != nil {
			return app.InvalidResumeError()
		}
		revisionRecord.OwnerUserID = claimed.OwnerUserID
		if err := tx.Create(&revisionRecord).Error; err != nil {
			return mapGormError(err)
		}
		updates := map[string]any{
			"parse_status":       string(resume.ParseReady),
			"parse_failure_code": nil,
			"version":            gorm.Expr("version + 1"),
			"updated_at":         nextResumeTimestamp(),
		}
		if currentRevision(record.CurrentRevision) == claimed.CurrentRevision {
			updates["current_revision"] = number
		}
		result := tx.Model(&resumeRecord{}).
			Where("owner_user_id = ? AND resume_id = ?", claimed.OwnerUserID, claimed.ID).
			Updates(updates)
		return mapGormError(result.Error)
	}))
}

// FailParse 保存稳定失败码并把已领取简历标记为解析失败。
func (r *GormRepository) FailParse(
	ctx context.Context,
	claimed resume.Resume,
	failureCode string,
) error {
	if r == nil || r.database == nil || ctx == nil || !validUUID(claimed.OwnerUserID) ||
		!validUUID(claimed.ID) || claimed.Version < 1 || !parseFailurePattern.MatchString(failureCode) {
		return app.InvalidResumeError()
	}
	result := r.database.WithContext(ctx).
		Model(&resumeRecord{}).
		Where(
			"owner_user_id = ? AND resume_id = ? AND object_key = ? AND checksum_sha256 = ?",
			claimed.OwnerUserID,
			claimed.ID,
			claimed.ObjectKey,
			claimed.ChecksumSHA256,
		).
		Where("parse_status = ?", string(resume.ParseRunning)).
		Updates(map[string]any{
			"parse_status":       string(resume.ParseFailed),
			"parse_failure_code": failureCode,
			"version":            gorm.Expr("version + 1"),
			"updated_at":         nextResumeTimestamp(),
		})
	if result.Error != nil {
		return mapGormError(result.Error)
	}
	if result.RowsAffected == 0 {
		return r.writeMiss(ctx, claimed.OwnerUserID, claimed.ID)
	}
	return nil
}

// currentRevision 把数据库可空修订号转换为领域层的零值语义。
func currentRevision(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

// lockOwnedResume 锁定指定用户的一份活动简历记录。
func lockOwnedResume(tx *gorm.DB, ownerUserID string, resumeID string) (resumeRecord, error) {
	var record resumeRecord
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("owner_user_id = ? AND resume_id = ?", ownerUserID, resumeID).
		Take(&record).Error; err != nil {
		return resumeRecord{}, mapGormError(err)
	}
	return record, nil
}

// nextRevision 返回指定简历在当前事务中的下一个修订号。
func nextRevision(tx *gorm.DB, resumeID string) (int64, error) {
	var number int64
	if err := tx.Model(&revisionRecord{}).
		Select("COALESCE(MAX(revision), 0) + 1").
		Where("resume_id = ?", resumeID).
		Scan(&number).Error; err != nil {
		return 0, mapGormError(err)
	}
	if number < 1 {
		return 0, app.RepositoryError(errors.New("resume: invalid next revision"))
	}
	return number, nil
}

// writeMiss 把零行更新区分为不可见资源和版本冲突。
func (r *GormRepository) writeMiss(ctx context.Context, ownerUserID string, resumeID string) error {
	_, err := r.FindByOwnerAndID(ctx, ownerUserID, resumeID)
	if err != nil {
		return err
	}
	return app.ResumeVersionConflictError()
}

// mappedResume 把更新后的 Record 安全转换为领域对象。
func mappedResume(record resumeRecord) (resume.Resume, error) {
	item, err := resumeFromRecord(record)
	if err != nil {
		return resume.Resume{}, app.RepositoryError(err)
	}
	return item, nil
}

// mapTransactionError 保留应用错误并净化数据库事务错误。
func mapTransactionError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := apperror.From(err); ok {
		return err
	}
	return mapGormError(err)
}

// mapGormError 把 GORM/PostgreSQL 错误转换为公共应用错误。
func mapGormError(err error) error {
	if err == nil {
		return nil
	}
	if appError, ok := apperror.From(err); ok {
		return appError
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return app.ResumeNotFoundError()
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505", "40001", "40P01":
			return app.ResumeVersionConflictError()
		case "23503", "23514", "22P02":
			return app.InvalidResumeError()
		}
	}
	return app.RepositoryError(err)
}

// nextResumeTimestamp 生成严格单调递增的数据库更新时间表达式。
func nextResumeTimestamp() clause.Expr {
	return gorm.Expr(
		"GREATEST(transaction_timestamp(), updated_at + interval '1 microsecond')",
	)
}

// validWriteInput 校验带所有权和乐观锁的写入公共输入。
func validWriteInput(
	repository *GormRepository,
	ctx context.Context,
	ownerUserID string,
	resumeID string,
	expectedVersion int64,
) bool {
	return repository != nil && repository.database != nil && ctx != nil &&
		validUUID(ownerUserID) && validUUID(resumeID) && expectedVersion >= 1
}

// validNewResume 校验新建持久化记录的必要不变量。
func validNewResume(item resume.Resume) bool {
	temporaryValid := (!item.Temporary && item.ExpiresAt == nil) ||
		(item.Temporary && item.ExpiresAt != nil && item.ExpiresAt.After(item.CreatedAt))
	return validUUID(item.ID) && validUUID(item.OwnerUserID) && validTitle(item.Title) &&
		validFileMetadata(
			item.OriginalFilename,
			item.ContentType,
			item.SizeBytes,
			item.ChecksumSHA256,
			item.ObjectKey,
		) && item.FileStatus == resume.FileUploading &&
		item.ParseStatus == resume.ParseQueued && item.ParseFailureCode == "" &&
		item.CurrentRevision == 0 && temporaryValid &&
		(item.Version == 0 || item.Version == 1)
}

// validFileMetadata 校验 PDF 元数据是否满足迁移约束。
func validFileMetadata(
	filename string,
	contentType string,
	sizeBytes int64,
	checksum string,
	objectKey string,
) bool {
	return strings.TrimSpace(filename) == filename && filename != "" &&
		len(filename) <= 1024 && !strings.ContainsAny(filename, "\x00\r\n/\\") &&
		contentType == "application/pdf" && sizeBytes >= 1 && sizeBytes <= 10*1024*1024 &&
		resumeChecksumPattern.MatchString(checksum) && len(objectKey) >= 16 &&
		len(objectKey) <= 1024 && !strings.Contains(objectKey, "..") &&
		!strings.ContainsAny(objectKey, "\x00\r\n\\")
}

// validTitle 校验简历展示名称是否满足迁移约束。
func validTitle(title string) bool {
	return strings.TrimSpace(title) == title && title != "" && len(title) <= 360 &&
		!strings.ContainsAny(title, "\x00\r\n")
}

// validUUID 校验资源标识是否为规范小写 UUID。
func validUUID(value string) bool {
	return resumeUUIDPattern.MatchString(value)
}

var _ app.Repository = (*GormRepository)(nil)
