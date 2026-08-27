package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type commandCall struct {
	name        string
	arguments   []string
	environment []string
}

type recordingRunner struct {
	mu             sync.Mutex
	calls          []commandCall
	failure        error
	receiptMode    os.FileMode
	receiptMutator func(map[string]any)
}

func (runner *recordingRunner) run(_ context.Context, name string, arguments []string, environment []string) error {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.calls = append(runner.calls, commandCall{
		name:        name,
		arguments:   append([]string(nil), arguments...),
		environment: append([]string(nil), environment...),
	})
	if runner.failure != nil {
		return runner.failure
	}
	if len(arguments) == 0 || (arguments[0] != "deploy" && arguments[0] != "rollback") {
		return nil
	}

	manifestPath := argumentValue(arguments, "--manifest")
	receiptPath := argumentValue(arguments, "--receipt")
	manifestContents, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var manifest struct {
		Version               string `json:"version"`
		GitSHA                string `json:"git_sha"`
		DatabaseSchemaVersion int64  `json:"database_schema_version"`
		PortalImageDigest     string `json:"portal_image_digest"`
		ServerImageDigest     string `json:"server_image_digest"`
	}
	if err := json.Unmarshal(manifestContents, &manifest); err != nil {
		return err
	}
	receipt := map[string]any{
		"receipt_version":         1,
		"manifest_sha256":         sha256Bytes(manifestContents),
		"version":                 manifest.Version,
		"git_sha":                 manifest.GitSHA,
		"database_schema_version": manifest.DatabaseSchemaVersion,
		"portal_image_digest":     manifest.PortalImageDigest,
		"server_image_digest":     manifest.ServerImageDigest,
		"portal_container_id":     strings.Repeat("1", 64),
		"server_container_id":     strings.Repeat("2", 64),
		"postgres_container_id":   strings.Repeat("3", 64),
		"deployed_at_utc":         "2026-08-26T01:02:03Z",
	}
	if runner.receiptMutator != nil {
		runner.receiptMutator(receipt)
	}
	contents, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	if err := os.WriteFile(receiptPath, contents, 0o600); err != nil {
		return err
	}
	mode := runner.receiptMode
	if mode == 0 {
		mode = 0o444
	}
	return os.Chmod(receiptPath, mode)
}

func (runner *recordingRunner) snapshot() []commandCall {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return append([]commandCall(nil), runner.calls...)
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
	stateDir := filepath.Join(root, "state")
	lockDir := filepath.Join(root, "lock")
	if err := os.Mkdir(lockDir, 0o700); err != nil {
		t.Fatalf("create lock directory: %v", err)
	}
	xdg := filepath.Join(root, "run", "user", "123")
	return Config{
		Repository:     officialRepository,
		ManagePath:     filepath.Join(root, "fixed", "manage.sh"),
		RuntimeEnvFile: filepath.Join(root, "fixed", "staging-runtime.env"),
		StateDir:       stateDir,
		PublicRoot:     filepath.Join(root, "public"),
		LockFile:       filepath.Join(lockDir, "broker.lock"),
		RequestTimeout: time.Second,
		PublishTimeout: time.Second,
		Timeout:        time.Second,
		Home:           filepath.Join(root, "fixed", "home"),
		XDGRuntimeDir:  xdg,
		DockerHost:     "unix://" + filepath.Join(xdg, "docker.sock"),
		PATH:           "/usr/local/bin:/usr/bin:/bin",
		Now: func() time.Time {
			return time.Date(2026, 8, 26, 1, 2, 4, 987, time.FixedZone("test", 8*60*60))
		},
		RunCommand: runner.run,
	}
}

func validManifestMap(candidateRunID int64, version string, schema int64) map[string]any {
	return map[string]any{
		"manifest_version":          1,
		"version":                   version,
		"git_sha":                   strings.Repeat("c", 40),
		"version_code":              2,
		"portal_image":              "ghcr.io/1024xengineer/xe3-esl-portal",
		"portal_image_digest":       "sha256:" + strings.Repeat("a", 64),
		"server_image":              "ghcr.io/1024xengineer/xe3-esl-server",
		"server_image_digest":       "sha256:" + strings.Repeat("b", 64),
		"staging_apk_file":          "speakup-v" + version + "-staging-arm64.apk",
		"staging_apk_sha256":        strings.Repeat("d", 64),
		"production_apk_file":       "speakup-v" + version + "-production-arm64.apk",
		"production_apk_size_bytes": 123456,
		"production_apk_sha256":     strings.Repeat("e", 64),
		"application_id":            "com.xengineer.speakup",
		"minimum_android_api":       24,
		"abis":                      []string{"arm64-v8a"},
		"apk_certificate_sha256":    strings.Repeat("f", 64),
		"database_schema_version":   schema,
		"quality_run_url":           fmt.Sprintf("https://github.com/1024XEngineer/XE3-ESL/actions/runs/%d", candidateRunID),
	}
}

