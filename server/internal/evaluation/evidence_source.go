package evaluation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	domainconversation "github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	conversation "github.com/1024XEngineer/XE3-ESL/server/internal/conversation/persistence"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	practice "github.com/1024XEngineer/XE3-ESL/server/internal/practice/persistence"
)

const (
	evidenceSnapshotSchemaVersion = "evidence-snapshot/v2"
	evidenceSourceManifestVersion = "evidence-source-manifest/v2"
	evidenceUnavailable           = "UNAVAILABLE"
	evidenceNotAssessed           = "NOT_ASSESSED"
)

type EvidencePracticeSource interface {
	GetContextSession(
		context.Context,
		practice.Actor,
		string,
	) (practice.ContextSession, error)
	GetContextSessionSnapshot(
		context.Context,
		practice.Actor,
		string,
	) (practice.ContextSessionSnapshot, error)
}

type EvidenceConversationSource interface {
	ListSessionQuestions(
		context.Context,
		conversation.Actor,
		string,
	) ([]conversation.PersistentQuestion, error)
	GetCandidate(
		context.Context,
		conversation.Actor,
		string,
	) (conversation.TranscriptCandidate, error)
	ListSessionTurns(
		context.Context,
		conversation.Actor,
		string,
	) ([]conversation.ConfirmedTurn, error)
}

type EvidenceAudioSource interface {
	GetByTurn(
		context.Context,
		string,
		string,
	) (domainconversation.AudioAsset, error)
}

// EvidenceSourceReader composes Evaluation's immutable input from the
// Practice and Conversation authorities. It deliberately exposes no Review
// types and never reads object bytes or storage locators.
type EvidenceSourceReader struct {
	practice     EvidencePracticeSource
	conversation EvidenceConversationSource
	audio        EvidenceAudioSource
}

func NewEvidenceSourceReader(
	practiceSource EvidencePracticeSource,
	conversationSource EvidenceConversationSource,
	audioSource EvidenceAudioSource,
) (*EvidenceSourceReader, error) {
	if practiceSource == nil || conversationSource == nil || audioSource == nil {
		return nil, ErrInvalidRequest
	}
	return &EvidenceSourceReader{
		practice:     practiceSource,
		conversation: conversationSource,
		audio:        audioSource,
	}, nil
}

