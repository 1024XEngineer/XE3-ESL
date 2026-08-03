package store

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	agentimage "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/image"
	agentrun "github.com/1024XEngineer/XE3-ESL/server/internal/agent/run"
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormImageRunRepository atomically creates a user message with image inputs,
// its image links, and the initial Run.
type GormImageRunRepository struct {
	*PostgresStore
	database *gorm.DB
	ids      idGenerator
}

func NewGormImageRunRepositoryFromPool(
	delegate *PostgresStore,
	pool *pgxpool.Pool,
	ids idGenerator,
) (*GormImageRunRepository, error) {
	if delegate == nil || ids == nil {
		return nil, agentrun.ErrRepository
	}
	database, err := newGormDatabaseFromPool(pool)
	if err != nil {
		return nil, agentrun.ErrRepository
	}
	return &GormImageRunRepository{
		PostgresStore: delegate,
		database:      database,
		ids:           ids,
	}, nil
}

type imageRunThreadRecord struct {
	ID                  string    `gorm:"column:id;type:uuid;primaryKey"`
	OwnerID             string    `gorm:"column:owner_user_id;type:uuid"`
	NextMessageSequence int64     `gorm:"column:next_message_sequence"`
	CreatedAt           time.Time `gorm:"column:created_at"`
	UpdatedAt           time.Time `gorm:"column:updated_at"`
}

func (imageRunThreadRecord) TableName() string {
	return "agent_threads"
}

