package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type recordingRunner struct {
	calls           [][]string
	releaseMetadata []byte
}

func (runner *recordingRunner) run(_ context.Context, _ string, arguments []string, _ []string) error {
	runner.calls = append(runner.calls, append([]string(nil), arguments...))
	if len(arguments) == 0 {
		return nil
	}
	switch arguments[0] {
	case "verify":
		return nil
	case "activate":
		root := argumentValue(arguments, "--root")
		metadataPath := filepath.Join(root, "downloads", "android", "release.json")
		if err := os.MkdirAll(filepath.Dir(metadataPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(metadataPath, runner.releaseMetadata, 0o644)
	}
	manifestPath := argumentValue(arguments, "--manifest")
	currentPath := argumentValue(arguments, "--current-receipt")
	receiptPath := argumentValue(arguments, "--receipt")
	bundlePath := argumentValue(arguments, "--bundle")
	manifestContents, _ := os.ReadFile(manifestPath)
	manifest, _ := parseReleaseManifest(manifestContents, 124)
	currentContents, _ := os.ReadFile(currentPath)
	bundleContents, _ := os.ReadFile(filepath.Join(bundlePath, "bundle-manifest.json"))
	contents := engineReceiptJSON(manifest, "deploy", sha256Bytes(currentContents), sha256Bytes(bundleContents))
	if err := os.WriteFile(receiptPath, contents, 0o444); err != nil {
		return err
	}
	return os.Chmod(receiptPath, 0o444)
}

func argumentValue(arguments []string, name string) string {
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == name {
			return arguments[index+1]
		}
	}
	return ""
}

func newTestConfig(t *testing.T, runner *recordingRunner) Config {
	t.Helper()
	root := t.TempDir()
	lockDir := filepath.Join(root, "lock")
	if err := os.Mkdir(lockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return Config{
		Repository:     officialRepository,
		ManagePath:     filepath.Join(root, "fixed", "manage.sh"),
		AndroidPath:    filepath.Join(root, "fixed", "android-manage.sh"),
		Environment:    filepath.Join(root, "fixed", "production.env"),
		PublicRoot:     filepath.Join(root, "public"),
		StateDir:       filepath.Join(root, "state"),
		LockFile:       filepath.Join(lockDir, "broker.lock"),
		PATH:           productionPATH,
		RequestTimeout: time.Second,
		DeployTimeout:  time.Second,
		Now:            func() time.Time { return time.Date(2026, 8, 26, 9, 10, 11, 0, time.UTC) },
		RunCommand:     runner.run,
		OwnerUID:       uint32(os.Geteuid()),
	}
}

func manifestJSON(version string, versionCode int64, candidateRunID int64) []byte {
	value := map[string]any{
		"manifest_version":          1,
		"version":                   version,
		"git_sha":                   strings.Repeat("c", 40),
		"version_code":              versionCode,
		"portal_image":              "ghcr.io/1024xengineer/xe3-esl-portal",
		"portal_image_digest":       "sha256:" + strings.Repeat("a", 64),
		"server_image":              "ghcr.io/1024xengineer/xe3-esl-server",
		"server_image_digest":       "sha256:" + strings.Repeat("b", 64),
		"staging_apk_file":          "speakup-v" + version + "-staging-arm64.apk",
		"staging_apk_sha256":        strings.Repeat("d", 64),
		"production_apk_file":       "speakup-v" + version + "-production-arm64.apk",
		"production_apk_size_bytes": int64(len("signed-production-apk\n")),
		"production_apk_sha256":     sha256Bytes([]byte("signed-production-apk\n")),
		"application_id":            "com.xengineer.speakup",
		"minimum_android_api":       24,
		"abis":                      []string{"arm64-v8a"},
		"apk_certificate_sha256":    strings.Repeat("f", 64),
		"database_schema_version":   9,
		"quality_run_url":           "https://github.com/1024XEngineer/XE3-ESL/actions/runs/" + strconv.FormatInt(candidateRunID, 10),
	}
	contents, _ := json.Marshal(value)
	return contents
}

func engineReceiptJSON(manifest releaseManifest, operation string, previous string, bundle string) []byte {
	nginx := "server { listen 443 ssl; }\n"
	value := map[string]any{
		"receipt_version": 1, "environment": "production", "operation": operation,
		"manifest_sha256": manifest.SHA256, "version": manifest.Version, "version_code": manifest.VersionCode,
		"git_sha": manifest.GitSHA, "database_schema_version": manifest.DatabaseSchemaVersion,
		"portal_image_digest": manifest.PortalImageDigest, "server_image_digest": manifest.ServerImageDigest,
		"production_apk_file": manifest.ProductionAPKFile, "production_apk_size_bytes": manifest.ProductionAPKSize,
		"production_apk_sha256": manifest.ProductionAPKSHA256, "apk_certificate_sha256": manifest.APKCertificateSHA256,
		"portal_container_id": strings.Repeat("1", 64), "server_container_id": strings.Repeat("2", 64),
		"postgres_container_id": strings.Repeat("3", 64), "nginx_config_sha256": sha256Bytes([]byte(nginx)), "nginx_config": nginx,
		"postgres_backup_id": nil, "portal_backup_id": nil, "android_bundle_manifest_sha256": nil,
		"previous_receipt_sha256": nil, "rollback_target_receipt_sha256": nil,
		"recorded_at_utc": "2026-08-26T09:10:10Z",
	}
	if operation == "deploy" {
		value["postgres_backup_id"] = "20260826T091000Z-predeploy"
		value["portal_backup_id"] = "20260826T091000000Z"
		value["android_bundle_manifest_sha256"] = bundle
		value["previous_receipt_sha256"] = previous
	}
	contents, _ := json.Marshal(value)
	return contents
}

func initializeTestState(t *testing.T, config Config) {
	t.Helper()
	manifestContents := manifestJSON("0.1.7", 8, 123)
	manifest, err := parseReleaseManifest(manifestContents, 123)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(config.StateDir)
	manifestPath := filepath.Join(root, "baseline-manifest.json")
	receiptPath := filepath.Join(root, "baseline-receipt.json")
	if err := os.WriteFile(manifestPath, manifestContents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receiptPath, engineReceiptJSON(manifest, "baseline", "", ""), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(receiptPath, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := initializeState(config, manifestPath, receiptPath); err != nil {
		t.Fatal(err)
	}
}

func stagingResponseJSON(manifest releaseManifest, candidateRunID, stagingRunID int64) []byte {
	receipt := map[string]any{
		"receipt_version": 2, "environment": "staging", "operation": "deploy", "repository": officialRepository,
		"manifest_sha256": manifest.SHA256, "version": manifest.Version, "version_code": manifest.VersionCode,
		"git_sha": manifest.GitSHA, "database_schema_version": manifest.DatabaseSchemaVersion,
		"portal_image_digest": manifest.PortalImageDigest, "server_image_digest": manifest.ServerImageDigest,
		"portal_container_id": strings.Repeat("4", 64), "server_container_id": strings.Repeat("5", 64),
		"postgres_container_id": strings.Repeat("6", 64), "candidate_run_id": candidateRunID,
		"deployment_run_id": stagingRunID, "deployment_run_attempt": 1,
		"previous_receipt_sha256": strings.Repeat("7", 64), "rollback_target_receipt_sha256": nil,
		"recorded_at_utc": "2026-08-26T09:00:00Z",
	}
	receiptContents, _ := json.Marshal(receipt)
	value := map[string]any{"protocol_version": 1, "ok": true, "action": "deploy", "receipt_sha256": sha256Bytes(receiptContents), "receipt": receipt}
	contents, _ := json.Marshal(value)
	return contents
}

func deploymentPayload(t *testing.T, currentSHA string, mutateStaging bool, attempts ...int64) []byte {
	t.Helper()
	attempt := int64(1)
	if len(attempts) == 1 {
		attempt = attempts[0]
	} else if len(attempts) > 1 {
		t.Fatal("deploymentPayload accepts at most one attempt")
	}
	manifestContents := manifestJSON("0.1.8", 9, 124)
	manifest, err := parseReleaseManifest(manifestContents, 124)
	if err != nil {
		t.Fatal(err)
	}
	stagingCandidate := int64(124)
	if mutateStaging {
		stagingCandidate = 999
	}
	stagingContents := stagingResponseJSON(manifest, stagingCandidate, 700)
	apk := []byte("signed-production-apk\n")
	published := "2026-08-26T09:05:00Z"
	prefix := "downloads/android/v" + manifest.Version + "/"
	apkPath := prefix + manifest.ProductionAPKFile
	metadata := map[string]any{
		"metadata_version": 1, "version": manifest.Version, "version_code": manifest.VersionCode, "published_at": published,
		"file_name": manifest.ProductionAPKFile, "download_path": "/" + apkPath, "size_bytes": int64(len(apk)),
		"minimum_android_api": 24, "abis": []string{"arm64-v8a"}, "apk_sha256": manifest.ProductionAPKSHA256,
		"apk_certificate_sha256": manifest.APKCertificateSHA256,
	}
	metadataContents, _ := json.Marshal(metadata)
	checksum := []byte(manifest.ProductionAPKSHA256 + "  " + manifest.ProductionAPKFile + "\n")
	files := map[string][]byte{
		apkPath:                 apk,
		apkPath + ".sha256":     checksum,
		prefix + "release.json": metadataContents,
	}
	entries := make([]map[string]any, 0, 3)
	for _, name := range []string{apkPath, apkPath + ".sha256", prefix + "release.json"} {
		entries = append(entries, map[string]any{"path": name, "size_bytes": len(files[name]), "sha256": sha256Bytes(files[name])})
	}
	bundleManifest, _ := json.Marshal(map[string]any{"bundle_version": 1, "version": manifest.Version, "published_at": published, "release_manifest_sha256": manifest.SHA256, "files": entries})
	requestContents, _ := json.Marshal(map[string]any{
		"protocol_version": 1, "action": "deploy", "repository": officialRepository, "candidate_run_id": 124,
		"staging_run_id": 700, "staging_run_attempt": 1, "deployment_run_id": 800, "deployment_run_attempt": attempt,
		"expected_current_receipt_sha256": currentSHA, "manifest_sha256": manifest.SHA256,
		"staging_receipt_sha256": sha256Bytes(stagingContents), "bundle_manifest_sha256": sha256Bytes(bundleManifest),
	})
	archiveFiles := map[string][]byte{
		"request.json": requestContents, "release-manifest.json": manifestContents, "staging-receipt.json": stagingContents,
		"bundle/bundle-manifest.json": bundleManifest,
	}
	for name, contents := range files {
		archiveFiles["bundle/"+name] = contents
	}
	return tarPayload(t, archiveFiles)
}

func tarPayload(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	for _, name := range names {
		contents := files[name]
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(contents)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func inspectPayload(t *testing.T) []byte {
	return tarPayload(t, map[string][]byte{"request.json": []byte(`{"protocol_version":1,"action":"inspect"}`)})
}

func releasePayload(t *testing.T, deployed response, attempt int64) []byte {
	t.Helper()
	requestContents, err := json.Marshal(map[string]any{
		"protocol_version": 1, "action": "release", "repository": officialRepository,
		"deployment_run_id": 800, "deployment_run_attempt": attempt,
		"expected_current_receipt_sha256": deployed.CurrentReceiptSHA256,
		"manifest_sha256":                 deployed.Receipt.ManifestSHA256,
		"bundle_manifest_sha256":          *deployed.Receipt.AndroidBundleManifestSHA256,
		"version":                         deployed.Receipt.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	return tarPayload(t, map[string][]byte{"request.json": requestContents})
}

func releaseMetadata(receipt auditReceipt) []byte {
	value := map[string]any{
		"metadata_version": 1, "version": receipt.Version, "version_code": receipt.VersionCode,
		"published_at": "2026-08-26T09:05:00Z", "file_name": receipt.ProductionAPKFile,
		"download_path": "/downloads/android/v" + receipt.Version + "/" + receipt.ProductionAPKFile,
		"size_bytes":    int64(len("signed-production-apk\n")), "minimum_android_api": 24,
		"abis": []string{"arm64-v8a"}, "apk_sha256": receipt.ProductionAPKSHA256,
		"apk_certificate_sha256": receipt.APKCertificateSHA256,
	}
	contents, _ := json.Marshal(value)
	return contents
}

func TestInspectAndDeploySameCandidateEvidence(t *testing.T) {
	runner := &recordingRunner{}
	config := newTestConfig(t, runner)
	initializeTestState(t, config)
	initial, err := execute(context.Background(), bytes.NewReader(inspectPayload(t)), config)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Action != "inspect" || initial.Receipt.Operation != "baseline" {
		t.Fatalf("initial response = %#v", initial)
	}
	deployed, err := execute(context.Background(), bytes.NewReader(deploymentPayload(t, initial.CurrentReceiptSHA256, false)), config)
	if err != nil {
		t.Fatal(err)
	}
	if deployed.Action != "deploy" || deployed.Receipt.CandidateRunID == nil || *deployed.Receipt.CandidateRunID != 124 || deployed.Receipt.StagingRunID == nil || *deployed.Receipt.StagingRunID != 700 || deployed.Receipt.PreviousReceiptSHA256 == nil || *deployed.Receipt.PreviousReceiptSHA256 != initial.CurrentReceiptSHA256 {
		t.Fatalf("deploy response = %#v", deployed)
	}
	receiptContents, err := json.Marshal(deployed.Receipt)
	if err != nil || sha256Bytes(receiptContents) != deployed.CurrentReceiptSHA256 {
		t.Fatal("response does not expose the immutable audit receipt hash")
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %d", len(runner.calls))
	}
	expected := []string{"deploy", "--manifest", "--env-file", "--bundle", "--current-receipt", "--receipt"}
	for _, token := range expected {
		if !contains(runner.calls[0], token) {
			t.Fatalf("missing fixed manage argument %q in %#v", token, runner.calls[0])
		}
	}
}

func TestRejectsMismatchedStagingReceiptBeforeManage(t *testing.T) {
	runner := &recordingRunner{}
	config := newTestConfig(t, runner)
	initializeTestState(t, config)
	initial, _ := execute(context.Background(), bytes.NewReader(inspectPayload(t)), config)
	_, err := execute(context.Background(), bytes.NewReader(deploymentPayload(t, initial.CurrentReceiptSHA256, true)), config)
	if err == nil || errorCode(err) != "invalid_request" {
		t.Fatalf("error = %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatal("manage ran for mismatched Staging evidence")
	}
}

func TestRetryReturnsExistingDeploymentWithoutRedeploying(t *testing.T) {
	runner := &recordingRunner{}
	config := newTestConfig(t, runner)
	initializeTestState(t, config)
	initial, _ := execute(context.Background(), bytes.NewReader(inspectPayload(t)), config)
	deployed, err := execute(context.Background(), bytes.NewReader(deploymentPayload(t, initial.CurrentReceiptSHA256, false)), config)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := execute(context.Background(), bytes.NewReader(deploymentPayload(t, deployed.CurrentReceiptSHA256, false, 2)), config)
	if err != nil {
		t.Fatal(err)
	}
	if retried.CurrentReceiptSHA256 != deployed.CurrentReceiptSHA256 || len(runner.calls) != 2 || runner.calls[1][0] != "verify" {
		t.Fatalf("retry response = %#v, calls = %#v", retried, runner.calls)
	}
}

func TestReleaseActivatesOnlyTheDeployedAPK(t *testing.T) {
	runner := &recordingRunner{}
	config := newTestConfig(t, runner)
	initializeTestState(t, config)
	initial, _ := execute(context.Background(), bytes.NewReader(inspectPayload(t)), config)
	deployed, err := execute(context.Background(), bytes.NewReader(deploymentPayload(t, initial.CurrentReceiptSHA256, false)), config)
	if err != nil {
		t.Fatal(err)
	}
	runner.releaseMetadata = releaseMetadata(deployed.Receipt)
	released, err := execute(context.Background(), bytes.NewReader(releasePayload(t, deployed, 1)), config)
	if err != nil {
		t.Fatal(err)
	}
	if released.Action != "release" || released.CurrentReceiptSHA256 != deployed.CurrentReceiptSHA256 || len(runner.calls) != 2 || runner.calls[1][0] != "activate" {
		t.Fatalf("release response = %#v, calls = %#v", released, runner.calls)
	}

	if _, err := execute(context.Background(), bytes.NewReader(releasePayload(t, deployed, 2)), config); err != nil {
		t.Fatalf("idempotent release failed: %v", err)
	}
	if len(runner.calls) != 3 || runner.calls[2][0] != "activate" {
		t.Fatalf("release retry calls = %#v", runner.calls)
	}
}

func TestReleaseRejectsMismatchedPublicMetadata(t *testing.T) {
	runner := &recordingRunner{releaseMetadata: []byte(`{"metadata_version":1}`)}
	config := newTestConfig(t, runner)
	initializeTestState(t, config)
	initial, _ := execute(context.Background(), bytes.NewReader(inspectPayload(t)), config)
	deployed, err := execute(context.Background(), bytes.NewReader(deploymentPayload(t, initial.CurrentReceiptSHA256, false)), config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = execute(context.Background(), bytes.NewReader(releasePayload(t, deployed, 1)), config)
	if err == nil || errorCode(err) != "operation_failed" {
		t.Fatalf("error = %v", err)
	}
}

func TestForcedInvocationRejectsCommandsAndArguments(t *testing.T) {
	config := newTestConfig(t, &recordingRunner{})
	for _, test := range []struct {
		args    []string
		command string
	}{
		{[]string{"broker", "unexpected"}, ""},
		{[]string{"broker"}, "scp -t /tmp/file"},
	} {
		var output bytes.Buffer
		if runCLI(test.args, test.command, bytes.NewReader(nil), &output, config) == 0 || !strings.Contains(output.String(), "invalid_invocation") {
			t.Fatalf("output = %q", output.String())
		}
	}
}

func TestRejectsNonRegularArchiveEntry(t *testing.T) {
	config := newTestConfig(t, &recordingRunner{})
	initializeTestState(t, config)
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	_ = writer.WriteHeader(&tar.Header{Name: "request.json", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"})
	_ = writer.Close()
	_, err := execute(context.Background(), bytes.NewReader(buffer.Bytes()), config)
	if err == nil || errorCode(err) != "invalid_request" {
		t.Fatalf("error = %v", err)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
