package interviewresume

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

func TestExtractKeepsOnlyParsedMaterial(t *testing.T) {
	now := time.Date(2026, time.August, 16, 8, 0, 0, 0, time.UTC)
	assets := &assetFake{asset: media.Asset{ID: "asset-1"}}
	parser := &parserFake{material: validMaterial()}
	service, err := New(assets, parser)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	service.now = func() time.Time { return now }
	pdf := []byte("%PDF-1.7")

	material, err := service.Extract(
		context.Background(),
		"user-1",
		"request-1",
		preparation.InterviewResumeUpload{
			Body: bytes.NewReader(pdf), Size: int64(len(pdf)), ChecksumSHA256: "checksum",
		},
	)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if material.TargetPosition != "Backend Engineer" || !parser.called {
		t.Fatalf("material=%#v parser_called=%t", material, parser.called)
	}
	if assets.upload.Kind != media.KindDocument ||
		assets.upload.UserID != "user-1" ||
		assets.upload.IdempotencyKey != "request-1" ||
		!assets.upload.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("upload=%#v", assets.upload)
	}
	if assets.openUserID != "user-1" || assets.openAssetID != "asset-1" {
		t.Fatalf("open user=%q asset=%q", assets.openUserID, assets.openAssetID)
	}
}

type assetFake struct {
	asset       media.Asset
	upload      media.Upload
	openUserID  string
	openAssetID string
}

func (fake *assetFake) Upload(_ context.Context, upload media.Upload) (media.Asset, error) {
	fake.upload = upload
	return fake.asset, nil
}

func (fake *assetFake) Open(_ context.Context, userID, assetID string) (io.ReadCloser, error) {
	fake.openUserID = userID
	fake.openAssetID = assetID
	return io.NopCloser(bytes.NewReader([]byte("document"))), nil
}

func (*assetFake) SignedGet(context.Context, string, string) (objectstore.SignedGetResult, error) {
	return objectstore.SignedGetResult{}, nil
}

type parserFake struct {
	material preparation.ResumeMaterial
	called   bool
}

func (fake *parserFake) Parse(context.Context, io.Reader) (preparation.ResumeMaterial, error) {
	fake.called = true
	return fake.material, nil
}

func (*parserFake) Version() string { return "test/v1" }

func validMaterial() preparation.ResumeMaterial {
	return preparation.ResumeMaterial{
		TargetPosition:       "Backend Engineer",
		WorkExperiences:      []preparation.ResumeWorkExperience{},
		ProjectExperiences:   []preparation.ResumeProjectExperience{},
		EducationExperiences: []preparation.ResumeEducationExperience{},
		Skills:               []string{},
		Awards:               []string{},
	}
}
