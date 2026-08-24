package main

import (
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
)

func TestAcousticDependencyWaitCoversMaximumAcceptedAudio(t *testing.T) {
	wait := evaluationAcousticDependencyMaxWait(
		true,
		defaultAcousticDependencyMaxWait,
	)
	if wait <= platformmedia.MaxAudioDuration {
		t.Fatalf(
			"acoustic dependency wait = %s, must exceed maximum audio duration %s",
			wait,
			platformmedia.MaxAudioDuration,
		)
	}
}

func TestAcousticDependencyWaitUsesConfiguredProviderAttempt(t *testing.T) {
	providerTimeout := 175 * time.Second
	if got := evaluationAcousticDependencyMaxWait(true, providerTimeout); got != providerTimeout {
		t.Fatalf("acoustic dependency wait = %s, want %s", got, providerTimeout)
	}
	if got := evaluationAcousticDependencyMaxWait(false, 0); got !=
		defaultAcousticDependencyMaxWait {
		t.Fatalf(
			"disabled acoustic dependency wait = %s, want %s",
			got,
			defaultAcousticDependencyMaxWait,
		)
	}
}

func TestAcousticDependencyWaitClampsShortProviderAttempt(t *testing.T) {
	providerTimeout := time.Second
	if got := evaluationAcousticDependencyMaxWait(true, providerTimeout); got !=
		evaluationDependencyDelay {
		t.Fatalf(
			"acoustic dependency wait = %s, want minimum %s",
			got,
			evaluationDependencyDelay,
		)
	}
}

func TestVoiceASRLeaseCoversUploadRecognitionAndFinalization(t *testing.T) {
	for _, test := range []struct {
		name            string
		realtimeTimeout time.Duration
		recordedTimeout time.Duration
		providerTimeout time.Duration
	}{
		{
			name:            "realtime is longer",
			realtimeTimeout: 150 * time.Second,
			recordedTimeout: 75 * time.Second,
			providerTimeout: 150 * time.Second,
		},
		{
			name:            "recorded is longer",
			realtimeTimeout: 150 * time.Second,
			recordedTimeout: 180 * time.Second,
			providerTimeout: 180 * time.Second,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			lease := voiceASRLease(config.SpeechRecognitionConfig{
				Timeout:         test.realtimeTimeout,
				RecordedTimeout: test.recordedTimeout,
			})
			want := voiceAudioUploadLease + test.providerTimeout +
				voiceASRFinalizationTimeMargin
			if lease != want {
				t.Fatalf("voice ASR lease = %s, want %s", lease, want)
			}
		})
	}
}
