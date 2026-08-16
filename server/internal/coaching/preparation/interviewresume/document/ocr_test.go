package document

import (
	"context"
	"errors"
	"strings"
	"testing"

	resumeocr "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/interviewresume/ocr"
)

func TestOCRPDFParserProducesStructuredDocument(t *testing.T) {
	client := &ocrClientFake{result: resumeocr.Result{Pages: []resumeocr.Page{{
		Number: 1,
		Words: []resumeocr.Word{
			{Text: "Backend Engineer", X: 10, Y: 10},
			{Text: "Go PostgreSQL", X: 100, Y: 20},
		},
	}}}}
	parser, err := NewOCRPDFParser(client)
	if err != nil {
		t.Fatalf("NewOCRPDFParser: %v", err)
	}
	document, err := parser.ParseURL(
		context.Background(),
		"https://private.example/resume.pdf?signature=redacted",
	)
	if err != nil {
		t.Fatalf("ParseURL: %v", err)
	}
	if client.receivedURL == "" || document.ParserVersion != ocrPDFParserVersion ||
		!strings.HasPrefix(document.Markdown, "Backend Engineer") ||
		len(document.Pages) != 1 || len(document.Pages[0].Blocks) != 1 {
		t.Fatalf("document = %#v, URL received = %t", document, client.receivedURL != "")
	}
}

func TestOCRPDFParserRejectsUnsafeURLAndInvalidOutput(t *testing.T) {
	for name, test := range map[string]struct {
		url    string
		result resumeocr.Result
	}{
		"non HTTPS":   {url: "http://private.example/resume.pdf"},
		"unicode URL": {url: "https://private.example/简历.pdf"},
		"empty output": {
			url:    "https://private.example/resume.pdf",
			result: resumeocr.Result{Pages: []resumeocr.Page{{Number: 1}}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			parser, err := NewOCRPDFParser(&ocrClientFake{result: test.result})
			if err != nil {
				t.Fatalf("NewOCRPDFParser: %v", err)
			}
			_, err = parser.ParseURL(context.Background(), test.url)
			var failure interface{ FailureCode() string }
			if !errors.As(err, &failure) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

type ocrClientFake struct {
	result      resumeocr.Result
	err         error
	receivedURL string
}

func (client *ocrClientFake) RecognizePDF(
	_ context.Context,
	sourceURL string,
) (resumeocr.Result, error) {
	client.receivedURL = sourceURL
	return client.result, client.err
}
