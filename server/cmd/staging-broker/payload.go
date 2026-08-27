package main

import (
	"archive/tar"
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	publishProtocolVersion = 2
	publishArchiveLimit    = int64(256 * 1024 * 1024)
	publishAPKLimit        = int64(250 * 1024 * 1024)
	publishMetadataLimit   = int64(128 * 1024)
)

type publicationPayload struct {
	root  string
	files map[string]string
}

type parsedEnvelope struct {
	request request
	payload *publicationPayload
}

func (envelope parsedEnvelope) close() {
	if envelope.payload != nil {
		_ = os.RemoveAll(envelope.payload.root)
	}
}

type envelopeResult struct {
	envelope parsedEnvelope
	err      error
}

func parseEnvelopeWithTimeouts(input io.Reader, repository string, incomingDir string, jsonTimeout time.Duration, publishTimeout time.Duration) (parsedEnvelope, error) {
	buffered := bufio.NewReader(input)
	jsonEnvelope, err := detectJSONEnvelope(input, buffered, jsonTimeout)
	if err != nil {
		return parsedEnvelope{}, err
	}
	timeout := publishTimeout
	if jsonEnvelope {
		timeout = jsonTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result := make(chan envelopeResult)
	go func() {
		envelope, err := parseEnvelopeBody(buffered, repository, incomingDir, jsonEnvelope)
		select {
		case result <- envelopeResult{envelope: envelope, err: err}:
		case <-ctx.Done():
			envelope.close()
		}
	}()

	select {
	case parsed := <-result:
		if ctx.Err() != nil {
			parsed.envelope.close()
			return parsedEnvelope{}, failure("operation_timeout")
		}
		return parsed.envelope, parsed.err
	case <-ctx.Done():
		if closer, ok := input.(io.Closer); ok {
			_ = closer.Close()
		}
		return parsedEnvelope{}, failure("operation_timeout")
	}
}

type envelopeKindResult struct {
	json bool
	err  error
}

func detectJSONEnvelope(input io.Reader, buffered *bufio.Reader, timeout time.Duration) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result := make(chan envelopeKindResult)
	go func() {
		first, err := buffered.Peek(1)
		if err != nil {
			select {
			case result <- envelopeKindResult{err: failure("invalid_request")}:
			case <-ctx.Done():
			}
			return
		}
		value := first[0]
		isJSON := value == '{' || value == ' ' || value == '\t' || value == '\r' || value == '\n'
		select {
		case result <- envelopeKindResult{json: isJSON}:
		case <-ctx.Done():
		}
	}()
	select {
	case detected := <-result:
		return detected.json, detected.err
	case <-ctx.Done():
		if closer, ok := input.(io.Closer); ok {
			_ = closer.Close()
		}
		return false, failure("operation_timeout")
	}
}

func parseEnvelopeBody(buffered *bufio.Reader, repository string, incomingDir string, jsonEnvelope bool) (parsedEnvelope, error) {
	if jsonEnvelope {
		parsed, err := parseRequest(buffered, repository)
		return parsedEnvelope{request: parsed}, err
	}

	payload, err := readPublicationPayload(buffered, incomingDir)
	if err != nil {
		return parsedEnvelope{}, err
	}
	envelope := parsedEnvelope{payload: &payload}
	requestContents, err := os.ReadFile(payload.files["request.json"])
	if err != nil {
		envelope.close()
		return parsedEnvelope{}, failure("invalid_request")
	}
	parsed, err := parsePublishRequest(requestContents, repository)
	if err != nil {
		envelope.close()
		return parsedEnvelope{}, err
	}
	envelope.request = parsed
	return envelope, nil
}

