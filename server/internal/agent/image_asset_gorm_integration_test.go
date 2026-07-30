package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/core"
	agentpersistence "github.com/1024XEngineer/XE3-ESL/server/internal/agent/persistence"
)

const (
	imageTestThreadID  = "40000000-0000-4000-8000-000000000001"
	imageTestMessageID = "50000000-0000-4000-8000-000000000001"
	imageTestAssetID   = "60000000-0000-4000-8000-000000000001"
	imageTestRunID     = "70000000-0000-4000-8000-000000000001"
	imageTestOtherID   = "80000000-0000-4000-8000-000000000001"
)

func TestGormImageAssetRepositoryLifecycleAndIsolation(t *testing.T) {
	database := newAgentTestDatabase(t)
	ctx := context.Background()
	if _, err := database.pool.Exec(ctx, `
INSERT INTO agent_threads (id, owner_user_id)
VALUES ($1, $2)`,
		imageTestThreadID,
		agentTestUserA,
	); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := database.pool.Exec(ctx, `
INSERT INTO agent_messages (
    id,
    owner_user_id,
    thread_id,
    sequence_no,
    role,
    client_message_id,
    content
) VALUES ($1, $2, $3, 1, 'user', 'image-message-1', 'look')`,
		imageTestMessageID,
		agentTestUserA,
		imageTestThreadID,
	); err != nil {
		t.Fatalf("insert message: %v", err)
	}

	repository, err := agentpersistence.NewGormImageAssetRepositoryFromPool(
		database.pool,
	)
	if err != nil {
		t.Fatalf("new GORM image repository: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	candidate := core.ImageAsset{
		ID:              imageTestAssetID,
		OwnerID:         agentTestUserA,
		ThreadID:        imageTestThreadID,
		UploadRequestID: "image-upload-1",
		ObjectKey: core.AgentImageObjectPrefix +
			imageTestAssetID +
			".png",
		ContentType: "image/png",
		Size:        128,
		Width:       16,
		Height:      8,
		ChecksumSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Status:    core.ImageAssetStaged,
		ExpiresAt: now.Add(time.Hour),
		CreatedAt: now,
		UpdatedAt: now,
	}

	stage, err := repository.StageImageAsset(ctx, candidate)
	if err != nil {
		t.Fatalf("stage image: %v", err)
	}
	if !stage.Created || stage.Asset.ID != candidate.ID {
		t.Fatalf("stage = %#v", stage)
	}
	replay, err := repository.StageImageAsset(ctx, candidate)
	if err != nil {
		t.Fatalf("replay image stage: %v", err)
	}
	if replay.Created || replay.Asset.ID != candidate.ID {
		t.Fatalf("replay = %#v", replay)
	}

	if _, err := repository.FindImageAsset(
		ctx,
		agentTestUserB,
		candidate.ID,
	); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("cross-owner find error = %v", err)
	}

	claim, acquired, err := repository.ClaimImageUpload(
		ctx,
		candidate.OwnerID,
		candidate.ID,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("claim upload: %v", err)
	}
	if !acquired || claim.FencingToken != 1 {
		t.Fatalf("claim = %#v, acquired = %v", claim, acquired)
	}
	uploaded, err := repository.CommitImageUpload(
		ctx,
		candidate.OwnerID,
		candidate.ID,
		claim.FencingToken,
		"etag-1",
	)
	if err != nil {
		t.Fatalf("commit upload: %v", err)
	}
	if uploaded.ETag != "etag-1" ||
		!uploaded.UploadLeaseUntil.IsZero() {
		t.Fatalf("uploaded = %#v", uploaded)
	}

	attached, err := repository.AttachImageAssets(
		ctx,
		candidate.OwnerID,
		candidate.ThreadID,
		imageTestMessageID,
		[]string{candidate.ID},
	)
	if err != nil {
		t.Fatalf("attach image: %v", err)
	}
	if len(attached) != 1 ||
		attached[0].Status != core.ImageAssetAttached ||
		attached[0].AttachedAt.IsZero() {
		t.Fatalf("attached = %#v", attached)
	}

	deleting, err := repository.BeginImageAssetDeletion(
		ctx,
		candidate.OwnerID,
		candidate.ID,
	)
	if err != nil {
		t.Fatalf("begin deletion: %v", err)
	}
	if deleting.Status != core.ImageAssetDeleting {
		t.Fatalf("deleting = %#v", deleting)
	}
	deleted, err := repository.FinishImageAssetDeletion(
		ctx,
		candidate.OwnerID,
		candidate.ID,
	)
	if err != nil {
		t.Fatalf("finish deletion: %v", err)
	}
	if deleted.Status != core.ImageAssetDeleted ||
		deleted.DeletedAt.IsZero() {
		t.Fatalf("deleted = %#v", deleted)
	}
}