func marshalJSON(t *testing.T, value any) []byte {
	t.Helper()
	contents, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return contents
}

func deployRequestJSON(t *testing.T, manifest []byte, candidateRunID int64, deploymentRunID int64, expected any) []byte {
	t.Helper()
	return marshalJSON(t, map[string]any{
		"protocol_version":                1,
		"action":                          "deploy",
		"repository":                      officialRepository,
		"candidate_run_id":                candidateRunID,
		"deployment_run_id":               deploymentRunID,
		"deployment_run_attempt":          1,
		"expected_current_receipt_sha256": expected,
		"manifest_sha256":                 sha256Bytes(manifest),
		"manifest_base64":                 base64.StdEncoding.EncodeToString(manifest),
	})
}

func rollbackRequestJSON(t *testing.T, deploymentRunID int64, expected string, target string) []byte {
	t.Helper()
	return marshalJSON(t, map[string]any{
		"protocol_version":                1,
		"action":                          "rollback",
		"repository":                      officialRepository,
		"deployment_run_id":               deploymentRunID,
		"deployment_run_attempt":          2,
		"expected_current_receipt_sha256": expected,
		"target_receipt_sha256":           target,
	})
}

func executeMutation(t *testing.T, config Config, contents []byte) mutationResponse {
	t.Helper()
	response, err := execute(bytes.NewReader(contents), config)
	if err != nil {
		t.Fatalf("execute mutation: %v", err)
	}
	mutation, ok := response.(mutationResponse)
	if !ok {
		t.Fatalf("response type = %T", response)
	}
	return mutation
}

func TestInspectDeployAndRollback(t *testing.T) {
	runner := &recordingRunner{}
	config := newTestConfig(t, runner)

	initial, err := execute(strings.NewReader(`{"protocol_version":1,"action":"inspect"}`), config)
	if err != nil {
		t.Fatalf("inspect empty state: %v", err)
	}
	initialInspect := initial.(inspectResponse)
	if !initialInspect.OK || initialInspect.CurrentReceiptSHA256 != nil || initialInspect.Receipt != nil {
		t.Fatalf("initial inspect = %#v", initialInspect)
	}

	firstManifest := marshalJSON(t, validManifestMap(123, "0.1.1", 7))
	first := executeMutation(t, config, deployRequestJSON(t, firstManifest, 123, 9001, nil))
	if first.Action != "deploy" || first.Receipt.PreviousReceiptSHA256 != nil || first.Receipt.RollbackTargetReceiptSHA256 != nil {
		t.Fatalf("first deploy response = %#v", first)
	}
	if first.Receipt.RecordedAtUTC != "2026-08-25T17:02:04Z" {
		t.Fatalf("recorded_at_utc = %q", first.Receipt.RecordedAtUTC)
	}
	assertStatePermissions(t, config, first, sha256Bytes(firstManifest))

	inspected, err := execute(strings.NewReader(`{"protocol_version":1,"action":"inspect"}`), config)
	if err != nil {
		t.Fatalf("inspect deployed state: %v", err)
	}
	deployedInspect := inspected.(inspectResponse)
	if deployedInspect.CurrentReceiptSHA256 == nil || *deployedInspect.CurrentReceiptSHA256 != first.ReceiptSHA256 ||
		deployedInspect.Receipt == nil || !reflect.DeepEqual(*deployedInspect.Receipt, first.Receipt) {
		t.Fatalf("deployed inspect = %#v", deployedInspect)
	}

	secondManifest := marshalJSON(t, validManifestMap(124, "0.1.2", 7))
	second := executeMutation(t, config, deployRequestJSON(t, secondManifest, 124, 9002, first.ReceiptSHA256))
	if second.Receipt.PreviousReceiptSHA256 == nil || *second.Receipt.PreviousReceiptSHA256 != first.ReceiptSHA256 {
		t.Fatalf("second previous = %#v", second.Receipt.PreviousReceiptSHA256)
	}

	rolledBack := executeMutation(t, config, rollbackRequestJSON(t, 9003, second.ReceiptSHA256, first.ReceiptSHA256))
	if rolledBack.Action != "rollback" || rolledBack.Receipt.CandidateRunID != 123 ||
		rolledBack.Receipt.PreviousReceiptSHA256 == nil || *rolledBack.Receipt.PreviousReceiptSHA256 != second.ReceiptSHA256 ||
		rolledBack.Receipt.RollbackTargetReceiptSHA256 == nil || *rolledBack.Receipt.RollbackTargetReceiptSHA256 != first.ReceiptSHA256 {
		t.Fatalf("rollback response = %#v", rolledBack)
	}

	calls := runner.snapshot()
	if len(calls) != 4 {
		t.Fatalf("manage call count = %d; calls = %#v", len(calls), calls)
	}
	firstManifestPath := filepath.Join(config.StateDir, "manifests", sha256Bytes(firstManifest)+".json")
	secondManifestPath := filepath.Join(config.StateDir, "manifests", sha256Bytes(secondManifest)+".json")
	assertArguments(t, calls[0].arguments, []string{
		"deploy", "--manifest", firstManifestPath,
		"--runtime-env-file", config.RuntimeEnvFile,
		"--receipt", argumentValue(calls[0].arguments, "--receipt"),
	})
	assertArguments(t, calls[1].arguments, []string{
		"verify", "--manifest", firstManifestPath,
		"--runtime-env-file", config.RuntimeEnvFile,
	})
	assertArguments(t, calls[2].arguments, []string{
		"deploy", "--manifest", secondManifestPath,
		"--runtime-env-file", config.RuntimeEnvFile,
		"--receipt", argumentValue(calls[2].arguments, "--receipt"),
	})
	assertArguments(t, calls[3].arguments, []string{
		"rollback", "--manifest", firstManifestPath,
		"--current-manifest", secondManifestPath,
		"--runtime-env-file", config.RuntimeEnvFile,
		"--receipt", argumentValue(calls[3].arguments, "--receipt"),
	})
	expectedEnvironment := []string{
		"HOME=" + config.Home,
		"XDG_RUNTIME_DIR=" + config.XDGRuntimeDir,
		"DOCKER_HOST=" + config.DockerHost,
		"PATH=" + config.PATH,
	}
	for _, call := range calls {
		if call.name != config.ManagePath || !reflect.DeepEqual(call.environment, expectedEnvironment) {
			t.Fatalf("manage boundary = %#v", call)
		}
		for _, argument := range call.arguments {
			if argument == "" {
				t.Fatalf("empty manage argument in %#v", call.arguments)
			}
		}
	}
}