// Compose freezes only a completed Session scope. The Actor must be the exact
// identity installed by authentication in ctx; ownership is never accepted
// from the snapshot request.
func (r *EvidenceSourceReader) Compose(
	ctx context.Context,
	actor requestcontext.Actor,
	practiceSessionID string,
	scope Scope,
	sceneType SceneType,
) (EnsureEvidenceSnapshotCommand, error) {
	practiceSessionID = strings.TrimSpace(practiceSessionID)
	trustedActor, ok := requestcontext.ActorFromContext(ctx)
	if r == nil || r.practice == nil || r.conversation == nil ||
		r.audio == nil || ctx == nil || !validActor(actor) || !ok ||
		trustedActor != actor || !validIdentifier(practiceSessionID) ||
		scope != ScopeSession || !validSceneType(sceneType) {
		return EnsureEvidenceSnapshotCommand{}, ErrInvalidRequest
	}

	practiceActor := practice.Actor{
		UserID:    actor.UserID,
		SessionID: actor.SessionID,
	}
	session, err := r.practice.GetContextSession(
		ctx,
		practiceActor,
		practiceSessionID,
	)
	if err != nil {
		return EnsureEvidenceSnapshotCommand{}, mapEvidencePracticeError(err)
	}
	snapshot, err := r.practice.GetContextSessionSnapshot(
		ctx,
		practiceActor,
		practiceSessionID,
	)
	if err != nil {
		return EnsureEvidenceSnapshotCommand{}, mapEvidencePracticeError(err)
	}
	if !validCompletedEvidenceSession(
		actor.UserID,
		practiceSessionID,
		sceneType,
		session,
		snapshot,
	) {
		return EnsureEvidenceSnapshotCommand{}, ErrInvalidRequest
	}

	conversationActor := conversation.Actor{
		UserID:    actor.UserID,
		SessionID: actor.SessionID,
	}
	turns, err := r.conversation.ListSessionTurns(
		ctx,
		conversationActor,
		practiceSessionID,
	)
	if err != nil {
		return EnsureEvidenceSnapshotCommand{},
			mapEvidenceConversationError(err)
	}
	slices.SortFunc(turns, func(left, right conversation.ConfirmedTurn) int {
		if left.Sequence != right.Sequence {
			return left.Sequence - right.Sequence
		}
		if compared := left.ConfirmedAt.Compare(right.ConfirmedAt); compared != 0 {
			return compared
		}
		return strings.Compare(left.ID, right.ID)
	})
	if len(turns) != session.EffectiveTurns {
		return EnsureEvidenceSnapshotCommand{}, ErrInvalidRequest
	}
	questions, err := r.conversation.ListSessionQuestions(
		ctx,
		conversationActor,
		practiceSessionID,
	)
	if err != nil {
		return EnsureEvidenceSnapshotCommand{},
			mapEvidenceConversationError(err)
	}
	slices.SortFunc(
		questions,
		func(left, right conversation.PersistentQuestion) int {
			if left.Sequence != right.Sequence {
				return left.Sequence - right.Sequence
			}
			if compared := left.CreatedAt.Compare(right.CreatedAt); compared != 0 {
				return compared
			}
			return strings.Compare(left.ID, right.ID)
		},
	)
	if len(questions) < len(turns) {
		return EnsureEvidenceSnapshotCommand{}, ErrInvalidRequest
	}

	practiceContext, allParticipants, candidateParticipants, ok :=
		evidencePracticeContextFromSnapshot(
			actor.UserID,
			session,
			snapshot,
		)
	if !ok {
		return EnsureEvidenceSnapshotCommand{}, ErrInvalidRequest
	}
	payload := evidencePayload{
		PracticeContext: practiceContext,
		OpportunityManifest: make(
			[]evidenceOpportunity,
			0,
			len(questions),
		),
		ConfirmedTurns: make([]evidenceConfirmedTurn, 0, len(turns)),
		EvidenceRefs:   make([]evidenceRef, 0, len(turns)),
		ProviderLineage: evidenceProviderLineage{
			ASR: make([]evidenceASRLineage, 0, len(turns)),
			UnavailableArtifacts: evidenceUnavailableArtifacts{
				WordTimestamps: evidenceUnavailable,
				ASRConfidence:  evidenceUnavailable,
				ASRNBest:       evidenceUnavailable,
				AudioQuality:   evidenceNotAssessed,
				ISE:            evidenceNotAssessed,
				FeatureBundle:  evidenceUnavailable,
			},
		},
		VersionManifest: evidenceVersionManifest{
			SchemaVersion:         evidenceSnapshotSchemaVersion,
			SourceManifestVersion: evidenceSourceManifestVersion,
			PracticeSession:       session.Version,
			PracticeSnapshot:      snapshot.ID,
			PlanRevision:          snapshot.PlanRevision,
			TurnEvidence:          make([]evidenceTurnVersion, 0, len(turns)),
			AudioQuality:          evidenceUnavailable,
			ISE:                   evidenceUnavailable,
			FeatureBundle:         evidenceUnavailable,
			ScoringPrompt:         evidenceUnavailable,
			Rubric:                evidenceUnavailable,
			Gate:                  evidenceUnavailable,
			Aggregation:           evidenceUnavailable,
			Calibration:           evidenceUnavailable,
			Pipeline:              evidenceUnavailable,
		},
	}
	turnsByQuestion := make(
		map[string]conversation.ConfirmedTurn,
		len(turns),
	)
	for _, turn := range turns {
		if _, duplicate := turnsByQuestion[turn.QuestionID]; duplicate {
			return EnsureEvidenceSnapshotCommand{}, ErrInvalidRequest
		}
		turnsByQuestion[turn.QuestionID] = turn
	}
	seenQuestions := make(map[string]struct{}, len(questions))
	confirmedIndex := 0
	for questionIndex, question := range questions {
		expectedSequence := questionIndex + 1
		if !validEvidenceQuestion(
			question,
			practiceSessionID,
			expectedSequence,
			allParticipants,
			candidateParticipants,
		) {
			return EnsureEvidenceSnapshotCommand{}, ErrInvalidRequest
		}
		if question.Type == "FOLLOW_UP" {
			if _, parentSeen := seenQuestions[question.ParentQuestionID]; !parentSeen {
				return EnsureEvidenceSnapshotCommand{}, ErrInvalidRequest
			}
		}
		if _, duplicate := seenQuestions[question.ID]; duplicate {
			return EnsureEvidenceSnapshotCommand{}, ErrInvalidRequest
		}
		seenQuestions[question.ID] = struct{}{}
		opportunity := evidenceOpportunityFromQuestion(question)
		turn, hasTurn := turnsByQuestion[question.ID]
		if hasTurn {
			if confirmedIndex >= len(turns) ||
				turn.ID != turns[confirmedIndex].ID {
				return EnsureEvidenceSnapshotCommand{}, ErrInvalidRequest
			}
			opportunity.ResponseTurnID = turn.ID
		}
		payload.OpportunityManifest = append(
			payload.OpportunityManifest,
			opportunity,
		)
		if !hasTurn {
			if questionIndex < len(turns) {
				return EnsureEvidenceSnapshotCommand{}, ErrInvalidRequest
			}
			continue
		}
		item, itemErr := r.composeTurn(
			ctx,
			actor.UserID,
			conversationActor,
			practiceSessionID,
			confirmedIndex+1,
			turn,
			question,
			candidateParticipants,
		)
		if itemErr != nil {
			return EnsureEvidenceSnapshotCommand{}, itemErr
		}
		payload.ConfirmedTurns = append(payload.ConfirmedTurns, item.Turn)
		payload.EvidenceRefs = append(payload.EvidenceRefs, item.Ref)
		payload.ProviderLineage.ASR = append(
			payload.ProviderLineage.ASR,
			item.ASR,
		)
		payload.VersionManifest.TurnEvidence = append(
			payload.VersionManifest.TurnEvidence,
			evidenceTurnVersion{
				TurnID:          turn.ID,
				EvidenceVersion: turn.EvidenceVersion,
				AudioVersion:    item.AudioVersion,
			},
		)
		confirmedIndex++
	}
	if confirmedIndex != len(turns) {
		return EnsureEvidenceSnapshotCommand{}, ErrInvalidRequest
	}

	manifest, err := evidenceSourceManifestFromPayload(payload)
	if err != nil {
		return EnsureEvidenceSnapshotCommand{}, err
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return EnsureEvidenceSnapshotCommand{}, ErrInvalidRequest
	}
	sourceManifestHash := sha256.Sum256(manifestJSON)
	snapshotID := deriveEvidenceSnapshotID(
		actor.UserID,
		practiceSessionID,
		scope,
		sourceManifestHash,
	)
	for index := range payload.EvidenceRefs {
		ref := &payload.EvidenceRefs[index]
		ref.SnapshotID = snapshotID
		ref.EvidenceRefID = stableEvidenceRefID(
			snapshotID,
			ref.TurnID,
			ref.Lineage.EvidenceVersion,
			payload.ConfirmedTurns[index].Audio.ChecksumSHA256,
		)
	}
	canonicalPayload, err := canonicalEvidenceJSON(payload)
	if err != nil {
		return EnsureEvidenceSnapshotCommand{}, err
	}
	return EnsureEvidenceSnapshotCommand{
		SnapshotID:         snapshotID,
		OwnerUserID:        actor.UserID,
		PracticeSessionID:  practiceSessionID,
		Scope:              scope,
		SceneType:          sceneType,
		SourceManifestHash: sourceManifestHash,
		CanonicalPayload:   canonicalPayload,
	}, nil
}

type composedEvidenceTurn struct {
	Turn         evidenceConfirmedTurn
	Ref          evidenceRef
	ASR          evidenceASRLineage
	AudioVersion uint64
}

