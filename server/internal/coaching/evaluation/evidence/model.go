package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice"
)

type EvidenceSnapshot struct {
	ID                 string
	OwnerUserID        string
	PracticeSessionID  string
	InputRevision      int
	Scope              evaluation.Scope
	SceneType          evaluation.SceneType
	SourceManifestHash [sha256.Size]byte
	SnapshotHash       [sha256.Size]byte
	Payload            json.RawMessage
	CreatedAt          time.Time
}

func (s EvidenceSnapshot) Valid() bool {
	canonical, err := CanonicalPayload(s.Payload)
	sourceManifestHash, sourceErr := SourceManifestHash(canonical)
	return validIdentifier(s.ID) &&
		validUUID(s.OwnerUserID) &&
		validIdentifier(s.PracticeSessionID) &&
		s.InputRevision > 0 &&
		validScope(s.Scope) &&
		validSceneType(s.SceneType) &&
		nonZeroDigest(s.SourceManifestHash) &&
		err == nil &&
		sourceErr == nil &&
		sourceManifestHash == s.SourceManifestHash &&
		s.ID == DeriveSnapshotID(
			s.OwnerUserID,
			s.PracticeSessionID,
			s.Scope,
			s.SourceManifestHash,
		) &&
		evidencePayloadMatchesSnapshot(
			canonical,
			s.ID,
			s.PracticeSessionID,
			s.Scope,
			s.SceneType,
		) &&
		sha256.Sum256(canonical) == s.SnapshotHash &&
		!s.CreatedAt.IsZero()
}

type EnsureEvidenceSnapshotCommand struct {
	SnapshotID         string
	OwnerUserID        string
	PracticeSessionID  string
	Scope              evaluation.Scope
	SceneType          evaluation.SceneType
	SourceManifestHash [sha256.Size]byte
	CanonicalPayload   json.RawMessage
}

type EvidenceSnapshotRepository interface {
	EnsureEvidenceSnapshot(
		ctx context.Context,
		command EnsureEvidenceSnapshotCommand,
	) (EvidenceSnapshot, bool, error)
	GetEvidenceSnapshot(
		ctx context.Context,
		ownerUserID string,
		snapshotID string,
	) (EvidenceSnapshot, error)
}

func normalizeEvidenceSnapshotCommand(
	command EnsureEvidenceSnapshotCommand,
) (EnsureEvidenceSnapshotCommand, error) {
	if !validIdentifier(command.SnapshotID) ||
		!validUUID(command.OwnerUserID) ||
		!validIdentifier(command.PracticeSessionID) ||
		!validScope(command.Scope) ||
		!validSceneType(command.SceneType) ||
		!nonZeroDigest(command.SourceManifestHash) {
		return EnsureEvidenceSnapshotCommand{}, evaluation.ErrInvalidRequest
	}
	if command.SnapshotID != DeriveSnapshotID(
		command.OwnerUserID,
		command.PracticeSessionID,
		command.Scope,
		command.SourceManifestHash,
	) {
		return EnsureEvidenceSnapshotCommand{}, evaluation.ErrInvalidRequest
	}
	canonical, err := CanonicalPayload(command.CanonicalPayload)
	if err != nil ||
		!evidencePayloadMatchesSnapshot(
			canonical,
			command.SnapshotID,
			command.PracticeSessionID,
			command.Scope,
			command.SceneType,
		) {
		return EnsureEvidenceSnapshotCommand{}, evaluation.ErrInvalidRequest
	}
	sourceManifestHash, err := SourceManifestHash(canonical)
	if err != nil || sourceManifestHash != command.SourceManifestHash {
		return EnsureEvidenceSnapshotCommand{}, evaluation.ErrInvalidRequest
	}
	command.PracticeSessionID = strings.TrimSpace(
		command.PracticeSessionID,
	)
	command.CanonicalPayload = canonical
	return command, nil
}

func nonZeroDigest(digest [sha256.Size]byte) bool {
	return digest != [sha256.Size]byte{}
}

func DeriveSnapshotID(
	ownerUserID string,
	practiceSessionID string,
	scope evaluation.Scope,
	sourceManifestHash [sha256.Size]byte,
) string {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte("evidence-snapshot-id:v2\x00"))
	_, _ = hasher.Write([]byte(ownerUserID))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(practiceSessionID))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(scope))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write(sourceManifestHash[:])
	return "snapshot_" + hex.EncodeToString(hasher.Sum(nil)[:16])
}

