package scoring

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
)

const IELTSAcousticSnapshotSchemaVersion = "ielts-speaking-acoustic-snapshot/v1"

var ErrIELTSAcousticSnapshotPending = errors.New(
	"evaluation: IELTS acoustic snapshot pending",
)

type IELTSAcousticSnapshotResolution string

const (
	IELTSAcousticSnapshotComplete IELTSAcousticSnapshotResolution = "COMPLETE"
	IELTSAcousticSnapshotPartial  IELTSAcousticSnapshotResolution = "PARTIAL"
	IELTSAcousticSnapshotTextOnly IELTSAcousticSnapshotResolution = "TEXT_ONLY"
)

type IELTSSpeakingAcousticRead struct {
	Values         []IELTSSpeakingTurnAcoustics
	PendingTurnIDs []string
}

type IELTSAcousticSnapshot struct {
	ID                string
	EvaluationID      string
	OwnerUserID       string
	InputSnapshotID   string
	InputSnapshotHash [sha256.Size]byte
	Resolution        IELTSAcousticSnapshotResolution
	SnapshotHash      [sha256.Size]byte
	Payload           json.RawMessage
	CreatedAt         time.Time
}

type ieltsAcousticSnapshotPayload struct {
	SchemaVersion     string                          `json:"schema_version"`
	EvaluationID      string                          `json:"evaluation_id"`
	OwnerUserID       string                          `json:"owner_user_id"`
	InputSnapshotID   string                          `json:"input_snapshot_id"`
	InputSnapshotHash string                          `json:"input_snapshot_hash"`
	Resolution        IELTSAcousticSnapshotResolution `json:"resolution"`
	Turns             []ieltsAcousticSnapshotTurn     `json:"turns"`
}

type ieltsAcousticSnapshotTurn struct {
	TurnID               string   `json:"turn_id"`
	EvidenceRefID        string   `json:"evidence_ref_id"`
	EvidenceVersion      int64    `json:"evidence_version"`
	AudioAssetID         string   `json:"audio_asset_id,omitempty"`
	AudioAssetVersion    uint64   `json:"audio_asset_version,omitempty"`
	AudioChecksumSHA256  string   `json:"audio_checksum_sha256,omitempty"`
	RecordingDurationMS  int64    `json:"recording_duration_ms"`
	Status               string   `json:"status"`
	UnavailableReason    string   `json:"unavailable_reason,omitempty"`
	PronunciationScore   *float64 `json:"pronunciation_score,omitempty"`
	AcousticFluencyScore *float64 `json:"acoustic_fluency_score,omitempty"`
	SpeakingSpeedWPM     *float64 `json:"speaking_speed_wpm,omitempty"`
	Provider             string   `json:"provider,omitempty"`
	ProviderRunHash      string   `json:"provider_run_hash,omitempty"`
}