func assertStatePermissions(t *testing.T, config Config, response mutationResponse, manifestSHA string) {
	t.Helper()
	for _, directory := range []string{
		config.StateDir,
		filepath.Join(config.StateDir, "manifests"),
		filepath.Join(config.StateDir, "receipts"),
		filepath.Join(config.StateDir, "engine-receipts"),
	} {
		info, err := os.Stat(directory)
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("state directory %q: info=%v err=%v", directory, info, err)
		}
	}
	for _, file := range []string{
		filepath.Join(config.StateDir, "manifests", manifestSHA+".json"),
		filepath.Join(config.StateDir, "receipts", response.ReceiptSHA256+".json"),
	} {
		info, err := os.Stat(file)
		if err != nil || info.Mode().Perm() != 0o444 {
			t.Fatalf("readonly object %q: info=%v err=%v", file, info, err)
		}
	}
	currentPath := filepath.Join(config.StateDir, "current")
	contents, err := os.ReadFile(currentPath)
	if err != nil || string(contents) != response.ReceiptSHA256 {
		t.Fatalf("current contents = %q, err=%v", contents, err)
	}
	info, err := os.Stat(currentPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("current mode: info=%v err=%v", info, err)
	}
}

func assertArguments(t *testing.T, actual []string, expected []string) {
	t.Helper()
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("arguments = %#v, want %#v", actual, expected)
	}
}

func TestCASRejectsBeforeManage(t *testing.T) {
	runner := &recordingRunner{}
	config := newTestConfig(t, runner)
	manifest := marshalJSON(t, validManifestMap(123, "0.1.1", 7))
	first := executeMutation(t, config, deployRequestJSON(t, manifest, 123, 9001, nil))
	baseline := len(runner.snapshot())

	for name, expected := range map[string]any{
		"missing expected current": nil,
		"wrong expected current":   strings.Repeat("9", 64),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := execute(bytes.NewReader(deployRequestJSON(t, manifest, 123, 9002, expected)), config)
			if errorCode(err) != "conflict" {
				t.Fatalf("error = %v", err)
			}
			if len(runner.snapshot()) != baseline {
				t.Fatal("CAS failure invoked manage")
			}
		})
	}

	_, err := execute(bytes.NewReader(rollbackRequestJSON(t, 9003, first.ReceiptSHA256, first.ReceiptSHA256)), config)
	if errorCode(err) != "conflict" || len(runner.snapshot()) != baseline {
		t.Fatalf("self rollback: err=%v calls=%d", err, len(runner.snapshot()))
	}
}

