package qianwen

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	stdimage "image"
	"image/color"
	"image/png"
	"os"
	"testing"
	"time"

	platformconfig "github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore/ossstore"
	protocol "github.com/1024XEngineer/XE3-ESL/server/internal/providers/qianwen/internal/protocol"
)

func TestLiveMultimodalGenerationWithToolCall(t *testing.T) {
	if os.Getenv("QIANWEN_IMAGE_LIVE_TEST") != "1" {
		t.Skip(
			"set QIANWEN_IMAGE_LIVE_TEST=1 plus Qianwen and OSS " +
				"environment variables; the real request may incur charges",
		)
	}

	textConfig, err := platformconfig.LoadTextGeneration()
	if err != nil {
		t.Fatalf("load text generation config: %v", err)
	}
	storageConfig, err := platformconfig.LoadObjectStorage()
	if err != nil {
		t.Fatalf("load object storage config: %v", err)
	}
	if !storageConfig.Enabled {
		t.Fatal("OSS_ENABLED must be true for the multimodal live test")
	}
	provider, err := ossstore.NewCredentialsProvider(storageConfig)
	if err != nil {
		t.Fatalf("create OSS credentials provider: %v", err)
	}
	store, err := ossstore.NewForPrefix(
		context.Background(),
		storageConfig,
		storageConfig.ImagePrefix,
		provider,
	)
	if err != nil {
		t.Fatalf("create OSS image store: %v", err)
	}
	payload := fixedMultimodalFixture(t)
	sum := sha256.Sum256(payload)
	key := fmt.Sprintf(
		"%s/live-tests/%s.png",
		storageConfig.ImagePrefix,
		liveImageID(t),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			15*time.Second,
		)
		defer cleanupCancel()
		if cleanupErr := store.Delete(cleanupCtx, key); cleanupErr != nil {
			t.Errorf("delete live image: %v", cleanupErr)
		}
	})
	if _, err := store.Put(ctx, objectstore.PutRequest{
		Key:            key,
		Body:           bytes.NewReader(payload),
		Size:           int64(len(payload)),
		ContentType:    "image/png",
		ChecksumSHA256: hex.EncodeToString(sum[:]),
	}); err != nil {
		t.Fatalf("upload live image: %v", err)
	}
	signed, err := store.SignedGet(ctx, key)
	if err != nil {
		t.Fatalf("sign live image: %v", err)
	}

	generator, err := newTextClient(TextConfig{
		BaseURL:         textConfig.BaseURL,
		Model:           textConfig.Model,
		Timeout:         textConfig.Timeout,
		MaxOutputTokens: textConfig.MaxOutputTokens,
	}, textConfig.APIKey.Reveal())
	if err != nil {
		t.Fatalf("create Qianwen generator: %v", err)
	}
	result, err := generator.Generate(ctx, protocol.TextRequest{
		Messages: []protocol.TextMessage{{
			Role: protocol.TextRoleUser,
			ContentParts: []protocol.ContentPart{
				{
					Kind: protocol.ContentPartText,
					Text: "Inspect this fixed test image, then report its colors using the tool.",
				},
				{Kind: protocol.ContentPartImageURL, ImageURL: signed.URL},
			},
		}},
		Tools: []protocol.ToolDefinition{{
			Name:        "report_image_colors",
			Description: "Report the two solid colors visible in the test image.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []any{"colors"},
				"properties": map[string]any{
					"colors": map[string]any{
						"type":     "array",
						"minItems": 2,
						"maxItems": 2,
						"items": map[string]any{
							"type": "string",
							"enum": []any{"red", "blue"},
						},
					},
				},
			},
		}},
		ToolChoice: protocol.ToolChoice{
			Mode: protocol.ToolChoiceSpecific,
			Name: "report_image_colors",
		},
	})
	if err != nil {
		t.Fatalf("live multimodal generation: %v", err)
	}
	if len(result.ToolCalls) != 1 ||
		result.ToolCalls[0].Name != "report_image_colors" {
		t.Fatalf("unexpected tool calls: %#v", result.ToolCalls)
	}
	var arguments struct {
		Colors []string `json:"colors"`
	}
	if err := json.Unmarshal(result.ToolCalls[0].Arguments, &arguments); err != nil {
		t.Fatalf("decode tool arguments: %v", err)
	}
	seen := make(map[string]bool, len(arguments.Colors))
	for _, value := range arguments.Colors {
		seen[value] = true
	}
	if !seen["red"] || !seen["blue"] {
		t.Fatalf("model did not identify fixture colors: %#v", arguments)
	}
}

func fixedMultimodalFixture(t *testing.T) []byte {
	t.Helper()
	value := stdimage.NewRGBA(stdimage.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			if x < 32 {
				value.Set(x, y, color.RGBA{R: 0xff, A: 0xff})
			} else {
				value.Set(x, y, color.RGBA{B: 0xff, A: 0xff})
			}
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, value); err != nil {
		t.Fatalf("encode fixed image: %v", err)
	}
	return output.Bytes()
}

func liveImageID(t *testing.T) string {
	t.Helper()
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		t.Fatalf("generate live image ID: %v", err)
	}
	return hex.EncodeToString(value)
}
