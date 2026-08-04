package review_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai/xfyun"
	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
	"github.com/1024XEngineer/XE3-ESL/server/migrations"
)

func TestPostgresSpeechFeedbackRejectsTextTurnWithoutReadableAudio(
	t *testing.T,
) {
	pool := speechFeedbackDatabase(t)
	const (
		ownerID     = "10000000-0000-4000-8000-000000000001"
		sessionID   = "practice-daily-1"
		turnID      = "turn-daily-1"
		candidateID = "candidate-daily-1"
	)
	ctx := context.Background()
	insertConversationSpeechFeedbackFixture(
		t,
		pool,
		ownerID,
		sessionID,
		turnID,
		candidateID,
		false,
	)
	repository := review.NewPostgresRepository(pool)
	if _, err := repository.EnsureConfirmedConversationTurn(
		ctx,
		ownerID,
		sessionID,
		turnID,
	); !errors.Is(err, review.ErrSpeechFeedbackNotApplicable) {
		t.Fatalf("text Turn ensure error = %v", err)
	}
	assertSpeechFeedbackCounts(t, pool, ownerID, 0, 0)

	if _, err := pool.Exec(ctx, `
		INSERT INTO conversation_audio_assets (
			audio_asset_id,
			owner_user_id,
			candidate_id,
			turn_id,
			checksum_sha256,
			status,
			version
		)
		VALUES (
			'audio-1',
			$1,
			$2,
			$3,
			repeat('a', 64),
			'readable',
			1
		)
	`, ownerID, candidateID, turnID); err != nil {
		t.Fatalf("insert readable audio fixture: %v", err)
	}
	first, err := repository.EnsureConfirmedConversationTurn(
		ctx,
		ownerID,
		sessionID,
		turnID,
	)
	if err != nil {
		t.Fatalf("ensure voice Turn SpeechFeedback: %v", err)
	}
	second, err := repository.EnsureConfirmedConversationTurn(
		ctx,
		ownerID,
		sessionID,
		turnID,
	)
	if err != nil {
		t.Fatalf("replay voice Turn SpeechFeedback: %v", err)
	}
	if first != second || first.SpeechFeedbackID == "" {
		t.Fatalf("idempotent references = %#v / %#v", first, second)
	}
	assertSpeechFeedbackCounts(t, pool, ownerID, 1, 1)
}

func TestPostgresSpeechFeedbackPersistsTopicAcousticEvidence(t *testing.T) {
	pool := speechFeedbackDatabase(t)
	const (
		ownerID     = "10000000-0000-4000-8000-000000000001"
		sessionID   = "practice-interview-topic"
		turnID      = "turn-interview-topic"
		candidateID = "candidate-interview-topic"
	)
	insertConversationSpeechFeedbackFixture(
		t,
		pool,
		ownerID,
		sessionID,
		turnID,
		candidateID,
		true,
	)
	repository := review.NewPostgresRepository(pool)
	reference, err := repository.EnsureConfirmedConversationTurn(
		context.Background(),
		ownerID,
		sessionID,
		turnID,
	)
	if err != nil {
		t.Fatal(err)
	}
	configuration := review.SpeechFeedbackWorkerConfiguration{
		MaxAttempts:     3,
		LeaseDuration:   time.Second,
		RetryDelay:      time.Second,
		StrategyRef:     review.SpeechFeedbackStrategyRef,
		PipelineVersion: review.SpeechFeedbackPipelineVersion,
		PromptVersion:   review.SpeechFeedbackPromptVersion,
		Provider:        "qianwen",
		Model:           "qwen-plus",
	}
	claim, acquired, err := repository.ClaimSpeechFeedback(
		context.Background(),
		configuration,
	)
	if err != nil || !acquired {
		t.Fatalf("claim = %#v, %t, %v", claim, acquired, err)
	}
	pronunciation, speed, semantic := 88.5, 156.0, 82.0
	err = repository.SaveSpeechFeedbackAcousticEvidence(
		context.Background(),
		claim,
		review.SpeechFeedbackAcousticEvidence{
			Assessment: review.SpeechFeedbackAcousticAssessment{
				Pronunciation:      review.SpeechFeedbackAssessed,
				AcousticFluency:    review.SpeechFeedbackAssessed,
				PronunciationScore: &pronunciation,
				SpeakingSpeedWPM:   &speed,
				SemanticScore:      &semantic,
				Provider: review.
					SpeechFeedbackAcousticProviderName,
				ProviderSession: "ise-topic-session-1",
				Category:        "topic",
				Notice: review.
					SpeechFeedbackAcousticNotice,
			},
			RawResult:       "<xml_result/>",
			AvailableFields: []xfyun.ResultField{},
		},
	)
	if err != nil {
		t.Fatalf("save topic acoustics: %v", err)
	}
	feedback, err := repository.GetSpeechFeedback(
		context.Background(),
		ownerID,
		reference.SpeechFeedbackID,
	)
	if err != nil {
		t.Fatal(err)
	}
	assessment := feedback.AcousticAssessment
	if assessment.Category != "topic" ||
		assessment.PronunciationScore == nil ||
		*assessment.PronunciationScore != pronunciation ||
		assessment.SpeakingSpeedWPM == nil ||
		*assessment.SpeakingSpeedWPM != speed ||
		assessment.SemanticScore == nil ||
		*assessment.SemanticScore != semantic {
		t.Fatalf("topic assessment = %#v", assessment)
	}
}