func TestRollbackRejectsSchemaChangeBeforeManage(t *testing.T) {
	runner := &recordingRunner{}
	config := newTestConfig(t, runner)
	firstManifest := marshalJSON(t, validManifestMap(123, "0.1.1", 7))
	first := executeMutation(t, config, deployRequestJSON(t, firstManifest, 123, 9001, nil))
	secondManifest := marshalJSON(t, validManifestMap(124, "0.2.0", 8))
	second := executeMutation(t, config, deployRequestJSON(t, secondManifest, 124, 9002, first.ReceiptSHA256))
	baseline := len(runner.snapshot())

	_, err := execute(bytes.NewReader(rollbackRequestJSON(t, 9003, second.ReceiptSHA256, first.ReceiptSHA256)), config)
	if errorCode(err) != "conflict" {
		t.Fatalf("rollback error = %v", err)
	}
	if len(runner.snapshot()) != baseline {
		t.Fatal("schema-incompatible rollback invoked manage")
	}
}

func TestRequestBoundary(t *testing.T) {
	manifest := marshalJSON(t, validManifestMap(123, "0.1.1", 7))
	valid := deployRequestJSON(t, manifest, 123, 9001, nil)
	var validObject map[string]any
	if err := json.Unmarshal(valid, &validObject); err != nil {
		t.Fatal(err)
	}

	tests := map[string][]byte{
		"empty":               nil,
		"unknown field":       marshalJSON(t, map[string]any{"protocol_version": 1, "action": "inspect", "extra": true}),
		"missing field":       marshalJSON(t, map[string]any{"protocol_version": 1, "action": "deploy"}),
		"wrong type":          []byte(`{"protocol_version":"1","action":"inspect"}`),
		"noncanonical number": []byte(`{"protocol_version":1.0,"action":"inspect"}`),
		"unknown action":      []byte(`{"protocol_version":1,"action":"shell"}`),
		"duplicate field":     []byte(`{"protocol_version":1,"action":"inspect","action":"inspect"}`),
		"recursive duplicate": []byte(`{"protocol_version":1,"action":{"nested":1,"nested":2}}`),
		"multiple documents":  []byte(`{"protocol_version":1,"action":"inspect"}{}`),
		"invalid UTF-8":       append([]byte(`{"protocol_version":1,"action":"`), 0xff, '"', '}'),
		"excessive nesting":   []byte(strings.Repeat("[", maxJSONDepth+2) + strings.Repeat("]", maxJSONDepth+2)),
		"oversize":            bytes.Repeat([]byte(" "), requestLimit+1),
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parseRequest(bytes.NewReader(contents), officialRepository)
			if err == nil {
				t.Fatal("request unexpectedly accepted")
			}
			if name == "oversize" && errorCode(err) != "request_too_large" {
				t.Fatalf("oversize error = %v", err)
			}
		})
	}

	mutations := map[string]func(map[string]any){
		"wrong repository":    func(value map[string]any) { value["repository"] = "attacker/repo" },
		"unsafe integer":      func(value map[string]any) { value["deployment_run_id"] = maxSafeInteger + 1 },
		"null run":            func(value map[string]any) { value["candidate_run_id"] = nil },
		"wrong hash":          func(value map[string]any) { value["manifest_sha256"] = strings.Repeat("9", 64) },
		"zero hash":           func(value map[string]any) { value["manifest_sha256"] = strings.Repeat("0", 64) },
		"noncanonical base64": func(value map[string]any) { value["manifest_base64"] = value["manifest_base64"].(string) + "\n" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			copyObject := make(map[string]any, len(validObject))
			for key, value := range validObject {
				copyObject[key] = value
			}
			mutate(copyObject)
			_, err := parseRequest(bytes.NewReader(marshalJSON(t, copyObject)), officialRepository)
			if err == nil {
				t.Fatal("request unexpectedly accepted")
			}
		})
	}

	tooLargeManifest := bytes.Repeat([]byte(" "), manifestLimit+1)
	_, err := parseRequest(bytes.NewReader(deployRequestJSON(t, tooLargeManifest, 123, 9001, nil)), officialRepository)
	if err == nil {
		t.Fatal("oversize manifest unexpectedly accepted")
	}
}