func (r *EvidenceSourceReader) composeTurn(
	ctx context.Context,
	ownerUserID string,
	actor conversation.Actor,
	practiceSessionID string,
	expectedSequence int,
	turn conversation.ConfirmedTurn,
	question conversation.PersistentQuestion,
	candidateParticipants map[string]struct{},
) (composedEvidenceTurn, error) {
	if turn.SessionID != practiceSessionID ||
		turn.Sequence != expectedSequence ||
		!validEvidenceTurn(turn) {
		return composedEvidenceTurn{}, ErrInvalidRequest
	}
	candidate, err := r.conversation.GetCandidate(ctx, actor, turn.CandidateID)
	if err != nil {
		return composedEvidenceTurn{}, mapEvidenceConversationError(err)
	}
	if !validEvidenceQuestionTurn(question, turn) ||
		!validEvidenceCandidate(candidate, turn) {
		return composedEvidenceTurn{}, ErrInvalidRequest
	}
	if _, ok := candidateParticipants[turn.RespondentParticipantID]; !ok {
		return composedEvidenceTurn{}, ErrInvalidRequest
	}

	audio, audioVersion, err := r.readEvidenceAudio(
		ctx,
		ownerUserID,
		turn,
	)
	if err != nil {
		return composedEvidenceTurn{}, err
	}
	transcript := evidenceTranscript{
		ID:                    candidate.TranscriptID,
		Text:                  turn.AnswerText,
		EvidenceVersion:       turn.EvidenceVersion,
		ASRConfidence:         evidenceUnavailable,
		WordTimestamps:        evidenceUnavailable,
		AlternativeHypotheses: evidenceUnavailable,
	}
	ref := evidenceRef{
		TurnID:  turn.ID,
		Speaker: "USER",
		TranscriptSpan: evidenceTranscriptSpan{
			StartUTF8Byte: 0,
			EndUTF8Byte:   len([]byte(turn.AnswerText)),
		},
		Quality: evidenceQuality{
			Audio:         evidenceNotAssessed,
			ASRConfidence: evidenceUnavailable,
			Alignment:     evidenceUnavailable,
			ISE:           evidenceNotAssessed,
		},
		Lineage: evidenceRefLineage{
			TranscriptID:    candidate.TranscriptID,
			CandidateID:     candidate.ID,
			EvidenceVersion: candidate.EvidenceVersion,
			ASRProvider:     candidate.Provider,
			ASRModel:        candidate.Model,
		},
	}
	if audio.Availability == "AVAILABLE" {
		ref.AudioSpan = &evidenceAudioSpan{
			AudioAssetID: audio.AudioAssetID,
			StartMS:      0,
			EndMS:        audio.DurationMS,
		}
		ref.Lineage.AudioAssetVersion = audio.Version
	}
	return composedEvidenceTurn{
		Turn: evidenceConfirmedTurn{
			TurnID:                  turn.ID,
			Sequence:                turn.Sequence,
			QuestionID:              turn.QuestionID,
			RespondentParticipantID: turn.RespondentParticipantID,
			InteractionMode:         turn.InteractionMode,
			Transcript:              transcript,
			Audio:                   audio,
		},
		Ref: ref,
		ASR: evidenceASRLineage{
			TurnID:            turn.ID,
			TranscriptID:      candidate.TranscriptID,
			CandidateID:       candidate.ID,
			EvidenceVersion:   candidate.EvidenceVersion,
			Provider:          candidate.Provider,
			Model:             candidate.Model,
			ProviderRequestID: candidate.ProviderRequestID,
		},
		AudioVersion: audioVersion,
	}, nil
}

func (r *EvidenceSourceReader) readEvidenceAudio(
	ctx context.Context,
	ownerUserID string,
	turn conversation.ConfirmedTurn,
) (evidenceAudio, uint64, error) {
	asset, err := r.audio.GetByTurn(ctx, ownerUserID, turn.ID)
	if errors.Is(err, domainconversation.ErrAudioAssetNotFound) {
		return evidenceAudio{
			Availability: evidenceUnavailable,
			Quality:      evidenceNotAssessed,
			ISE:          evidenceNotAssessed,
		}, 0, nil
	}
	if err != nil {
		return evidenceAudio{}, 0, err
	}
	if !validEvidenceAudio(asset, ownerUserID, turn) {
		return evidenceAudio{}, 0, ErrInvalidRequest
	}
	durationMS := int64((asset.Duration-1)/time.Millisecond) + 1
	availability := evidenceUnavailable
	if asset.Status == domainconversation.AudioAssetReadable {
		availability = "AVAILABLE"
	}
	return evidenceAudio{
		Availability:   availability,
		AudioAssetID:   asset.ID,
		ChecksumSHA256: asset.ChecksumSHA256,
		DurationMS:     durationMS,
		ContentType:    asset.ContentType,
		SizeBytes:      asset.Size,
		Status:         string(asset.Status),
		Version:        asset.Version,
		Quality:        evidenceNotAssessed,
		ISE:            evidenceNotAssessed,
	}, asset.Version, nil
}

type evidencePayload struct {
	PracticeContext     evidencePracticeContext `json:"practice_context"`
	OpportunityManifest []evidenceOpportunity   `json:"opportunity_manifest"`
	ConfirmedTurns      []evidenceConfirmedTurn `json:"confirmed_turns"`
	EvidenceRefs        []evidenceRef           `json:"evidence_refs"`
	ProviderLineage     evidenceProviderLineage `json:"provider_lineage"`
	VersionManifest     evidenceVersionManifest `json:"version_manifest"`
}

type evidencePracticeContext struct {
	PracticeSessionID  string                     `json:"practice_session_id"`
	SessionSnapshotID  string                     `json:"session_snapshot_id"`
	SessionVersion     int                        `json:"session_version"`
	PlanRevision       int                        `json:"plan_revision"`
	SceneFamily        string                     `json:"scene_family"`
	ScenarioModel      string                     `json:"scenario_model"`
	ScenarioDefinition evidenceVersionedRef       `json:"scenario_definition"`
	ScenarioConfig     evidenceVersionedRef       `json:"scenario_config"`
	PracticeOption     evidencePracticeOption     `json:"practice_option"`
	UserRole           string                     `json:"user_role"`
	FacilitatorRole    string                     `json:"facilitator_role"`
	PracticeGoal       string                     `json:"practice_goal"`
	Preparation        evidencePreparationContext `json:"preparation"`
	TaskContext        evidenceTaskContext        `json:"task_context"`
	TaskBlueprints     []string                   `json:"task_blueprints"`
	Participants       []evidenceParticipant      `json:"participants"`
	Objectives         evidenceObjectiveSet       `json:"objectives"`
}

type evidenceTaskContext struct {
	JobTitle                 string   `json:"job_title,omitempty"`
	JobDescription           string   `json:"job_description,omitempty"`
	PublicSceneBrief         string   `json:"public_scene_brief"`
	PersonaSummary           string   `json:"persona_summary"`
	ConfigFocusAreas         []string `json:"config_focus_areas"`
	PromptFocusAreas         []string `json:"prompt_focus_areas"`
	SuggestedDurationSeconds int      `json:"suggested_duration_seconds"`
}

type evidencePreparationContext struct {
	SnapshotID                         string                      `json:"snapshot_id,omitempty"`
	SourceProfileID                    string                      `json:"source_profile_id,omitempty"`
	SourceVersion                      int                         `json:"source_version,omitempty"`
	SourceJobTargetID                  string                      `json:"source_job_target_id,omitempty"`
	SourceJobTargetConfirmationVersion int                         `json:"source_job_target_confirmation_version,omitempty"`
	JobTargetInput                     *evidenceJobTargetInput     `json:"job_target_input,omitempty"`
	JobTargetCandidate                 *evidenceJobTargetCandidate `json:"job_target_candidate,omitempty"`
	ResumeSnapshotHash                 string                      `json:"resume_snapshot_hash,omitempty"`
	JobDescriptionSnapshotHash         string                      `json:"job_description_snapshot_hash,omitempty"`
	BackgroundSnapshotHash             string                      `json:"background_snapshot_hash,omitempty"`
}