func readPublicationPayload(input io.Reader, incomingDir string) (publicationPayload, error) {
	root, err := os.MkdirTemp(incomingDir, ".publish-")
	if err != nil {
		return publicationPayload{}, failure("state_invalid")
	}
	if err := os.Chmod(root, 0o700); err != nil {
		_ = os.RemoveAll(root)
		return publicationPayload{}, failure("state_invalid")
	}
	fail := func(code string) (publicationPayload, error) {
		_ = os.RemoveAll(root)
		return publicationPayload{}, failure(code)
	}

	limited := &io.LimitedReader{R: input, N: publishArchiveLimit + 1}
	reader := tar.NewReader(limited)
	payload := publicationPayload{root: root, files: make(map[string]string)}
	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil || header == nil || header.Typeflag != tar.TypeReg || header.Size <= 0 {
			return fail("invalid_request")
		}
		if !validPublicationPayloadName(header.Name) || header.Size > publicationFileLimit(header.Name) {
			return fail("invalid_request")
		}
		if _, duplicate := payload.files[header.Name]; duplicate {
			return fail("invalid_request")
		}
		target := filepath.Join(root, filepath.FromSlash(header.Name))
		if !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			return fail("invalid_request")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return fail("state_invalid")
		}
		file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return fail("state_invalid")
		}
		written, copyErr := io.CopyN(file, reader, header.Size)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil || written != header.Size {
			return fail("invalid_request")
		}
		payload.files[header.Name] = target
	}

	trailing, err := io.ReadAll(limited)
	if err != nil {
		return fail("invalid_request")
	}
	consumed := publishArchiveLimit + 1 - limited.N
	if consumed > publishArchiveLimit {
		return fail("request_too_large")
	}
	for _, value := range trailing {
		if value != 0 {
			return fail("invalid_request")
		}
	}
	if len(payload.files) != 6 || payload.files["request.json"] == "" || payload.files["release-manifest.json"] == "" || payload.files["bundle/bundle-manifest.json"] == "" {
		return fail("invalid_request")
	}
	return payload, nil
}

func validPublicationPayloadName(name string) bool {
	if name == "request.json" || name == "release-manifest.json" || name == "bundle/bundle-manifest.json" {
		return true
	}
	if filepath.ToSlash(filepath.Clean(name)) != name || strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) != 6 || parts[0] != "bundle" || parts[1] != "downloads" || parts[2] != "android" || parts[3] != "candidates" || !positiveIntegerPattern.MatchString(parts[4]) {
		return false
	}
	if parts[5] == "candidate.json" {
		return true
	}
	name = parts[5]
	if strings.HasSuffix(name, ".sha256") {
		name = strings.TrimSuffix(name, ".sha256")
	}
	if !strings.HasPrefix(name, "speakup-v") || !strings.HasSuffix(name, "-staging-arm64.apk") {
		return false
	}
	version := strings.TrimSuffix(strings.TrimPrefix(name, "speakup-v"), "-staging-arm64.apk")
	return versionPattern.MatchString(version)
}

func publicationFileLimit(name string) int64 {
	if strings.HasSuffix(name, ".apk") {
		return publishAPKLimit
	}
	return publishMetadataLimit
}

func parsePublishRequest(contents []byte, repository string) (request, error) {
	if len(contents) == 0 || int64(len(contents)) > publishMetadataLimit {
		return request{}, failure("invalid_request")
	}
	value, err := decodeStrictJSON(contents)
	if err != nil {
		return request{}, failure("invalid_request")
	}
	object, ok := value.(map[string]any)
	if !ok || !hasExactKeys(object,
		"protocol_version", "action", "repository", "candidate_run_id",
		"deployment_run_id", "deployment_run_attempt", "expected_runtime_receipt_sha256",
		"manifest_sha256", "bundle_manifest_sha256",
	) {
		return request{}, failure("invalid_request")
	}
	version, ok := integerValue(object["protocol_version"])
	if !ok || version != publishProtocolVersion || object["action"] != "publish" || object["repository"] != repository {
		return request{}, failure("invalid_request")
	}
	parsed := request{Action: "publish"}
	if parsed.CandidateRunID, ok = positiveSafeInteger(object["candidate_run_id"]); !ok {
		return request{}, failure("invalid_request")
	}
	if parsed.DeploymentRunID, ok = positiveSafeInteger(object["deployment_run_id"]); !ok {
		return request{}, failure("invalid_request")
	}
	if parsed.DeploymentRunAttempt, ok = positiveSafeInteger(object["deployment_run_attempt"]); !ok {
		return request{}, failure("invalid_request")
	}
	runtimeSHA, ok := object["expected_runtime_receipt_sha256"].(string)
	if !ok || !validNonzeroSHA256(runtimeSHA) {
		return request{}, failure("invalid_request")
	}
	parsed.ExpectedCurrentReceiptSHA256 = &runtimeSHA
	manifestSHA, ok := object["manifest_sha256"].(string)
	if !ok || !validNonzeroSHA256(manifestSHA) {
		return request{}, failure("invalid_request")
	}
	parsed.ManifestSHA256 = manifestSHA
	bundleSHA, ok := object["bundle_manifest_sha256"].(string)
	if !ok || !validNonzeroSHA256(bundleSHA) {
		return request{}, failure("invalid_request")
	}
	parsed.BundleManifestSHA256 = bundleSHA
	return parsed, nil
}