func TestManifestBoundary(t *testing.T) {
	tests := map[string]func(map[string]any){
		"unknown":           func(value map[string]any) { value["unknown"] = true },
		"missing":           func(value map[string]any) { delete(value, "git_sha") },
		"portal repository": func(value map[string]any) { value["portal_image"] = "ghcr.io/attacker/portal" },
		"server digest":     func(value map[string]any) { value["server_image_digest"] = "sha256:" + strings.Repeat("0", 64) },
		"schema type":       func(value map[string]any) { value["database_schema_version"] = "7" },
		"abis":              func(value map[string]any) { value["abis"] = []string{"arm64-v8a", "x86_64"} },
		"quality run": func(value map[string]any) {
			value["quality_run_url"] = "https://github.com/1024XEngineer/XE3-ESL/actions/runs/124"
		},
		"apk name": func(value map[string]any) { value["staging_apk_file"] = "other.apk" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			value := validManifestMap(123, "0.1.1", 7)
			mutate(value)
			if _, err := validateManifest(marshalJSON(t, value), 123); err == nil {
				t.Fatal("manifest unexpectedly accepted")
			}
		})
	}

	valid := marshalJSON(t, validManifestMap(123, "0.1.1", 7))
	duplicate := append(bytes.TrimSuffix(valid, []byte("}")), []byte(`,"version":"0.1.1"}`)...)
	if _, err := validateManifest(duplicate, 123); err == nil {
		t.Fatal("duplicate manifest key unexpectedly accepted")
	}
	if _, err := validateManifest(valid, 124); err == nil {
		t.Fatal("candidate run mismatch unexpectedly accepted")
	}
}

func TestEngineReceiptBoundary(t *testing.T) {
	tests := map[string]struct {
		mode   os.FileMode
		mutate func(map[string]any)
	}{
		"unknown field": {
			mutate: func(value map[string]any) { value["unknown"] = true },
		},
		"manifest mismatch": {
			mutate: func(value map[string]any) { value["manifest_sha256"] = strings.Repeat("9", 64) },
		},
		"container id": {
			mutate: func(value map[string]any) { value["portal_container_id"] = "short" },
		},
		"writable mode": {mode: 0o644},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			runner := &recordingRunner{receiptMode: test.mode, receiptMutator: test.mutate}
			config := newTestConfig(t, runner)
			manifest := marshalJSON(t, validManifestMap(123, "0.1.1", 7))
			_, err := execute(bytes.NewReader(deployRequestJSON(t, manifest, 123, 9001, nil)), config)
			if errorCode(err) != "operation_failed" {
				t.Fatalf("error = %v", err)
			}
			if _, statErr := os.Stat(filepath.Join(config.StateDir, "current")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("current written after invalid engine receipt: %v", statErr)
			}
		})
	}
}

func TestInvocationAndErrorsAreRedacted(t *testing.T) {
	runner := &recordingRunner{failure: errors.New("SECRET_VALUE at /private/runtime.env")}
	config := newTestConfig(t, runner)
	tests := []struct {
		arguments []string
		original  string
	}{
		{arguments: []string{"broker", "inspect"}},
		{arguments: []string{"broker"}, original: "cat /etc/shadow"},
	}
	for _, test := range tests {
		var output bytes.Buffer
		if status := runCLI(test.arguments, test.original, strings.NewReader(`{"secret":"SECRET_VALUE"}`), &output, config); status == 0 {
			t.Fatal("invalid invocation succeeded")
		}
		assertRedactedError(t, output.Bytes(), "invalid_invocation")
	}

	manifest := marshalJSON(t, validManifestMap(123, "0.1.1", 7))
	var output bytes.Buffer
	if status := runCLI([]string{"broker"}, "", bytes.NewReader(deployRequestJSON(t, manifest, 123, 9001, nil)), &output, config); status == 0 {
		t.Fatal("failed manage invocation succeeded")
	}
	assertRedactedError(t, output.Bytes(), "operation_failed")
	if strings.Contains(output.String(), "SECRET_VALUE") || strings.Contains(output.String(), "runtime.env") {
		t.Fatalf("error leaked sensitive text: %s", output.String())
	}
}

func assertRedactedError(t *testing.T, contents []byte, code string) {
	t.Helper()
	var response map[string]any
	if err := json.Unmarshal(contents, &response); err != nil {
		t.Fatalf("decode error response: %v; contents=%q", err, contents)
	}
	if len(response) != 3 || response["protocol_version"] != float64(1) || response["ok"] != false || response["error"] != code {
		t.Fatalf("error response = %#v", response)
	}
}