type evidenceJobTargetInput struct {
	Source              string `json:"source"`
	JobTitle            string `json:"job_title,omitempty"`
	JobDescription      string `json:"job_description,omitempty"`
	Company             string `json:"company,omitempty"`
	Seniority           string `json:"seniority,omitempty"`
	CandidateBackground string `json:"candidate_background,omitempty"`
	PracticeFocus       string `json:"practice_focus,omitempty"`
}

type evidenceJobTargetCandidate struct {
	Source                string                        `json:"source"`
	GeneralAdviceOnly     bool                          `json:"general_advice_only"`
	JobTitle              string                        `json:"job_title"`
	Seniority             string                        `json:"seniority"`
	Responsibilities      []string                      `json:"responsibilities"`
	CoreSkills            []string                      `json:"core_skills"`
	CommunicationFocus    []string                      `json:"communication_focus"`
	PracticeGoals         []string                      `json:"practice_goals"`
	ScopeNotice           string                        `json:"scope_notice"`
	CatalogRecommendation evidenceCatalogRecommendation `json:"catalog_recommendation"`
}

type evidenceCatalogRecommendation struct {
	ScenarioDefinitionID      string   `json:"scenario_definition_id"`
	ScenarioDefinitionVersion int      `json:"scenario_definition_version"`
	SelectedRoleIDs           []string `json:"selected_role_ids"`
	PracticeOptionID          string   `json:"practice_option_id"`
	PracticeOptionVersion     int      `json:"practice_option_version"`
}

type evidenceVersionedRef struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

type evidencePracticeOption struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Version int    `json:"version"`
}

type evidenceParticipant struct {
	ID                    string   `json:"participant_id"`
	Role                  string   `json:"role"`
	RoleDefinitionID      string   `json:"role_definition_id,omitempty"`
	RoleDefinitionVersion int      `json:"role_definition_version,omitempty"`
	DisplayName           string   `json:"display_name,omitempty"`
	Responsibilities      string   `json:"responsibilities,omitempty"`
	Style                 string   `json:"style,omitempty"`
	FocusAreas            []string `json:"focus_areas,omitempty"`
	Order                 int      `json:"order"`
}

type evidenceObjectiveSet struct {
	SessionPolicy []evidenceObjective `json:"session_policy"`
	PracticeFocus []evidenceObjective `json:"practice_focus"`
}

type evidenceObjective struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type evidenceOpportunity struct {
	Sequence                int      `json:"sequence"`
	QuestionID              string   `json:"question_id"`
	QuestionType            string   `json:"question_type"`
	ParentQuestionID        string   `json:"parent_question_id,omitempty"`
	ObjectiveID             string   `json:"objective_id"`
	QuestionText            string   `json:"question_text"`
	SpeakerParticipantID    string   `json:"speaker_participant_id"`
	AddresseeParticipantIDs []string `json:"addressee_participant_ids"`
	ResponseTurnID          string   `json:"response_turn_id,omitempty"`
}

type evidenceConfirmedTurn struct {
	TurnID                  string             `json:"turn_id"`
	Sequence                int                `json:"sequence"`
	QuestionID              string             `json:"question_id"`
	RespondentParticipantID string             `json:"respondent_participant_id"`
	InteractionMode         string             `json:"interaction_mode"`
	Transcript              evidenceTranscript `json:"transcript"`
	Audio                   evidenceAudio      `json:"audio"`
}

type evidenceTranscript struct {
	ID                    string `json:"transcript_id"`
	Text                  string `json:"text"`
	EvidenceVersion       int64  `json:"evidence_version"`
	ASRConfidence         string `json:"asr_confidence"`
	WordTimestamps        string `json:"word_timestamps"`
	AlternativeHypotheses string `json:"alternative_hypotheses"`
}

type evidenceAudio struct {
	Availability   string `json:"availability"`
	AudioAssetID   string `json:"audio_asset_id,omitempty"`
	ChecksumSHA256 string `json:"checksum_sha256,omitempty"`
	DurationMS     int64  `json:"duration_ms,omitempty"`
	ContentType    string `json:"content_type,omitempty"`
	SizeBytes      int64  `json:"size_bytes,omitempty"`
	Status         string `json:"status,omitempty"`
	Version        uint64 `json:"version,omitempty"`
	Quality        string `json:"quality"`
	ISE            string `json:"ise"`
}

type evidenceRef struct {
	EvidenceRefID  string                 `json:"evidence_ref_id"`
	SnapshotID     string                 `json:"snapshot_id"`
	TurnID         string                 `json:"turn_id"`
	Speaker        string                 `json:"speaker"`
	TranscriptSpan evidenceTranscriptSpan `json:"transcript_span"`
	AudioSpan      *evidenceAudioSpan     `json:"audio_span,omitempty"`
	Quality        evidenceQuality        `json:"quality"`
	Lineage        evidenceRefLineage     `json:"lineage"`
}

type evidenceTranscriptSpan struct {
	StartUTF8Byte int `json:"start_utf8_byte"`
	EndUTF8Byte   int `json:"end_utf8_byte"`
}

type evidenceAudioSpan struct {
	AudioAssetID string `json:"audio_asset_id"`
	StartMS      int64  `json:"start_ms"`
	EndMS        int64  `json:"end_ms"`
}

type evidenceQuality struct {
	Audio         string `json:"audio"`
	ASRConfidence string `json:"asr_confidence"`
	Alignment     string `json:"alignment"`
	ISE           string `json:"ise"`
}

type evidenceRefLineage struct {
	TranscriptID      string `json:"transcript_id"`
	CandidateID       string `json:"candidate_id"`
	EvidenceVersion   int64  `json:"evidence_version"`
	ASRProvider       string `json:"asr_provider"`
	ASRModel          string `json:"asr_model"`
	AudioAssetVersion uint64 `json:"audio_asset_version,omitempty"`
}

type evidenceProviderLineage struct {
	ASR                  []evidenceASRLineage         `json:"asr"`
	UnavailableArtifacts evidenceUnavailableArtifacts `json:"unavailable_artifacts"`
}

type evidenceASRLineage struct {
	TurnID            string `json:"turn_id"`
	TranscriptID      string `json:"transcript_id"`
	CandidateID       string `json:"candidate_id"`
	EvidenceVersion   int64  `json:"evidence_version"`
	Provider          string `json:"provider"`
	Model             string `json:"model"`
	ProviderRequestID string `json:"provider_request_id"`
}

type evidenceUnavailableArtifacts struct {
	WordTimestamps string `json:"word_timestamps"`
	ASRConfidence  string `json:"asr_confidence"`
	ASRNBest       string `json:"asr_n_best"`
	AudioQuality   string `json:"audio_quality"`
	ISE            string `json:"ise"`
	FeatureBundle  string `json:"feature_bundle"`
}

