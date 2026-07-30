package persistence

import (
	"context"
	"errors"
	"slices"
	"time"

	. "github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormMultimodalRunRepository adds the one transaction that crosses image,
// Message, Thread, and initial Run tables. All other Run operations continue
// through the embedded repository.
type GormMultimodalRunRepository struct {
	RunRepository
	database *gorm.DB
	ids      IDGenerator
}

func NewGormMultimodalRunRepositoryFromPool(
	delegate RunRepository,
	pool *pgxpool.Pool,
	ids IDGenerator,
) (*GormMultimodalRunRepository, error) {
	if delegate == nil || ids == nil {
		return nil, ErrRepository
	}
	database, err := newGormDatabaseFromPool(pool)
	if err != nil {
		return nil, err
	}
	return &GormMultimodalRunRepository{
		RunRepository: delegate,
		database:      database,
		ids:           ids,
	}, nil
}

type multimodalThreadRecord struct {
	ID                  string    `gorm:"column:id;type:uuid;primaryKey"`
	OwnerID             string    `gorm:"column:owner_user_id;type:uuid"`
	NextMessageSequence int64     `gorm:"column:next_message_sequence"`
	CreatedAt           time.Time `gorm:"column:created_at"`
	UpdatedAt           time.Time `gorm:"column:updated_at"`
}

func (multimodalThreadRecord) TableName() string {
	return "agent_threads"
}

type multimodalMessageRecord struct {
	ID              string    `gorm:"column:id;type:uuid;primaryKey"`
	OwnerID         string    `gorm:"column:owner_user_id;type:uuid"`
	ThreadID        string    `gorm:"column:thread_id;type:uuid"`
	Sequence        int64     `gorm:"column:sequence_no"`
	Role            string    `gorm:"column:role"`
	ClientMessageID *string   `gorm:"column:client_message_id"`
	ProducedByRunID *string   `gorm:"column:produced_by_run_id;type:uuid"`
	Modality        string    `gorm:"column:modality"`
	Content         string    `gorm:"column:content"`
	CreatedAt       time.Time `gorm:"column:created_at"`
}

func (multimodalMessageRecord) TableName() string {
	return "agent_messages"
}

type multimodalRunRecord struct {
	ID                   string     `gorm:"column:id;type:uuid;primaryKey"`
	OwnerID              string     `gorm:"column:owner_user_id;type:uuid"`
	ThreadID             string     `gorm:"column:thread_id;type:uuid"`
	InputMessageID       string     `gorm:"column:input_message_id;type:uuid"`
	Attempt              int        `gorm:"column:attempt_no"`
	RetryOfRunID         *string    `gorm:"column:retry_of_run_id;type:uuid"`
	RetryClientID        *string    `gorm:"column:retry_client_id"`
	Status               string     `gorm:"column:status"`
	RequestedProvider    string     `gorm:"column:requested_provider"`
	RequestedModel       string     `gorm:"column:requested_model"`
	MaxOutputTokens      int        `gorm:"column:max_output_tokens"`
	MaxInputCharacters   int        `gorm:"column:max_input_characters"`
	WorkerLeaseToken     *string    `gorm:"column:worker_lease_token;type:uuid"`
	WorkerLeaseExpiresAt *time.Time `gorm:"column:worker_lease_expires_at"`
	AssistantMessageID   *string    `gorm:"column:assistant_message_id;type:uuid"`
	ProviderCompletionID *string    `gorm:"column:provider_completion_id"`
	ProviderModel        *string    `gorm:"column:provider_model"`
	FinishReason         *string    `gorm:"column:finish_reason"`
	InputTokens          *int       `gorm:"column:input_tokens"`
	OutputTokens         *int       `gorm:"column:output_tokens"`
	TotalTokens          *int       `gorm:"column:total_tokens"`
	FailureKind          *string    `gorm:"column:failure_kind"`
	FailureRetryable     *bool      `gorm:"column:failure_retryable"`
	CreatedAt            time.Time  `gorm:"column:created_at"`
	StartedAt            *time.Time `gorm:"column:started_at"`
	CompletedAt          *time.Time `gorm:"column:completed_at"`
	UpdatedAt            time.Time  `gorm:"column:updated_at"`
}

func (multimodalRunRecord) TableName() string {
	return "agent_runs"
}

func (r *GormMultimodalRunRepository) CreateInitialMultimodalRun(
	ctx context.Context,
	ownerID string,
	threadID string,
	clientMessageID string,
	content string,
	imageAssetIDs []string,
	configuration RunConfiguration,
) (RunSubmission, error) {
	if ctx == nil || !ValidUUID(ownerID) || !ValidUUID(threadID) ||
		!ValidClientMessageID(clientMessageID) ||
		!ValidMessageContent(content) ||
		!validImageAssetIDs(imageAssetIDs) ||
		!ValidRunConfiguration(configuration) {
		return RunSubmission{}, ErrInvalidRequest
	}

	var submission RunSubmission
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var thread multimodalThreadRecord
		err := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND owner_user_id = ?", threadID, ownerID).
			Take(&thread).Error
		if err != nil {
			return mapGormError(err)
		}
		var activeOwner int64
		if err := tx.
			Table("identity_users").
			Where("id = ? AND account_status = ?", ownerID, "active").
			Count(&activeOwner).Error; err != nil {
			return mapGormError(err)
		}
		if activeOwner != 1 {
			return ErrNotFound
		}

		message, found, err := findMultimodalMessage(
			tx,
			ownerID,
			threadID,
			clientMessageID,
		)
		if err != nil {
			return err
		}
		if found {
			if message.Content != content ||
				message.Role != string(MessageRoleUser) ||
				message.Modality != string(MessageModalityMultimodal) {
				return ErrIdempotencyConflict
			}
			existingIDs, err := findMessageImageIDs(tx, message.ID)
			if err != nil {
				return err
			}
			if !slices.Equal(existingIDs, imageAssetIDs) {
				return ErrIdempotencyConflict
			}
			run, found, err := findInitialMultimodalRun(
				tx,
				ownerID,
				threadID,
				message.ID,
			)
			if err != nil {
				return err
			}
			if !found {
				return ErrConflict
			}
			submission = RunSubmission{
				Run:         run.toDomain(),
				UserMessage: message.toDomain(),
				Created:     false,
			}
			return nil
		}

		assets, err := lockAttachableImageAssets(
			tx,
			ownerID,
			threadID,
			imageAssetIDs,
		)
		if err != nil {
			return err
		}
		messageID, err := r.ids.NewID()
		if err != nil {
			return ErrRepository
		}
		runID, err := r.ids.NewID()
		if err != nil {
			return ErrRepository
		}
		now := time.Now().UTC()
		message = multimodalMessageRecord{
			ID:              messageID,
			OwnerID:         ownerID,
			ThreadID:        threadID,
			Sequence:        thread.NextMessageSequence,
			Role:            string(MessageRoleUser),
			ClientMessageID: &clientMessageID,
			Modality:        string(MessageModalityMultimodal),
			Content:         content,
			CreatedAt:       now,
		}
		if err := tx.Create(&message).Error; err != nil {
			return mapGormError(err)
		}
		if err := attachLockedImageAssets(
			tx,
			ownerID,
			threadID,
			messageID,
			imageAssetIDs,
			assets,
			now,
		); err != nil {
			return err
		}
		result := tx.
			Model(&multimodalThreadRecord{}).
			Where("id = ? AND owner_user_id = ?", threadID, ownerID).
			Updates(map[string]any{
				"next_message_sequence": gorm.Expr(
					"next_message_sequence + 1",
				),
				"updated_at": nextDatabaseTimestamp(),
			})
		if result.Error != nil {
			return mapGormError(result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrNotFound
		}

		run := multimodalRunRecord{
			ID:                 runID,
			OwnerID:            ownerID,
			ThreadID:           threadID,
			InputMessageID:     messageID,
			Attempt:            1,
			Status:             string(RunStatusPending),
			RequestedProvider:  configuration.Provider,
			RequestedModel:     configuration.Model,
			MaxOutputTokens:    configuration.MaxOutputTokens,
			MaxInputCharacters: configuration.MaxInputCharacters,
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		if err := tx.Create(&run).Error; err != nil {
			return mapGormError(err)
		}
		submission = RunSubmission{
			Run:         run.toDomain(),
			UserMessage: message.toDomain(),
			Created:     true,
		}
		return nil
	})
	if err != nil {
		return RunSubmission{}, err
	}
	return submission, nil
}

func findMultimodalMessage(
	tx *gorm.DB,
	ownerID string,
	threadID string,
	clientMessageID string,
) (multimodalMessageRecord, bool, error) {
	var message multimodalMessageRecord
	err := tx.
		Where(
			"owner_user_id = ? AND thread_id = ? AND client_message_id = ?",
			ownerID,
			threadID,
			clientMessageID,
		).
		Take(&message).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return multimodalMessageRecord{}, false, nil
	}
	if err != nil {
		return multimodalMessageRecord{}, false, mapGormError(err)
	}
	return message, true, nil
}

