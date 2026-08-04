package bootstrap

import (
	"errors"
	"net/http"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	practiceinput "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/input/voice"
	conversationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/providers/xfyun"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewSpeechFeedbackAcousticProvider(
	database *pgxpool.Pool,
	store objectstore.Store,
	configuration config.ISEConfig,
) (evaluation.SpeechFeedbackAcousticProvider, error) {
	if database == nil || store == nil {
		return nil, errors.New(
			"bootstrap: SpeechFeedback acoustic dependencies are required",
		)
	}
	evaluator, err := xfyun.NewSpeechFeedbackEvaluator(
		xfyun.ISEConfig{
			Endpoint: configuration.Endpoint,
			Timeout:  configuration.Timeout,
		},
		configuration.AppID.Reveal(),
		configuration.APIKey.Reveal(),
		configuration.APISecret.Reveal(),
	)
	if err != nil {
		return nil, err
	}
	repository, err := conversationpostgres.NewAudioAssetRepository(database)
	if err != nil {
		return nil, err
	}
	service, err := practiceinput.NewAudioAssetService(
		repository,
		store,
		practiceinput.SecureAudioAssetIDGenerator{},
		practiceinput.NewAudioAssetSystemClock(),
		repository,
		24*time.Hour,
	)
	if err != nil {
		return nil, err
	}
	reader, err := evaluation.NewSpeechFeedbackAudioReader(
		service,
		store,
		&http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(
				_ *http.Request,
				_ []*http.Request,
			) error {
				return http.ErrUseLastResponse
			},
		},
	)
	if err != nil {
		return nil, err
	}
	return evaluation.NewSpeechFeedbackAcousticProvider(reader, evaluator)
}