func TestGormMultimodalSubmissionCommitsMessageRunAndImagesTogether(
	t *testing.T,
) {
	database := newAgentTestDatabase(t)
	ctx := context.Background()
	if _, err := database.pool.Exec(ctx, `
INSERT INTO agent_threads (id, owner_user_id)
VALUES ($1, $2)`,
		imageTestThreadID,
		agentTestUserA,
	); err != nil {
		t.Fatalf("insert thread: %v", err)
	}

	imageRepository, err :=
		agentpersistence.NewGormImageAssetRepositoryFromPool(database.pool)
	if err != nil {
		t.Fatalf("new image repository: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	candidate := core.ImageAsset{
		ID:              imageTestAssetID,
		OwnerID:         agentTestUserA,
		ThreadID:        imageTestThreadID,
		UploadRequestID: "multimodal-upload-1",
		ObjectKey: core.AgentImageObjectPrefix +
			imageTestAssetID +
			".png",
		ContentType: "image/png",
		Size:        128,
		Width:       16,
		Height:      8,
		ChecksumSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" +
			"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Status:    core.ImageAssetStaged,
		ExpiresAt: now.Add(time.Hour),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := imageRepository.StageImageAsset(ctx, candidate); err != nil {
		t.Fatalf("stage image: %v", err)
	}
	claim, acquired, err := imageRepository.ClaimImageUpload(
		ctx,
		candidate.OwnerID,
		candidate.ID,
		time.Minute,
	)
	if err != nil || !acquired {
		t.Fatalf("claim upload = %#v, %v, %v", claim, acquired, err)
	}
	if _, err := imageRepository.CommitImageUpload(
		ctx,
		candidate.OwnerID,
		candidate.ID,
		claim.FencingToken,
		"etag-multimodal",
	); err != nil {
		t.Fatalf("commit upload: %v", err)
	}

	ids := &gormMultimodalTestIDs{
		values: []string{imageTestMessageID, imageTestRunID},
	}
	baseRepository, err := NewPostgresRepository(database.pool, ids)
	if err != nil {
		t.Fatalf("new base repository: %v", err)
	}
	repository, err :=
		agentpersistence.NewGormMultimodalRunRepositoryFromPool(
			baseRepository,
			database.pool,
			ids,
		)
	if err != nil {
		t.Fatalf("new multimodal repository: %v", err)
	}
	configuration := core.RunConfiguration{
		Provider:           "fake",
		Model:              "fake-multimodal-v1",
		MaxOutputTokens:    256,
		MaxInputCharacters: 12000,
	}
	submission, err := repository.CreateInitialMultimodalRun(
		ctx,
		agentTestUserA,
		imageTestThreadID,
		"multimodal-message-1",
		"Please review this image.",
		[]string{imageTestAssetID},
		configuration,
	)
	if err != nil {
		t.Fatalf("create multimodal run: %v", err)
	}
	if !submission.Created ||
		submission.UserMessage.Modality != core.MessageModalityMultimodal ||
		submission.Run.ID != imageTestRunID {
		t.Fatalf("submission = %#v", submission)
	}
	var linkCount int
	if err := database.pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM agent_message_images
WHERE message_id = $1
  AND image_asset_id = $2`,
		imageTestMessageID,
		imageTestAssetID,
	).Scan(&linkCount); err != nil {
		t.Fatalf("count image links: %v", err)
	}
	if linkCount != 1 {
		t.Fatalf("link count = %d", linkCount)
	}

	replay, err := repository.CreateInitialMultimodalRun(
		ctx,
		agentTestUserA,
		imageTestThreadID,
		"multimodal-message-1",
		"Please review this image.",
		[]string{imageTestAssetID},
		configuration,
	)
	if err != nil {
		t.Fatalf("replay multimodal run: %v", err)
	}
	if replay.Created || replay.Run.ID != submission.Run.ID {
		t.Fatalf("replay = %#v", replay)
	}
	if _, err := repository.CreateInitialMultimodalRun(
		ctx,
		agentTestUserA,
		imageTestThreadID,
		"multimodal-message-1",
		"Please review this image.",
		[]string{imageTestOtherID},
		configuration,
	); !errors.Is(err, core.ErrIdempotencyConflict) {
		t.Fatalf("changed image replay error = %v", err)
	}
}

type gormMultimodalTestIDs struct {
	mu     sync.Mutex
	values []string
}

func (ids *gormMultimodalTestIDs) NewID() (string, error) {
	ids.mu.Lock()
	defer ids.mu.Unlock()
	if len(ids.values) == 0 {
		return "", errors.New("test IDs exhausted")
	}
	value := ids.values[0]
	ids.values = ids.values[1:]
	return value, nil
}