func TestContentAddressedObjectsAreNoClobber(t *testing.T) {
	runner := &recordingRunner{}
	config := newTestConfig(t, runner)
	manifest := marshalJSON(t, validManifestMap(123, "0.1.1", 7))
	response := executeMutation(t, config, deployRequestJSON(t, manifest, 123, 9001, nil))
	store, err := openStateStore(config.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath, err := store.saveManifest(sha256Bytes(manifest), manifest)
	if err != nil {
		t.Fatalf("reuse identical manifest: %v", err)
	}
	if info, statErr := os.Stat(manifestPath); statErr != nil || info.Mode().Perm() != 0o444 {
		t.Fatalf("reused manifest mode: info=%v err=%v", info, statErr)
	}
	receiptPath := filepath.Join(config.StateDir, "receipts", response.ReceiptSHA256+".json")
	receiptContents, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.saveReceipt(response.ReceiptSHA256, receiptContents, false); errorCode(err) != "state_invalid" {
		t.Fatalf("duplicate receipt error = %v", err)
	}

	if err := os.Chmod(receiptPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(strings.NewReader(`{"protocol_version":1,"action":"inspect"}`), config); errorCode(err) != "state_invalid" {
		t.Fatalf("writable receipt inspect error = %v", err)
	}
}

func TestBrokerLockSerializes(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, "broker.lock")
	first, err := acquireBrokerLock(context.Background(), lockPath)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	defer first.close()
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	if _, err := acquireBrokerLock(ctx, lockPath); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second lock error = %v", err)
	}
}

func TestStalledInputUsesOperationTimeout(t *testing.T) {
	runner := &recordingRunner{}
	config := newTestConfig(t, runner)
	config.RequestTimeout = 75 * time.Millisecond
	reader, writer := io.Pipe()
	defer writer.Close()

	started := time.Now()
	_, err := execute(reader, config)
	if errorCode(err) != "operation_timeout" {
		t.Fatalf("stalled input error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("stalled input returned after %s", elapsed)
	}
	if _, err := writer.Write([]byte(`{"protocol_version":1,"action":"inspect"}`)); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("stalled reader was not closed: %v", err)
	}
	if len(runner.snapshot()) != 0 {
		t.Fatal("stalled input invoked manage")
	}
}

func TestPostEngineInterruptionRecoversWithoutRepeatingManage(t *testing.T) {
	runner := &recordingRunner{}
	config := newTestConfig(t, runner)
	config.AfterEngine = func() error { return errors.New("simulated process interruption") }
	manifest := marshalJSON(t, validManifestMap(123, "0.1.1", 7))
	request := deployRequestJSON(t, manifest, 123, 9001, nil)

	_, err := execute(bytes.NewReader(request), config)
	if errorCode(err) != "operation_interrupted" {
		t.Fatalf("interrupted deploy error = %v", err)
	}
	if len(runner.snapshot()) != 1 {
		t.Fatalf("manage calls after interruption = %d", len(runner.snapshot()))
	}
	assertMode(t, filepath.Join(config.StateDir, "pending.json"), 0o600)
	engineEntries, err := os.ReadDir(filepath.Join(config.StateDir, "engine-receipts"))
	if err != nil || len(engineEntries) != 1 {
		t.Fatalf("durable engine receipts = %v, err=%v", engineEntries, err)
	}
	assertMode(t, filepath.Join(config.StateDir, "engine-receipts", engineEntries[0].Name()), 0o444)
	if _, err := os.Stat(filepath.Join(config.StateDir, "current")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted deploy wrote current: %v", err)
	}

	config.AfterEngine = nil
	recovered := executeMutation(t, config, request)
	if recovered.Receipt.CandidateRunID != 123 || recovered.Receipt.PreviousReceiptSHA256 != nil {
		t.Fatalf("recovered receipt = %#v", recovered.Receipt)
	}
	if len(runner.snapshot()) != 1 {
		t.Fatal("recovery repeated manage mutation")
	}
	if _, err := os.Stat(filepath.Join(config.StateDir, "pending.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovery did not clear pending: %v", err)
	}
	assertStatePermissions(t, config, recovered, sha256Bytes(manifest))
}