func BuildIELTSAcousticSnapshot(
	evaluationID string,
	base evidence.EvidenceSnapshot,
	read IELTSSpeakingAcousticRead,
	deadlineReached bool,
) (IELTSAcousticSnapshot, error) {
	if !validUUID(evaluationID) || !base.Valid() ||
		base.SceneType != evaluation.SceneIELTSSpeaking ||
		base.Scope != evaluation.ScopeSession {
		return IELTSAcousticSnapshot{}, evaluation.ErrInvalidRequest
	}
	requests, err := ieltsSpeakingAcousticRequests(base)
	if err != nil {
		return IELTSAcousticSnapshot{}, err
	}
	requestsByTurn := make(
		map[string]IELTSSpeakingAcousticRequest,
		len(requests),
	)
	for _, request := range requests {
		requestsByTurn[request.TurnID] = request
	}
	values := make(map[string]IELTSSpeakingTurnAcoustics, len(read.Values))
	for _, value := range read.Values {
		request, requested := requestsByTurn[value.TurnID]
		if !requested || value.EvidenceRefID != request.EvidenceRefID ||
			request.RecordingDurationMS <= 0 ||
			!validIELTSSpeakingTurnAcoustics(value) ||
			!validIELTSAcousticProviderRunHash(value.ProviderRun) {
			return IELTSAcousticSnapshot{}, evaluation.ErrInvalidRequest
		}
		if _, duplicate := values[value.TurnID]; duplicate {
			return IELTSAcousticSnapshot{}, evaluation.ErrInvalidRequest
		}
		values[value.TurnID] = value
	}
	pending := make(map[string]struct{}, len(read.PendingTurnIDs))
	for _, turnID := range read.PendingTurnIDs {
		request, requested := requestsByTurn[turnID]
		if !requested || request.RecordingDurationMS <= 0 ||
			!validIdentifier(turnID) {
			return IELTSAcousticSnapshot{}, evaluation.ErrInvalidRequest
		}
		if _, duplicate := pending[turnID]; duplicate {
			return IELTSAcousticSnapshot{}, evaluation.ErrInvalidRequest
		}
		if _, assessed := values[turnID]; assessed {
			return IELTSAcousticSnapshot{}, evaluation.ErrInvalidRequest
		}
		pending[turnID] = struct{}{}
	}
	if len(pending) > 0 && !deadlineReached {
		return IELTSAcousticSnapshot{}, ErrIELTSAcousticSnapshotPending
	}
	payload := ieltsAcousticSnapshotPayload{
		SchemaVersion:     IELTSAcousticSnapshotSchemaVersion,
		EvaluationID:      evaluationID,
		OwnerUserID:       base.OwnerUserID,
		InputSnapshotID:   base.ID,
		InputSnapshotHash: hex.EncodeToString(base.SnapshotHash[:]),
		Turns:             make([]ieltsAcousticSnapshotTurn, 0, len(requests)),
	}
	assessed := 0
	for _, request := range requests {
		turn := ieltsAcousticSnapshotTurn{
			TurnID:              request.TurnID,
			EvidenceRefID:       request.EvidenceRefID,
			EvidenceVersion:     request.EvidenceVersion,
			AudioAssetID:        request.AudioAssetID,
			AudioAssetVersion:   request.AudioAssetVersion,
			AudioChecksumSHA256: request.AudioChecksumSHA256,
			RecordingDurationMS: request.RecordingDurationMS,
		}
		value, ok := values[request.TurnID]
		switch {
		case ok:
			turn.Status = "ASSESSED"
			pronunciation := value.PronunciationScore
			turn.PronunciationScore = &pronunciation
			turn.AcousticFluencyScore = cloneFloat64(value.AcousticFluencyScore)
			turn.SpeakingSpeedWPM = cloneFloat64(value.SpeakingSpeedWPM)
			turn.Provider = value.Provider
			turn.ProviderRunHash = value.ProviderRun
			assessed++
		case containsStringKey(pending, request.TurnID):
			turn.Status = "UNAVAILABLE"
			turn.UnavailableReason = "DEADLINE_EXCEEDED"
		default:
			turn.Status = "UNAVAILABLE"
			if request.RecordingDurationMS == 0 {
				turn.UnavailableReason = "NO_AUDIO"
			} else {
				turn.UnavailableReason = "NOT_ASSESSED"
			}
		}
		payload.Turns = append(payload.Turns, turn)
		delete(values, request.TurnID)
		delete(pending, request.TurnID)
	}
	if len(values) != 0 || len(pending) != 0 {
		return IELTSAcousticSnapshot{}, evaluation.ErrInvalidRequest
	}
	switch {
	case assessed == len(requests):
		payload.Resolution = IELTSAcousticSnapshotComplete
	case assessed > 0:
		payload.Resolution = IELTSAcousticSnapshotPartial
	default:
		payload.Resolution = IELTSAcousticSnapshotTextOnly
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return IELTSAcousticSnapshot{}, evaluation.ErrInvalidRequest
	}
	snapshotHash := sha256.Sum256(encoded)
	result := IELTSAcousticSnapshot{
		EvaluationID:      evaluationID,
		OwnerUserID:       base.OwnerUserID,
		InputSnapshotID:   base.ID,
		InputSnapshotHash: base.SnapshotHash,
		Resolution:        payload.Resolution,
		SnapshotHash:      snapshotHash,
		Payload:           encoded,
	}
	result.ID = deriveIELTSAcousticSnapshotID(evaluationID, snapshotHash)
	if !result.ValidFor(base) {
		return IELTSAcousticSnapshot{}, evaluation.ErrInvalidRequest
	}
	return result, nil
}

