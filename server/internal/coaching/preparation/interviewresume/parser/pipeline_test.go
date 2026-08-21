package parser

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/interviewresume/document"
)

func TestPipelineRunsDocumentParserBeforeFieldExtractor(t *testing.T) {
	documents := &documentParserFake{document: document.StructuredDocument{
		Format: "pdf", Markdown: "Backend Engineer",
	}}
	fields := &fieldExtractorFake{content: preparation.ResumeMaterial{
		TargetPosition: "Backend Engineer",
	}}
	pipeline, err := NewPipeline(documents, fields)
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	content, err := pipeline.Parse(
		context.Background(),
		bytes.NewReader([]byte("pdf")),
	)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if content.TargetPosition != "Backend Engineer" ||
		fields.received.Markdown != "Backend Engineer" ||
		pipeline.Version() != "document-test/v1+fields-test/v1" {
		t.Fatalf("content=%#v received=%#v version=%q", content, fields.received, pipeline.Version())
	}
}

func TestPipelineDoesNotCallFieldsAfterDocumentFailure(t *testing.T) {
	documents := &documentParserFake{err: errors.New("document failed")}
	fields := &fieldExtractorFake{}
	pipeline, err := NewPipeline(documents, fields)
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	if _, err := pipeline.Parse(context.Background(), bytes.NewReader(nil)); err == nil {
		t.Fatal("Parse succeeded")
	}
	if fields.called {
		t.Fatal("field extractor called after document failure")
	}
}

type documentParserFake struct {
	document document.StructuredDocument
	err      error
}

func (parser *documentParserFake) Parse(
	context.Context,
	io.Reader,
) (document.StructuredDocument, error) {
	return parser.document, parser.err
}

func (*documentParserFake) Version() string { return "document-test/v1" }

type fieldExtractorFake struct {
	content  preparation.ResumeMaterial
	err      error
	received document.StructuredDocument
	called   bool
}

func (extractor *fieldExtractorFake) Extract(
	_ context.Context,
	document document.StructuredDocument,
) (preparation.ResumeMaterial, error) {
	extractor.called = true
	extractor.received = document
	return extractor.content, extractor.err
}

func (*fieldExtractorFake) Version() string { return "fields-test/v1" }