func CanonicalPayload(
	payload json.RawMessage,
) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, evaluation.ErrInvalidRequest
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok || !validEvidencePayloadShape(object) ||
		containsPrivateStorageLocator(object) {
		return nil, evaluation.ErrInvalidRequest
	}
	canonical, err := json.Marshal(object)
	if err != nil {
		return nil, evaluation.ErrInvalidRequest
	}
	return canonical, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	return evaluation.ErrInvalidRequest
}

func validEvidencePayloadShape(object map[string]any) bool {
	required := [...]string{
		"practice_context",
		"opportunity_manifest",
		"confirmed_turns",
		"evidence_refs",
		"provider_lineage",
		"version_manifest",
	}
	if len(object) != len(required) {
		return false
	}
	for _, field := range required {
		if value, exists := object[field]; !exists || value == nil {
			return false
		}
	}
	_, turnsAreArray := object["confirmed_turns"].([]any)
	_, refsAreArray := object["evidence_refs"].([]any)
	return turnsAreArray && refsAreArray
}

func evidencePayloadMatchesSnapshot(
	payload json.RawMessage,
	snapshotID string,
	practiceSessionID string,
	scope evaluation.Scope,
	sceneType evaluation.SceneType,
) bool {
	var evidence SnapshotPayload
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&evidence) != nil ||
		ensureJSONEOF(decoder) != nil ||
		!typedEvidencePayloadMatchesCanonical(payload, evidence) ||
		scope != evaluation.ScopeSession ||
		evidence.PracticeContext.PracticeSessionID != practiceSessionID ||
		!evidencePracticeContextMatchesScene(
			evidence.PracticeContext,
			sceneType,
		) ||
		!validEvidenceUnavailableDeclarations(evidence) ||
		len(evidence.ConfirmedTurns) == 0 ||
		len(evidence.EvidenceRefs) != len(evidence.ConfirmedTurns) ||
		len(evidence.ProviderLineage.ASR) != len(evidence.ConfirmedTurns) ||
		len(evidence.VersionManifest.TurnEvidence) !=
			len(evidence.ConfirmedTurns) {
		return false
	}
	participants, candidates, ok := validEvidenceParticipantBindings(
		evidence.PracticeContext.Participants,
	)
	if !ok {
		return false
	}
	turns := make(
		map[string]ConfirmedTurn,
		len(evidence.ConfirmedTurns),
	)
	seenAudioAssets := make(map[string]struct{}, len(evidence.ConfirmedTurns))
	for index, turn := range evidence.ConfirmedTurns {
		if !validEvidenceConfirmedTurnBinding(turn, index+1) {
			return false
		}
		if _, candidate := candidates[turn.RespondentParticipantID]; !candidate {
			return false
		}
		if _, duplicate := turns[turn.TurnID]; duplicate {
			return false
		}
		if turn.Audio.AudioAssetID != "" {
			if _, duplicate := seenAudioAssets[turn.Audio.AudioAssetID]; duplicate {
				return false
			}
			seenAudioAssets[turn.Audio.AudioAssetID] = struct{}{}
		}
		turns[turn.TurnID] = turn
	}
	asrByTurn := make(
		map[string]ASRLineage,
		len(evidence.ProviderLineage.ASR),
	)
	for _, lineage := range evidence.ProviderLineage.ASR {
		if !validIdentifier(lineage.TurnID) ||
			!validIdentifier(lineage.TranscriptID) ||
			!validIdentifier(lineage.CandidateID) ||
			lineage.EvidenceVersion < 1 ||
			strings.TrimSpace(lineage.Provider) == "" ||
			strings.TrimSpace(lineage.Model) == "" ||
			strings.TrimSpace(lineage.ProviderRequestID) == "" {
			return false
		}
		if _, duplicate := asrByTurn[lineage.TurnID]; duplicate {
			return false
		}
		asrByTurn[lineage.TurnID] = lineage
	}
	versionByTurn := make(
		map[string]TurnVersion,
		len(evidence.VersionManifest.TurnEvidence),
	)
	for _, version := range evidence.VersionManifest.TurnEvidence {
		if !validIdentifier(version.TurnID) ||
			version.EvidenceVersion < 1 {
			return false
		}
		if _, duplicate := versionByTurn[version.TurnID]; duplicate {
			return false
		}
		versionByTurn[version.TurnID] = version
	}
	seenRefs := make(map[string]struct{}, len(evidence.EvidenceRefs))
	seenRefTurns := make(map[string]struct{}, len(evidence.EvidenceRefs))
	seenCandidates := make(map[string]struct{}, len(evidence.EvidenceRefs))
	type transcriptVersionKey struct {
		id      string
		version int64
	}
	seenTranscripts := make(
		map[transcriptVersionKey]struct{},
		len(evidence.EvidenceRefs),
	)
	for _, ref := range evidence.EvidenceRefs {
		turn, exists := turns[ref.TurnID]
		lineage, lineageExists := asrByTurn[ref.TurnID]
		version, versionExists := versionByTurn[ref.TurnID]
		if !exists || !lineageExists || !versionExists ||
			!validEvidenceRefBinding(snapshotID, ref, turn, lineage, version) {
			return false
		}
		if _, duplicate := seenRefs[ref.EvidenceRefID]; duplicate {
			return false
		}
		if _, duplicate := seenRefTurns[ref.TurnID]; duplicate {
			return false
		}
		if _, duplicate := seenCandidates[ref.Lineage.CandidateID]; duplicate {
			return false
		}
		transcriptKey := transcriptVersionKey{
			id:      ref.Lineage.TranscriptID,
			version: ref.Lineage.EvidenceVersion,
		}
		if _, duplicate := seenTranscripts[transcriptKey]; duplicate {
			return false
		}
		seenRefs[ref.EvidenceRefID] = struct{}{}
		seenRefTurns[ref.TurnID] = struct{}{}
		seenCandidates[ref.Lineage.CandidateID] = struct{}{}
		seenTranscripts[transcriptKey] = struct{}{}
	}
	if len(seenRefTurns) != len(turns) {
		return false
	}
	return opportunitiesBindToTurns(
		evidence.OpportunityManifest,
		turns,
		participants,
		candidates,
	)
}

