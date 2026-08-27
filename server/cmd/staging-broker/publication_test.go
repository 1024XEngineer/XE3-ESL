package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

type publicationFixture struct {
	payload        []byte
	runtimeReceipt mutationResponse
	metadata       []byte
	apk            []byte
	apkName        string
	candidateRunID int64
}

func newPublicationFixture(t *testing.T, config Config, candidateRunID int64) publicationFixture {
	t.Helper()
	version := "0.1.8"
	apk := []byte("signed-staging-apk\n")
	manifestValue := validManifestMap(candidateRunID, version, 7)
	manifestValue["version_code"] = int64(9)
	manifestValue["staging_apk_sha256"] = sha256Bytes(apk)
	manifestContents := marshalJSON(t, manifestValue)
	runtimeReceipt := executeMutation(t, config, deployRequestJSON(t, manifestContents, candidateRunID, 700, nil))
	apkName := "speakup-v" + version + "-staging-arm64.apk"
	prefix := "downloads/android/candidates/" + strconv.FormatInt(candidateRunID, 10) + "/"
	metadata := marshalJSON(t, map[string]any{
		"candidate_metadata_version": 1,
		"environment":                "staging",
		"version":                    version,
		"version_code":               9,
		"git_sha":                    strings.Repeat("c", 40),
		"candidate_run_id":           candidateRunID,
		"manifest_sha256":            sha256Bytes(manifestContents),
		"file_name":                  apkName,
		"download_path":              "/" + prefix + apkName,
		"size_bytes":                 len(apk),
		"minimum_android_api":        24,
		"abis":                       []string{"arm64-v8a"},
		"apk_sha256":                 sha256Bytes(apk),
		"apk_certificate_sha256":     strings.Repeat("f", 64),
	})
	checksum := []byte(sha256Bytes(apk) + "  " + apkName + "\n")
	bundleFiles := map[string][]byte{
		prefix + apkName:             apk,
		prefix + apkName + ".sha256": checksum,
		prefix + "candidate.json":    metadata,
	}
	entries := make([]map[string]any, 0, len(bundleFiles))
	paths := make([]string, 0, len(bundleFiles))
	for path := range bundleFiles {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		contents := bundleFiles[path]
		entries = append(entries, map[string]any{"path": path, "size_bytes": len(contents), "sha256": sha256Bytes(contents)})
	}
	bundleManifest := marshalJSON(t, map[string]any{
		"bundle_version": 1, "environment": "staging", "version": version,
		"git_sha": strings.Repeat("c", 40), "candidate_run_id": candidateRunID,
		"release_manifest_sha256": sha256Bytes(manifestContents), "files": entries,
	})
	requestContents := marshalJSON(t, map[string]any{
		"protocol_version": 2, "action": "publish", "repository": officialRepository,
		"candidate_run_id": candidateRunID, "deployment_run_id": 800, "deployment_run_attempt": 1,
		"expected_runtime_receipt_sha256": runtimeReceipt.ReceiptSHA256,
		"manifest_sha256":                 sha256Bytes(manifestContents), "bundle_manifest_sha256": sha256Bytes(bundleManifest),
	})
	archiveFiles := map[string][]byte{
		"request.json": requestContents, "release-manifest.json": manifestContents,
		"bundle/bundle-manifest.json": bundleManifest,
	}
	for path, contents := range bundleFiles {
		archiveFiles["bundle/"+path] = contents
	}
	return publicationFixture{
		payload: tarPublicationPayload(t, archiveFiles, nil), runtimeReceipt: runtimeReceipt,
		metadata: metadata, apk: apk, apkName: apkName, candidateRunID: candidateRunID,
	}
}