type imageRunMessageRecord struct {
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

func (imageRunMessageRecord) TableName() string {
	return "agent_messages"
}

type imageRunRecord struct {
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

func (imageRunRecord) TableName() string {
	return "agent_runs"
}

func (r *GormImageRunRepository) CreateInitialWithImages(
	ctx context.Context,
	ownerID string,
	threadID string,
	clientMessageID string,
	content string,
	imageAssetIDs []string,
	configuration agentrun.Configuration,
) (agentrun.Submission, error) {
	if ctx == nil || !conversation.ValidUUID(ownerID) || !conversation.ValidUUID(threadID) ||
		!conversation.ValidClientMessageID(clientMessageID) ||
		!conversation.ValidMessageContent(content) ||
		!agentimage.ValidAssetIDs(imageAssetIDs) ||
		!agentrun.ValidConfiguration(configuration) {
		return agentrun.Submission{}, agentrun.ErrInvalidRequest
	}

	var submission agentrun.Submission
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var thread imageRunThreadRecord
		err := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND owner_user_id = ?", threadID, ownerID).
			Take(&thread).Error
		if err != nil {
			return mapRunGormError(err)
		}
		var activeOwner int64
		if err := tx.
			Table("identity_users").
			Where("id = ? AND account_status = ?", ownerID, "active").
			Count(&activeOwner).Error; err != nil {
			return mapRunGormError(err)
		}
		if activeOwner != 1 {
			return agentrun.ErrNotFound
		}

		message, found, err := findImageRunMessage(
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
				message.Role != string(conversation.MessageRoleUser) ||
				message.Modality != string(conversation.MessageModalityMultimodal) {
				return agentrun.ErrIdempotencyConflict
			}
			existingIDs, err := findMessageImageIDs(tx, message.ID)
			if err != nil {
				return err
			}
			if !slices.Equal(existingIDs, imageAssetIDs) {
				return agentrun.ErrIdempotencyConflict
			}
			run, found, err := findInitialImageRun(
				tx,
				ownerID,
				threadID,
				message.ID,
			)
			if err != nil {
				return err
			}
			if !found {
				return agentrun.ErrConflict
			}
			submission = agentrun.Submission{
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
			return agentrun.ErrRepository
		}
		runID, err := r.ids.NewID()
		if err != nil {
			return agentrun.ErrRepository
		}
		now := time.Now().UTC()
		message = imageRunMessageRecord{
			ID:              messageID,
			OwnerID:         ownerID,
			ThreadID:        threadID,
			Sequence:        thread.NextMessageSequence,
			Role:            string(conversation.MessageRoleUser),
			ClientMessageID: &clientMessageID,
			Modality:        string(conversation.MessageModalityMultimodal),
			Content:         content,
			CreatedAt:       now,
		}
		if err := tx.Create(&message).Error; err != nil {
			return mapRunGormError(err)
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
			Model(&imageRunThreadRecord{}).
			Where("id = ? AND owner_user_id = ?", threadID, ownerID).
			Updates(map[string]any{
				"next_message_sequence": gorm.Expr(
					"next_message_sequence + 1",
				),
				"updated_at": nextDatabaseTimestamp(),
			})
		if result.Error != nil {
			return mapRunGormError(result.Error)
		}
		if result.RowsAffected != 1 {
			return agentrun.ErrNotFound
		}

		run := imageRunRecord{
			ID:                 runID,
			OwnerID:            ownerID,
			ThreadID:           threadID,
			InputMessageID:     messageID,
			Attempt:            1,
			Status:             string(agentrun.StatusPending),
			RequestedProvider:  configuration.Provider,
			RequestedModel:     configuration.Model,
			MaxOutputTokens:    configuration.MaxOutputTokens,
			MaxInputCharacters: configuration.MaxInputCharacters,
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		if err := tx.Create(&run).Error; err != nil {
			return mapRunGormError(err)
		}
		submission = agentrun.Submission{
			Run:         run.toDomain(),
			UserMessage: message.toDomain(),
			Created:     true,
		}
		return nil
	})
	if err != nil {
		return agentrun.Submission{}, err
	}
	return submission, nil
}

func findImageRunMessage(
	tx *gorm.DB,
	ownerID string,
	threadID string,
	clientMessageID string,
) (imageRunMessageRecord, bool, error) {
	var message imageRunMessageRecord
	err := tx.
		Where(
			"owner_user_id = ? AND thread_id = ? AND client_message_id = ?",
			ownerID,
			threadID,
			clientMessageID,
		).
		Take(&message).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return imageRunMessageRecord{}, false, nil
	}
	if err != nil {
		return imageRunMessageRecord{}, false, mapRunGormError(err)
	}
	return message, true, nil
}

func findMessageImageIDs(tx *gorm.DB, messageID string) ([]string, error) {
	var links []messageImageRecord
	if err := tx.
		Where("message_id = ?", messageID).
		Order("position").
		Find(&links).Error; err != nil {
		return nil, mapRunGormError(err)
	}
	result := make([]string, 0, len(links))
	for _, link := range links {
		result = append(result, link.AssetID)
	}
	return result, nil
}

func findInitialImageRun(
	tx *gorm.DB,
	ownerID string,
	threadID string,
	messageID string,
) (imageRunRecord, bool, error) {
	var run imageRunRecord
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
		return imageRunRecord{}, false, nil
	}
	if err != nil {
		return imageRunRecord{}, false, mapRunGormError(err)
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
		return nil, mapRunGormError(err)
	}
	if len(records) != len(imageAssetIDs) {
		return nil, agentrun.ErrNotFound
	}
	now := time.Now().UTC()
	result := make(map[string]imageAssetRecord, len(records))
	for _, record := range records {
		if record.Status != string(agentimage.StatusStaged) ||
			record.ETag == "" ||
			record.UploadLeaseUntil != nil ||
			!record.ExpiresAt.After(now) {
			return nil, agentrun.ErrConflict
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
			return agentrun.ErrNotFound
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
			return mapRunGormError(err)
		}
		result := tx.
			Model(&imageAssetRecord{}).
			Where(
				`image_asset_id = ? AND owner_user_id = ?
					AND thread_id = ? AND status = ? AND etag <> ''`,
				assetID,
				ownerID,
				threadID,
				string(agentimage.StatusStaged),
			).
			Updates(map[string]any{
				"status":      string(agentimage.StatusAttached),
				"attached_at": now,
				"updated_at":  nextDatabaseTimestamp(),
			})
		if result.Error != nil {
			return mapRunGormError(result.Error)
		}
		if result.RowsAffected != 1 {
			return agentrun.ErrConflict
		}
	}
	return nil
}

func (record imageRunMessageRecord) toDomain() conversation.Message {
	message := conversation.Message{
		ID:        record.ID,
		OwnerID:   record.OwnerID,
		ThreadID:  record.ThreadID,
		Sequence:  record.Sequence,
		Role:      conversation.MessageRole(record.Role),
		Modality:  conversation.MessageModality(record.Modality),
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

func (record imageRunRecord) toDomain() agentrun.Run {
	run := agentrun.Run{
		ID:                 record.ID,
		OwnerID:            record.OwnerID,
		ThreadID:           record.ThreadID,
		InputMessageID:     record.InputMessageID,
		Attempt:            record.Attempt,
		Status:             agentrun.Status(record.Status),
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

func mapRunGormError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return agentrun.ErrNotFound
	}
	return mapRunPostgresError(err)
}

var _ agentrun.ImageSubmissionRepository = (*GormImageRunRepository)(nil)