func (snapshot IELTSAcousticSnapshot) ValidFor(
	base evidence.EvidenceSnapshot,
) bool {
	if !base.Valid() || !validIdentifier(snapshot.ID) ||
		!validUUID(snapshot.EvaluationID) ||
		snapshot.OwnerUserID != base.OwnerUserID ||
		snapshot.InputSnapshotID != base.ID ||
		snapshot.InputSnapshotHash != base.SnapshotHash ||
		snapshot.ID != deriveIELTSAcousticSnapshotID(
			snapshot.EvaluationID,
			snapshot.SnapshotHash,
		) || sha256.Sum256(snapshot.Payload) != snapshot.SnapshotHash {
		return false
	}
	var payload ieltsAcousticSnapshotPayload
	decoder := json.NewDecoder(bytes.NewReader(snapshot.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || ensureJSONEOF(decoder) != nil {
		return false
	}
	encoded, err := json.Marshal(payload)
	if err != nil || !bytes.Equal(encoded, snapshot.Payload) ||
		payload.SchemaVersion != IELTSAcousticSnapshotSchemaVersion ||
		payload.EvaluationID != snapshot.EvaluationID ||
		payload.OwnerUserID != snapshot.OwnerUserID ||
		payload.InputSnapshotID != snapshot.InputSnapshotID ||
		payload.InputSnapshotHash != hex.EncodeToString(base.SnapshotHash[:]) ||
		payload.Resolution != snapshot.Resolution {
		return false
	}
	requests, err := ieltsSpeakingAcousticRequests(base)
	if err != nil || len(payload.Turns) != len(requests) {
		return false
	}
	assessed := 0
	for index, turn := range payload.Turns {
		request := requests[index]
		if turn.TurnID != request.TurnID ||
			turn.EvidenceRefID != request.EvidenceRefID ||
			turn.EvidenceVersion != request.EvidenceVersion ||
			turn.AudioAssetID != request.AudioAssetID ||
			turn.AudioAssetVersion != request.AudioAssetVersion ||
			turn.AudioChecksumSHA256 != request.AudioChecksumSHA256 ||
			turn.RecordingDurationMS != request.RecordingDurationMS {
			return false
		}
		switch turn.Status {
		case "ASSESSED":
			if request.RecordingDurationMS <= 0 {
				return false
			}
			value := IELTSSpeakingTurnAcoustics{
				TurnID:               turn.TurnID,
				EvidenceRefID:        turn.EvidenceRefID,
				AcousticFluencyScore: turn.AcousticFluencyScore,
				SpeakingSpeedWPM:     turn.SpeakingSpeedWPM,
				Provider:             turn.Provider,
				ProviderRun:          turn.ProviderRunHash,
			}
			if turn.PronunciationScore == nil {
				return false
			}
			value.PronunciationScore = *turn.PronunciationScore
			if turn.UnavailableReason != "" ||
				!validIELTSSpeakingTurnAcoustics(value) ||
				!validIELTSAcousticProviderRunHash(turn.ProviderRunHash) {
				return false
			}
			assessed++
		case "UNAVAILABLE":
			if !slices.Contains(
				[]string{"NO_AUDIO", "NOT_ASSESSED", "DEADLINE_EXCEEDED"},
				turn.UnavailableReason,
			) || (turn.UnavailableReason == "NO_AUDIO") !=
				(request.RecordingDurationMS == 0) ||
				turn.PronunciationScore != nil ||
				turn.AcousticFluencyScore != nil ||
				turn.SpeakingSpeedWPM != nil || turn.Provider != "" ||
				turn.ProviderRunHash != "" {
				return false
			}
		default:
			return false
		}
	}
	expected := IELTSAcousticSnapshotTextOnly
	if assessed == len(payload.Turns) {
		expected = IELTSAcousticSnapshotComplete
	} else if assessed > 0 {
		expected = IELTSAcousticSnapshotPartial
	}
	return snapshot.Resolution == expected
}

func (snapshot IELTSAcousticSnapshot) assessedValues(
	base evidence.EvidenceSnapshot,
) ([]IELTSSpeakingTurnAcoustics, error) {
	if !snapshot.ValidFor(base) {
		return nil, evaluation.ErrInvalidRequest
	}
	var payload ieltsAcousticSnapshotPayload
	if json.Unmarshal(snapshot.Payload, &payload) != nil {
		return nil, evaluation.ErrInvalidRequest
	}
	values := make([]IELTSSpeakingTurnAcoustics, 0, len(payload.Turns))
	for _, turn := range payload.Turns {
		if turn.Status != "ASSESSED" {
			continue
		}
		if turn.PronunciationScore == nil {
			return nil, evaluation.ErrInvalidRequest
		}
		values = append(values, IELTSSpeakingTurnAcoustics{
			TurnID:               turn.TurnID,
			EvidenceRefID:        turn.EvidenceRefID,
			PronunciationScore:   *turn.PronunciationScore,
			AcousticFluencyScore: cloneFloat64(turn.AcousticFluencyScore),
			SpeakingSpeedWPM:     cloneFloat64(turn.SpeakingSpeedWPM),
			Provider:             turn.Provider,
			ProviderRun:          turn.ProviderRunHash,
		})
	}
	return values, nil
}

func IELTSAcousticInputBundleHash(
	base evidence.EvidenceSnapshot,
	acoustics IELTSAcousticSnapshot,
) [sha256.Size]byte {
	if !acoustics.ValidFor(base) {
		return [sha256.Size]byte{}
	}
	hasher := sha256.New()
	_, _ = hasher.Write([]byte("ielts-speaking-input-bundle/v1\x00"))
	_, _ = hasher.Write(base.SnapshotHash[:])
	_, _ = hasher.Write(acoustics.SnapshotHash[:])
	var result [sha256.Size]byte
	copy(result[:], hasher.Sum(nil))
	return result
}

func ieltsSpeakingAcousticRequests(
	base evidence.EvidenceSnapshot,
) ([]IELTSSpeakingAcousticRequest, error) {
	if !base.Valid() || base.SceneType != evaluation.SceneIELTSSpeaking ||
		base.Scope != evaluation.ScopeSession {
		return nil, evaluation.ErrInvalidRequest
	}
	var payload evidence.SnapshotPayload
	decoder := json.NewDecoder(bytes.NewReader(base.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || ensureJSONEOF(decoder) != nil {
		return nil, evaluation.ErrInvalidRequest
	}
	refs := make(map[string]evidence.Ref, len(payload.EvidenceRefs))
	for _, ref := range payload.EvidenceRefs {
		refs[ref.TurnID] = ref
	}
	requests := make([]IELTSSpeakingAcousticRequest, 0, len(payload.ConfirmedTurns))
	for _, turn := range payload.ConfirmedTurns {
		ref, ok := refs[turn.TurnID]
		if !ok {
			return nil, evaluation.ErrInvalidRequest
		}
		request := IELTSSpeakingAcousticRequest{
			TurnID:              turn.TurnID,
			EvidenceRefID:       ref.EvidenceRefID,
			EvidenceVersion:     turn.Transcript.EvidenceVersion,
			AudioAssetID:        turn.Audio.AudioAssetID,
			AudioAssetVersion:   turn.Audio.Version,
			AudioChecksumSHA256: turn.Audio.ChecksumSHA256,
			RecordingDurationMS: turn.Audio.DurationMS,
		}
		if !validIELTSAcousticRequest(request) {
			return nil, evaluation.ErrInvalidRequest
		}
		requests = append(requests, request)
	}
	return requests, nil
}

func validIELTSAcousticRequest(request IELTSSpeakingAcousticRequest) bool {
	if !validIdentifier(request.TurnID) ||
		!validIdentifier(request.EvidenceRefID) || request.EvidenceVersion < 1 ||
		request.RecordingDurationMS < 0 {
		return false
	}
	if request.RecordingDurationMS == 0 {
		return request.AudioAssetID == "" && request.AudioAssetVersion == 0 &&
			request.AudioChecksumSHA256 == ""
	}
	decoded, err := hex.DecodeString(request.AudioChecksumSHA256)
	return validIdentifier(request.AudioAssetID) &&
		request.AudioAssetVersion > 0 && len(decoded) == sha256.Size && err == nil
}

func deriveIELTSAcousticSnapshotID(
	evaluationID string,
	snapshotHash [sha256.Size]byte,
) string {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte("ielts-speaking-acoustic-snapshot-id/v1\x00"))
	_, _ = hasher.Write([]byte(evaluationID))
	_, _ = hasher.Write(snapshotHash[:])
	return "ielts_acoustic_" + hex.EncodeToString(hasher.Sum(nil)[:16])
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func containsStringKey(values map[string]struct{}, key string) bool {
	_, ok := values[key]
	return ok
}

func validIELTSAcousticProviderRunHash(value string) bool {
	if len(value) != len("run_")+24 || value[:len("run_")] != "run_" {
		return false
	}
	decoded, err := hex.DecodeString(value[len("run_"):])
	return err == nil && len(decoded) == 12
}
