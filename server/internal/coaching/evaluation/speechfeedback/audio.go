package speechfeedback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

const maxSpeechFeedbackAudioBytes = 9_600_000

// SpeechFeedbackAudioReadTimeout bounds the protected audio download.
const SpeechFeedbackAudioReadTimeout = 30 * time.Second

// NewSpeechFeedbackAudioHTTPClient owns the bounded read and redirect policy
// used to fetch protected Practice audio for acoustic assessment.
func NewSpeechFeedbackAudioHTTPClient() *http.Client {
	return &http.Client{
		Timeout: SpeechFeedbackAudioReadTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

type speechFeedbackAudioReader struct {
	service *practicevoice.AudioAssetService
	store   objectstore.Store
	client  *http.Client
}

func NewSpeechFeedbackAudioReader(
	service *practicevoice.AudioAssetService,
	store objectstore.Store,
	client *http.Client,
) (SpeechFeedbackAudioReader, error) {
	if service == nil || store == nil || client == nil {
		return nil, ErrInvalidSpeechFeedback
	}
	return &speechFeedbackAudioReader{
		service: service,
		store:   store,
		client:  client,
	}, nil
}

func (reader *speechFeedbackAudioReader) ReadSpeechFeedbackAudio(
	ctx context.Context,
	ownerUserID string,
	audioAssetID string,
	audioObjectKey string,
	expectedChecksum string,
) ([]byte, error) {
	if reader == nil || reader.service == nil ||
		reader.store == nil || reader.client == nil {
		return nil, ErrSpeechFeedbackAcousticUnavailable
	}
	var playback objectstore.SignedGetResult
	var err error
	if audioObjectKey == "" {
		playback, err = reader.service.Playback(
			ctx,
			practicevoice.AudioAssetActor{UserID: ownerUserID},
			audioAssetID,
		)
	} else {
		playback, err = reader.store.SignedGet(ctx, audioObjectKey)
	}
	if err != nil || !playback.ExpiresAt.After(time.Now()) {
		return nil, ErrSpeechFeedbackAcousticUnavailable
	}
	playbackURL, err := url.Parse(playback.URL)
	if err != nil ||
		!strings.EqualFold(playbackURL.Scheme, "https") ||
		playbackURL.Host == "" ||
		playback.ExpiresAt.After(
			time.Now().Add(practicevoice.MaxPlaybackURLTTL),
		) {
		return nil, ErrSpeechFeedbackAcousticUnavailable
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		playback.URL,
		nil,
	)
	if err != nil {
		return nil, ErrSpeechFeedbackAcousticUnavailable
	}
	response, err := reader.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"read SpeechFeedback audio: HTTP %d",
			response.StatusCode,
		)
	}
	audio, err := io.ReadAll(io.LimitReader(
		response.Body,
		maxSpeechFeedbackAudioBytes+1,
	))
	if err != nil {
		return nil, err
	}
	if len(audio) == 0 || len(audio) > maxSpeechFeedbackAudioBytes {
		return nil, ErrSpeechFeedbackAcousticUnavailable
	}
	checksum := sha256.Sum256(audio)
	if hex.EncodeToString(checksum[:]) != expectedChecksum {
		return nil, ErrSpeechFeedbackAcousticUnavailable
	}
	return audio, nil
}

var _ SpeechFeedbackAudioReader = (*speechFeedbackAudioReader)(nil)