func TestPostgresSpeechFeedbackLeaseFencingAndExactItemRead(
	t *testing.T,
) {
	pool := speechFeedbackDatabase(t)
	const (
		ownerID     = "10000000-0000-4000-8000-000000000001"
		otherOwner  = "10000000-0000-4000-8000-000000000002"
		sessionID   = "practice-daily-1"
		turnID      = "turn-daily-1"
		candidateID = "candidate-daily-1"
	)
	insertConversationSpeechFeedbackFixture(
		t,
		pool,
		ownerID,
		sessionID,
		turnID,
		candidateID,
		true,
	)
	if _, err := pool.Exec(
		context.Background(),
		`INSERT INTO identity_users (id) VALUES ($1)`,
		otherOwner,
	); err != nil {
		t.Fatal(err)
	}
	repository := review.NewPostgresRepository(pool)
	reference, err := repository.EnsureConfirmedConversationTurn(
		context.Background(),
		ownerID,
		sessionID,
		turnID,
	)
	if err != nil {
		t.Fatal(err)
	}
	configuration := review.SpeechFeedbackWorkerConfiguration{
		MaxAttempts:     3,
		LeaseDuration:   time.Second,
		RetryDelay:      time.Second,
		StrategyRef:     review.SpeechFeedbackStrategyRef,
		PipelineVersion: review.SpeechFeedbackPipelineVersion,
		PromptVersion:   review.SpeechFeedbackPromptVersion,
		Provider:        "qianwen",
		Model:           "qwen-plus",
	}
	first, acquired, err := repository.ClaimSpeechFeedback(
		context.Background(),
		configuration,
	)
	if err != nil || !acquired {
		t.Fatalf("first claim = %#v, %t, %v", first, acquired, err)
	}
	if _, acquired, err := repository.ClaimSpeechFeedback(
		context.Background(),
		configuration,
	); err != nil || acquired {
		t.Fatalf("live lease second claim = %t, %v", acquired, err)
	}
	if _, err := pool.Exec(context.Background(), `
		UPDATE review_speech_feedbacks
		SET lease_expires_at = clock_timestamp() - interval '1 second'
		WHERE id = $1
	`, reference.SpeechFeedbackID); err != nil {
		t.Fatal(err)
	}
	second, acquired, err := repository.ClaimSpeechFeedback(
		context.Background(),
		configuration,
	)
	if err != nil || !acquired ||
		second.FencingToken <= first.FencingToken {
		t.Fatalf("takeover claim = %#v, %t, %v", second, acquired, err)
	}
	suggestion := "Could I have a quieter room, please?"
	drafts := []review.SpeechFeedbackDraftItem{{
		Kind: review.SpeechFeedbackItemImprovement,
		Anchor: review.SpeechFeedbackAnchor{
			AnchorKind:      review.SpeechFeedbackAnchorConversationTranscript,
			EvidenceRefID:   second.EvidenceRefID,
			TurnID:          turnID,
			StartUTF8Byte:   0,
			EndUTF8Byte:     len(second.CanonicalText),
			OriginalExcerpt: second.CanonicalText,
		},
		Explanation:    "Add a polite closing to make the request warmer.",
		SuggestedText:  &suggestion,
		RepracticeMode: review.SpeechFeedbackRepracticeSameQuestion,
	}}
	if _, err := repository.CompleteSpeechFeedback(
		context.Background(),
		first,
		drafts,
	); !errors.Is(err, review.ErrSpeechFeedbackClaimLost) {
		t.Fatalf("stale claim completion error = %v", err)
	}
	completed, err := repository.CompleteSpeechFeedback(
		context.Background(),
		second,
		drafts,
	)
	if err != nil {
		t.Fatalf("complete takeover: %v", err)
	}
	if completed.FeedbackStatus != review.SpeechFeedbackReady ||
		len(completed.Items) != 1 ||
		completed.Items[0].Anchor.OriginalExcerpt !=
			second.CanonicalText {
		t.Fatalf("completed feedback = %#v", completed)
	}
	if _, err := repository.GetSpeechFeedback(
		context.Background(),
		otherOwner,
		reference.SpeechFeedbackID,
	); !errors.Is(err, review.ErrSpeechFeedbackNotFound) {
		t.Fatalf("cross-owner read error = %v", err)
	}
}

