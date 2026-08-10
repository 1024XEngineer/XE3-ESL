package kodostore

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

func TestLiveKodoObjectLifecycle(t *testing.T) {
	if os.Getenv("KODO_LIVE_TEST") != "1" {
		t.Skip("set KODO_LIVE_TEST=1 for an explicit real Kodo lifecycle test")
	}
	storageConfig, err := config.LoadObjectStorage()
	if err != nil {
		t.Fatalf("load object storage config: %v", err)
	}
	client, err := New(context.Background(), storageConfig)
	if err != nil {
		t.Fatalf("create Kodo client: %v", err)
	}
	payload := minimalWAV()
	digest := sha256.Sum256(payload)
	key := fmt.Sprintf(
		"%s/live-tests/%s.wav",
		storageConfig.AudioPrefix,
		randomID(t),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cleanupNeeded := true
	t.Cleanup(func() {
		if !cleanupNeeded {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if cleanupErr := client.Delete(cleanupCtx, key); cleanupErr != nil {
			t.Errorf("cleanup live object: %v", cleanupErr)
		}
	})

	if _, err := client.Put(ctx, objectstore.PutRequest{
		Key: key, Body: bytes.NewReader(payload), Size: int64(len(payload)),
		ContentType: "audio/wav", ChecksumSHA256: hex.EncodeToString(digest[:]),
	}); err != nil {
		t.Fatalf("put live object: %v", err)
	}
	signed, err := client.SignedGet(ctx, key)
	if err != nil {
		t.Fatalf("sign live object download: %v", err)
	}
	downloadClient := &http.Client{Timeout: 15 * time.Second}
	unsignedURL, err := withoutSignature(signed.URL)
	if err != nil {
		t.Fatal(err)
	}
	unsignedResponse, err := downloadClient.Get(unsignedURL)
	if err != nil {
		t.Fatalf("verify private live object: %v", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(unsignedResponse.Body, 4096))
	_ = unsignedResponse.Body.Close()
	if unsignedResponse.StatusCode != http.StatusForbidden &&
		unsignedResponse.StatusCode != http.StatusUnauthorized &&
		unsignedResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("anonymous object status = %d", unsignedResponse.StatusCode)
	}

	response, err := downloadClient.Get(signed.URL)
	if err != nil {
		t.Fatalf("download live object: %v", err)
	}
	downloaded, readErr := io.ReadAll(io.LimitReader(response.Body, int64(len(payload)+1)))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK ||
		!bytes.Equal(downloaded, payload) {
		t.Fatalf(
			"downloaded live object mismatch: status=%d size=%d read=%v close=%v",
			response.StatusCode,
			len(downloaded),
			readErr,
			closeErr,
		)
	}
	if err := client.Delete(ctx, key); err != nil {
		t.Fatalf("delete live object: %v", err)
	}
	cleanupNeeded = false
}

func withoutSignature(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String(), nil
}

func randomID(t *testing.T) string {
	t.Helper()
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		t.Fatalf("generate live object id: %v", err)
	}
	return hex.EncodeToString(value)
}

func minimalWAV() []byte {
	return []byte{
		'R', 'I', 'F', 'F', 36, 0, 0, 0,
		'W', 'A', 'V', 'E',
		'f', 'm', 't', ' ', 16, 0, 0, 0,
		1, 0, 1, 0,
		0x80, 0x3e, 0, 0,
		0, 0x7d, 0, 0,
		2, 0, 16, 0,
		'd', 'a', 't', 'a', 0, 0, 0, 0,
	}
}
