package memory

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/jackc/pgx/v5/pgtype"
)

func (repository *PostgresRepository) SearchCandidates(
	ctx context.Context,
	actor requestcontext.Actor,
	queryVector []float32,
	goalID string,
	excludedCanonicalKeys []string,
	configuration SearchConfig,
) ([]SearchCandidate, error) {
	if ctx == nil ||
		!validActor(actor) ||
		(goalID != "" && !validUUID(goalID)) ||
		!ValidStableProfileCanonicalKeys(excludedCanonicalKeys) ||
		!configuration.Valid() {
		return nil, ErrInvalidArgument
	}
	vector, err := vectorLiteral(queryVector, configuration.Dimensions)
	if err != nil {
		return nil, err
	}
	if len(excludedCanonicalKeys) == 0 {
		excludedCanonicalKeys = []string{}
	}
	if goalID != "" {
		var owned bool
		if err := repository.database.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM coaching_goals
    WHERE goal_id = $1
      AND owner_user_id = $2
      AND status = 'active'
)`,
			goalID,
			actor.UserID,
		).Scan(&owned); err != nil {
			return nil, ErrRepository
		}
		if !owned {
			return nil, ErrNotFound
		}
	}
	rows, err := repository.database.Query(ctx, `
WITH eligible AS MATERIALIZED (
    SELECT
        memories.id,
        memories.owner_user_id,
        memories.memory_type,
        memories.canonical_key,
        memories.content,
        memories.scope_type,
        memories.goal_id,
        memories.status,
        memories.version,
        memories.policy_version,
        memories.expires_at,
        memories.created_at,
        memories.updated_at,
        memories.inactivated_at,
        vectors.embedding
    FROM agent_memory_vectors AS vectors
    JOIN agent_memories AS memories
      ON memories.id = vectors.memory_id
     AND memories.owner_user_id = vectors.owner_user_id
     AND memories.version = vectors.memory_version
    JOIN identity_users AS users
      ON users.id = memories.owner_user_id
     AND users.account_status = 'active'
    WHERE memories.owner_user_id = $1
      AND memories.status = 'active'
      AND NOT (memories.canonical_key = ANY($8::text[]))
      AND (
          memories.expires_at IS NULL
          OR memories.expires_at > clock_timestamp()
      )
      AND vectors.provider = $3
      AND vectors.model = $4
      AND vectors.dimension = $5
      AND vectors.embedding_policy_version = $6
      AND (
          (
              memories.scope_type = 'user'
              AND memories.goal_id IS NULL
          )
          OR (
              $7::uuid IS NOT NULL
              AND memories.scope_type = 'goal'
              AND memories.goal_id = $7::uuid
          )
      )
)
SELECT
    eligible.id::text,
    eligible.owner_user_id::text,
    eligible.memory_type,
    eligible.canonical_key,
    eligible.content,
    eligible.scope_type,
    coalesce(eligible.goal_id::text, ''),
    eligible.status,
    eligible.version,
    eligible.policy_version,
    eligible.expires_at,
    eligible.created_at,
    eligible.updated_at,
    eligible.inactivated_at,
    1 - (
        eligible.embedding OPERATOR(public.<=>) $2::public.vector
    ) AS similarity
FROM eligible
ORDER BY
    eligible.embedding OPERATOR(public.<=>) $2::public.vector,
    eligible.id
LIMIT $9`,
		actor.UserID,
		vector,
		configuration.Provider,
		configuration.Model,
		configuration.Dimensions,
		configuration.EmbeddingPolicyVersion,
		nullableUUID(goalID),
		excludedCanonicalKeys,
		configuration.CandidateLimit,
	)
	if err != nil {
		return nil, ErrRepository
	}
	defer rows.Close()
	candidates := make([]SearchCandidate, 0)
	for rows.Next() {
		var candidate SearchCandidate
		item, scanErr := scanMemoryWithSimilarity(rows, &candidate.Similarity)
		if scanErr != nil {
			return nil, ErrRepository
		}
		candidate.Memory = item
		candidates = append(candidates, candidate)
	}
	if rows.Err() != nil {
		return nil, ErrRepository
	}
	return candidates, nil
}

func scanMemoryWithSimilarity(
	row rowScanner,
	similarity *float64,
) (Memory, error) {
	var item Memory
	var expiresAt pgtypeTimestamptz
	var inactivatedAt pgtypeTimestamptz
	if err := row.Scan(
		&item.ID,
		&item.OwnerID,
		&item.Type,
		&item.CanonicalKey,
		&item.Content,
		&item.Scope,
		&item.GoalID,
		&item.Status,
		&item.Version,
		&item.PolicyVersion,
		&expiresAt,
		&item.CreatedAt,
		&item.UpdatedAt,
		&inactivatedAt,
		similarity,
	); err != nil {
		return Memory{}, err
	}
	assignOptionalMemoryTimes(&item, expiresAt, inactivatedAt)
	if !item.Valid() {
		return Memory{}, ErrRepository
	}
	return item, nil
}

// Keep optional timestamp decoding aligned with scanMemory without exposing
// pgx implementation types in the search contract.
type pgtypeTimestamptz = pgtype.Timestamptz

func assignOptionalMemoryTimes(
	item *Memory,
	expiresAt pgtypeTimestamptz,
	inactivatedAt pgtypeTimestamptz,
) {
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	if expiresAt.Valid {
		value := expiresAt.Time.UTC()
		item.ExpiresAt = &value
	}
	if inactivatedAt.Valid {
		value := inactivatedAt.Time.UTC()
		item.InactivatedAt = &value
	}
}
