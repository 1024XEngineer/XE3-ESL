package main

import (
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

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