func TestPostgresSpeechFeedbackPersistsTerminalRetryableFailure(
	t *testing.T,
) {
	pool := speechFeedbackDatabase(t)
	const (
		ownerID     = "10000000-0000-4000-8000-000000000001"
		sessionID   = "practice-daily-1"
		turnID      = "turn-daily-1"
		candidateID = "candidate-daily-1"
	)
	insertConversationSpeechFeedbackFixture(
		t,
		pool,
		ownerID,
		sessionID,
		turnID,
		candidateID,
		true,
	)
	repository := review.NewPostgresRepository(pool)
	if _, err := repository.EnsureConfirmedConversationTurn(
		context.Background(),
		ownerID,
		sessionID,
		turnID,
	); err != nil {
		t.Fatal(err)
	}
	configuration := review.SpeechFeedbackWorkerConfiguration{
		MaxAttempts:     1,
		LeaseDuration:   time.Minute,
		RetryDelay:      time.Second,
		StrategyRef:     review.SpeechFeedbackStrategyRef,
		PipelineVersion: review.SpeechFeedbackPipelineVersion,
		PromptVersion:   review.SpeechFeedbackPromptVersion,
		Provider:        "qianwen",
		Model:           "qwen-plus",
	}
	claim, acquired, err := repository.ClaimSpeechFeedback(
		context.Background(),
		configuration,
	)
	if err != nil || !acquired {
		t.Fatalf("claim = %#v, %t, %v", claim, acquired, err)
	}
	status, err := repository.FailSpeechFeedback(
		context.Background(),
		claim,
		review.SpeechFeedbackStableFailure{
			ReasonCode: review.SpeechFeedbackFailureProviderUnavailable,
			Retryable:  true,
		},
		configuration,
	)
	if err != nil {
		t.Fatalf("persist terminal failure: %v", err)
	}
	if status != review.SpeechFeedbackFailed {
		t.Fatalf("failure status = %q", status)
	}
	var (
		persistedCode      string
		persistedRetryable bool
		leaseExpiresAt     *time.Time
	)
	if err := pool.QueryRow(context.Background(), `
		SELECT
			stable_failure_code,
			stable_failure_retryable,
			lease_expires_at
		FROM review_speech_feedbacks
		WHERE id = $1
	`, claim.SpeechFeedbackID).Scan(
		&persistedCode,
		&persistedRetryable,
		&leaseExpiresAt,
	); err != nil {
		t.Fatal(err)
	}
	if persistedCode != string(
		review.SpeechFeedbackFailureProviderUnavailable,
	) || persistedRetryable || leaseExpiresAt != nil {
		t.Fatalf(
			"persisted failure = %q, %t, %v",
			persistedCode,
			persistedRetryable,
			leaseExpiresAt,
		)
	}
}

