// Package interviewresume owns the optional PDF input used to build one
// InterviewPreparation. Raw bytes are temporary Media assets; only extracted
// structured material crosses into the Preparation aggregate.
package interviewresume

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

const (
	temporaryAssetLifetime = time.Hour
	maximumOCRPages        = 5
)

type Parser interface {
	Parse(context.Context, io.Reader) (preparation.ResumeMaterial, error)
	Version() string
}

type URLFallbackParser interface {
	Parser
	ParseURL(context.Context, string) (preparation.ResumeMaterial, error)
	OCRVersion() string
}

type assets interface {
	Upload(context.Context, media.Upload) (media.Asset, error)
	Open(context.Context, string, string) (io.ReadCloser, error)
	SignedGet(context.Context, string, string) (objectstore.SignedGetResult, error)
}

type Service struct {
	assets assets
	parser Parser
	now    func() time.Time
}

func New(assets assets, parser Parser) (*Service, error) {
	if assets == nil || parser == nil || strings.TrimSpace(parser.Version()) == "" {
		return nil, errors.New("preparation: interview Resume dependencies are required")
	}
	return &Service{
		assets: assets,
		parser: parser,
		now:    func() time.Time { return time.Now().UTC() },
	}, nil
}

func (service *Service) Extract(
	ctx context.Context,
	userID string,
	requestID string,
	upload preparation.InterviewResumeUpload,
) (preparation.ResumeMaterial, error) {
	if service == nil || service.assets == nil || service.parser == nil ||
		ctx == nil || ctx.Err() != nil || upload.Body == nil {
		return preparation.ResumeMaterial{}, preparation.ErrInterviewPreparationInvalid
	}
	asset, err := service.assets.Upload(ctx, media.Upload{
		UserID:         userID,
		Kind:           media.KindDocument,
		IdempotencyKey: requestID,
		ContentType:    "application/pdf",
		Body:           upload.Body,
		Size:           upload.Size,
		ChecksumSHA256: upload.ChecksumSHA256,
		ExpiresAt:      service.now().Add(temporaryAssetLifetime),
	})
	if err != nil {
		return preparation.ResumeMaterial{}, errors.Join(
			preparation.ErrInterviewPreparationGeneration,
			err,
		)
	}
	reader, err := service.assets.Open(ctx, userID, asset.ID)
	if err != nil {
		return preparation.ResumeMaterial{}, errors.Join(
			preparation.ErrInterviewPreparationGeneration,
			err,
		)
	}
	material, parseErr := service.parser.Parse(ctx, reader)
	closeErr := reader.Close()
	if parseErr != nil {
		fallback, ok := service.parser.(URLFallbackParser)
		if !ok || failureCode(parseErr) != "pdf_text_unavailable" ||
			failurePageCount(parseErr) > maximumOCRPages {
			return preparation.ResumeMaterial{}, errors.Join(
				preparation.ErrInterviewPreparationGeneration,
				parseErr,
			)
		}
		if closeErr != nil {
			return preparation.ResumeMaterial{}, errors.Join(
				preparation.ErrInterviewPreparationGeneration,
				closeErr,
			)
		}
		signed, signErr := service.assets.SignedGet(ctx, userID, asset.ID)
		if signErr != nil {
			return preparation.ResumeMaterial{}, errors.Join(
				preparation.ErrInterviewPreparationGeneration,
				signErr,
			)
		}
		material, parseErr = fallback.ParseURL(ctx, signed.URL)
	}
	if parseErr != nil || closeErr != nil {
		return preparation.ResumeMaterial{}, errors.Join(
			preparation.ErrInterviewPreparationGeneration,
			parseErr,
			closeErr,
		)
	}
	if !preparation.ValidResumeMaterial(material) {
		return preparation.ResumeMaterial{}, errors.Join(
			preparation.ErrInterviewPreparationGeneration,
			errors.New("interview Resume parser returned invalid material"),
		)
	}
	return preparation.CloneResumeMaterial(material), nil
}

func failureCode(err error) string {
	type coded interface{ FailureCode() string }
	var value coded
	if errors.As(err, &value) {
		return value.FailureCode()
	}
	return ""
}

func failurePageCount(err error) int {
	type counted interface{ PageCount() int }
	var value counted
	if errors.As(err, &value) {
		return value.PageCount()
	}
	return 0
}
