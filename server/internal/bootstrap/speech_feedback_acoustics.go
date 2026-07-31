package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai/xfyun"
	"github.com/1024XEngineer/XE3-ESL/server/internal/conversation"
	conversationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/conversation/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/review"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxSpeechFeedbackAudioBytes = 9_600_000

func NewSpeechFeedbackAcousticProvider(
	database *pgxpool.Pool,
	store objectstore.Store,
	evaluator *xfyun.Evaluator,
) (review.SpeechFeedbackAcousticProvider, error) {
	if database == nil || store == nil || evaluator == nil {
		return nil, errors.New(
			"bootstrap: SpeechFeedback acoustic dependencies are required",
		)
	}
	repository, err := conversationpostgres.NewAudioAssetRepository(database)
	if err != nil {
		return nil, err
	}
	service, err := conversation.NewAudioAssetService(
		repository,
		store,
		conversation.SecureAudioAssetIDGenerator{},
		conversation.NewAudioAssetSystemClock(),
		repository,
		24*time.Hour,
	)
	if err != nil {
		return nil, err
	}
	return review.NewXFYUNSpeechFeedbackAcousticProvider(
		&speechFeedbackAudioReader{
			service: service,
			client: &http.Client{
				Timeout: 30 * time.Second,
				CheckRedirect: func(
					_ *http.Request,
					_ []*http.Request,
				) error {
					return http.ErrUseLastResponse
				},
			},
		},
		evaluator,
	)
}

type speechFeedbackAudioReader struct {
	service *conversation.AudioAssetService
	client  *http.Client
}

func (reader *speechFeedbackAudioReader) ReadSpeechFeedbackAudio(
	ctx context.Context,
	ownerUserID string,
	audioAssetID string,
	expectedChecksum string,
) ([]byte, error) {
	if reader == nil || reader.service == nil || reader.client == nil {
		return nil, review.ErrSpeechFeedbackAcousticUnavailable
	}
	playback, err := reader.service.Playback(
		ctx,
		conversation.AudioAssetActor{UserID: ownerUserID},
		audioAssetID,
	)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		playback.URL,
		nil,
	)
	if err != nil {
		return nil, review.ErrSpeechFeedbackAcousticUnavailable
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
		return nil, review.ErrSpeechFeedbackAcousticUnavailable
	}
	checksum := sha256.Sum256(audio)
	if hex.EncodeToString(checksum[:]) != expectedChecksum {
		return nil, review.ErrSpeechFeedbackAcousticUnavailable
	}
	return audio, nil
}
