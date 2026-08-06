package paddleocr

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore/ossstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume/document"
)

func TestLiveRecognizePDF(t *testing.T) {
	if os.Getenv("RESUME_OCR_LIVE_TEST") != "1" {
		t.Skip("set RESUME_OCR_LIVE_TEST=1 to run the PaddleOCR hosted API test")
	}
	path := os.Getenv("RESUME_OCR_LIVE_TEST_PDF")
	if path == "" {
		t.Fatal("RESUME_OCR_LIVE_TEST_PDF is required")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read live PDF: %v", err)
	}
	if len(body) < 5 || len(body) > 10*1024*1024 ||
		!bytes.Equal(body[:5], []byte("%PDF-")) {
		t.Fatal("live fixture must be a PDF no larger than 10 MiB")
	}
	storageConfiguration, err := config.LoadObjectStorage()
	if err != nil || !storageConfiguration.Enabled {
		t.Fatalf("load enabled object storage: %v", err)
	}
	ocrConfiguration, err := config.LoadResumeOCR()
	if err != nil || !ocrConfiguration.Enabled {
		t.Fatalf("load enabled Resume OCR: %v", err)
	}
	provider, err := ossstore.NewCredentialsProvider(storageConfiguration)
	if err != nil {
		t.Fatalf("create credentials provider: %v", err)
	}
	store, err := ossstore.NewForPrefix(
		context.Background(),
		storageConfiguration,
		storageConfiguration.ResumePrefix,
		provider,
	)
	if err != nil {
		t.Fatalf("create Resume object store: %v", err)
	}
	key := fmt.Sprintf(
		"%s/live-tests/%s.pdf",
		storageConfiguration.ResumePrefix,
		randomLiveID(t),
	)
	ctx, cancel := context.WithTimeout(context.Background(), ocrConfiguration.Timeout)
	defer cancel()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if cleanupErr := store.Delete(cleanupCtx, key); cleanupErr != nil {
			t.Errorf("delete live OCR object: %v", cleanupErr)
		}
	})
	sum := sha256.Sum256(body)
	if _, err := store.Put(ctx, objectstore.PutRequest{
		Key:            key,
		Body:           bytes.NewReader(body),
		Size:           int64(len(body)),
		ContentType:    "application/pdf",
		ChecksumSHA256: hex.EncodeToString(sum[:]),
	}); err != nil {
		t.Fatalf("upload live OCR object: %v", err)
	}
	signed, err := store.SignedGet(ctx, key)
	if err != nil {
		t.Fatalf("sign live OCR object: %v", err)
	}
	client, err := New(Config{
		AccessToken: ocrConfiguration.AccessToken,
		BaseURL:     ocrConfiguration.BaseURL,
		Model:       ocrConfiguration.Model,
		Timeout:     ocrConfiguration.Timeout,
	})
	if err != nil {
		t.Fatalf("create PaddleOCR client: %v", err)
	}
	parser, err := document.NewOCRPDFParser(client)
	if err != nil {
		t.Fatalf("create OCR document parser: %v", err)
	}
	result, err := parser.ParseURL(ctx, signed.URL)
	if err != nil {
		t.Fatalf("PaddleOCR live call: %v", err)
	}
	if result.Markdown == "" || len(result.Pages) == 0 {
		t.Fatal("PaddleOCR returned no usable text")
	}
	t.Logf(
		"recognized_pages=%d recognized_characters=%d",
		len(result.Pages),
		len([]rune(result.Markdown)),
	)
}

func randomLiveID(t *testing.T) string {
	t.Helper()
	body := make([]byte, 16)
	if _, err := rand.Read(body); err != nil {
		t.Fatalf("generate live object ID: %v", err)
	}
	return hex.EncodeToString(body)
}
