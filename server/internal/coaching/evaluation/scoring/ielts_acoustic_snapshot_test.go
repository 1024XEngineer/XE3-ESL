package scoring

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
)

func TestBuildIELTSAcousticSnapshotFreezesStableInputBundle(t *testing.T) {
	t.Parallel()
	base := ieltsSpeakingAcousticTestSnapshot(t)
	requests, err := ieltsSpeakingAcousticRequests(base)
	if err != nil {
		t.Fatal(err)
	}
	fluency := 76.0
	read := IELTSSpeakingAcousticRead{
		Values: []IELTSSpeakingTurnAcoustics{{
			TurnID:               requests[0].TurnID,
			EvidenceRefID:        requests[0].EvidenceRefID,
			PronunciationScore:   72,
			AcousticFluencyScore: &fluency,
			Provider:             "xfyun_ise",
			ProviderRun:          "run_0123456789abcdef01234567",
		}},
	}
	first, err := BuildIELTSAcousticSnapshot(
		"71000000-0000-4000-8000-000000000007",
		base,
		read,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildIELTSAcousticSnapshot(
		"71000000-0000-4000-8000-000000000007",
		base,
		read,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID ||
		first.SnapshotHash != second.SnapshotHash ||
		!bytes.Equal(first.Payload, second.Payload) ||
		first.Resolution != IELTSAcousticSnapshotPartial ||
		!first.ValidFor(base) {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	firstBundle := IELTSAcousticInputBundleHash(base, first)
	secondBundle := IELTSAcousticInputBundleHash(base, second)
	if firstBundle != secondBundle || firstBundle == ([32]byte{}) {
		t.Fatalf("bundle hashes differ: %x %x", firstBundle, secondBundle)
	}
}

func TestBuildIELTSAcousticSnapshotWaitsBeforeDeadline(t *testing.T) {
	t.Parallel()
	base := ieltsSpeakingAcousticTestSnapshot(t)
	requests, err := ieltsSpeakingAcousticRequests(base)
	if err != nil {
		t.Fatal(err)
	}
	_, err = BuildIELTSAcousticSnapshot(
		"71000000-0000-4000-8000-000000000007",
		base,
		IELTSSpeakingAcousticRead{
			PendingTurnIDs: []string{requests[0].TurnID},
		},
		false,
	)
	if err != ErrIELTSAcousticSnapshotPending {
		t.Fatalf("pending error = %v", err)
	}
}

func TestBuildIELTSAcousticSnapshotHashChangesWithEvidence(t *testing.T) {
	t.Parallel()
	base := ieltsSpeakingAcousticTestSnapshot(t)
	requests, err := ieltsSpeakingAcousticRequests(base)
	if err != nil {
		t.Fatal(err)
	}
	fluency := 76.0
	read := IELTSSpeakingAcousticRead{Values: []IELTSSpeakingTurnAcoustics{{
		TurnID:               requests[0].TurnID,
		EvidenceRefID:        requests[0].EvidenceRefID,
		PronunciationScore:   72,
		AcousticFluencyScore: &fluency,
		Provider:             "xfyun_ise",
		ProviderRun:          "run_0123456789abcdef01234567",
	}}}
	first, err := BuildIELTSAcousticSnapshot(
		"71000000-0000-4000-8000-000000000007",
		base,
		read,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	read.Values[0].PronunciationScore = 73
	second, err := BuildIELTSAcousticSnapshot(
		"71000000-0000-4000-8000-000000000007",
		base,
		read,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if first.SnapshotHash == second.SnapshotHash || first.ID == second.ID {
		t.Fatalf("evidence change kept hash: %#v %#v", first, second)
	}
}

func ieltsSpeakingAcousticTestSnapshot(t *testing.T) evidence.EvidenceSnapshot {
	t.Helper()
	base := ieltsSpeakingTestSnapshot(t, ieltsTestQuestionCount)
	var payload evidence.SnapshotPayload
	if err := json.Unmarshal(base.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	for index := range payload.ConfirmedTurns {
		turn := &payload.ConfirmedTurns[index]
		turn.Audio = evidence.Audio{
			Availability: "AVAILABLE",
			AudioAssetID: "audio-" + turn.TurnID,
			ChecksumSHA256: strings.Repeat(
				string("abcdef0123456789"[index%16]),
				64,
			),
			DurationMS:  4_000,
			ContentType: "audio/wav",
			SizeBytes:   64_000,
			Status:      "readable",
			Version:     1,
			Quality:     evidenceNotAssessed,
			ISE:         evidenceNotAssessed,
		}
		payload.EvidenceRefs[index].AudioSpan = &evidence.AudioSpan{
			AudioAssetID: turn.Audio.AudioAssetID,
			StartMS:      0,
			EndMS:        turn.Audio.DurationMS,
		}
		payload.EvidenceRefs[index].Lineage.AudioAssetVersion =
			turn.Audio.Version
		payload.VersionManifest.TurnEvidence[index].AudioVersion =
			turn.Audio.Version
	}
	return rebuildIELTSSpeakingSnapshot(t, payload)
}

func ieltsAcousticSnapshotForTest(
	t *testing.T,
	base evidence.EvidenceSnapshot,
	limit int,
) IELTSAcousticSnapshot {
	t.Helper()
	requests, err := ieltsSpeakingAcousticRequests(base)
	if err != nil {
		t.Fatal(err)
	}
	if limit <= 0 || limit > len(requests) {
		limit = len(requests)
	}
	read := IELTSSpeakingAcousticRead{
		Values: make([]IELTSSpeakingTurnAcoustics, 0, limit),
	}
	for _, request := range requests[:limit] {
		fluency := 76.0
		read.Values = append(read.Values, IELTSSpeakingTurnAcoustics{
			TurnID:               request.TurnID,
			EvidenceRefID:        request.EvidenceRefID,
			PronunciationScore:   72,
			AcousticFluencyScore: &fluency,
			Provider:             "xfyun_ise",
			ProviderRun:          "run_0123456789abcdef01234567",
		})
	}
	result, err := BuildIELTSAcousticSnapshot(
		testEvalID,
		base,
		read,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