func TestPostgresSpeechFeedbackDeletionFencesLateWorker(
	t *testing.T,
) {
	pool := speechFeedbackDatabase(t)
	const (
		ownerID     = "10000000-0000-4000-8000-000000000001"
		sessionID   = "practice-daily-1"
		turnID      = "turn-daily-1"
		candidateID = "candidate-daily-1"
	)
	insertConversationSpeechFeedbackFixture(
		t,
		pool,
		ownerID,
		sessionID,
		turnID,
		candidateID,
		true,
	)
	repository := review.NewPostgresRepository(pool)
	if _, err := repository.EnsureConfirmedConversationTurn(
		context.Background(),
		ownerID,
		sessionID,
		turnID,
	); err != nil {
		t.Fatal(err)
	}
	configuration := review.SpeechFeedbackWorkerConfiguration{
		MaxAttempts:     3,
		LeaseDuration:   time.Minute,
		RetryDelay:      time.Second,
		StrategyRef:     review.SpeechFeedbackStrategyRef,
		PipelineVersion: review.SpeechFeedbackPipelineVersion,
		PromptVersion:   review.SpeechFeedbackPromptVersion,
		Provider:        "qianwen",
		Model:           "qwen-plus",
	}
	claim, acquired, err := repository.ClaimSpeechFeedback(
		context.Background(),
		configuration,
	)
	if err != nil || !acquired {
		t.Fatalf("claim before deletion = %t, %v", acquired, err)
	}
	if _, err := pool.Exec(context.Background(), `
		UPDATE identity_users
		SET account_status = 'deleting'
		WHERE id = $1
	`, ownerID); err != nil {
		t.Fatal(err)
	}
	if err := repository.DeleteUserData(
		context.Background(),
		review.DeleteUserReviewsCommand{
			UserID:             ownerID,
			DeletionGeneration: 1,
		},
	); err != nil {
		t.Fatalf("delete Review data: %v", err)
	}
	suggestion := "Could I have a quieter room, please?"
	_, err = repository.CompleteSpeechFeedback(
		context.Background(),
		claim,
		[]review.SpeechFeedbackDraftItem{{
			Kind: review.SpeechFeedbackItemCorrection,
			Anchor: review.SpeechFeedbackAnchor{
				AnchorKind:      review.SpeechFeedbackAnchorConversationTranscript,
				EvidenceRefID:   claim.EvidenceRefID,
				TurnID:          turnID,
				StartUTF8Byte:   0,
				EndUTF8Byte:     len(claim.CanonicalText),
				OriginalExcerpt: claim.CanonicalText,
			},
			Explanation:    "Use a polite closing.",
			SuggestedText:  &suggestion,
			RepracticeMode: review.SpeechFeedbackRepracticeSameQuestion,
		}},
	)
	if !errors.Is(err, review.ErrSpeechFeedbackClaimLost) {
		t.Fatalf("late completion error = %v", err)
	}
	assertSpeechFeedbackCounts(t, pool, ownerID, 0, 0)
	if _, err := repository.EnsureConfirmedConversationTurn(
		context.Background(),
		ownerID,
		sessionID,
		turnID,
	); !errors.Is(err, review.ErrSpeechFeedbackNotApplicable) {
		t.Fatalf("post-delete ensure error = %v", err)
	}
}