func typedEvidencePayloadMatchesCanonical(
	payload json.RawMessage,
	evidence SnapshotPayload,
) bool {
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return false
	}
	canonical, err := CanonicalPayload(encoded)
	return err == nil && bytes.Equal(payload, canonical)
}

func validEvidenceConfirmedTurnBinding(
	turn ConfirmedTurn,
	expectedSequence int,
) bool {
	if !validIdentifier(turn.TurnID) ||
		turn.Sequence != expectedSequence ||
		!validIdentifier(turn.QuestionID) ||
		!validIdentifier(turn.RespondentParticipantID) ||
		strings.TrimSpace(turn.InteractionMode) == "" ||
		!validIdentifier(turn.Transcript.ID) ||
		strings.TrimSpace(turn.Transcript.Text) == "" ||
		turn.Transcript.EvidenceVersion < 1 {
		return false
	}
	audio := turn.Audio
	if audio.Availability != "AVAILABLE" &&
		audio.Availability != evidenceUnavailable {
		return false
	}
	if audio.AudioAssetID == "" {
		return audio.Availability == evidenceUnavailable &&
			audio.ChecksumSHA256 == "" &&
			audio.DurationMS == 0 &&
			audio.ContentType == "" &&
			audio.SizeBytes == 0 &&
			audio.Status == "" &&
			audio.Version == 0
	}
	decodedChecksum, err := hex.DecodeString(audio.ChecksumSHA256)
	validStatus := (audio.Availability == "AVAILABLE" &&
		audio.Status == "readable") ||
		(audio.Availability == evidenceUnavailable &&
			(audio.Status == "deleting" || audio.Status == "deleted"))
	return validStatus &&
		validIdentifier(audio.AudioAssetID) &&
		len(decodedChecksum) == sha256.Size &&
		audio.DurationMS > 0 &&
		(audio.ContentType == "audio/wav" ||
			audio.ContentType == "audio/x-wav") &&
		audio.SizeBytes > 0 &&
		strings.TrimSpace(audio.Status) != "" &&
		audio.Version > 0 &&
		err == nil
}