func findMessageImageIDs(tx *gorm.DB, messageID string) ([]string, error) {
	var links []messageImageRecord
	if err := tx.
		Where("message_id = ?", messageID).
		Order("position").
		Find(&links).Error; err != nil {
		return nil, mapGormError(err)
	}
	result := make([]string, 0, len(links))
	for _, link := range links {
		result = append(result, link.AssetID)
	}
	return result, nil
}

func findInitialMultimodalRun(
	tx *gorm.DB,
	ownerID string,
	threadID string,
	messageID string,
) (multimodalRunRecord, bool, error) {
	var run multimodalRunRecord
	err := tx.
		Where(
			`owner_user_id = ? AND thread_id = ?
				AND input_message_id = ? AND attempt_no = 1`,
			ownerID,
			threadID,
			messageID,
		).
		Take(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return multimodalRunRecord{}, false, nil
	}
	if err != nil {
		return multimodalRunRecord{}, false, mapGormError(err)
	}
	return run, true, nil
}

func lockAttachableImageAssets(
	tx *gorm.DB,
	ownerID string,
	threadID string,
	imageAssetIDs []string,
) (map[string]imageAssetRecord, error) {
	var records []imageAssetRecord
	err := tx.
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("owner_user_id = ? AND thread_id = ?", ownerID, threadID).
		Where("image_asset_id IN ?", imageAssetIDs).
		Order("image_asset_id").
		Find(&records).Error
	if err != nil {
		return nil, mapGormError(err)
	}
	if len(records) != len(imageAssetIDs) {
		return nil, ErrNotFound
	}
	now := time.Now().UTC()
	result := make(map[string]imageAssetRecord, len(records))
	for _, record := range records {
		if record.Status != string(ImageAssetStaged) ||
			record.ETag == "" ||
			record.UploadLeaseUntil != nil ||
			!record.ExpiresAt.After(now) {
			return nil, ErrConflict
		}
		result[record.ID] = record
	}
	return result, nil
}

