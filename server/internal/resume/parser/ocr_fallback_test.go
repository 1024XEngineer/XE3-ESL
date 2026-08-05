package parser

import (
	"bytes"
	"context"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume/document"
)

func TestOCRFallbackPipelineKeepsPathsAndVersionsSeparate(t *testing.T) {
	native := &documentParserFake{document: document.StructuredDocument{
		Markdown: "native document",
	}}
	ocr := &urlDocumentParserFake{document: document.StructuredDocument{
		Markdown: "recognized scanned document",
	}}
	fields := &fieldExtractorFake{content: resume.Content{TargetPosition: "Backend Engineer"}}
	pipeline, err := NewOCRFallbackPipeline(native, ocr, fields)
	if err != nil {
		t.Fatalf("NewOCRFallbackPipeline: %v", err)
	}
	if _, err := pipeline.Parse(context.Background(), bytes.NewReader(nil)); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if ocr.called {
		t.Fatal("OCR was called on the native path")
	}
	if _, err := pipeline.ParseURL(
		context.Background(),
		"https://private.example/resume.pdf",
	); err != nil {
		t.Fatalf("ParseURL: %v", err)
	}
	if !ocr.called || fields.received.Markdown != "recognized scanned document" ||
		pipeline.Version() != "document-test/v1+fields-test/v1" ||
		pipeline.OCRVersion() != "ocr-document-test/v1+fields-test/v1" {
		t.Fatalf(
			"called=%v received=%#v versions=%q/%q",
			ocr.called,
			fields.received,
			pipeline.Version(),
			pipeline.OCRVersion(),
		)
	}
}

type urlDocumentParserFake struct {
	document document.StructuredDocument
	err      error
	called   bool
}

func (parser *urlDocumentParserFake) ParseURL(
	context.Context,
	string,
) (document.StructuredDocument, error) {
	parser.called = true
	return parser.document, parser.err
}

func (*urlDocumentParserFake) Version() string { return "ocr-document-test/v1" }
