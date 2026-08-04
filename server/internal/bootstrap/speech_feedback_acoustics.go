package bootstrap

import (
	"errors"
	"net/http"
	"time"

	speechfeedback "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	practicevoice "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice"
	practicevoicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/providers/xfyun"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewSpeechFeedbackAcousticProvider(
	database *pgxpool.Pool,
	store objectstore.Store,
	configuration config.ISEConfig,
) (speechfeedback.SpeechFeedbackAcousticProvider, error) {
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
	repository, err := practicevoicepostgres.NewAudioAssetRepository(database)
	if err != nil {
		return nil, err
	}
	service, err := practicevoice.NewAudioAssetService(
		repository,
		store,
		practicevoice.SecureAudioAssetIDGenerator{},
		practicevoice.NewAudioAssetSystemClock(),
		repository,
		24*time.Hour,
	)
	if err != nil {
		return nil, err
	}
	reader, err := speechfeedback.NewSpeechFeedbackAudioReader(
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
	return speechfeedback.NewSpeechFeedbackAcousticProvider(reader, evaluator)
}
