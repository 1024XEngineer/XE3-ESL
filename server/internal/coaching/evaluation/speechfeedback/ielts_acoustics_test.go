package speechfeedback

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
)

func TestNewIELTSSpeakingAcousticSourceRejectsNilReader(t *testing.T) {
	if _, err := NewIELTSSpeakingAcousticSource(nil); !errors.Is(
		err,
		evaluation.ErrInvalidRequest,
	) {
		t.Fatalf("NewIELTSSpeakingAcousticSource error = %v", err)
	}
}

func TestIELTSSpeakingAcousticSourceReturnsVersionBoundAssessment(t *testing.T) {
	request := ieltsAcousticRequestFixture()
	accuracy := 74.0
	fluency := 68.0
	reader := &ieltsSpeakingFeedbackReaderStub{
		projections: map[string]ieltsSpeakingAcousticProjection{
			request.TurnID: {
				FeedbackStatus:    SpeechFeedbackReady,
				EvidenceVersion:   request.EvidenceVersion,
				AudioAssetID:      request.AudioAssetID,
				AudioAssetVersion: int64(request.AudioAssetVersion),
				AudioChecksum:     request.AudioChecksumSHA256,
				Assessment: SpeechFeedbackAcousticAssessment{
					Pronunciation:   SpeechFeedbackAssessed,
					AcousticFluency: SpeechFeedbackAssessed,
					AccuracyScore:   &accuracy,
					FluencyScore:    &fluency,
					Provider:        "xfyun_ise",
					ProviderSession: "secret-provider-session",
				},
			},
		},
	}
	source, err := NewIELTSSpeakingAcousticSource(reader)
	if err != nil {
		t.Fatal(err)
	}
	read, err := source.ReadIELTSSpeakingAcoustics(
		context.Background(),
		"10000000-0000-4000-8000-000000000001",
		[]scoring.IELTSSpeakingAcousticRequest{request},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Values) != 1 || len(read.PendingTurnIDs) != 0 ||
		read.Values[0].PronunciationScore != accuracy ||
		read.Values[0].ProviderRun == "secret-provider-session" ||
		!strings.HasPrefix(read.Values[0].ProviderRun, "run_") {
		t.Fatalf("read = %#v", read)
	}
}

func TestIELTSSpeakingAcousticSourceReportsPendingWithoutPartialShortcut(
	t *testing.T,
) {
	request := ieltsAcousticRequestFixture()
	reader := &ieltsSpeakingFeedbackReaderStub{
		projections: map[string]ieltsSpeakingAcousticProjection{
			request.TurnID: {
				FeedbackStatus:    SpeechFeedbackRunning,
				EvidenceVersion:   request.EvidenceVersion,
				AudioAssetID:      request.AudioAssetID,
				AudioAssetVersion: int64(request.AudioAssetVersion),
				AudioChecksum:     request.AudioChecksumSHA256,
			},
		},
	}
	source, err := NewIELTSSpeakingAcousticSource(reader)
	if err != nil {
		t.Fatal(err)
	}
	read, err := source.ReadIELTSSpeakingAcoustics(
		context.Background(),
		"10000000-0000-4000-8000-000000000001",
		[]scoring.IELTSSpeakingAcousticRequest{request},
	)
	if err != nil || len(read.Values) != 0 ||
		len(read.PendingTurnIDs) != 1 ||
		read.PendingTurnIDs[0] != request.TurnID {
		t.Fatalf("read = %#v, err = %v", read, err)
	}
}

func TestIELTSSpeakingAcousticSourceRejectsStaleAudioProjection(t *testing.T) {
	request := ieltsAcousticRequestFixture()
	base := ieltsSpeakingAcousticProjection{
		FeedbackStatus:    SpeechFeedbackReady,
		EvidenceVersion:   request.EvidenceVersion,
		AudioAssetID:      request.AudioAssetID,
		AudioAssetVersion: int64(request.AudioAssetVersion),
		AudioChecksum:     request.AudioChecksumSHA256,
	}
	tests := map[string]func(*ieltsSpeakingAcousticProjection){
		"evidence version": func(value *ieltsSpeakingAcousticProjection) {
			value.EvidenceVersion++
		},
		"audio asset": func(value *ieltsSpeakingAcousticProjection) {
			value.AudioAssetID = "audio-stale"
		},
		"audio version": func(value *ieltsSpeakingAcousticProjection) {
			value.AudioAssetVersion++
		},
		"audio checksum": func(value *ieltsSpeakingAcousticProjection) {
			value.AudioChecksum = strings.Repeat("cd", 32)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			projection := base
			mutate(&projection)
			source, err := NewIELTSSpeakingAcousticSource(
				&ieltsSpeakingFeedbackReaderStub{
					projections: map[string]ieltsSpeakingAcousticProjection{
						request.TurnID: projection,
					},
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			_, err = source.ReadIELTSSpeakingAcoustics(
				context.Background(),
				"10000000-0000-4000-8000-000000000001",
				[]scoring.IELTSSpeakingAcousticRequest{request},
			)
			if !errors.Is(err, scoring.ErrIELTSAcousticEvidenceInvalid) ||
				!errors.Is(err, evaluation.ErrInvalidRequest) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func ieltsAcousticRequestFixture() scoring.IELTSSpeakingAcousticRequest {
	return scoring.IELTSSpeakingAcousticRequest{
		TurnID:              "turn-1",
		EvidenceRefID:       "evidence-1",
		EvidenceVersion:     2,
		AudioAssetID:        "audio-1",
		AudioAssetVersion:   3,
		AudioChecksumSHA256: strings.Repeat("ab", 32),
		RecordingDurationMS: 4_000,
	}
}

type ieltsSpeakingFeedbackReaderStub struct {
	projections map[string]ieltsSpeakingAcousticProjection
}

func (stub *ieltsSpeakingFeedbackReaderStub) ReadIELTSSpeakingAcousticProjection(
	_ context.Context,
	_ string,
	turnID string,
) (ieltsSpeakingAcousticProjection, bool, error) {
	projection, found := stub.projections[turnID]
	return projection, found, nil
}
