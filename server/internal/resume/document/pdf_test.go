package document

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestTextPDFParserProducesStructuredDocument(t *testing.T) {
	body := testPDF(strings.Join([]string{
		"Backend Engineer",
		"Built reliable APIs with Go and PostgreSQL",
	}, "\n"))
	document, err := NewTextPDFParser().Parse(
		context.Background(),
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if document.Format != "pdf" ||
		document.ParserVersion != "pdf-native-text/v1" ||
		!strings.Contains(document.Markdown, "PostgreSQL") ||
		len(document.Pages) != 1 || len(document.Pages[0].Blocks) != 1 {
		t.Fatalf("document = %#v", document)
	}
}

func TestTextPDFParserRejectsInvalidAndTextlessFiles(t *testing.T) {
	for name, test := range map[string]struct {
		body []byte
		code string
	}{
		"invalid":       {body: []byte("not-pdf"), code: "pdf_invalid"},
		"textless scan": {body: testScannedPDF(), code: "pdf_text_unavailable"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewTextPDFParser().Parse(
				context.Background(),
				bytes.NewReader(test.body),
			)
			failure, ok := err.(interface{ FailureCode() string })
			if !ok || failure.FailureCode() != test.code {
				t.Fatalf("error = %v, want code %q", err, test.code)
			}
		})
	}
}

func testScannedPDF() []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Im0 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /ASCIIHexDecode /Length 7 >>\nstream\nFFFFFF>\nendstream",
		"<< /Length 31 >>\nstream\nq 612 0 0 792 0 0 cm /Im0 Do Q\nendstream",
	}
	var buffer bytes.Buffer
	buffer.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for index, object := range objects {
		offsets = append(offsets, buffer.Len())
		fmt.Fprintf(&buffer, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := buffer.Len()
	fmt.Fprintf(&buffer, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&buffer, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(
		&buffer,
		"trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objects)+1,
		xref,
	)
	return buffer.Bytes()
}

func testPDF(text string) []byte {
	commands := "BT /F1 10 Tf 14 TL 72 760 Td "
	for index, line := range strings.Split(text, "\n") {
		if index > 0 {
			commands += "T* "
		}
		line = strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)").Replace(line)
		commands += "(" + line + ") Tj "
	}
	commands += "ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(commands), commands),
	}
	var buffer bytes.Buffer
	buffer.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for index, object := range objects {
		offsets = append(offsets, buffer.Len())
		fmt.Fprintf(&buffer, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := buffer.Len()
	fmt.Fprintf(&buffer, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&buffer, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&buffer, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buffer.Bytes()
}