func TestPendingWithoutEngineReceiptOnlyResumesExactDeploy(t *testing.T) {
	runner := &recordingRunner{failure: errors.New("manage may have partially mutated runtime")}
	config := newTestConfig(t, runner)
	manifest := marshalJSON(t, validManifestMap(123, "0.1.1", 7))
	request := deployRequestJSON(t, manifest, 123, 9001, nil)

	_, err := execute(bytes.NewReader(request), config)
	if errorCode(err) != "operation_failed" {
		t.Fatalf("initial manage error = %v", err)
	}
	runner.failure = nil
	_, err = execute(strings.NewReader(`{"protocol_version":1,"action":"inspect"}`), config)
	if errorCode(err) != "recovery_required" {
		t.Fatalf("nonmatching recovery error = %v", err)
	}
	if len(runner.snapshot()) != 1 {
		t.Fatal("nonmatching recovery invoked manage")
	}
	recovered := executeMutation(t, config, request)
	if recovered.Action != "deploy" || len(runner.snapshot()) != 2 {
		t.Fatalf("exact deploy recovery = %#v, calls=%d", recovered, len(runner.snapshot()))
	}
	if _, err := os.Stat(filepath.Join(config.StateDir, "pending.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("exact deploy recovery did not clear pending: %v", err)
	}
}

func TestPendingRollbackResumesThroughLockedEngineContract(t *testing.T) {
	runner := &recordingRunner{}
	config := newTestConfig(t, runner)
	firstManifest := marshalJSON(t, validManifestMap(123, "0.1.1", 7))
	first := executeMutation(t, config, deployRequestJSON(t, firstManifest, 123, 9001, nil))
	secondManifest := marshalJSON(t, validManifestMap(124, "0.1.2", 7))
	second := executeMutation(t, config, deployRequestJSON(t, secondManifest, 124, 9002, first.ReceiptSHA256))
	request := rollbackRequestJSON(t, 9003, second.ReceiptSHA256, first.ReceiptSHA256)
	runner.failure = errors.New("rollback exited before receipt")

	_, err := execute(bytes.NewReader(request), config)
	if errorCode(err) != "operation_failed" {
		t.Fatalf("initial rollback error = %v", err)
	}
	failedCalls := len(runner.snapshot())
	_, err = execute(bytes.NewReader(request), config)
	if errorCode(err) != "operation_failed" {
		t.Fatalf("failed locked rollback retry error = %v", err)
	}
	if len(runner.snapshot()) != failedCalls+1 || runner.snapshot()[failedCalls].arguments[0] != "rollback" {
		t.Fatalf("rollback recovery did not reuse engine contract: %#v", runner.snapshot()[failedCalls:])
	}

	runner.failure = nil
	recovered := executeMutation(t, config, request)
	latestCalls := runner.snapshot()
	if recovered.Action != "rollback" || len(latestCalls) != failedCalls+2 {
		t.Fatalf("rollback retry response=%#v calls=%#v", recovered, latestCalls[failedCalls:])
	}
	if latestCalls[failedCalls+1].arguments[0] != "rollback" {
		t.Fatalf("rollback recovery order = %#v", latestCalls[failedCalls+1:])
	}
}

func TestCompletedCurrentWithPendingJournalRecoversIdempotently(t *testing.T) {
	runner := &recordingRunner{}
	config := newTestConfig(t, runner)
	manifest := marshalJSON(t, validManifestMap(123, "0.1.1", 7))
	completed := executeMutation(t, config, deployRequestJSON(t, manifest, 123, 9001, nil))
	store, err := openStateStore(config.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	pending := pendingMutation{
		JournalVersion:       1,
		Environment:          "staging",
		Operation:            "deploy",
		Repository:           officialRepository,
		ManifestSHA256:       completed.Receipt.ManifestSHA256,
		CandidateRunID:       completed.Receipt.CandidateRunID,
		DeploymentRunID:      completed.Receipt.DeploymentRunID,
		DeploymentRunAttempt: completed.Receipt.DeploymentRunAttempt,
		RecordedAtUTC:        completed.Receipt.RecordedAtUTC,
	}
	pendingContents := marshalJSON(t, pending)
	if _, err := os.Stat(store.engineReceiptPath(pendingContents)); err != nil {
		t.Fatalf("durable engine receipt missing: %v", err)
	}
	if err := writeNoClobber(store.root, store.pendingPath, pendingContents, 0o600, false); err != nil {
		t.Fatalf("recreate pending journal: %v", err)
	}

	response, err := execute(strings.NewReader(`{"protocol_version":1,"action":"inspect"}`), config)
	if err != nil {
		t.Fatalf("idempotent recovery inspect: %v", err)
	}
	inspected := response.(inspectResponse)
	if inspected.CurrentReceiptSHA256 == nil || *inspected.CurrentReceiptSHA256 != completed.ReceiptSHA256 {
		t.Fatalf("inspect after idempotent recovery = %#v", inspected)
	}
	if len(runner.snapshot()) != 1 {
		t.Fatal("idempotent recovery repeated manage")
	}
	if _, err := os.Stat(store.pendingPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("idempotent recovery did not clear pending: %v", err)
	}
}

func TestRollbackRejectsEnginePostgresMismatch(t *testing.T) {
	runner := &recordingRunner{}
	config := newTestConfig(t, runner)
	firstManifest := marshalJSON(t, validManifestMap(123, "0.1.1", 7))
	first := executeMutation(t, config, deployRequestJSON(t, firstManifest, 123, 9001, nil))
	secondManifest := marshalJSON(t, validManifestMap(124, "0.1.2", 7))
	second := executeMutation(t, config, deployRequestJSON(t, secondManifest, 124, 9002, first.ReceiptSHA256))
	runner.receiptMutator = func(receipt map[string]any) {
		receipt["postgres_container_id"] = strings.Repeat("4", 64)
	}

	_, err := execute(bytes.NewReader(rollbackRequestJSON(t, 9003, second.ReceiptSHA256, first.ReceiptSHA256)), config)
	if errorCode(err) != "operation_failed" {
		t.Fatalf("postgres mismatch error = %v", err)
	}
	current, err := os.ReadFile(filepath.Join(config.StateDir, "current"))
	if err != nil || string(current) != second.ReceiptSHA256 {
		t.Fatalf("postgres mismatch changed current: %q err=%v", current, err)
	}
}

func TestReceiptChainValidation(t *testing.T) {
	a := strings.Repeat("a", 64)
	b := strings.Repeat("b", 64)
	c := strings.Repeat("c", 64)
	manifestA := strings.Repeat("d", 64)
	manifestB := strings.Repeat("e", 64)
	postgres := strings.Repeat("3", 64)
	pointer := func(value string) *string { return &value }
	node := func(sha string, operation string, previous *string, target *string, manifest string) *loadedReceipt {
		return &loadedReceipt{
			SHA256: sha,
			Receipt: brokerReceipt{
				Operation:                   operation,
				PreviousReceiptSHA256:       previous,
				RollbackTargetReceiptSHA256: target,
				ManifestSHA256:              manifest,
				CandidateRunID:              123,
				DatabaseSchemaVersion:       7,
				PostgresContainerID:         postgres,
			},
		}
	}

	tests := map[string]map[string]*loadedReceipt{
		"missing previous": {
			a: node(a, "deploy", pointer(b), nil, manifestA),
		},
		"cycle": {
			a: node(a, "deploy", pointer(b), nil, manifestA),
			b: node(b, "deploy", pointer(a), nil, manifestA),
		},
		"rollback target missing": {
			a: node(a, "rollback", pointer(b), pointer(c), manifestA),
			b: node(b, "deploy", nil, nil, manifestA),
		},
		"rollback target manifest mismatch": {
			a: node(a, "rollback", pointer(b), pointer(b), manifestA),
			b: node(b, "deploy", nil, nil, manifestB),
		},
	}
	for name, nodes := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := walkReceiptChain(a, func(sha string) (*loadedReceipt, error) {
				loaded, ok := nodes[sha]
				if !ok {
					return nil, os.ErrNotExist
				}
				return loaded, nil
			})
			if errorCode(err) != "state_invalid" {
				t.Fatalf("chain error = %v", err)
			}
		})
	}
}