func validEvidenceRefBinding(
	snapshotID string,
	ref Ref,
	turn ConfirmedTurn,
	lineage ASRLineage,
	version TurnVersion,
) bool {
	if !validIdentifier(ref.EvidenceRefID) ||
		ref.SnapshotID != snapshotID ||
		ref.TurnID != turn.TurnID ||
		ref.Speaker != "USER" ||
		ref.TranscriptSpan.StartUTF8Byte != 0 ||
		ref.TranscriptSpan.EndUTF8Byte != len([]byte(turn.Transcript.Text)) ||
		ref.Lineage.TranscriptID != turn.Transcript.ID ||
		ref.Lineage.TranscriptID != lineage.TranscriptID ||
		!validIdentifier(ref.Lineage.CandidateID) ||
		ref.Lineage.CandidateID != lineage.CandidateID ||
		ref.Lineage.EvidenceVersion != turn.Transcript.EvidenceVersion ||
		ref.Lineage.EvidenceVersion != lineage.EvidenceVersion ||
		ref.Lineage.EvidenceVersion != version.EvidenceVersion ||
		ref.Lineage.ASRProvider != lineage.Provider ||
		ref.Lineage.ASRModel != lineage.Model ||
		version.TurnID != turn.TurnID {
		return false
	}
	expectedRefID := StableRefID(
		snapshotID,
		turn.TurnID,
		turn.Transcript.EvidenceVersion,
		turn.Audio.ChecksumSHA256,
	)
	if ref.EvidenceRefID != expectedRefID {
		return false
	}
	if turn.Audio.Availability == "AVAILABLE" {
		return ref.AudioSpan != nil &&
			ref.AudioSpan.AudioAssetID == turn.Audio.AudioAssetID &&
			ref.AudioSpan.StartMS == 0 &&
			ref.AudioSpan.EndMS == turn.Audio.DurationMS &&
			ref.Lineage.AudioAssetVersion == turn.Audio.Version &&
			version.AudioVersion == turn.Audio.Version
	}
	return ref.AudioSpan == nil &&
		ref.Lineage.AudioAssetVersion == 0 &&
		version.AudioVersion == turn.Audio.Version
}

func opportunitiesBindToTurns(
	opportunities []Opportunity,
	turns map[string]ConfirmedTurn,
	participants map[string]struct{},
	candidates map[string]struct{},
) bool {
	if len(opportunities) < len(turns) {
		return false
	}
	seenQuestions := make(map[string]struct{}, len(opportunities))
	seenResponseTurns := make(map[string]struct{}, len(turns))
	for index, opportunity := range opportunities {
		if opportunity.Sequence != index+1 ||
			!validIdentifier(opportunity.QuestionID) ||
			!validIdentifier(opportunity.ObjectiveID) ||
			strings.TrimSpace(opportunity.QuestionText) == "" ||
			!validIdentifier(opportunity.SpeakerParticipantID) ||
			len(opportunity.AddresseeParticipantIDs) == 0 {
			return false
		}
		if _, exists := participants[opportunity.SpeakerParticipantID]; !exists {
			return false
		}
		seenAddressees := make(
			map[string]struct{},
			len(opportunity.AddresseeParticipantIDs),
		)
		hasCandidate := false
		for _, addressee := range opportunity.AddresseeParticipantIDs {
			if _, exists := participants[addressee]; !exists {
				return false
			}
			if _, duplicate := seenAddressees[addressee]; duplicate {
				return false
			}
			seenAddressees[addressee] = struct{}{}
			if _, candidate := candidates[addressee]; candidate {
				hasCandidate = true
			}
		}
		if !hasCandidate {
			return false
		}
		if _, duplicate := seenQuestions[opportunity.QuestionID]; duplicate {
			return false
		}
		if opportunity.QuestionType == "FOLLOW_UP" {
			if _, exists := seenQuestions[opportunity.ParentQuestionID]; !exists {
				return false
			}
		} else if opportunity.QuestionType != "PRIMARY" ||
			opportunity.ParentQuestionID != "" {
			return false
		}
		seenQuestions[opportunity.QuestionID] = struct{}{}
		if opportunity.ResponseTurnID == "" {
			continue
		}
		turn, exists := turns[opportunity.ResponseTurnID]
		if !exists || turn.QuestionID != opportunity.QuestionID ||
			turn.Sequence != opportunity.Sequence {
			return false
		}
		if _, addressed := seenAddressees[turn.RespondentParticipantID]; !addressed {
			return false
		}
		if _, duplicate := seenResponseTurns[turn.TurnID]; duplicate {
			return false
		}
		seenResponseTurns[turn.TurnID] = struct{}{}
	}
	return len(seenResponseTurns) == len(turns)
}