type evidenceVersionManifest struct {
	SchemaVersion         string                `json:"schema_version"`
	SourceManifestVersion string                `json:"source_manifest_version"`
	PracticeSession       int                   `json:"practice_session"`
	PracticeSnapshot      string                `json:"practice_snapshot"`
	PlanRevision          int                   `json:"plan_revision"`
	TurnEvidence          []evidenceTurnVersion `json:"turn_evidence"`
	AudioQuality          string                `json:"audio_quality"`
	ISE                   string                `json:"ise"`
	FeatureBundle         string                `json:"feature_bundle"`
	ScoringPrompt         string                `json:"scoring_prompt"`
	Rubric                string                `json:"rubric"`
	Gate                  string                `json:"gate"`
	Aggregation           string                `json:"aggregation"`
	Calibration           string                `json:"calibration"`
	Pipeline              string                `json:"pipeline"`
}

type evidenceTurnVersion struct {
	TurnID          string `json:"turn_id"`
	EvidenceVersion int64  `json:"evidence_version"`
	AudioVersion    uint64 `json:"audio_version,omitempty"`
}

type evidenceSourceManifest struct {
	Version             string                   `json:"version"`
	PracticeSessionID   string                   `json:"practice_session_id"`
	SessionVersion      int                      `json:"session_version"`
	SessionSnapshotID   string                   `json:"session_snapshot_id"`
	PracticeContextHash [sha256.Size]byte        `json:"practice_context_hash"`
	ProviderLineageHash [sha256.Size]byte        `json:"provider_lineage_hash"`
	VersionManifestHash [sha256.Size]byte        `json:"version_manifest_hash"`
	Questions           []evidenceSourceQuestion `json:"questions"`
	Turns               []evidenceSourceTurn     `json:"turns"`
}

type evidenceSourceQuestion struct {
	QuestionID   string            `json:"question_id"`
	QuestionHash [sha256.Size]byte `json:"question_hash"`
}

type evidenceSourceTurn struct {
	TurnID              string              `json:"turn_id"`
	TurnEvidenceVersion int64               `json:"turn_evidence_version"`
	QuestionID          string              `json:"question_id"`
	TurnHash            [sha256.Size]byte   `json:"turn_hash"`
	TranscriptID        string              `json:"transcript_id"`
	TranscriptHash      [sha256.Size]byte   `json:"transcript_hash"`
	CandidateID         string              `json:"candidate_id"`
	ASRProvider         string              `json:"asr_provider"`
	ASRModel            string              `json:"asr_model"`
	ProviderRequestID   string              `json:"provider_request_id"`
	Audio               evidenceSourceAudio `json:"audio"`
}

type evidenceSourceAudio struct {
	Availability   string `json:"availability"`
	AudioAssetID   string `json:"audio_asset_id,omitempty"`
	ChecksumSHA256 string `json:"checksum_sha256,omitempty"`
	Status         string `json:"status,omitempty"`
	Version        uint64 `json:"version,omitempty"`
}

func validCompletedEvidenceSession(
	ownerUserID string,
	practiceSessionID string,
	sceneType SceneType,
	session practice.ContextSession,
	snapshot practice.ContextSessionSnapshot,
) bool {
	if session.ID != practiceSessionID ||
		session.Status != practice.ContextSessionCompleted ||
		session.Version < 1 ||
		session.EffectiveTurns < 1 ||
		session.StartedAt == nil ||
		session.EndedAt == nil ||
		strings.TrimSpace(session.EndReason) == "" ||
		session.SnapshotID == "" ||
		snapshot.ID != session.SnapshotID ||
		snapshot.SessionID != practiceSessionID ||
		snapshot.PlanRevision < 1 ||
		snapshot.ScenarioType != session.ScenarioType ||
		snapshot.ScenarioModel != session.ScenarioModel ||
		snapshot.ScenarioDefinition.Type != snapshot.ScenarioType ||
		snapshot.ScenarioDefinition.Model != snapshot.ScenarioModel ||
		snapshot.ScenarioDefinition.Version < 1 ||
		snapshot.ScenarioConfig.ScenarioDefinitionID !=
			snapshot.ScenarioDefinition.ID ||
		snapshot.ScenarioConfig.Type != snapshot.ScenarioType ||
		snapshot.ScenarioConfig.Model != snapshot.ScenarioModel ||
		snapshot.ScenarioConfig.Version < 1 ||
		snapshot.PracticeOption.ScenarioDefinitionID !=
			snapshot.ScenarioDefinition.ID ||
		snapshot.PracticeOption.Version < 1 ||
		!validIdentifier(snapshot.Preparation.ID) ||
		!validIdentifier(snapshot.Preparation.SourceProfileID) ||
		snapshot.Preparation.SourceVersion < 1 ||
		snapshot.SessionPolicy.MaxEffectiveTurns < session.EffectiveTurns ||
		snapshot.SessionPolicy.MinEffectiveTurns < 1 ||
		snapshot.SessionPolicy.MinEffectiveTurns >
			snapshot.SessionPolicy.MaxEffectiveTurns ||
		!evidenceSceneMatches(
			snapshot.ScenarioType,
			snapshot.ScenarioModel,
			sceneType,
		) {
		return false
	}
	for _, participant := range snapshot.Participants {
		if (participant.Role == "CANDIDATE" ||
			participant.Role == "LEARNER") &&
			participant.SubjectRef.Namespace == "speakup.user" &&
			participant.SubjectRef.SubjectID == ownerUserID {
			return true
		}
	}
	return false
}

func evidenceSceneMatches(
	family practice.ScenarioFamily,
	model practice.ScenarioModel,
	sceneType SceneType,
) bool {
	switch family {
	case practice.ScenarioFamilyExam:
		return sceneType == SceneIELTSSpeaking &&
			(model == practice.ScenarioModelIELTSSpeakingPart2 ||
				model == practice.ScenarioModelIELTSSpeakingFullMock ||
				model == practice.ScenarioModelExamBasicDialogue)
	case practice.ScenarioFamilyInterview:
		return sceneType == SceneInterview
	case practice.ScenarioFamilyDaily:
		return sceneType == SceneOverseasDaily
	case practice.ScenarioFamilyWorkplace:
		return sceneType == SceneOverseasWorkplace
	default:
		return false
	}
}