func TestMutationRejectsFullReceiptChainBeforeManage(t *testing.T) {
	runner := &recordingRunner{}
	config := newTestConfig(t, runner)
	manifestContents := marshalJSON(t, validManifestMap(123, "0.1.1", 7))
	parsed, err := parseRequest(bytes.NewReader(deployRequestJSON(t, manifestContents, 123, 9001, nil)), officialRepository)
	if err != nil {
		t.Fatal(err)
	}
	chain := make([]*loadedReceipt, maxReceiptChainLength)

	_, err = deploy(context.Background(), nil, config, parsed, chain)
	if errorCode(err) != "state_limit_reached" {
		t.Fatalf("full-chain deploy error = %v", err)
	}
	if len(runner.snapshot()) != 0 {
		t.Fatal("full-chain deploy invoked manage")
	}
}

func TestInspectRejectsCorruptHistoricalReceipt(t *testing.T) {
	runner := &recordingRunner{}
	config := newTestConfig(t, runner)
	firstManifest := marshalJSON(t, validManifestMap(123, "0.1.1", 7))
	first := executeMutation(t, config, deployRequestJSON(t, firstManifest, 123, 9001, nil))
	secondManifest := marshalJSON(t, validManifestMap(124, "0.1.2", 7))
	_ = executeMutation(t, config, deployRequestJSON(t, secondManifest, 124, 9002, first.ReceiptSHA256))
	historicalPath := filepath.Join(config.StateDir, "receipts", first.ReceiptSHA256+".json")
	if err := os.Chmod(historicalPath, 0o644); err != nil {
		t.Fatal(err)
	}
	baseline := len(runner.snapshot())

	_, err := execute(strings.NewReader(`{"protocol_version":1,"action":"inspect"}`), config)
	if errorCode(err) != "state_invalid" {
		t.Fatalf("corrupt history inspect error = %v", err)
	}
	if len(runner.snapshot()) != baseline {
		t.Fatal("corrupt history invoked manage")
	}
}

func assertMode(t *testing.T, path string, expected os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != expected {
		t.Fatalf("mode for %q: info=%v err=%v", path, info, err)
	}
}
