package evaluation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool                     *pgxpool.Pool
	afterEvidenceSourceFence func()
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) Ensure(
	ctx context.Context,
	command EnsureCommand,
) (Evaluation, bool, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validEnsureCommand(command) {
		return Evaluation{}, false, ErrInvalidRequest
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Evaluation{}, false, fmt.Errorf("begin Evaluation ensure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockActiveOwner(ctx, tx, command.OwnerUserID); err != nil {
		return Evaluation{}, false, err
	}

	var evaluationID string
	err = tx.QueryRow(ctx, `
		INSERT INTO evaluation_ledgers (
			owner_user_id,
			root_idempotency_key,
			root_request_fingerprint,
			practice_session_id,
			input_snapshot_id,
			input_revision,
			scope,
			scene_type
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (owner_user_id, root_idempotency_key) DO NOTHING
		RETURNING id::text
	`, command.OwnerUserID, command.RootIdempotencyKey[:],
		command.RootFingerprint[:], command.Input.PracticeSessionID,
		command.Input.InputSnapshotID, command.Input.InputRevision,
		command.Input.Scope, command.Input.SceneType).Scan(&evaluationID)
	switch {
	case err == nil:
		evaluation, insertErr := insertRevision(
			ctx,
			tx,
			evaluationID,
			command.OwnerUserID,
			command.RootIdempotencyKey,
			1,
			"",
			command.RevisionFingerprint,
			command.Input.Config,
		)
		if insertErr != nil {
			return Evaluation{}, false, insertErr
		}
		if err := tx.Commit(ctx); err != nil {
			return Evaluation{}, false, fmt.Errorf(
				"commit Evaluation ensure: %w",
				err,
			)
		}
		return evaluation, false, nil
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return Evaluation{}, false, fmt.Errorf(
			"insert Evaluation ledger: %w",
			err,
		)
	}

	var persistedFingerprint []byte
	err = tx.QueryRow(ctx, `
		SELECT id::text, root_request_fingerprint
		FROM evaluation_ledgers
		WHERE owner_user_id = $1
		  AND root_idempotency_key = $2
		FOR UPDATE
	`, command.OwnerUserID, command.RootIdempotencyKey[:]).Scan(
		&evaluationID,
		&persistedFingerprint,
	)
	if err != nil {
		return Evaluation{}, false, fmt.Errorf(
			"read Evaluation idempotency replay: %w",
			err,
		)
	}
	if !bytes.Equal(persistedFingerprint, command.RootFingerprint[:]) {
		return Evaluation{}, false, ErrIdempotencyConflict
	}
	evaluation, err := selectLatest(
		ctx,
		tx,
		command.OwnerUserID,
		evaluationID,
	)
	if err != nil {
		return Evaluation{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Evaluation{}, false, fmt.Errorf(
			"commit Evaluation replay: %w",
			err,
		)
	}
	return evaluation, true, nil
}

func (r *PostgresRepository) Reevaluate(
	ctx context.Context,
	command ReevaluateCommand,
) (Evaluation, bool, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validReevaluateCommand(command) {
		return Evaluation{}, false, ErrInvalidRequest
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Evaluation{}, false, fmt.Errorf(
			"begin Evaluation re-evaluation: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockActiveOwner(ctx, tx, command.OwnerUserID); err != nil {
		return Evaluation{}, false, err
	}

	var rootKeyBytes []byte
	err = tx.QueryRow(ctx, `
		SELECT root_idempotency_key
		FROM evaluation_ledgers
		WHERE id = $1
		  AND owner_user_id = $2
		FOR UPDATE
	`, command.EvaluationID, command.OwnerUserID).Scan(&rootKeyBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return Evaluation{}, false, ErrNotFound
	}
	if err != nil {
		return Evaluation{}, false, fmt.Errorf(
			"lock Evaluation ledger: %w",
			err,
		)
	}
	if len(rootKeyBytes) != sha256.Size {
		return Evaluation{}, false, ErrInvalidRequest
	}
	var rootKey [sha256.Size]byte
	copy(rootKey[:], rootKeyBytes)

	var latestRevisionID string
	var latestRevision int
	var latestFingerprint []byte
	err = tx.QueryRow(ctx, `
		SELECT id::text, revision, request_fingerprint
		FROM evaluation_revisions
		WHERE evaluation_id = $1
		  AND owner_user_id = $2
		ORDER BY revision DESC
		LIMIT 1
		FOR UPDATE
	`, command.EvaluationID, command.OwnerUserID).Scan(
		&latestRevisionID,
		&latestRevision,
		&latestFingerprint,
	)
	if err != nil {
		return Evaluation{}, false, fmt.Errorf(
			"read latest Evaluation revision: %w",
			err,
		)
	}
	if bytes.Equal(latestFingerprint, command.RevisionFingerprint[:]) {
		evaluation, selectErr := selectLatest(
			ctx,
			tx,
			command.OwnerUserID,
			command.EvaluationID,
		)
		if selectErr != nil {
			return Evaluation{}, false, selectErr
		}
		if err := tx.Commit(ctx); err != nil {
			return Evaluation{}, false, fmt.Errorf(
				"commit re-evaluation replay: %w",
				err,
			)
		}
		return evaluation, true, nil
	}

	_, err = tx.Exec(ctx, `
		UPDATE evaluation_revision_states
		SET evaluation_status = 'SUPERSEDED',
		    updated_at = transaction_timestamp(),
		    completed_at = transaction_timestamp()
		WHERE revision_id = $1
		  AND evaluation_id = $2
		  AND owner_user_id = $3
	`, latestRevisionID, command.EvaluationID, command.OwnerUserID)
	if err != nil {
		return Evaluation{}, false, fmt.Errorf(
			"supersede previous Evaluation revision: %w",
			err,
		)
	}

	evaluation, err := insertRevision(
		ctx,
		tx,
		command.EvaluationID,
		command.OwnerUserID,
		rootKey,
		latestRevision+1,
		latestRevisionID,
		command.RevisionFingerprint,
		command.Config,
	)
	if err != nil {
		return Evaluation{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Evaluation{}, false, fmt.Errorf(
			"commit Evaluation re-evaluation: %w",
			err,
		)
	}
	return evaluation, false, nil
}

func (r *PostgresRepository) Get(
	ctx context.Context,
	ownerUserID string,
	evaluationID string,
) (Evaluation, error) {
	if r == nil || r.pool == nil || ctx == nil ||
		!validUUID(ownerUserID) || !validUUID(evaluationID) {
		return Evaluation{}, ErrInvalidRequest
	}
	evaluation, err := selectLatest(
		ctx,
		r.pool,
		ownerUserID,
		evaluationID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Evaluation{}, ErrNotFound
	}
	return evaluation, err
}

type queryable interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func selectLatest(
	ctx context.Context,
	db queryable,
	ownerUserID string,
	evaluationID string,
) (Evaluation, error) {
	row := db.QueryRow(ctx, evaluationSelect+`
		WHERE ledger.id = $1
		  AND ledger.owner_user_id = $2
		ORDER BY revision.revision DESC
		LIMIT 1
	`, evaluationID, ownerUserID)
	evaluation, err := scanEvaluation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Evaluation{}, ErrNotFound
	}
	if err != nil {
		return Evaluation{}, fmt.Errorf("read Evaluation: %w", err)
	}
	return evaluation, nil
}

func insertRevision(
	ctx context.Context,
	tx pgx.Tx,
	evaluationID string,
	ownerUserID string,
	rootKey [sha256.Size]byte,
	revision int,
	supersedesRevisionID string,
	fingerprint [sha256.Size]byte,
	config revisionConfig,
) (Evaluation, error) {
	channelStrings := make([]string, len(config.Channels))
	for index, channel := range config.Channels {
		channelStrings[index] = string(channel)
	}
	var revisionID string
	err := tx.QueryRow(ctx, `
		INSERT INTO evaluation_revisions (
			evaluation_id,
			owner_user_id,
			revision,
			supersedes_revision_id,
			channels,
			scene_strategy_ref,
			core_4d_strategy_ref,
			pipeline_version,
			schema_version,
			request_fingerprint,
			client_request_id
		)
		VALUES (
			$1, $2, $3, nullif($4, '')::uuid, $5, nullif($6, ''),
			nullif($7, ''), $8, $9, $10, nullif($11, '')
		)
		RETURNING id::text
	`, evaluationID, ownerUserID, revision, supersedesRevisionID,
		channelStrings, config.SceneStrategyRef, config.Core4DStrategyRef,
		config.PipelineVersion, config.SchemaVersion, fingerprint[:],
		config.ClientRequestID).Scan(&revisionID)
	if err != nil {
		return Evaluation{}, fmt.Errorf("insert Evaluation revision: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO evaluation_revision_states (
			revision_id,
			evaluation_id,
			owner_user_id
		)
		VALUES ($1, $2, $3)
	`, revisionID, evaluationID, ownerUserID)
	if err != nil {
		return Evaluation{}, fmt.Errorf(
			"insert Evaluation revision state: %w",
			err,
		)
	}
	for _, channel := range config.Channels {
		channelKey := deriveChannelKey(
			rootKey,
			revision,
			channel,
			strategyForChannel(config, channel),
		)
		_, err = tx.Exec(ctx, `
			INSERT INTO evaluation_outbox (
				evaluation_id,
				evaluation_revision_id,
				owner_user_id,
				channel,
				channel_key,
				event_type,
				payload
			)
			VALUES (
				$1::uuid,
				$2::uuid,
				$3::uuid,
				$4::text,
				$5::bytea,
				'evaluation.revision.queued',
				jsonb_build_object(
					'evaluation_id', $1::text,
					'evaluation_revision_id', $2::text,
					'revision', $6::integer,
					'channel', $4::text
				)
			)
		`, evaluationID, revisionID, ownerUserID, channel,
			channelKey[:], revision)
		if err != nil {
			return Evaluation{}, fmt.Errorf(
				"insert %s Evaluation outbox: %w",
				channel,
				err,
			)
		}
	}
	evaluation, err := selectLatest(ctx, tx, ownerUserID, evaluationID)
	if err != nil {
		return Evaluation{}, err
	}
	return evaluation, nil
}

func lockActiveOwner(
	ctx context.Context,
	tx pgx.Tx,
	ownerUserID string,
) error {
	var status string
	err := tx.QueryRow(ctx, `
		SELECT owner.account_status
		FROM identity_users AS owner
		WHERE owner.id = $1
		  AND NOT EXISTS (
		      SELECT 1
		      FROM evaluation_deletion_fences AS fence
		      WHERE fence.owner_user_id = owner.id
		  )
		FOR SHARE OF owner
	`, ownerUserID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != "active") {
		return ErrAccountUnavailable
	}
	if err != nil {
		return fmt.Errorf("lock Evaluation owner: %w", err)
	}
	return nil
}

const evaluationSelect = `
	SELECT
		ledger.id::text,
		ledger.owner_user_id::text,
		ledger.practice_session_id,
		ledger.input_snapshot_id,
		ledger.input_revision,
		ledger.scope,
		ledger.scene_type,
		ledger.created_at,
		revision.id::text,
		revision.revision,
		coalesce(revision.supersedes_revision_id::text, ''),
		revision.channels,
		coalesce(revision.scene_strategy_ref, ''),
		coalesce(revision.core_4d_strategy_ref, ''),
		revision.pipeline_version,
		revision.schema_version,
		coalesce(revision.client_request_id, ''),
		state.evaluation_status,
		state.is_final,
		revision.created_at,
		state.updated_at,
		state.completed_at
	FROM evaluation_ledgers AS ledger
	JOIN evaluation_revisions AS revision
	  ON revision.evaluation_id = ledger.id
	 AND revision.owner_user_id = ledger.owner_user_id
	JOIN evaluation_revision_states AS state
	  ON state.revision_id = revision.id
	 AND state.evaluation_id = ledger.id
	 AND state.owner_user_id = ledger.owner_user_id
`

func scanEvaluation(row pgx.Row) (Evaluation, error) {
	var evaluation Evaluation
	var channels []string
	err := row.Scan(
		&evaluation.ID,
		&evaluation.OwnerUserID,
		&evaluation.PracticeSessionID,
		&evaluation.InputSnapshotID,
		&evaluation.InputRevision,
		&evaluation.Scope,
		&evaluation.SceneType,
		&evaluation.CreatedAt,
		&evaluation.Revision.ID,
		&evaluation.Revision.Number,
		&evaluation.Revision.SupersedesRevisionID,
		&channels,
		&evaluation.Revision.SceneStrategyRef,
		&evaluation.Revision.Core4DStrategyRef,
		&evaluation.Revision.PipelineVersion,
		&evaluation.Revision.SchemaVersion,
		&evaluation.Revision.ClientRequestID,
		&evaluation.Revision.Status,
		&evaluation.Revision.IsFinal,
		&evaluation.Revision.CreatedAt,
		&evaluation.Revision.UpdatedAt,
		&evaluation.Revision.CompletedAt,
	)
	if err != nil {
		return Evaluation{}, err
	}
	evaluation.Revision.EvaluationID = evaluation.ID
	evaluation.Revision.OwnerUserID = evaluation.OwnerUserID
	evaluation.Revision.Channels = make([]Channel, len(channels))
	for index, channel := range channels {
		evaluation.Revision.Channels[index] = Channel(channel)
	}
	if !evaluation.Valid() {
		return Evaluation{}, ErrInvalidRequest
	}
	return evaluation, nil
}

func deriveChannelKey(
	rootKey [sha256.Size]byte,
	revision int,
	channel Channel,
	strategyRef string,
) [sha256.Size]byte {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte("evaluation-channel:v1\x00"))
	_, _ = hasher.Write(rootKey[:])
	var revisionBytes [8]byte
	binary.BigEndian.PutUint64(revisionBytes[:], uint64(revision))
	_, _ = hasher.Write(revisionBytes[:])
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(channel))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(strategyRef))
	var result [sha256.Size]byte
	copy(result[:], hasher.Sum(nil))
	return result
}

func strategyForChannel(config revisionConfig, channel Channel) string {
	if channel == ChannelScene {
		return config.SceneStrategyRef
	}
	return config.Core4DStrategyRef
}

func validEnsureCommand(command EnsureCommand) bool {
	return validUUID(command.OwnerUserID) &&
		command.Input.PracticeSessionID != "" &&
		command.Input.InputSnapshotID != "" &&
		command.Input.InputRevision > 0 &&
		validScope(command.Input.Scope) &&
		validSceneType(command.Input.SceneType) &&
		validChannels(command.Input.Config.Channels) &&
		validStrategies(
			command.Input.Config.Channels,
			command.Input.Config.SceneStrategyRef,
			command.Input.Config.Core4DStrategyRef,
		) &&
		validVersion(command.Input.Config.PipelineVersion) &&
		command.Input.Config.SchemaVersion == SchemaVersion &&
		validClientRequestID(command.Input.Config.ClientRequestID)
}

func validReevaluateCommand(command ReevaluateCommand) bool {
	return validUUID(command.OwnerUserID) &&
		validUUID(command.EvaluationID) &&
		validChannels(command.Config.Channels) &&
		validStrategies(
			command.Config.Channels,
			command.Config.SceneStrategyRef,
			command.Config.Core4DStrategyRef,
		) &&
		validVersion(command.Config.PipelineVersion) &&
		command.Config.SchemaVersion == SchemaVersion &&
		validClientRequestID(command.Config.ClientRequestID)
}