func attachLockedImageAssets(
	tx *gorm.DB,
	ownerID string,
	threadID string,
	messageID string,
	imageAssetIDs []string,
	assets map[string]imageAssetRecord,
	now time.Time,
) error {
	for position, assetID := range imageAssetIDs {
		if _, found := assets[assetID]; !found {
			return ErrNotFound
		}
		link := messageImageRecord{
			OwnerID:   ownerID,
			ThreadID:  threadID,
			MessageID: messageID,
			AssetID:   assetID,
			Position:  position,
			CreatedAt: now,
		}
		if err := tx.Create(&link).Error; err != nil {
			return mapGormError(err)
		}
		result := tx.
			Model(&imageAssetRecord{}).
			Where(
				`image_asset_id = ? AND owner_user_id = ?
					AND thread_id = ? AND status = ? AND etag <> ''`,
				assetID,
				ownerID,
				threadID,
				string(ImageAssetStaged),
			).
			Updates(map[string]any{
				"status":      string(ImageAssetAttached),
				"attached_at": now,
				"updated_at":  nextDatabaseTimestamp(),
			})
		if result.Error != nil {
			return mapGormError(result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
	}
	return nil
}

func (record multimodalMessageRecord) toDomain() Message {
	message := Message{
		ID:        record.ID,
		OwnerID:   record.OwnerID,
		ThreadID:  record.ThreadID,
		Sequence:  record.Sequence,
		Role:      MessageRole(record.Role),
		Modality:  MessageModality(record.Modality),
		Content:   record.Content,
		CreatedAt: record.CreatedAt.UTC(),
	}
	if record.ClientMessageID != nil {
		message.ClientMessageID = *record.ClientMessageID
	}
	if record.ProducedByRunID != nil {
		message.ProducedByRunID = *record.ProducedByRunID
	}
	return message
}

func (record multimodalRunRecord) toDomain() Run {
	run := Run{
		ID:                 record.ID,
		OwnerID:            record.OwnerID,
		ThreadID:           record.ThreadID,
		InputMessageID:     record.InputMessageID,
		Attempt:            record.Attempt,
		Status:             RunStatus(record.Status),
		RequestedProvider:  record.RequestedProvider,
		RequestedModel:     record.RequestedModel,
		MaxOutputTokens:    record.MaxOutputTokens,
		MaxInputCharacters: record.MaxInputCharacters,
		CreatedAt:          record.CreatedAt.UTC(),
		UpdatedAt:          record.UpdatedAt.UTC(),
	}
	run.RetryOfRunID = stringValue(record.RetryOfRunID)
	run.RetryClientID = stringValue(record.RetryClientID)
	run.WorkerLeaseToken = stringValue(record.WorkerLeaseToken)
	run.AssistantMessageID = stringValue(record.AssistantMessageID)
	run.ProviderCompletionID = stringValue(record.ProviderCompletionID)
	run.ProviderModel = stringValue(record.ProviderModel)
	run.FinishReason = stringValue(record.FinishReason)
	run.FailureKind = stringValue(record.FailureKind)
	if record.WorkerLeaseExpiresAt != nil {
		run.WorkerLeaseExpiresAt = record.WorkerLeaseExpiresAt.UTC()
	}
	if record.StartedAt != nil {
		run.StartedAt = record.StartedAt.UTC()
	}
	if record.CompletedAt != nil {
		run.CompletedAt = record.CompletedAt.UTC()
	}
	if record.InputTokens != nil {
		run.Usage.InputTokens = *record.InputTokens
	}
	if record.OutputTokens != nil {
		run.Usage.OutputTokens = *record.OutputTokens
	}
	if record.TotalTokens != nil {
		run.Usage.TotalTokens = *record.TotalTokens
	}
	if record.FailureRetryable != nil {
		run.FailureRetryable = *record.FailureRetryable
	}
	return run
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

var _ MultimodalRunRepository = (*GormMultimodalRunRepository)(nil)
var _ RunRepository = (*GormMultimodalRunRepository)(nil)