func evidenceParticipants(
	ownerUserID string,
	practiceSessionID string,
	scenarioDefinitionID string,
	source []practice.ContextParticipant,
) (
	[]evidenceParticipant,
	map[string]struct{},
	map[string]struct{},
	bool,
) {
	result := make([]evidenceParticipant, 0, len(source))
	allParticipants := make(map[string]struct{}, len(source))
	candidates := make(map[string]struct{})
	seen := make(map[string]struct{}, len(source))
	for _, participant := range source {
		if strings.TrimSpace(participant.ID) == "" ||
			participant.SessionID != practiceSessionID ||
			strings.TrimSpace(participant.Role) == "" ||
			participant.Order < 1 {
			return nil, nil, nil, false
		}
		if _, duplicate := seen[participant.ID]; duplicate {
			return nil, nil, nil, false
		}
		seen[participant.ID] = struct{}{}
		allParticipants[participant.ID] = struct{}{}
		version := 0
		displayName := ""
		responsibilities := ""
		style := ""
		var focusAreas []string
		if participant.RoleDefinitionID != "" {
			if participant.RoleSnapshot == nil ||
				participant.RoleSnapshot.ID != participant.RoleDefinitionID ||
				participant.RoleSnapshot.ScenarioDefinitionID !=
					scenarioDefinitionID ||
				participant.RoleSnapshot.Version < 1 {
				return nil, nil, nil, false
			}
			version = participant.RoleSnapshot.Version
			displayName = participant.RoleSnapshot.DisplayName
			responsibilities = participant.RoleSnapshot.Responsibilities
			style = participant.RoleSnapshot.Style
			focusAreas = cloneSortedStrings(participant.RoleSnapshot.FocusAreas)
		}
		if participant.Role == "CANDIDATE" ||
			participant.Role == "LEARNER" {
			if participant.SubjectRef.Namespace != "speakup.user" ||
				participant.SubjectRef.SubjectID != ownerUserID {
				return nil, nil, nil, false
			}
			candidates[participant.ID] = struct{}{}
		}
		result = append(result, evidenceParticipant{
			ID:                    participant.ID,
			Role:                  participant.Role,
			RoleDefinitionID:      participant.RoleDefinitionID,
			RoleDefinitionVersion: version,
			DisplayName:           displayName,
			Responsibilities:      responsibilities,
			Style:                 style,
			FocusAreas:            focusAreas,
			Order:                 participant.Order,
		})
	}
	if len(result) < 2 || len(candidates) != 1 {
		return nil, nil, nil, false
	}
	slices.SortFunc(result, func(left, right evidenceParticipant) int {
		if left.Order != right.Order {
			return left.Order - right.Order
		}
		return strings.Compare(left.ID, right.ID)
	})
	for index, participant := range result {
		if participant.Order != index+1 {
			return nil, nil, nil, false
		}
	}
	return result, allParticipants, candidates, true
}

func evidencePracticeContextFromSnapshot(
	ownerUserID string,
	session practice.ContextSession,
	snapshot practice.ContextSessionSnapshot,
) (
	evidencePracticeContext,
	map[string]struct{},
	map[string]struct{},
	bool,
) {
	participants, allParticipants, candidates, ok := evidenceParticipants(
		ownerUserID,
		session.ID,
		snapshot.ScenarioDefinition.ID,
		snapshot.Participants,
	)
	if !ok {
		return evidencePracticeContext{}, nil, nil, false
	}
	return evidencePracticeContext{
		PracticeSessionID: session.ID,
		SessionSnapshotID: snapshot.ID,
		SessionVersion:    session.Version,
		PlanRevision:      snapshot.PlanRevision,
		SceneFamily:       string(snapshot.ScenarioType),
		ScenarioModel:     string(snapshot.ScenarioModel),
		ScenarioDefinition: evidenceVersionedRef{
			ID:      snapshot.ScenarioDefinition.ID,
			Version: snapshot.ScenarioDefinition.Version,
		},
		ScenarioConfig: evidenceVersionedRef{
			ID:      snapshot.ScenarioConfig.ID,
			Version: snapshot.ScenarioConfig.Version,
		},
		PracticeOption: evidencePracticeOption{
			ID:      snapshot.PracticeOption.ID,
			Type:    snapshot.PracticeOption.Type,
			Version: snapshot.PracticeOption.Version,
		},
		UserRole:        snapshot.ScenarioConfig.PromptModel.UserRole,
		FacilitatorRole: snapshot.ScenarioConfig.PromptModel.AIRole,
		PracticeGoal:    snapshot.ScenarioConfig.PromptModel.PracticeGoal,
		Preparation: evidencePreparationContextFromSnapshot(
			snapshot.Preparation,
		),
		TaskContext: evidenceTaskContext{
			JobTitle:       snapshot.ScenarioConfig.JobTitle,
			JobDescription: snapshot.ScenarioConfig.JobDescription,
			PublicSceneBrief: snapshot.ScenarioConfig.PromptModel.
				PublicSceneBrief,
			PersonaSummary: snapshot.ScenarioConfig.PromptModel.
				PersonaSummary,
			ConfigFocusAreas: cloneSortedStrings(
				snapshot.ScenarioConfig.FocusAreas,
			),
			PromptFocusAreas: cloneSortedStrings(
				snapshot.ScenarioConfig.PromptModel.FocusAreas,
			),
			SuggestedDurationSeconds: snapshot.ScenarioConfig.PromptModel.
				SuggestedDurationSeconds,
		},
		TaskBlueprints: slices.Clone(
			snapshot.ScenarioConfig.PromptModel.TurnBlueprints,
		),
		Participants: participants,
		Objectives: evidenceObjectives(
			snapshot.SessionPolicy.TargetObjectives,
			snapshot.PracticeFocuses,
		),
	}, allParticipants, candidates, true
}

func evidenceObjectives(
	sessionPolicy []practice.PracticeObjective,
	practiceFocus []practice.PracticeObjective,
) evidenceObjectiveSet {
	mapObjectives := func(source []practice.PracticeObjective) []evidenceObjective {
		result := make([]evidenceObjective, len(source))
		for index, item := range source {
			result[index] = evidenceObjective{
				ID:          item.ID,
				Description: item.Description,
			}
		}
		slices.SortFunc(result, func(left, right evidenceObjective) int {
			return strings.Compare(left.ID, right.ID)
		})
		return result
	}
	return evidenceObjectiveSet{
		SessionPolicy: mapObjectives(sessionPolicy),
		PracticeFocus: mapObjectives(practiceFocus),
	}
}