func tarPublicationPayload(t *testing.T, files map[string][]byte, extraHeaders []*tar.Header) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		contents := files[name]
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(contents)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	for _, header := range extraHeaders {
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := writer.Write(make([]byte, header.Size)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestPublishCandidateIsAtomicAndIdempotent(t *testing.T) {
	runner := &recordingRunner{}
	config := newTestConfig(t, runner)
	fixture := newPublicationFixture(t, config, 124)

	result, err := execute(bytes.NewReader(fixture.payload), config)
	if err != nil {
		t.Fatalf("publish candidate: %v", err)
	}
	response, ok := result.(publicationResponse)
	if !ok || !response.OK || response.ProtocolVersion != 2 || response.RuntimeReceiptSHA256 != fixture.runtimeReceipt.ReceiptSHA256 {
		t.Fatalf("publish response = %#v", result)
	}
	candidateRoot := filepath.Join(config.PublicRoot, "downloads", "android", "candidates", strconv.FormatInt(fixture.candidateRunID, 10))
	if !publicContentsMatch(filepath.Join(candidateRoot, "candidate.json"), fixture.metadata) ||
		!publicContentsMatch(filepath.Join(config.PublicRoot, "downloads", "android", "staging-candidate.json"), fixture.metadata) ||
		!publicFileMatches(filepath.Join(candidateRoot, fixture.apkName), int64(len(fixture.apk)), sha256Bytes(fixture.apk)) {
		t.Fatal("published candidate files do not match the validated bundle")
	}
	if _, err := os.Stat(filepath.Join(config.StateDir, "publication-pending.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("publication pending remains: %v", err)
	}

	retried, err := execute(bytes.NewReader(fixture.payload), config)
	if err != nil {
		t.Fatalf("idempotent publish: %v", err)
	}
	retryResponse := retried.(publicationResponse)
	if retryResponse.PublicationReceiptSHA256 != response.PublicationReceiptSHA256 {
		t.Fatalf("retry receipt = %s, want %s", retryResponse.PublicationReceiptSHA256, response.PublicationReceiptSHA256)
	}
	entries, err := os.ReadDir(filepath.Join(config.StateDir, "publication-receipts"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("publication receipts = %v, err=%v", entries, err)
	}
}

func TestPublishCandidateRecoversAfterPointerActivation(t *testing.T) {
	runner := &recordingRunner{}
	config := newTestConfig(t, runner)
	fixture := newPublicationFixture(t, config, 124)
	config.AfterPublish = func() error { return errors.New("simulated interruption") }

	_, err := execute(bytes.NewReader(fixture.payload), config)
	if errorCode(err) != "operation_interrupted" {
		t.Fatalf("interrupted publish error = %v", err)
	}
	if !publicContentsMatch(filepath.Join(config.PublicRoot, "downloads", "android", "staging-candidate.json"), fixture.metadata) {
		t.Fatal("pointer was not atomically activated before interruption")
	}
	if _, err := os.Stat(filepath.Join(config.StateDir, "publication-pending.json")); err != nil {
		t.Fatalf("pending journal missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(config.StateDir, "publication-current")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("publication current unexpectedly committed: %v", err)
	}
	secondManifest := marshalJSON(t, validManifestMap(125, "0.1.9", 7))
	_, err = execute(bytes.NewReader(deployRequestJSON(t, secondManifest, 125, 701, fixture.runtimeReceipt.ReceiptSHA256)), config)
	if errorCode(err) != "recovery_required" || len(runner.snapshot()) != 1 {
		t.Fatalf("runtime mutation crossed publication pending: err=%v calls=%d", err, len(runner.snapshot()))
	}

	config.AfterPublish = nil
	result, err := execute(bytes.NewReader(fixture.payload), config)
	if err != nil || !result.(publicationResponse).OK {
		t.Fatalf("publish recovery result=%#v err=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(config.StateDir, "publication-pending.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovery did not clear pending: %v", err)
	}
}

func TestPublishUploadUsesDedicatedTimeout(t *testing.T) {
	runner := &recordingRunner{}
	config := newTestConfig(t, runner)
	config.RequestTimeout = time.Second
	config.PublishTimeout = 75 * time.Millisecond
	reader, writer := io.Pipe()
	defer writer.Close()
	go func() {
		_, _ = writer.Write([]byte("request.json"))
	}()
	started := time.Now()
	_, err := execute(reader, config)
	if errorCode(err) != "operation_timeout" {
		t.Fatalf("stalled publish error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("stalled publish returned after %s", elapsed)
	}
}

func TestPublishArchiveRejectsUnsafeAndExtraEntries(t *testing.T) {
	incoming := filepath.Join(t.TempDir(), "incoming")
	if err := os.Mkdir(incoming, 0o700); err != nil {
		t.Fatal(err)
	}
	base := map[string][]byte{
		"request.json": {1}, "release-manifest.json": {1}, "bundle/bundle-manifest.json": {1},
		"bundle/downloads/android/candidates/124/speakup-v0.1.8-staging-arm64.apk":        {1},
		"bundle/downloads/android/candidates/124/speakup-v0.1.8-staging-arm64.apk.sha256": {1},
		"bundle/downloads/android/candidates/124/candidate.json":                          {1},
	}
	tests := map[string][]byte{
		"extra": tarPublicationPayload(t, func() map[string][]byte {
			copy := make(map[string][]byte, len(base)+1)
			for name, contents := range base {
				copy[name] = contents
			}
			copy["bundle/downloads/android/candidates/124/extra.json"] = []byte{1}
			return copy
		}(), nil),
		"traversal": tarPublicationPayload(t, base, []*tar.Header{{Name: "../escape", Mode: 0o600, Size: 1, Typeflag: tar.TypeReg}}),
		"link":      tarPublicationPayload(t, base, []*tar.Header{{Name: "link", Linkname: "/etc/passwd", Typeflag: tar.TypeSymlink}}),
	}
	for name, archive := range tests {
		t.Run(name, func(t *testing.T) {
			payload, err := readPublicationPayload(bytes.NewReader(archive), incoming)
			payloadRoot := payload.root
			if err == nil {
				payloadRoot = payload.root
				_ = os.RemoveAll(payload.root)
				t.Fatal("unsafe archive unexpectedly accepted")
			}
			if payloadRoot != "" {
				t.Fatalf("failed archive leaked payload root %q", payloadRoot)
			}
		})
	}
}

func TestPublishRejectsAPKHashMismatchBeforePublicMutation(t *testing.T) {
	runner := &recordingRunner{}
	config := newTestConfig(t, runner)
	fixture := newPublicationFixture(t, config, 124)

	reader := tar.NewReader(bytes.NewReader(fixture.payload))
	files := make(map[string][]byte)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		contents, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		files[header.Name] = contents
	}
	for name := range files {
		if strings.HasSuffix(name, ".apk") {
			files[name] = []byte("tampered")
		}
	}
	tampered := tarPublicationPayload(t, files, nil)
	_, err := execute(bytes.NewReader(tampered), config)
	if errorCode(err) != "invalid_bundle" {
		t.Fatalf("tampered APK error = %v", err)
	}
	if _, err := os.Stat(config.PublicRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid bundle mutated public root: %v", err)
	}
}

func TestPublishRequestIsStrictProtocolV2(t *testing.T) {
	valid := map[string]any{
		"protocol_version": 2, "action": "publish", "repository": officialRepository,
		"candidate_run_id": 124, "deployment_run_id": 800, "deployment_run_attempt": 1,
		"expected_runtime_receipt_sha256": strings.Repeat("a", 64),
		"manifest_sha256":                 strings.Repeat("b", 64), "bundle_manifest_sha256": strings.Repeat("c", 64),
	}
	contents, _ := json.Marshal(valid)
	if _, err := parsePublishRequest(contents, officialRepository); err != nil {
		t.Fatalf("valid publish request: %v", err)
	}
	valid["extra"] = true
	contents, _ = json.Marshal(valid)
	if _, err := parsePublishRequest(contents, officialRepository); err == nil {
		t.Fatal("unknown publish field unexpectedly accepted")
	}
}
