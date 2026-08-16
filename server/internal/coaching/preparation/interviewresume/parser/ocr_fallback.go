package parser

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/interviewresume/document"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/interviewresume/fieldextractor"
)

// OCRFallbackPipeline keeps native PDF text extraction as the primary path and
// exposes a separate URL entry point used only by ParseWorker after a textless
// failure.
type OCRFallbackPipeline struct {
	native     *Pipeline
	ocr        document.URLParser
	fields     fieldextractor.Extractor
	ocrVersion string
}

// NewOCRFallbackPipeline creates the two-path Resume parsing pipeline.
func NewOCRFallbackPipeline(
	native document.Parser,
	ocr document.URLParser,
	fields fieldextractor.Extractor,
) (*OCRFallbackPipeline, error) {
	primary, err := NewPipeline(native, fields)
	if err != nil {
		return nil, err
	}
	if ocr == nil || strings.TrimSpace(ocr.Version()) == "" {
		return nil, errors.New("resume OCR document parser is required")
	}
	ocrVersion := ocr.Version() + "+" + fields.Version()
	if len(ocrVersion) > maximumPipelineVersionBytes {
		return nil, errors.New("resume OCR pipeline version is invalid")
	}
	return &OCRFallbackPipeline{
		native:     primary,
		ocr:        ocr,
		fields:     fields,
		ocrVersion: ocrVersion,
	}, nil
}

// Parse executes the unchanged native-text path.
func (pipeline *OCRFallbackPipeline) Parse(
	ctx context.Context,
	reader io.Reader,
) (preparation.ResumeMaterial, error) {
	if pipeline == nil || pipeline.native == nil {
		return preparation.ResumeMaterial{}, errors.New("interview resume OCR pipeline is invalid")
	}
	return pipeline.native.Parse(ctx, reader)
}

// ParseURL executes OCR and then reuses the existing field extractor.
func (pipeline *OCRFallbackPipeline) ParseURL(
	ctx context.Context,
	sourceURL string,
) (preparation.ResumeMaterial, error) {
	if pipeline == nil || pipeline.ocr == nil || pipeline.fields == nil ||
		ctx == nil || ctx.Err() != nil {
		return preparation.ResumeMaterial{}, errors.New("interview resume OCR pipeline input is invalid")
	}
	structured, err := pipeline.ocr.ParseURL(ctx, sourceURL)
	if err != nil {
		return preparation.ResumeMaterial{}, err
	}
	return pipeline.fields.Extract(ctx, structured)
}

// Version preserves the native path's existing audit version.
func (pipeline *OCRFallbackPipeline) Version() string {
	if pipeline == nil || pipeline.native == nil {
		return ""
	}
	return pipeline.native.Version()
}

// OCRVersion returns the version recorded only for OCR-generated content.
func (pipeline *OCRFallbackPipeline) OCRVersion() string {
	if pipeline == nil {
		return ""
	}
	return pipeline.ocrVersion
}
