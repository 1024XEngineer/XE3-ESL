package document

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"unicode"

	pdf "github.com/ledongthuc/pdf"
)

const (
	pdfParserVersion = "pdf-native-text/v1"
	maximumPDFBytes  = 10 * 1024 * 1024
	maximumTextBytes = 512 * 1024
)

// Failure 是可由 Resume Worker 安全持久化的文档解析失败。
type Failure struct {
	code string
}

func (failure *Failure) Error() string {
	if failure == nil {
		return "resume document parsing failed"
	}
	return "resume document parsing failed: " + failure.code
}

// FailureCode 返回不包含原始文档内容的稳定失败码。
func (failure *Failure) FailureCode() string {
	if failure == nil || failure.code == "" {
		return "pdf_parse_failed"
	}
	return failure.code
}

// Parser 定义原始文件到统一文档结构的边界。
type Parser interface {
	Parse(context.Context, io.Reader) (StructuredDocument, error)
	Version() string
}

// TextPDFParser 解析带可用文本层的 PDF；扫描件由后续供应商适配器处理。
type TextPDFParser struct{}

// NewTextPDFParser 创建原生文本 PDF 文档解析器。
func NewTextPDFParser() *TextPDFParser {
	return &TextPDFParser{}
}

// Version 返回文档解析实现版本。
func (*TextPDFParser) Version() string {
	return pdfParserVersion
}

// Parse 提取 PDF 文本并投影为最小统一文档结构。
func (parser *TextPDFParser) Parse(
	ctx context.Context,
	reader io.Reader,
) (document StructuredDocument, err error) {
	defer func() {
		if recover() != nil {
			document = StructuredDocument{}
			err = &Failure{code: "pdf_invalid"}
		}
	}()
	if parser == nil || ctx == nil || reader == nil || ctx.Err() != nil {
		return StructuredDocument{}, &Failure{code: "pdf_parse_failed"}
	}
	body, err := io.ReadAll(io.LimitReader(reader, maximumPDFBytes+1))
	if err != nil {
		return StructuredDocument{}, &Failure{code: "pdf_read_failed"}
	}
	if len(body) < 5 || len(body) > maximumPDFBytes ||
		!bytes.Equal(body[:5], []byte("%PDF-")) {
		return StructuredDocument{}, &Failure{code: "pdf_invalid"}
	}
	pdfDocument, err := pdf.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return StructuredDocument{}, &Failure{code: "pdf_invalid"}
	}
	pages, text, err := extractPages(ctx, pdfDocument)
	if err != nil {
		return StructuredDocument{}, err
	}
	if visibleRuneCount(text) < 20 {
		return StructuredDocument{}, &Failure{code: "pdf_text_unavailable"}
	}
	return StructuredDocument{
		Format:        "pdf",
		Markdown:      text,
		ParserVersion: parser.Version(),
		Pages:         pages,
	}, nil
}

func extractPages(
	ctx context.Context,
	pdfDocument *pdf.Reader,
) ([]Page, string, error) {
	if pdfDocument == nil || pdfDocument.NumPage() < 1 {
		return nil, "", &Failure{code: "pdf_invalid"}
	}
	fonts := make(map[string]*pdf.Font)
	pages := make([]Page, 0, pdfDocument.NumPage())
	texts := make([]string, 0, pdfDocument.NumPage())
	totalBytes := 0
	for pageNumber := 1; pageNumber <= pdfDocument.NumPage(); pageNumber++ {
		if ctx.Err() != nil {
			return nil, "", &Failure{code: "pdf_parse_failed"}
		}
		pdfPage := pdfDocument.Page(pageNumber)
		for _, name := range pdfPage.Fonts() {
			if _, exists := fonts[name]; !exists {
				font := pdfPage.Font(name)
				fonts[name] = &font
			}
		}
		raw, err := pdfPage.GetPlainText(fonts)
		if err != nil {
			return nil, "", &Failure{code: "pdf_text_unavailable"}
		}
		text := normalizeText(raw)
		if text == "" {
			pages = append(pages, Page{Number: pageNumber, Blocks: []Block{}})
			continue
		}
		totalBytes += len(text)
		if totalBytes > maximumTextBytes {
			return nil, "", &Failure{code: "pdf_text_unavailable"}
		}
		pages = append(pages, Page{
			Number: pageNumber,
			Blocks: []Block{{
				ID:   "page-" + fmt.Sprint(pageNumber) + "-block-1",
				Type: "text", Text: text, Page: pageNumber, Confidence: 1,
			}},
		})
		texts = append(texts, text)
	}
	return pages, strings.Join(texts, "\n\n"), nil
}

func normalizeText(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	lines := strings.Split(raw, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Map(func(value rune) rune {
			if value == '\t' {
				return ' '
			}
			if unicode.IsControl(value) {
				return -1
			}
			return value
		}, line)
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return strings.Join(cleaned, "\n")
}

func visibleRuneCount(value string) int {
	count := 0
	for _, character := range value {
		if !unicode.IsSpace(character) {
			count++
		}
	}
	return count
}

var _ Parser = (*TextPDFParser)(nil)