func TestPostgresSpeechFeedbackPublishesInsufficientForMixedLanguageTurn(
	t *testing.T,
) {
	pool := speechFeedbackDatabase(t)
	const (
		ownerID     = "10000000-0000-4000-8000-000000000009"
		sessionID   = "practice-mixed-language-1"
		turnID      = "turn-mixed-language-1"
		candidateID = "candidate-mixed-language-1"
	)
	ctx := context.Background()
	insertConversationSpeechFeedbackFixture(
		t,
		pool,
		ownerID,
		sessionID,
		turnID,
		candidateID,
		true,
	)
	if _, err := pool.Exec(ctx, `
		UPDATE conversation_confirmed_turns
		SET answer_text = '是不是拿下了？ My name is Nai Long. I like AI.'
		WHERE owner_user_id = $1
		  AND turn_id = $2
	`, ownerID, turnID); err != nil {
		t.Fatal(err)
	}

	repository := review.NewPostgresRepository(pool)
	reference, err := repository.EnsureConfirmedConversationTurn(
		ctx,
		ownerID,
		sessionID,
		turnID,
	)
	if err != nil {
		t.Fatalf("ensure mixed-language SpeechFeedback: %v", err)
	}
	feedback, err := repository.GetSpeechFeedback(
		ctx,
		ownerID,
		reference.SpeechFeedbackID,
	)
	if err != nil {
		t.Fatalf("get mixed-language SpeechFeedback: %v", err)
	}
	if feedback.FeedbackStatus != review.SpeechFeedbackReady ||
		feedback.ScoreabilityStatus == nil ||
		*feedback.ScoreabilityStatus != review.SpeechFeedbackInsufficient ||
		len(feedback.ReasonCodes) != 1 ||
		feedback.ReasonCodes[0] !=
			review.SpeechFeedbackReasonTranscriptConfidenceInsufficient {
		t.Fatalf("mixed-language SpeechFeedback = %#v", feedback)
	}
	assertSpeechFeedbackCounts(t, pool, ownerID, 1, 1)
}

func TestPostgresSpeechFeedbackUsesAgentTranscriptIdentityWithoutPracticeIDs(
	t *testing.T,
) {
	pool := speechFeedbackDatabase(t)
	const (
		ownerID     = "10000000-0000-4000-8000-000000000001"
		threadID    = "20000000-0000-4000-8000-000000000001"
		messageID   = "30000000-0000-4000-8000-000000000001"
		candidateID = "40000000-0000-4000-8000-000000000001"
		evidenceID  = "50000000-0000-4000-8000-000000000001"
	)
	ctx := context.Background()
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO identity_users (id) VALUES ($1)`,
		ownerID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_messages (
			id,
			owner_user_id,
			thread_id,
			role,
			modality,
			content
		)
		VALUES ($1, $2, $3, 'user', 'voice', 'I enjoy learning English.')
	`, messageID, ownerID, threadID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_voice_candidates (
			candidate_id,
			owner_user_id,
			thread_id,
			candidate_version,
			status,
			confirmed_message_id
		)
		VALUES ($1, $2, $3, 1, 'confirmed', $4)
	`, candidateID, ownerID, threadID, messageID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_voice_transcript_evidence (
			evidence_id,
			owner_user_id,
			thread_id,
			candidate_id,
			candidate_version,
			message_id,
			confirmed_text
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			1,
			$5,
			'I enjoy learning English.'
		)
	`, evidenceID, ownerID, threadID, candidateID, messageID); err != nil {
		t.Fatal(err)
	}
	repository := review.NewPostgresRepository(pool)
	reference, err := repository.EnsureConfirmedAgentVoiceMessage(
		ctx,
		ownerID,
		threadID,
		messageID,
	)
	if err != nil {
		t.Fatal(err)
	}
	feedback, err := repository.GetSpeechFeedback(
		ctx,
		ownerID,
		reference.SpeechFeedbackID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if feedback.Source.SourceKind !=
		review.SpeechFeedbackSourceAgentVoiceMessage ||
		feedback.Source.TranscriptEvidenceID != evidenceID ||
		feedback.Source.CandidateVersion != 1 ||
		feedback.Source.PracticeSessionID != "" ||
		feedback.Source.TurnID != "" ||
		feedback.Source.EvidenceSnapshotID != "" {
		t.Fatalf("Agent source = %#v", feedback.Source)
	}
}

func insertConversationSpeechFeedbackFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	ownerID string,
	sessionID string,
	turnID string,
	candidateID string,
	withAudio bool,
) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO identity_users (id) VALUES ($1)`,
		ownerID,
	); err != nil {
		t.Fatalf("insert owner fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO practice_sessions (
			owner_user_id,
			session_id,
			scene_family,
			scene_model
		)
		VALUES (
			$1,
			$2,
			'DAILY',
			'DAILY_BASIC_DIALOGUE'
		)
	`, ownerID, sessionID); err != nil {
		t.Fatalf("insert Practice Session fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO conversation_questions (
			owner_user_id,
			question_id,
			practice_session_id,
			content
		)
		VALUES (
			$1,
			'question-1',
			$2,
			'Could you describe what you need from the hotel staff?'
		)
	`, ownerID, sessionID); err != nil {
		t.Fatalf("insert question fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO conversation_transcript_candidates (
			owner_user_id,
			candidate_id,
			practice_session_id,
			question_id,
			transcript_id,
			status
		)
		VALUES ($1, $3, $2, 'question-1', 'transcript-1', 'confirmed')
	`, ownerID, sessionID, candidateID); err != nil {
		t.Fatalf("insert transcript candidate fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO conversation_confirmed_turns (
			owner_user_id,
			turn_id,
			candidate_id,
			question_id,
			practice_session_id,
			answer_text,
			evidence_version
		)
		VALUES (
			$1,
			$4,
			$3,
			'question-1',
			$2,
			'Could I have a quieter room?',
			1
		)
	`, ownerID, sessionID, candidateID, turnID); err != nil {
		t.Fatalf("insert confirmed Turn fixture: %v", err)
	}
	if !withAudio {
		return
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO conversation_audio_assets (
			audio_asset_id,
			owner_user_id,
			candidate_id,
			turn_id,
			checksum_sha256,
			status,
			version
		)
		VALUES (
			'audio-1',
			$1,
			$2,
			$3,
			repeat('a', 64),
			'readable',
			1
		)
	`, ownerID, candidateID, turnID); err != nil {
		t.Fatalf("insert readable audio fixture: %v", err)
	}
}

func speechFeedbackDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL admin pool: %v", err)
	}
	t.Cleanup(admin.Close)
	schema := fmt.Sprintf(
		"speech_feedback_%d_%d",
		time.Now().UnixNano(),
		schemaSequence.Add(1),
	)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create SpeechFeedback test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cleanupCancel()
		if _, err := admin.Exec(
			cleanupCtx,
			"DROP SCHEMA IF EXISTS "+schema+" CASCADE",
		); err != nil {
			t.Errorf("drop SpeechFeedback test schema: %v", err)
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, speechFeedbackPrerequisiteSQL); err != nil {
		t.Fatalf("create SpeechFeedback prerequisites: %v", err)
	}
	up, err := migrations.Files.ReadFile(
		"000040_review_speech_feedback.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(up)); err != nil {
		t.Fatalf("apply SpeechFeedback migration: %v", err)
	}
	iseEvidenceUp, err := migrations.Files.ReadFile(
		"000043_speech_feedback_ise_evidence.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(iseEvidenceUp)); err != nil {
		t.Fatalf("apply SpeechFeedback ISE evidence migration: %v", err)
	}
	iseTopicUp, err := migrations.Files.ReadFile(
		"000045_speech_feedback_ise_topic.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(iseTopicUp)); err != nil {
		t.Fatalf("apply SpeechFeedback ISE topic migration: %v", err)
	}
	return pool
}

const speechFeedbackPrerequisiteSQL = `
	CREATE TABLE identity_users (
		id uuid PRIMARY KEY,
		account_status text NOT NULL DEFAULT 'active'
	);
	CREATE TABLE review_deletion_fences (
		owner_user_id uuid PRIMARY KEY,
		deletion_generation bigint NOT NULL,
		deleted_at timestamptz NOT NULL
	);
	CREATE TABLE reviews (
		owner_user_id uuid NOT NULL
	);
` + speechFeedbackModulePrerequisiteSQL