func evidencePreparationContextFromSnapshot(
	source practice.PreparationSnapshot,
) evidencePreparationContext {
	result := evidencePreparationContext{
		SnapshotID:                         source.ID,
		SourceProfileID:                    source.SourceProfileID,
		SourceVersion:                      source.SourceVersion,
		SourceJobTargetID:                  source.SourceJobTargetID,
		SourceJobTargetConfirmationVersion: source.SourceJobTargetConfirmationVersion,
		ResumeSnapshotHash: evidenceTextHash(
			source.ResumeSnapshot,
		),
		JobDescriptionSnapshotHash: evidenceTextHash(
			source.JobDescriptionSnapshot,
		),
		BackgroundSnapshotHash: evidenceTextHash(
			source.BackgroundSnapshot,
		),
	}
	if input := source.JobTargetInputSnapshot; input != nil {
		result.JobTargetInput = &evidenceJobTargetInput{
			Source:              input.Source,
			JobTitle:            input.JobTitle,
			JobDescription:      input.JobDescription,
			Company:             input.Company,
			Seniority:           input.Seniority,
			CandidateBackground: input.CandidateBackground,
			PracticeFocus:       input.PracticeFocus,
		}
	}
	if candidate := source.JobTargetCandidateSnapshot; candidate != nil {
		result.JobTargetCandidate = &evidenceJobTargetCandidate{
			Source:             candidate.Source,
			GeneralAdviceOnly:  candidate.GeneralAdviceOnly,
			JobTitle:           candidate.JobTitle,
			Seniority:          candidate.Seniority,
			Responsibilities:   slices.Clone(candidate.Responsibilities),
			CoreSkills:         slices.Clone(candidate.CoreSkills),
			CommunicationFocus: slices.Clone(candidate.CommunicationFocus),
			PracticeGoals:      slices.Clone(candidate.PracticeGoals),
			ScopeNotice:        candidate.ScopeNotice,
			CatalogRecommendation: evidenceCatalogRecommendation{
				ScenarioDefinitionID: candidate.CatalogRecommendation.
					ScenarioDefinitionID,
				ScenarioDefinitionVersion: candidate.CatalogRecommendation.
					ScenarioDefinitionVersion,
				SelectedRoleIDs: cloneSortedStrings(
					candidate.CatalogRecommendation.SelectedRoleIDs,
				),
				PracticeOptionID: candidate.CatalogRecommendation.
					PracticeOptionID,
				PracticeOptionVersion: candidate.CatalogRecommendation.
					PracticeOptionVersion,
			},
		}
	}
	return result
}

func evidenceTextHash(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func validEvidenceTurn(turn conversation.ConfirmedTurn) bool {
	return strings.TrimSpace(turn.ID) != "" &&
		strings.TrimSpace(turn.QuestionID) != "" &&
		strings.TrimSpace(turn.SpeakerParticipantID) != "" &&
		len(turn.AddresseeParticipantIDs) > 0 &&
		strings.TrimSpace(turn.RespondentParticipantID) != "" &&
		strings.TrimSpace(turn.InteractionMode) != "" &&
		strings.TrimSpace(turn.AnswerText) != "" &&
		utf8.ValidString(turn.AnswerText) &&
		strings.TrimSpace(turn.CandidateID) != "" &&
		turn.EvidenceVersion > 0 &&
		!turn.ConfirmedAt.IsZero()
}

func validEvidenceQuestion(
	question conversation.PersistentQuestion,
	practiceSessionID string,
	expectedSequence int,
	allParticipants map[string]struct{},
	candidateParticipants map[string]struct{},
) bool {
	if strings.TrimSpace(question.ID) == "" ||
		question.SessionID != practiceSessionID ||
		question.Sequence != expectedSequence ||
		strings.TrimSpace(question.SpeakerParticipantID) == "" ||
		strings.TrimSpace(question.ObjectiveID) == "" ||
		strings.TrimSpace(question.Content) == "" ||
		!utf8.ValidString(question.Content) ||
		question.CreatedAt.IsZero() ||
		len(question.AddresseeParticipantIDs) == 0 {
		return false
	}
	if _, ok := allParticipants[question.SpeakerParticipantID]; !ok {
		return false
	}
	seenAddressees := make(
		map[string]struct{},
		len(question.AddresseeParticipantIDs),
	)
	hasCandidate := false
	for _, participantID := range question.AddresseeParticipantIDs {
		if _, ok := allParticipants[participantID]; !ok {
			return false
		}
		if _, duplicate := seenAddressees[participantID]; duplicate {
			return false
		}
		seenAddressees[participantID] = struct{}{}
		if _, ok := candidateParticipants[participantID]; ok {
			hasCandidate = true
		}
	}
	if !hasCandidate {
		return false
	}
	switch question.Type {
	case "PRIMARY":
		return question.ParentQuestionID == ""
	case "FOLLOW_UP":
		return strings.TrimSpace(question.ParentQuestionID) != ""
	default:
		return false
	}
}

func validEvidenceQuestionTurn(
	question conversation.PersistentQuestion,
	turn conversation.ConfirmedTurn,
) bool {
	if question.ID != turn.QuestionID ||
		question.SessionID != turn.SessionID ||
		question.Sequence != turn.Sequence ||
		question.SpeakerParticipantID != turn.SpeakerParticipantID ||
		question.Content == "" ||
		!utf8.ValidString(question.Content) ||
		question.ObjectiveID == "" ||
		!slices.Contains(
			question.AddresseeParticipantIDs,
			turn.RespondentParticipantID,
		) ||
		!slices.Equal(
			cloneSortedStrings(question.AddresseeParticipantIDs),
			cloneSortedStrings(turn.AddresseeParticipantIDs),
		) {
		return false
	}
	switch question.Type {
	case "PRIMARY":
		return question.ParentQuestionID == ""
	case "FOLLOW_UP":
		return strings.TrimSpace(question.ParentQuestionID) != ""
	default:
		return false
	}
}

func evidenceOpportunityFromQuestion(
	question conversation.PersistentQuestion,
) evidenceOpportunity {
	return evidenceOpportunity{
		Sequence:             question.Sequence,
		QuestionID:           question.ID,
		QuestionType:         question.Type,
		ParentQuestionID:     question.ParentQuestionID,
		ObjectiveID:          question.ObjectiveID,
		QuestionText:         question.Content,
		SpeakerParticipantID: question.SpeakerParticipantID,
		AddresseeParticipantIDs: cloneSortedStrings(
			question.AddresseeParticipantIDs,
		),
	}
}

func validEvidenceCandidate(
	candidate conversation.TranscriptCandidate,
	turn conversation.ConfirmedTurn,
) bool {
	return candidate.ID == turn.CandidateID &&
		candidate.SessionID == turn.SessionID &&
		candidate.QuestionID == turn.QuestionID &&
		candidate.RespondentParticipantID ==
			turn.RespondentParticipantID &&
		candidate.EvidenceVersion == turn.EvidenceVersion &&
		candidate.Text == turn.AnswerText &&
		candidate.Status == conversation.CandidateConfirmed &&
		strings.TrimSpace(candidate.TranscriptID) != "" &&
		strings.TrimSpace(candidate.Provider) != "" &&
		strings.TrimSpace(candidate.Model) != "" &&
		strings.TrimSpace(candidate.ProviderRequestID) != ""
}

func validEvidenceAudio(
	asset domainconversation.AudioAsset,
	ownerUserID string,
	turn conversation.ConfirmedTurn,
) bool {
	switch asset.Status {
	case domainconversation.AudioAssetReadable,
		domainconversation.AudioAssetDeleting,
		domainconversation.AudioAssetDeleted:
	default:
		return false
	}
	_, checksumErr := hex.DecodeString(asset.ChecksumSHA256)
	return strings.TrimSpace(asset.ID) != "" &&
		asset.OwnerID == ownerUserID &&
		asset.TurnID == turn.ID &&
		asset.CandidateID == turn.CandidateID &&
		(asset.ContentType == "audio/wav" ||
			asset.ContentType == "audio/x-wav") &&
		asset.Size > 0 &&
		len(asset.ChecksumSHA256) == sha256.Size*2 &&
		checksumErr == nil &&
		asset.Duration > 0 &&
		asset.Version > 0
}

func canonicalEvidenceJSON(payload evidencePayload) (json.RawMessage, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	return canonicalEvidencePayload(encoded)
}

func mustEvidenceHash(value any) [sha256.Size]byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("marshal typed evidence source: %v", err))
	}
	return sha256.Sum256(encoded)
}

