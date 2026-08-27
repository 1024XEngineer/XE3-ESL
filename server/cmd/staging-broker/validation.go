package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"
)

const (
	manifestLimit      = 16 * 1024
	engineReceiptLimit = 16 * 1024
	storedReceiptLimit = 64 * 1024
	maxSafeInteger     = int64(9007199254740991)
	maxJSONDepth       = 64
)

var (
	positiveIntegerPattern = regexp.MustCompile(`^[1-9][0-9]*$`)
	sha256Pattern          = regexp.MustCompile(`^[0-9a-f]{64}$`)
	gitSHAPattern          = regexp.MustCompile(`^[0-9a-f]{40}$`)
	digestPattern          = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	versionPattern         = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	containerIDPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type request struct {
	Action                       string
	CandidateRunID               int64
	DeploymentRunID              int64
	DeploymentRunAttempt         int64
	ExpectedCurrentReceiptSHA256 *string
	ManifestSHA256               string
	Manifest                     []byte
	TargetReceiptSHA256          string
	BundleManifestSHA256         string
}

type releaseManifest struct {
	SHA256                string
	Version               string
	VersionCode           int64
	GitSHA                string
	DatabaseSchemaVersion int64
	PortalImageDigest     string
	ServerImageDigest     string
	StagingAPKFile        string
	StagingAPKSHA256      string
	APKCertificateSHA256  string
}

type engineReceipt struct {
	ManifestSHA256        string
	Version               string
	GitSHA                string
	DatabaseSchemaVersion int64
	PortalImageDigest     string
	ServerImageDigest     string
	PortalContainerID     string
	ServerContainerID     string
	PostgresContainerID   string
	DeployedAtUTC         string
}

type brokerReceipt struct {
	ReceiptVersion              int     `json:"receipt_version"`
	Environment                 string  `json:"environment"`
	Operation                   string  `json:"operation"`
	Repository                  string  `json:"repository"`
	ManifestSHA256              string  `json:"manifest_sha256"`
	Version                     string  `json:"version"`
	VersionCode                 int64   `json:"version_code"`
	GitSHA                      string  `json:"git_sha"`
	DatabaseSchemaVersion       int64   `json:"database_schema_version"`
	PortalImageDigest           string  `json:"portal_image_digest"`
	ServerImageDigest           string  `json:"server_image_digest"`
	PortalContainerID           string  `json:"portal_container_id"`
	ServerContainerID           string  `json:"server_container_id"`
	PostgresContainerID         string  `json:"postgres_container_id"`
	CandidateRunID              int64   `json:"candidate_run_id"`
	DeploymentRunID             int64   `json:"deployment_run_id"`
	DeploymentRunAttempt        int64   `json:"deployment_run_attempt"`
	PreviousReceiptSHA256       *string `json:"previous_receipt_sha256"`
	RollbackTargetReceiptSHA256 *string `json:"rollback_target_receipt_sha256"`
	RecordedAtUTC               string  `json:"recorded_at_utc"`
}

func parseRequest(input io.Reader, repository string) (request, error) {
	contents, err := io.ReadAll(io.LimitReader(input, requestLimit+1))
	if err != nil {
		return request{}, failure("invalid_request")
	}
	if len(contents) > requestLimit {
		return request{}, failure("request_too_large")
	}
	value, err := decodeStrictJSON(contents)
	if err != nil {
		return request{}, failure("invalid_request")
	}
	object, ok := value.(map[string]any)
	if !ok {
		return request{}, failure("invalid_request")
	}
	if integer, ok := integerValue(object["protocol_version"]); !ok || integer != protocolVersion {
		return request{}, failure("invalid_request")
	}
	action, ok := object["action"].(string)
	if !ok {
		return request{}, failure("invalid_request")
	}

	parsed := request{Action: action}
	switch action {
	case "inspect":
		if !hasExactKeys(object, "protocol_version", "action") {
			return request{}, failure("invalid_request")
		}
		return parsed, nil
	case "deploy":
		if !hasExactKeys(object,
			"protocol_version",
			"action",
			"repository",
			"candidate_run_id",
			"deployment_run_id",
			"deployment_run_attempt",
			"expected_current_receipt_sha256",
			"manifest_sha256",
			"manifest_base64",
		) {
			return request{}, failure("invalid_request")
		}
		if object["repository"] != repository {
			return request{}, failure("invalid_request")
		}
		if parsed.CandidateRunID, ok = positiveSafeInteger(object["candidate_run_id"]); !ok {
			return request{}, failure("invalid_request")
		}
		if parsed.DeploymentRunID, ok = positiveSafeInteger(object["deployment_run_id"]); !ok {
			return request{}, failure("invalid_request")
		}
		if parsed.DeploymentRunAttempt, ok = positiveSafeInteger(object["deployment_run_attempt"]); !ok {
			return request{}, failure("invalid_request")
		}
		if parsed.ExpectedCurrentReceiptSHA256, ok = optionalSHA256(object["expected_current_receipt_sha256"]); !ok {
			return request{}, failure("invalid_request")
		}
		manifestSHA, ok := object["manifest_sha256"].(string)
		if !ok || !validNonzeroSHA256(manifestSHA) {
			return request{}, failure("invalid_request")
		}
		encodedManifest, ok := object["manifest_base64"].(string)
		if !ok || len(encodedManifest) > base64.StdEncoding.EncodedLen(manifestLimit) {
			return request{}, failure("invalid_request")
		}
		manifest, err := base64.StdEncoding.Strict().DecodeString(encodedManifest)
		if err != nil || len(manifest) > manifestLimit || base64.StdEncoding.EncodeToString(manifest) != encodedManifest {
			return request{}, failure("invalid_request")
		}
		if sha256Bytes(manifest) != manifestSHA {
			return request{}, failure("invalid_request")
		}
		parsed.ManifestSHA256 = manifestSHA
		parsed.Manifest = manifest
		return parsed, nil
	case "rollback":
		if !hasExactKeys(object,
			"protocol_version",
			"action",
			"repository",
			"deployment_run_id",
			"deployment_run_attempt",
			"expected_current_receipt_sha256",
			"target_receipt_sha256",
		) {
			return request{}, failure("invalid_request")
		}
		if object["repository"] != repository {
			return request{}, failure("invalid_request")
		}
		if parsed.DeploymentRunID, ok = positiveSafeInteger(object["deployment_run_id"]); !ok {
			return request{}, failure("invalid_request")
		}
		if parsed.DeploymentRunAttempt, ok = positiveSafeInteger(object["deployment_run_attempt"]); !ok {
			return request{}, failure("invalid_request")
		}
		expected, ok := object["expected_current_receipt_sha256"].(string)
		if !ok || !validNonzeroSHA256(expected) {
			return request{}, failure("invalid_request")
		}
		target, ok := object["target_receipt_sha256"].(string)
		if !ok || !validNonzeroSHA256(target) {
			return request{}, failure("invalid_request")
		}
		parsed.ExpectedCurrentReceiptSHA256 = &expected
		parsed.TargetReceiptSHA256 = target
		return parsed, nil
	default:
		return request{}, failure("invalid_request")
	}
}

func validateManifest(contents []byte, candidateRunID int64) (releaseManifest, error) {
	if len(contents) == 0 || len(contents) > manifestLimit {
		return releaseManifest{}, failure("invalid_request")
	}
	value, err := decodeStrictJSON(contents)
	if err != nil {
		return releaseManifest{}, failure("invalid_request")
	}
	object, ok := value.(map[string]any)
	if !ok || !hasExactKeys(object,
		"manifest_version",
		"version",
		"git_sha",
		"version_code",
		"portal_image",
		"portal_image_digest",
		"server_image",
		"server_image_digest",
		"staging_apk_file",
		"staging_apk_sha256",
		"production_apk_file",
		"production_apk_size_bytes",
		"production_apk_sha256",
		"application_id",
		"minimum_android_api",
		"abis",
		"apk_certificate_sha256",
		"database_schema_version",
		"quality_run_url",
	) {
		return releaseManifest{}, failure("invalid_request")
	}
	manifestVersion, ok := integerValue(object["manifest_version"])
	if !ok || manifestVersion != 1 {
		return releaseManifest{}, failure("invalid_request")
	}
	version, ok := object["version"].(string)
	if !ok || !versionPattern.MatchString(version) {
		return releaseManifest{}, failure("invalid_request")
	}
	versionCode, ok := positiveSafeInteger(object["version_code"])
	if !ok {
		return releaseManifest{}, failure("invalid_request")
	}
	gitSHA, ok := object["git_sha"].(string)
	if !ok || !validNonzeroHex(gitSHA, gitSHAPattern) {
		return releaseManifest{}, failure("invalid_request")
	}
	portalDigest, ok := object["portal_image_digest"].(string)
	if object["portal_image"] != "ghcr.io/1024xengineer/xe3-esl-portal" || !ok || !validDigest(portalDigest) {
		return releaseManifest{}, failure("invalid_request")
	}
	serverDigest, ok := object["server_image_digest"].(string)
	if object["server_image"] != "ghcr.io/1024xengineer/xe3-esl-server" || !ok || !validDigest(serverDigest) {
		return releaseManifest{}, failure("invalid_request")
	}
	stagingAPKFile, ok := object["staging_apk_file"].(string)
	if !ok || stagingAPKFile != "speakup-v"+version+"-staging-arm64.apk" {
		return releaseManifest{}, failure("invalid_request")
	}
	productionAPKFile, ok := object["production_apk_file"].(string)
	if !ok || productionAPKFile != "speakup-v"+version+"-production-arm64.apk" {
		return releaseManifest{}, failure("invalid_request")
	}
	if !stringIsNonzeroSHA256(object["staging_apk_sha256"]) ||
		!stringIsNonzeroSHA256(object["production_apk_sha256"]) ||
		!stringIsNonzeroSHA256(object["apk_certificate_sha256"]) {
		return releaseManifest{}, failure("invalid_request")
	}
	if _, ok := positiveSafeInteger(object["production_apk_size_bytes"]); !ok {
		return releaseManifest{}, failure("invalid_request")
	}
	if object["application_id"] != "com.xengineer.speakup" {
		return releaseManifest{}, failure("invalid_request")
	}
	minimumAPI, ok := integerValue(object["minimum_android_api"])
	if !ok || minimumAPI != 24 {
		return releaseManifest{}, failure("invalid_request")
	}
	abis, ok := object["abis"].([]any)
	if !ok || len(abis) != 1 || abis[0] != "arm64-v8a" {
		return releaseManifest{}, failure("invalid_request")
	}
	schemaVersion, ok := positiveSafeInteger(object["database_schema_version"])
	if !ok {
		return releaseManifest{}, failure("invalid_request")
	}
	qualityRunURL, ok := object["quality_run_url"].(string)
	if !ok || qualityRunURL != "https://github.com/1024XEngineer/XE3-ESL/actions/runs/"+strconv.FormatInt(candidateRunID, 10) {
		return releaseManifest{}, failure("invalid_request")
	}

	return releaseManifest{
		SHA256:                sha256Bytes(contents),
		Version:               version,
		VersionCode:           versionCode,
		GitSHA:                gitSHA,
		DatabaseSchemaVersion: schemaVersion,
		PortalImageDigest:     portalDigest,
		ServerImageDigest:     serverDigest,
		StagingAPKFile:        stagingAPKFile,
		StagingAPKSHA256:      object["staging_apk_sha256"].(string),
		APKCertificateSHA256:  object["apk_certificate_sha256"].(string),
	}, nil
}

func parseBrokerReceipt(contents []byte) (brokerReceipt, error) {
	value, err := decodeStrictJSON(contents)
	if err != nil {
		return brokerReceipt{}, failure("state_invalid")
	}
	object, ok := value.(map[string]any)
	if !ok || !hasExactKeys(object,
		"receipt_version",
		"environment",
		"operation",
		"repository",
		"manifest_sha256",
		"version",
		"version_code",
		"git_sha",
		"database_schema_version",
		"portal_image_digest",
		"server_image_digest",
		"portal_container_id",
		"server_container_id",
		"postgres_container_id",
		"candidate_run_id",
		"deployment_run_id",
		"deployment_run_attempt",
		"previous_receipt_sha256",
		"rollback_target_receipt_sha256",
		"recorded_at_utc",
	) {
		return brokerReceipt{}, failure("state_invalid")
	}
	receiptVersion, ok := integerValue(object["receipt_version"])
	if !ok || receiptVersion != 2 || object["environment"] != "staging" || object["repository"] != officialRepository {
		return brokerReceipt{}, failure("state_invalid")
	}
	operation, ok := object["operation"].(string)
	if !ok || (operation != "deploy" && operation != "rollback") {
		return brokerReceipt{}, failure("state_invalid")
	}
	manifestSHA, ok := object["manifest_sha256"].(string)
	if !ok || !validNonzeroSHA256(manifestSHA) {
		return brokerReceipt{}, failure("state_invalid")
	}
	version, ok := object["version"].(string)
	if !ok || !versionPattern.MatchString(version) {
		return brokerReceipt{}, failure("state_invalid")
	}
	versionCode, ok := positiveSafeInteger(object["version_code"])
	if !ok {
		return brokerReceipt{}, failure("state_invalid")
	}
	gitSHA, ok := object["git_sha"].(string)
	if !ok || !validNonzeroHex(gitSHA, gitSHAPattern) {
		return brokerReceipt{}, failure("state_invalid")
	}
	schemaVersion, ok := positiveSafeInteger(object["database_schema_version"])
	if !ok {
		return brokerReceipt{}, failure("state_invalid")
	}
	portalDigest, portalOK := object["portal_image_digest"].(string)
	serverDigest, serverOK := object["server_image_digest"].(string)
	if !portalOK || !serverOK || !validDigest(portalDigest) || !validDigest(serverDigest) {
		return brokerReceipt{}, failure("state_invalid")
	}
	portalID, portalOK := object["portal_container_id"].(string)
	serverID, serverOK := object["server_container_id"].(string)
	postgresID, postgresOK := object["postgres_container_id"].(string)
	if !portalOK || !serverOK || !postgresOK ||
		!validContainerID(portalID) || !validContainerID(serverID) || !validContainerID(postgresID) ||
		portalID == serverID || portalID == postgresID || serverID == postgresID {
		return brokerReceipt{}, failure("state_invalid")
	}
	candidateRunID, ok := positiveSafeInteger(object["candidate_run_id"])
	if !ok {
		return brokerReceipt{}, failure("state_invalid")
	}
	deploymentRunID, ok := positiveSafeInteger(object["deployment_run_id"])
	if !ok {
		return brokerReceipt{}, failure("state_invalid")
	}
	deploymentRunAttempt, ok := positiveSafeInteger(object["deployment_run_attempt"])
	if !ok {
		return brokerReceipt{}, failure("state_invalid")
	}
	previous, ok := optionalSHA256(object["previous_receipt_sha256"])
	if !ok {
		return brokerReceipt{}, failure("state_invalid")
	}
	rollbackTarget, ok := optionalSHA256(object["rollback_target_receipt_sha256"])
	if !ok || (operation == "deploy" && rollbackTarget != nil) || (operation == "rollback" && (rollbackTarget == nil || previous == nil)) {
		return brokerReceipt{}, failure("state_invalid")
	}
	recordedAt, ok := object["recorded_at_utc"].(string)
	if !ok || !validUTCTimestamp(recordedAt) {
		return brokerReceipt{}, failure("state_invalid")
	}

	return brokerReceipt{
		ReceiptVersion:              2,
		Environment:                 "staging",
		Operation:                   operation,
		Repository:                  officialRepository,
		ManifestSHA256:              manifestSHA,
		Version:                     version,
		VersionCode:                 versionCode,
		GitSHA:                      gitSHA,
		DatabaseSchemaVersion:       schemaVersion,
		PortalImageDigest:           portalDigest,
		ServerImageDigest:           serverDigest,
		PortalContainerID:           portalID,
		ServerContainerID:           serverID,
		PostgresContainerID:         postgresID,
		CandidateRunID:              candidateRunID,
		DeploymentRunID:             deploymentRunID,
		DeploymentRunAttempt:        deploymentRunAttempt,
		PreviousReceiptSHA256:       previous,
		RollbackTargetReceiptSHA256: rollbackTarget,
		RecordedAtUTC:               recordedAt,
	}, nil
}

func loadEngineReceipt(path string, manifest releaseManifest) (engineReceipt, error) {
	contents, err := readSecureFile(path, 0o444, engineReceiptLimit)
	if err != nil {
		return engineReceipt{}, failure("operation_failed")
	}
	value, err := decodeStrictJSON(contents)
	if err != nil {
		return engineReceipt{}, failure("operation_failed")
	}
	object, ok := value.(map[string]any)
	if !ok || !hasExactKeys(object,
		"receipt_version",
		"manifest_sha256",
		"version",
		"git_sha",
		"database_schema_version",
		"portal_image_digest",
		"server_image_digest",
		"portal_container_id",
		"server_container_id",
		"postgres_container_id",
		"deployed_at_utc",
	) {
		return engineReceipt{}, failure("operation_failed")
	}
	receiptVersion, ok := integerValue(object["receipt_version"])
	if !ok || receiptVersion != 1 || object["manifest_sha256"] != manifest.SHA256 ||
		object["version"] != manifest.Version || object["git_sha"] != manifest.GitSHA ||
		object["portal_image_digest"] != manifest.PortalImageDigest || object["server_image_digest"] != manifest.ServerImageDigest {
		return engineReceipt{}, failure("operation_failed")
	}
	schemaVersion, ok := positiveSafeInteger(object["database_schema_version"])
	if !ok || schemaVersion != manifest.DatabaseSchemaVersion {
		return engineReceipt{}, failure("operation_failed")
	}
	portalID, portalOK := object["portal_container_id"].(string)
	serverID, serverOK := object["server_container_id"].(string)
	postgresID, postgresOK := object["postgres_container_id"].(string)
	if !portalOK || !serverOK || !postgresOK ||
		!validContainerID(portalID) || !validContainerID(serverID) || !validContainerID(postgresID) ||
		portalID == serverID || portalID == postgresID || serverID == postgresID {
		return engineReceipt{}, failure("operation_failed")
	}
	deployedAt, ok := object["deployed_at_utc"].(string)
	if !ok || !validUTCTimestamp(deployedAt) {
		return engineReceipt{}, failure("operation_failed")
	}
	return engineReceipt{
		ManifestSHA256:        manifest.SHA256,
		Version:               manifest.Version,
		GitSHA:                manifest.GitSHA,
		DatabaseSchemaVersion: schemaVersion,
		PortalImageDigest:     manifest.PortalImageDigest,
		ServerImageDigest:     manifest.ServerImageDigest,
		PortalContainerID:     portalID,
		ServerContainerID:     serverID,
		PostgresContainerID:   postgresID,
		DeployedAtUTC:         deployedAt,
	}, nil
}

func decodeStrictJSON(contents []byte) (any, error) {
	if !utf8.Valid(contents) {
		return nil, failure("invalid_json")
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	value, err := decodeJSONValue(decoder, 0)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return nil, failure("invalid_json")
		}
		return nil, err
	}
	return value, nil
}

func decodeJSONValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > maxJSONDepth {
		return nil, failure("invalid_json")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return token, nil
	}
	switch delimiter {
	case '{':
		object := make(map[string]any)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, failure("invalid_json")
			}
			if _, duplicate := object[key]; duplicate {
				return nil, failure("duplicate_key")
			}
			value, err := decodeJSONValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return nil, failure("invalid_json")
		}
		return object, nil
	case '[':
		array := make([]any, 0)
		for decoder.More() {
			value, err := decodeJSONValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return nil, failure("invalid_json")
		}
		return array, nil
	default:
		return nil, failure("invalid_json")
	}
}

func hasExactKeys(object map[string]any, keys ...string) bool {
	if len(object) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			return false
		}
	}
	return true
}

func integerValue(value any) (int64, bool) {
	number, ok := value.(json.Number)
	if !ok || !positiveIntegerPattern.MatchString(number.String()) && number.String() != "0" {
		return 0, false
	}
	integer, err := strconv.ParseInt(number.String(), 10, 64)
	return integer, err == nil
}

func positiveSafeInteger(value any) (int64, bool) {
	integer, ok := integerValue(value)
	return integer, ok && integer >= 1 && integer <= maxSafeInteger
}

func optionalSHA256(value any) (*string, bool) {
	if value == nil {
		return nil, true
	}
	text, ok := value.(string)
	if !ok || !validNonzeroSHA256(text) {
		return nil, false
	}
	return &text, true
}

func validNonzeroSHA256(value string) bool {
	return validNonzeroHex(value, sha256Pattern)
}

func stringIsNonzeroSHA256(value any) bool {
	text, ok := value.(string)
	return ok && validNonzeroSHA256(text)
}

func validNonzeroHex(value string, pattern *regexp.Regexp) bool {
	if !pattern.MatchString(value) {
		return false
	}
	for _, character := range value {
		if character != '0' {
			return true
		}
	}
	return false
}

func validDigest(value string) bool {
	if !digestPattern.MatchString(value) {
		return false
	}
	return validNonzeroSHA256(value[len("sha256:"):])
}

func validContainerID(value string) bool {
	return validNonzeroHex(value, containerIDPattern)
}

func validUTCTimestamp(value string) bool {
	parsed, err := time.Parse("2006-01-02T15:04:05Z", value)
	return err == nil && parsed.UTC().Format("2006-01-02T15:04:05Z") == value
}

func sha256Bytes(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}