const speechFeedbackModulePrerequisiteSQL = `
	CREATE TABLE practice_sessions (
		owner_user_id uuid NOT NULL,
		session_id text NOT NULL,
		scene_family text,
		scene_model text,
		PRIMARY KEY (owner_user_id, session_id)
	);
	CREATE TABLE conversation_questions (
		owner_user_id uuid NOT NULL,
		question_id text NOT NULL,
		practice_session_id text NOT NULL,
		content text NOT NULL,
		PRIMARY KEY (owner_user_id, question_id)
	);
	CREATE TABLE conversation_transcript_candidates (
		owner_user_id uuid NOT NULL,
		candidate_id text NOT NULL,
		practice_session_id text NOT NULL,
		question_id text NOT NULL,
		transcript_id text NOT NULL,
		status text NOT NULL,
		PRIMARY KEY (owner_user_id, candidate_id)
	);
	CREATE TABLE conversation_confirmed_turns (
		owner_user_id uuid NOT NULL,
		turn_id text NOT NULL,
		candidate_id text NOT NULL,
		question_id text NOT NULL,
		practice_session_id text NOT NULL,
		answer_text text NOT NULL,
		evidence_version bigint NOT NULL,
		PRIMARY KEY (owner_user_id, turn_id)
	);
	CREATE TABLE conversation_audio_assets (
		audio_asset_id text PRIMARY KEY,
		owner_user_id uuid NOT NULL,
		candidate_id text,
		turn_id text,
		checksum_sha256 text NOT NULL,
		status text NOT NULL,
		version bigint NOT NULL,
		UNIQUE (owner_user_id, turn_id)
	);
	CREATE TABLE agent_messages (
		id uuid PRIMARY KEY,
		owner_user_id uuid NOT NULL,
		thread_id uuid NOT NULL,
		role text NOT NULL,
		modality text NOT NULL,
		content text NOT NULL,
		UNIQUE (id, owner_user_id, thread_id)
	);
	CREATE TABLE agent_voice_candidates (
		candidate_id uuid PRIMARY KEY,
		owner_user_id uuid NOT NULL,
		thread_id uuid NOT NULL,
		candidate_version bigint NOT NULL,
		status text NOT NULL,
		confirmed_message_id uuid,
		UNIQUE (candidate_id, owner_user_id, thread_id)
	);
	CREATE TABLE agent_voice_transcript_evidence (
		evidence_id uuid PRIMARY KEY,
		owner_user_id uuid NOT NULL,
		thread_id uuid NOT NULL,
		candidate_id uuid NOT NULL,
		candidate_version bigint NOT NULL,
		message_id uuid NOT NULL,
		confirmed_text text NOT NULL
	);
	CREATE TABLE agent_message_audios (
		audio_id uuid PRIMARY KEY,
		owner_user_id uuid NOT NULL,
		thread_id uuid NOT NULL,
		message_id uuid NOT NULL,
		candidate_id uuid NOT NULL,
		checksum_sha256 text NOT NULL,
		object_key text NOT NULL,
		status text NOT NULL
	);
`

func assertSpeechFeedbackCounts(
	t *testing.T,
	pool *pgxpool.Pool,
	ownerUserID string,
	feedbackWant int,
	snapshotWant int,
) {
	t.Helper()
	var feedbackCount int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM review_speech_feedbacks
		WHERE owner_user_id = $1
	`, ownerUserID).Scan(&feedbackCount); err != nil {
		t.Fatal(err)
	}
	var snapshotCount int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM review_speech_feedback_turn_snapshots
		WHERE owner_user_id = $1
	`, ownerUserID).Scan(&snapshotCount); err != nil {
		t.Fatal(err)
	}
	if feedbackCount != feedbackWant || snapshotCount != snapshotWant {
		t.Fatalf(
			"SpeechFeedback counts = %d/%d, want %d/%d",
			feedbackCount,
			snapshotCount,
			feedbackWant,
			snapshotWant,
		)
	}
}
