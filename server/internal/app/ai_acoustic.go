package app

import (
	"context"
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	sharedmedia "github.com/1024XEngineer/XE3-ESL/server/internal/media"
	mediapostgres "github.com/1024XEngineer/XE3-ESL/server/internal/media/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/providers/xfyun"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewEvaluationAcousticEvaluator(
	database *pgxpool.Pool,
	store objectstore.Store,
	configuration config.ISEConfig,
) (evaluation.AcousticEvaluator, error) {
	if database == nil || store == nil {
		return nil, errors.New("bootstrap: Evaluation acoustic dependencies are required")
	}
	provider, err := xfyun.NewSpeechFeedbackEvaluator(
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
	repository, err := mediapostgres.New(database)
	if err != nil {
		return nil, err
	}
	audio, err := speechfeedback.NewProductionAudioReader(
		sharedAudioMetadataReader{repository: repository},
		store,
		speechfeedback.NewAudioHTTPClient(),
	)
	if err != nil {
		return nil, err
	}
	return speechfeedback.NewCompactAcousticEvaluator(audio, provider)
}

type ownedMediaRepository interface {
	FindOwned(context.Context, string, string) (sharedmedia.Asset, error)
}

type sharedAudioMetadataReader struct {
	repository ownedMediaRepository
}

func (reader sharedAudioMetadataReader) GetReadableOwnedAudio(
	ctx context.Context,
	userID string,
	audioAssetID string,
) (speechfeedback.AudioMetadata, error) {
	if reader.repository == nil {
		return speechfeedback.AudioMetadata{}, speechfeedback.ErrAcousticUnavailable
	}
	asset, err := reader.repository.FindOwned(ctx, userID, audioAssetID)
	if err != nil || asset.Kind != sharedmedia.KindAudio ||
		asset.Status != sharedmedia.StatusReady || asset.ETag == "" {
		return speechfeedback.AudioMetadata{}, speechfeedback.ErrAcousticUnavailable
	}
	return speechfeedback.AudioMetadata{
		ObjectKey:      asset.ObjectKey,
		Size:           asset.Size,
		ChecksumSHA256: asset.ChecksumSHA256,
	}, nil
}

var _ speechfeedback.AudioMetadataReader = sharedAudioMetadataReader{}