func validEvidenceParticipantBindings(
	participants []Participant,
) (map[string]struct{}, map[string]struct{}, bool) {
	all := make(map[string]struct{}, len(participants))
	candidates := make(map[string]struct{})
	facilitators := make(map[string]struct{})
	for index, participant := range participants {
		if !validIdentifier(participant.ID) ||
			(participant.Role != "FACILITATOR" &&
				participant.Role != "LEARNER") ||
			participant.Order != index+1 {
			return nil, nil, false
		}
		if _, duplicate := all[participant.ID]; duplicate {
			return nil, nil, false
		}
		all[participant.ID] = struct{}{}
		if participant.Role == "LEARNER" {
			candidates[participant.ID] = struct{}{}
		} else {
			facilitators[participant.ID] = struct{}{}
		}
		if participant.RoleDefinitionID != "" &&
			!validIdentifier(participant.RoleDefinitionID) {
			return nil, nil, false
		}
	}
	return all, candidates,
		len(all) > 1 && len(candidates) == 1 && len(facilitators) > 0
}

func evidencePracticeContextMatchesScene(
	context PracticeContext,
	sceneType evaluation.SceneType,
) bool {
	return validIdentifier(context.PracticeSessionID) &&
		validIdentifier(context.SessionSnapshotID) &&
		context.SessionVersion > 0 &&
		context.PlanRevision > 0 &&
		validIdentifier(context.Scene.ID) &&
		context.Scene.Version > 0 &&
		validIdentifier(context.PracticeOption.ID) &&
		strings.TrimSpace(context.PracticeOption.Type) != "" &&
		strings.TrimSpace(context.UserRole) != "" &&
		strings.TrimSpace(context.FacilitatorRole) != "" &&
		strings.TrimSpace(context.PracticeGoal) != "" &&
		validIdentifier(context.Preparation.SnapshotID) &&
		validIdentifier(context.Preparation.SourceProfileID) &&
		context.Preparation.SourceVersion > 0 &&
		len(context.TaskBlueprints) > 0 &&
		len(context.Participants) > 1 &&
		evidenceSceneMatches(
			practice.SceneFamily(context.SceneFamily),
			practice.SceneModel(context.SceneModel),
			sceneType,
		)
}

func validEvidenceUnavailableDeclarations(evidence SnapshotPayload) bool {
	unavailable := evidence.ProviderLineage.UnavailableArtifacts
	version := evidence.VersionManifest
	if unavailable.WordTimestamps != evidenceUnavailable ||
		unavailable.ASRConfidence != evidenceUnavailable ||
		unavailable.ASRNBest != evidenceUnavailable ||
		unavailable.AudioQuality != evidenceNotAssessed ||
		unavailable.ISE != evidenceNotAssessed ||
		unavailable.FeatureBundle != evidenceUnavailable ||
		version.SchemaVersion != evidenceSnapshotSchemaVersion ||
		version.SourceManifestVersion != evidenceSourceManifestVersion ||
		version.PracticeSession !=
			evidence.PracticeContext.SessionVersion ||
		version.PracticeSnapshot !=
			evidence.PracticeContext.SessionSnapshotID ||
		version.PlanRevision != evidence.PracticeContext.PlanRevision ||
		version.AudioQuality != evidenceUnavailable ||
		version.ISE != evidenceUnavailable ||
		version.FeatureBundle != evidenceUnavailable ||
		version.ScoringPrompt != evidenceUnavailable ||
		version.Rubric != evidenceUnavailable ||
		version.Gate != evidenceUnavailable ||
		version.Aggregation != evidenceUnavailable ||
		version.Calibration != evidenceUnavailable ||
		version.Pipeline != evidenceUnavailable {
		return false
	}
	for _, turn := range evidence.ConfirmedTurns {
		if turn.Transcript.ASRConfidence != evidenceUnavailable ||
			turn.Transcript.WordTimestamps != evidenceUnavailable ||
			turn.Transcript.AlternativeHypotheses != evidenceUnavailable ||
			turn.Audio.Quality != evidenceNotAssessed ||
			turn.Audio.ISE != evidenceNotAssessed {
			return false
		}
	}
	for _, ref := range evidence.EvidenceRefs {
		if ref.Quality.Audio != evidenceNotAssessed ||
			ref.Quality.ASRConfidence != evidenceUnavailable ||
			ref.Quality.Alignment != evidenceUnavailable ||
			ref.Quality.ISE != evidenceNotAssessed {
			return false
		}
	}
	return true
}

func containsPrivateStorageLocator(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			normalized := strings.NewReplacer(
				"_", "",
				"-", "",
			).Replace(strings.ToLower(key))
			switch normalized {
			case "objectkey", "signedurl", "audiourl", "url":
				return true
			}
			if containsPrivateStorageLocator(nested) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if containsPrivateStorageLocator(nested) {
				return true
			}
		}
	}
	return false
}
