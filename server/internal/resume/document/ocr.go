package document

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode"

	resumeocr "github.com/1024XEngineer/XE3-ESL/server/internal/resume/ocr"
)

const ocrPDFParserVersion = "paddleocr-vl-1.6/v1"

// URLParser parses a remotely readable private document URL.
type URLParser interface {
	ParseURL(context.Context, string) (StructuredDocument, error)
	Version() string
}

// OCRPDFParser maps supplier-neutral OCR results into StructuredDocument.
type OCRPDFParser struct {
	client resumeocr.Client
}

// NewOCRPDFParser creates the OCR document adapter.
func NewOCRPDFParser(client resumeocr.Client) (*OCRPDFParser, error) {
	if client == nil {
		return nil, errors.New("resume OCR client is required")
	}
	return &OCRPDFParser{client: client}, nil
}

// Version returns the auditable OCR adapter version.
func (*OCRPDFParser) Version() string { return ocrPDFParserVersion }

// ParseURL recognizes one short-lived private PDF URL.
func (parser *OCRPDFParser) ParseURL(
	ctx context.Context,
	sourceURL string,
) (StructuredDocument, error) {
	if parser == nil || parser.client == nil || ctx == nil || ctx.Err() != nil ||
		!validOCRSourceURL(sourceURL) {
		return StructuredDocument{}, resumeocr.NewFailure(resumeocr.FailureProvider)
	}
	result, err := parser.client.RecognizePDF(ctx, sourceURL)
	if err != nil {
		return StructuredDocument{}, err
	}
	pages, markdown, err := normalizeOCRResult(result)
	if err != nil {
		return StructuredDocument{}, err
	}
	return StructuredDocument{
		Format:        "pdf",
		Markdown:      markdown,
		ParserVersion: parser.Version(),
		Pages:         pages,
	}, nil
}

func validOCRSourceURL(raw string) bool {
	if raw == "" || strings.IndexFunc(raw, func(character rune) bool {
		return character > unicode.MaxASCII
	}) >= 0 {
		return false
	}
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" &&
		parsed.User == nil && parsed.Fragment == ""
}

func normalizeOCRResult(result resumeocr.Result) ([]Page, string, error) {
	if len(result.Pages) < 1 {
		return nil, "", resumeocr.NewFailure(resumeocr.FailureOutputInvalid)
	}
	pageCount := result.PageCount
	if pageCount == 0 {
		pageCount = len(result.Pages)
	}
	if pageCount < 1 || pageCount > resumeocr.MaximumRecognizedPages ||
		len(result.Pages) > resumeocr.MaximumRecognizedPages {
		return nil, "", resumeocr.NewFailure(resumeocr.FailurePageLimit)
	}
	providerPages := append([]resumeocr.Page(nil), result.Pages...)
	sort.SliceStable(providerPages, func(left, right int) bool {
		return providerPages[left].Number < providerPages[right].Number
	})
	pages := make([]Page, 0, len(providerPages))
	pageTexts := make([]string, 0, len(providerPages))
	seen := make(map[int]struct{}, len(providerPages))
	for _, providerPage := range providerPages {
		if providerPage.Number < 1 ||
			providerPage.Number > resumeocr.MaximumRecognizedPages {
			return nil, "", resumeocr.NewFailure(resumeocr.FailurePageLimit)
		}
		if _, duplicate := seen[providerPage.Number]; duplicate {
			return nil, "", resumeocr.NewFailure(resumeocr.FailureOutputInvalid)
		}
		seen[providerPage.Number] = struct{}{}
		words := providerPage.Words
		texts := make([]string, 0, len(words))
		for _, word := range words {
			text := normalizeText(word.Text)
			if text != "" {
				texts = append(texts, text)
			}
		}
		pageText := strings.Join(texts, "\n")
		if pageText == "" {
			continue
		}
		pageTexts = append(pageTexts, pageText)
		pages = append(pages, Page{
			Number: providerPage.Number,
			Blocks: []Block{{
				ID:         "page-" + strconv.Itoa(providerPage.Number) + "-ocr-1",
				Type:       "text",
				Text:       pageText,
				Page:       providerPage.Number,
				Confidence: 1,
			}},
		})
	}
	markdown := strings.Join(pageTexts, "\n\n")
	if len(markdown) > maximumTextBytes || visibleRuneCount(markdown) < 20 {
		return nil, "", resumeocr.NewFailure(resumeocr.FailureOutputInvalid)
	}
	return pages, markdown, nil
}

var _ URLParser = (*OCRPDFParser)(nil)
