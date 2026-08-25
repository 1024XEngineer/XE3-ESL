package speechfeedback

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
)

func TestCompactAcousticEvaluatorAcceptsOpaqueProviderSession(t *testing.T) {
	score, rejected := 75.2, false
	var providerRequest AcousticAssessmentRequest
	evaluator, err := NewCompactAcousticEvaluator(
		acousticAudioReaderFake{audio: acousticTestWAV()},
		acousticProviderFake{result: AcousticAssessmentResult{
			Provider:  "xfyun-ise",
			SessionID: "ise000da9a8@gz1a0092fc5e55075812",
			Summary: AcousticAssessmentSummary{
				AccuracyScore:  &score,
				FluencyScore:   &score,
				IntegrityScore: &score,
				Rejected:       &rejected,
			},
		}, request: &providerRequest},
	)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := evaluator.EvaluateAcoustic(
		context.Background(),
		evaluation.Record{
			Kind:       evaluation.KindPracticeTurnFeedback,
			LeaseToken: "20000000-0000-4000-8000-000000000001",
		},
		evaluation.SpeechInputSnapshot{
			Transcript:   "I enjoy upbeat music.",
			AudioAssetID: "10000000-0000-4000-8000-000000000001",
		},
	)
	if err != nil {
		t.Fatalf("EvaluateAcoustic() error = %v", err)
	}
	if !checkpoint.Valid() ||
		checkpoint.ProviderSession != "ise000da9a8@gz1a0092fc5e55075812" ||
		providerRequest.RequestID != "20000000-0000-4000-8000-000000000001" {
		t.Fatalf("checkpoint = %#v", checkpoint)
	}
}

func TestCompactAcousticEvaluatorAcceptsWordAssessmentWithoutSentenceDimensions(
	t *testing.T,
) {
	accuracy, rejected := 81.5, false
	evaluator, err := NewCompactAcousticEvaluator(
		acousticAudioReaderFake{audio: acousticTestWAV()},
		acousticProviderFake{result: AcousticAssessmentResult{
			Provider:  "xfyun-ise",
			SessionID: "ise-word-session",
			Summary: AcousticAssessmentSummary{
				AccuracyScore: &accuracy,
				Rejected:      &rejected,
			},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := evaluator.EvaluateAcoustic(
		context.Background(),
		evaluation.Record{
			Kind:       evaluation.KindPracticeTurnFeedback,
			LeaseToken: "20000000-0000-4000-8000-000000000002",
		},
		evaluation.SpeechInputSnapshot{
			Transcript:   "No.",
			AudioAssetID: "10000000-0000-4000-8000-000000000002",
		},
	)
	if err != nil || !checkpoint.Valid() ||
		checkpoint.Pronunciation == nil || *checkpoint.Pronunciation != accuracy ||
		checkpoint.Fluency != nil || checkpoint.Integrity != nil {
		t.Fatalf("checkpoint = %#v, error = %v", checkpoint, err)
	}
}

type acousticAudioReaderFake struct{ audio []byte }

func (reader acousticAudioReaderFake) ReadOwnedAudio(
	context.Context,
	string,
	string,
) ([]byte, error) {
	return reader.audio, nil
}

type acousticProviderFake struct {
	result  AcousticAssessmentResult
	request *AcousticAssessmentRequest
}

func (provider acousticProviderFake) Evaluate(
	_ context.Context,
	request AcousticAssessmentRequest,
) (AcousticAssessmentResult, error) {
	if provider.request != nil {
		*provider.request = request
	}
	return provider.result, nil
}

func acousticTestWAV() []byte {
	pcm := []byte{0, 0, 1, 0}
	wav := make([]byte, 44+len(pcm))
	copy(wav[0:4], "RIFF")
	binary.LittleEndian.PutUint32(wav[4:8], uint32(len(wav)-8))
	copy(wav[8:12], "WAVE")
	copy(wav[12:16], "fmt ")
	binary.LittleEndian.PutUint32(wav[16:20], 16)
	binary.LittleEndian.PutUint16(wav[20:22], 1)
	binary.LittleEndian.PutUint16(wav[22:24], 1)
	binary.LittleEndian.PutUint32(wav[24:28], 16_000)
	binary.LittleEndian.PutUint32(wav[28:32], 32_000)
	binary.LittleEndian.PutUint16(wav[32:34], 2)
	binary.LittleEndian.PutUint16(wav[34:36], 16)
	copy(wav[36:40], "data")
	binary.LittleEndian.PutUint32(wav[40:44], uint32(len(pcm)))
	copy(wav[44:], pcm)
	return wav
}