func evidenceSourceManifestFromPayload(
	payload evidencePayload,
) (evidenceSourceManifest, error) {
	if len(payload.OpportunityManifest) < len(payload.ConfirmedTurns) ||
		len(payload.ConfirmedTurns) != len(payload.EvidenceRefs) ||
		len(payload.ConfirmedTurns) != len(payload.ProviderLineage.ASR) {
		return evidenceSourceManifest{}, ErrInvalidRequest
	}
	refsByTurn := make(map[string]evidenceRef, len(payload.EvidenceRefs))
	for _, ref := range payload.EvidenceRefs {
		if _, duplicate := refsByTurn[ref.TurnID]; duplicate {
			return evidenceSourceManifest{}, ErrInvalidRequest
		}
		refsByTurn[ref.TurnID] = ref
	}
	asrByTurn := make(
		map[string]evidenceASRLineage,
		len(payload.ProviderLineage.ASR),
	)
	for _, lineage := range payload.ProviderLineage.ASR {
		if _, duplicate := asrByTurn[lineage.TurnID]; duplicate {
			return evidenceSourceManifest{}, ErrInvalidRequest
		}
		asrByTurn[lineage.TurnID] = lineage
	}
	manifest := evidenceSourceManifest{
		Version:             evidenceSourceManifestVersion,
		PracticeSessionID:   payload.PracticeContext.PracticeSessionID,
		SessionVersion:      payload.PracticeContext.SessionVersion,
		SessionSnapshotID:   payload.PracticeContext.SessionSnapshotID,
		PracticeContextHash: mustEvidenceHash(payload.PracticeContext),
		ProviderLineageHash: mustEvidenceHash(payload.ProviderLineage),
		VersionManifestHash: mustEvidenceHash(payload.VersionManifest),
		Questions: make(
			[]evidenceSourceQuestion,
			0,
			len(payload.OpportunityManifest),
		),
		Turns: make(
			[]evidenceSourceTurn,
			0,
			len(payload.ConfirmedTurns),
		),
	}
	for _, opportunity := range payload.OpportunityManifest {
		manifest.Questions = append(
			manifest.Questions,
			evidenceSourceQuestion{
				QuestionID:   opportunity.QuestionID,
				QuestionHash: mustEvidenceHash(opportunity),
			},
		)
	}
	for _, turn := range payload.ConfirmedTurns {
		ref, refExists := refsByTurn[turn.TurnID]
		lineage, lineageExists := asrByTurn[turn.TurnID]
		if !refExists || !lineageExists ||
			ref.Lineage.CandidateID != lineage.CandidateID {
			return evidenceSourceManifest{}, ErrInvalidRequest
		}
		manifest.Turns = append(
			manifest.Turns,
			evidenceSourceTurn{
				TurnID:              turn.TurnID,
				TurnEvidenceVersion: turn.Transcript.EvidenceVersion,
				QuestionID:          turn.QuestionID,
				TurnHash:            mustEvidenceHash(turn),
				TranscriptID:        turn.Transcript.ID,
				TranscriptHash: sha256.Sum256(
					[]byte(turn.Transcript.Text),
				),
				CandidateID:       lineage.CandidateID,
				ASRProvider:       lineage.Provider,
				ASRModel:          lineage.Model,
				ProviderRequestID: lineage.ProviderRequestID,
				Audio: evidenceSourceAudio{
					Availability:   turn.Audio.Availability,
					AudioAssetID:   turn.Audio.AudioAssetID,
					ChecksumSHA256: turn.Audio.ChecksumSHA256,
					Status:         turn.Audio.Status,
					Version:        turn.Audio.Version,
				},
			},
		)
	}
	return manifest, nil
}

func evidenceSourceManifestHash(
	payload json.RawMessage,
) ([sha256.Size]byte, error) {
	var evidence evidencePayload
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil {
		return [sha256.Size]byte{}, ErrInvalidRequest
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return [sha256.Size]byte{}, err
	}
	manifest, err := evidenceSourceManifestFromPayload(evidence)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return [sha256.Size]byte{}, ErrInvalidRequest
	}
	return sha256.Sum256(encoded), nil
}

func stableEvidenceRefID(
	snapshotID string,
	turnID string,
	evidenceVersion int64,
	audioChecksum string,
) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf(
		"%s\x00%s\x00%d\x00%s",
		snapshotID,
		turnID,
		evidenceVersion,
		audioChecksum,
	)))
	return "evidence_ref_" + hex.EncodeToString(sum[:16])
}

func cloneSortedStrings(source []string) []string {
	result := slices.Clone(source)
	slices.Sort(result)
	return result
}

func mapEvidencePracticeError(err error) error {
	switch {
	case errors.Is(err, practice.ErrNotFound),
		errors.Is(err, practice.ErrDeletionGeneration):
		return ErrNotFound
	case errors.Is(err, practice.ErrInvalidArgument),
		errors.Is(err, practice.ErrConflict):
		return ErrInvalidRequest
	default:
		return err
	}
}

func mapEvidenceConversationError(err error) error {
	switch {
	case errors.Is(err, conversation.ErrPersistenceNotFound),
		errors.Is(err, conversation.ErrActorDeleted):
		return ErrNotFound
	case errors.Is(err, conversation.ErrPersistenceInvalid),
		errors.Is(err, conversation.ErrPersistenceConflict):
		return ErrInvalidRequest
	default:
		return err
	}
}
