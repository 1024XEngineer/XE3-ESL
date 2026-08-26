package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	protocolVersion      = 1
	officialRepository   = "1024XEngineer/XE3-ESL"
	productionManagePath = "/opt/xe3-speakup-production-control/current/deploy/production/manage.sh"
	androidManagePath    = "/opt/xe3-speakup-production-control/current/deploy/android-download/manage.sh"
	productionEnvFile    = "/etc/speakup/production.env"
	productionPublicRoot = "/var/www/speakup-production-public"
	productionStateDir   = "/var/lib/speakup/production-broker"
	productionLockFile   = "/run/lock/xe3-speakup-production-broker/broker.lock"
	productionPATH       = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	requestLimit         = int64(256 * 1024 * 1024)
	metadataLimit        = int64(128 * 1024)
	apkLimit             = int64(250 * 1024 * 1024)
	maxJSONDepth         = 64
	maxSafeInteger       = int64(9007199254740991)
)

var (
	sha256Pattern         = regexp.MustCompile(`^[0-9a-f]{64}$`)
	gitSHAPattern         = regexp.MustCompile(`^[0-9a-f]{40}$`)
	digestPattern         = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	versionPattern        = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	containerIDPattern    = regexp.MustCompile(`^[0-9a-f]{12,64}$`)
	postgresBackupPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-predeploy$`)
	portalBackupPattern   = regexp.MustCompile(`^[0-9]{8}T[0-9]{9}Z$`)
)

type commandRunner func(context.Context, string, []string, []string) error

type Config struct {
	Repository     string
	ManagePath     string
	AndroidPath    string
	Environment    string
	PublicRoot     string
	StateDir       string
	LockFile       string
	PATH           string
	RequestTimeout time.Duration
	DeployTimeout  time.Duration
	Now            func() time.Time
	RunCommand     commandRunner
	OwnerUID       uint32
}

type brokerError struct{ code string }

func (e *brokerError) Error() string { return e.code }
func failure(code string) error      { return &brokerError{code: code} }

func errorCode(err error) string {
	var target *brokerError
	if errors.As(err, &target) {
		return target.code
	}
	return "internal_error"
}

type request struct {
	Action                       string
	Repository                   string
	CandidateRunID               int64
	StagingRunID                 int64
	StagingRunAttempt            int64
	DeploymentRunID              int64
	DeploymentRunAttempt         int64
	ExpectedCurrentReceiptSHA256 *string
	ManifestSHA256               string
	StagingReceiptSHA256         string
	BundleManifestSHA256         string
	Version                      string
}

type releaseManifest struct {
	SHA256                string
	Version               string
	VersionCode           int64
	GitSHA                string
	DatabaseSchemaVersion int64
	PortalImageDigest     string
	ServerImageDigest     string
	ProductionAPKFile     string
	ProductionAPKSize     int64
	ProductionAPKSHA256   string
	APKCertificateSHA256  string
}

type stagingReceipt struct {
	ManifestSHA256        string
	Version               string
	VersionCode           int64
	GitSHA                string
	DatabaseSchemaVersion int64
	PortalImageDigest     string
	ServerImageDigest     string
	CandidateRunID        int64
	DeploymentRunID       int64
	DeploymentRunAttempt  int64
}

type engineReceipt struct {
	ManifestSHA256              string
	Version                     string
	VersionCode                 int64
	GitSHA                      string
	DatabaseSchemaVersion       int64
	PortalImageDigest           string
	ServerImageDigest           string
	ProductionAPKFile           string
	ProductionAPKSize           int64
	ProductionAPKSHA256         string
	APKCertificateSHA256        string
	PortalContainerID           string
	ServerContainerID           string
	PostgresContainerID         string
	PostgresBackupID            *string
	PortalBackupID              *string
	AndroidBundleManifestSHA256 *string
	PreviousReceiptSHA256       *string
	RecordedAtUTC               string
	Operation                   string
}

type auditReceipt struct {
	ReceiptVersion                        int     `json:"receipt_version"`
	Environment                           string  `json:"environment"`
	Operation                             string  `json:"operation"`
	Repository                            string  `json:"repository"`
	ManifestSHA256                        string  `json:"manifest_sha256"`
	Version                               string  `json:"version"`
	VersionCode                           int64   `json:"version_code"`
	GitSHA                                string  `json:"git_sha"`
	DatabaseSchemaVersion                 int64   `json:"database_schema_version"`
	PortalImageDigest                     string  `json:"portal_image_digest"`
	ServerImageDigest                     string  `json:"server_image_digest"`
	ProductionAPKFile                     string  `json:"production_apk_file"`
	ProductionAPKSHA256                   string  `json:"production_apk_sha256"`
	APKCertificateSHA256                  string  `json:"apk_certificate_sha256"`
	PortalContainerID                     string  `json:"portal_container_id"`
	ServerContainerID                     string  `json:"server_container_id"`
	PostgresContainerID                   string  `json:"postgres_container_id"`
	PostgresBackupID                      *string `json:"postgres_backup_id"`
	PortalBackupID                        *string `json:"portal_backup_id"`
	AndroidBundleManifestSHA256           *string `json:"android_bundle_manifest_sha256"`
	CandidateRunID                        *int64  `json:"candidate_run_id"`
	StagingRunID                          *int64  `json:"staging_run_id"`
	StagingRunAttempt                     *int64  `json:"staging_run_attempt"`
	StagingReceiptSHA256                  *string `json:"staging_receipt_sha256"`
	DeploymentRunID                       *int64  `json:"deployment_run_id"`
	DeploymentRunAttempt                  *int64  `json:"deployment_run_attempt"`
	PreviousReceiptSHA256                 *string `json:"previous_receipt_sha256"`
	ProductionEngineReceiptSHA256         string  `json:"production_engine_receipt_sha256"`
	ProductionEnginePreviousReceiptSHA256 *string `json:"production_engine_previous_receipt_sha256"`
	RecordedAtUTC                         string  `json:"recorded_at_utc"`
}

type response struct {
	ProtocolVersion      int          `json:"protocol_version"`
	OK                   bool         `json:"ok"`
	Action               string       `json:"action"`
	CurrentReceiptSHA256 string       `json:"current_receipt_sha256"`
	Receipt              auditReceipt `json:"receipt"`
}

type errorResponse struct {
	ProtocolVersion int    `json:"protocol_version"`
	OK              bool   `json:"ok"`
	Error           string `json:"error"`
}

func main() {
	config, err := productionConfig()
	if err != nil {
		_ = writeJSON(os.Stdout, errorResponse{ProtocolVersion: protocolVersion, Error: errorCode(err)})
		os.Exit(1)
	}
	if len(os.Args) == 6 && os.Args[1] == "initialize" && os.Args[2] == "--manifest" && os.Args[4] == "--receipt" && os.Getenv("SSH_CONNECTION") == "" && os.Getenv("SSH_ORIGINAL_COMMAND") == "" {
		if err := initializeState(config, os.Args[3], os.Args[5]); err != nil {
			fmt.Fprintln(os.Stderr, errorCode(err))
			os.Exit(1)
		}
		return
	}
	os.Exit(runCLI(os.Args, os.Getenv("SSH_ORIGINAL_COMMAND"), os.Stdin, os.Stdout, config))
}

func productionConfig() (Config, error) {
	if os.Geteuid() != 0 {
		return Config{}, failure("invalid_runtime_identity")
	}
	account, err := user.LookupId("0")
	if err != nil || account.Uid != "0" {
		return Config{}, failure("invalid_runtime_identity")
	}
	return Config{
		Repository:     officialRepository,
		ManagePath:     productionManagePath,
		AndroidPath:    androidManagePath,
		Environment:    productionEnvFile,
		PublicRoot:     productionPublicRoot,
		StateDir:       productionStateDir,
		LockFile:       productionLockFile,
		PATH:           productionPATH,
		RequestTimeout: 30 * time.Second,
		DeployTimeout:  20 * time.Minute,
		Now:            time.Now,
		RunCommand:     runCommand,
		OwnerUID:       0,
	}, nil
}

func runCLI(arguments []string, originalCommand string, input io.Reader, output io.Writer, config Config) int {
	if len(arguments) != 1 || originalCommand != "" {
		_ = writeJSON(output, errorResponse{ProtocolVersion: protocolVersion, Error: "invalid_invocation"})
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), config.RequestTimeout+config.DeployTimeout)
	defer cancel()
	result, err := execute(ctx, input, config)
	if err != nil {
		_ = writeJSON(output, errorResponse{ProtocolVersion: protocolVersion, Error: errorCode(err)})
		return 1
	}
	if err := writeJSON(output, result); err != nil {
		return 1
	}
	return 0
}

func execute(ctx context.Context, input io.Reader, config Config) (response, error) {
	if err := validateConfig(config); err != nil {
		return response{}, err
	}
	store, err := openStateStore(config.StateDir, config.OwnerUID)
	if err != nil {
		return response{}, err
	}
	lock, err := acquireLock(ctx, config.LockFile, config.OwnerUID)
	if err != nil {
		return response{}, failure("operation_timeout")
	}
	defer lock.close()

	payload, err := readPayload(input, store.incomingDir)
	if err != nil {
		return response{}, err
	}
	defer os.RemoveAll(payload.root)
	requestContents, err := os.ReadFile(payload.files["request.json"])
	if err != nil {
		return response{}, failure("invalid_request")
	}
	req, err := parseRequest(requestContents)
	if err != nil {
		return response{}, err
	}
	current, currentSHA, err := store.loadCurrent()
	if err != nil {
		return response{}, err
	}
	if current == nil {
		return response{}, failure("state_uninitialized")
	}
	if req.Action == "inspect" {
		if len(payload.files) != 1 {
			return response{}, failure("invalid_request")
		}
		return response{ProtocolVersion: protocolVersion, OK: true, Action: "inspect", CurrentReceiptSHA256: currentSHA, Receipt: *current}, nil
	}
	if req.Repository != config.Repository {
		return response{}, failure("invalid_request")
	}
	switch req.Action {
	case "deploy":
		return deploy(ctx, store, config, payload, req, *current, currentSHA)
	case "release":
		return release(ctx, store, config, payload, req, *current, currentSHA)
	default:
		return response{}, failure("invalid_request")
	}
}

func deploy(ctx context.Context, store *stateStore, config Config, payload extractedPayload, req request, current auditReceipt, currentSHA string) (response, error) {
	if req.ExpectedCurrentReceiptSHA256 == nil || *req.ExpectedCurrentReceiptSHA256 != currentSHA {
		return response{}, failure("state_conflict")
	}
	if err := store.requireNoPending(); err != nil {
		return response{}, err
	}
	manifestPath := payload.files["release-manifest.json"]
	stagingPath := payload.files["staging-receipt.json"]
	if manifestPath == "" || stagingPath == "" {
		return response{}, failure("invalid_request")
	}
	manifestContents, err := os.ReadFile(manifestPath)
	if err != nil || sha256Bytes(manifestContents) != req.ManifestSHA256 {
		return response{}, failure("invalid_request")
	}
	manifest, err := parseReleaseManifest(manifestContents, req.CandidateRunID)
	if err != nil {
		return response{}, err
	}
	stagingContents, err := os.ReadFile(stagingPath)
	if err != nil || sha256Bytes(stagingContents) != req.StagingReceiptSHA256 {
		return response{}, failure("invalid_request")
	}
	staging, err := parseStagingReceipt(stagingContents)
	if err != nil || !stagingMatches(staging, manifest, req) {
		return response{}, failure("invalid_request")
	}
	bundlePath := filepath.Join(payload.root, "bundle")
	if err := validateBundle(bundlePath, payload.files, manifest, req.BundleManifestSHA256); err != nil {
		return response{}, err
	}
	currentEnginePath, currentEngine, err := store.loadEngine(current.ProductionEngineReceiptSHA256)
	if err != nil || !engineMatchesAudit(currentEngine, current) {
		return response{}, failure("state_invalid")
	}
	if deploymentAlreadySucceeded(current, manifest, req) {
		verifyContext, cancel := context.WithTimeout(ctx, config.RequestTimeout)
		defer cancel()
		arguments := []string{"verify", "--manifest", manifestPath, "--env-file", config.Environment}
		environment := []string{"HOME=/root", "PATH=" + config.PATH}
		if err := config.RunCommand(verifyContext, config.ManagePath, arguments, environment); err != nil {
			return response{}, failure("operation_failed")
		}
		return response{ProtocolVersion: protocolVersion, OK: true, Action: "deploy", CurrentReceiptSHA256: currentSHA, Receipt: current}, nil
	}
	if req.CandidateRunID == valueOrZero(current.CandidateRunID) || manifest.VersionCode <= current.VersionCode {
		return response{}, failure("release_not_newer")
	}

	journal := map[string]any{
		"journal_version":         1,
		"candidate_run_id":        req.CandidateRunID,
		"staging_run_id":          req.StagingRunID,
		"deployment_run_id":       req.DeploymentRunID,
		"deployment_run_attempt":  req.DeploymentRunAttempt,
		"manifest_sha256":         manifest.SHA256,
		"previous_receipt_sha256": currentSHA,
	}
	journalContents, _ := json.Marshal(journal)
	if err := writeNoClobber(store.pendingJournal, journalContents, 0o600); err != nil {
		return response{}, failure("state_invalid")
	}

	deployContext, cancel := context.WithTimeout(ctx, config.DeployTimeout)
	defer cancel()
	arguments := []string{
		"deploy",
		"--manifest", manifestPath,
		"--env-file", config.Environment,
		"--bundle", bundlePath,
		"--current-receipt", currentEnginePath,
		"--receipt", store.pendingEngine,
	}
	environment := []string{"HOME=/root", "PATH=" + config.PATH}
	if err := config.RunCommand(deployContext, config.ManagePath, arguments, environment); err != nil {
		return response{}, failure("operation_failed")
	}
	engineContents, err := readSecureFile(store.pendingEngine, 0o444, metadataLimit, config.OwnerUID)
	if err != nil {
		return response{}, failure("operation_failed")
	}
	engine, err := parseEngineReceipt(engineContents)
	if err != nil || !engineMatchesDeployment(engine, manifest, current.ProductionEngineReceiptSHA256, req.BundleManifestSHA256) {
		return response{}, failure("operation_failed")
	}
	engineSHA := sha256Bytes(engineContents)
	if err := store.storeObject(store.engineDir, engineSHA, engineContents); err != nil {
		return response{}, err
	}
	if err := store.storeObject(store.manifestDir, manifest.SHA256, manifestContents); err != nil {
		return response{}, err
	}
	newReceipt := deploymentAudit(config, req, manifest, engine, currentSHA, engineSHA)
	newContents, err := json.Marshal(newReceipt)
	if err != nil {
		return response{}, failure("internal_error")
	}
	newSHA := sha256Bytes(newContents)
	if err := store.storeObject(store.auditDir, newSHA, newContents); err != nil {
		return response{}, err
	}
	if err := store.writeCurrent(newSHA); err != nil {
		return response{}, err
	}
	if err := os.Remove(store.pendingEngine); err != nil {
		return response{}, failure("state_invalid")
	}
	if err := os.Remove(store.pendingJournal); err != nil {
		return response{}, failure("state_invalid")
	}
	return response{ProtocolVersion: protocolVersion, OK: true, Action: "deploy", CurrentReceiptSHA256: newSHA, Receipt: newReceipt}, nil
}

func deploymentAlreadySucceeded(current auditReceipt, manifest releaseManifest, req request) bool {
	return current.Operation == "deploy" &&
		current.CandidateRunID != nil && *current.CandidateRunID == req.CandidateRunID &&
		current.StagingRunID != nil && *current.StagingRunID == req.StagingRunID &&
		current.StagingRunAttempt != nil && *current.StagingRunAttempt == req.StagingRunAttempt &&
		current.StagingReceiptSHA256 != nil && *current.StagingReceiptSHA256 == req.StagingReceiptSHA256 &&
		current.DeploymentRunID != nil && *current.DeploymentRunID == req.DeploymentRunID &&
		current.DeploymentRunAttempt != nil && *current.DeploymentRunAttempt <= req.DeploymentRunAttempt &&
		current.ManifestSHA256 == manifest.SHA256 && current.Version == manifest.Version &&
		current.VersionCode == manifest.VersionCode && current.GitSHA == manifest.GitSHA &&
		current.DatabaseSchemaVersion == manifest.DatabaseSchemaVersion &&
		current.PortalImageDigest == manifest.PortalImageDigest &&
		current.ServerImageDigest == manifest.ServerImageDigest &&
		current.ProductionAPKFile == manifest.ProductionAPKFile &&
		current.ProductionAPKSHA256 == manifest.ProductionAPKSHA256 &&
		current.APKCertificateSHA256 == manifest.APKCertificateSHA256 &&
		current.AndroidBundleManifestSHA256 != nil &&
		*current.AndroidBundleManifestSHA256 == req.BundleManifestSHA256
}

func release(ctx context.Context, store *stateStore, config Config, payload extractedPayload, req request, current auditReceipt, currentSHA string) (response, error) {
	if len(payload.files) != 1 || payload.files["request.json"] == "" ||
		req.ExpectedCurrentReceiptSHA256 == nil || *req.ExpectedCurrentReceiptSHA256 != currentSHA {
		return response{}, failure("state_conflict")
	}
	if err := store.requireNoPending(); err != nil {
		return response{}, err
	}
	if current.Operation != "deploy" || current.DeploymentRunID == nil ||
		*current.DeploymentRunID != req.DeploymentRunID || current.DeploymentRunAttempt == nil ||
		*current.DeploymentRunAttempt > req.DeploymentRunAttempt ||
		current.ManifestSHA256 != req.ManifestSHA256 || current.Version != req.Version ||
		current.AndroidBundleManifestSHA256 == nil ||
		*current.AndroidBundleManifestSHA256 != req.BundleManifestSHA256 {
		return response{}, failure("invalid_request")
	}
	releaseContext, cancel := context.WithTimeout(ctx, config.RequestTimeout)
	defer cancel()
	arguments := []string{"activate", "--root", config.PublicRoot, "--version", current.Version}
	environment := []string{"HOME=/root", "PATH=" + config.PATH}
	if err := config.RunCommand(releaseContext, config.AndroidPath, arguments, environment); err != nil {
		return response{}, failure("operation_failed")
	}
	metadataPath := filepath.Join(config.PublicRoot, "downloads", "android", "release.json")
	metadataContents, err := readSecureFile(metadataPath, 0o644, metadataLimit, config.OwnerUID)
	if err != nil || !publicReleaseMatchesAudit(metadataContents, current) {
		return response{}, failure("operation_failed")
	}
	return response{ProtocolVersion: protocolVersion, OK: true, Action: "release", CurrentReceiptSHA256: currentSHA, Receipt: current}, nil
}

func publicReleaseMatchesAudit(contents []byte, current auditReceipt) bool {
	value, err := decodeStrictJSON(contents)
	if err != nil {
		return false
	}
	object, ok := value.(map[string]any)
	if !ok || !hasExactKeys(object,
		"metadata_version", "version", "version_code", "published_at", "file_name",
		"download_path", "size_bytes", "minimum_android_api", "abis", "apk_sha256",
		"apk_certificate_sha256") {
		return false
	}
	version, versionOK := object["version"].(string)
	versionCode, codeOK := positiveSafeInteger(object["version_code"])
	fileName, fileOK := object["file_name"].(string)
	downloadPath, pathOK := object["download_path"].(string)
	_, sizeOK := positiveSafeInteger(object["size_bytes"])
	apkSHA, apkOK := object["apk_sha256"].(string)
	certificate, certificateOK := object["apk_certificate_sha256"].(string)
	abis, abisOK := object["abis"].([]any)
	return object["metadata_version"] == json.Number("1") && versionOK && version == current.Version &&
		codeOK && versionCode == current.VersionCode && validTimestampValue(object["published_at"]) &&
		fileOK && fileName == current.ProductionAPKFile && pathOK &&
		downloadPath == "/downloads/android/v"+current.Version+"/"+current.ProductionAPKFile &&
		sizeOK && object["minimum_android_api"] == json.Number("24") &&
		abisOK && len(abis) == 1 && abis[0] == "arm64-v8a" && apkOK &&
		apkSHA == current.ProductionAPKSHA256 && certificateOK && certificate == current.APKCertificateSHA256
}

func deploymentAudit(config Config, req request, manifest releaseManifest, engine engineReceipt, previous string, engineSHA string) auditReceipt {
	candidate, stagingRun, stagingAttempt := req.CandidateRunID, req.StagingRunID, req.StagingRunAttempt
	deploymentRun, deploymentAttempt := req.DeploymentRunID, req.DeploymentRunAttempt
	stagingSHA := req.StagingReceiptSHA256
	return auditReceipt{
		ReceiptVersion: 1, Environment: "production", Operation: "deploy", Repository: config.Repository,
		ManifestSHA256: manifest.SHA256, Version: manifest.Version, VersionCode: manifest.VersionCode,
		GitSHA: manifest.GitSHA, DatabaseSchemaVersion: manifest.DatabaseSchemaVersion,
		PortalImageDigest: manifest.PortalImageDigest, ServerImageDigest: manifest.ServerImageDigest,
		ProductionAPKFile: manifest.ProductionAPKFile, ProductionAPKSHA256: manifest.ProductionAPKSHA256,
		APKCertificateSHA256: manifest.APKCertificateSHA256,
		PortalContainerID:    engine.PortalContainerID, ServerContainerID: engine.ServerContainerID,
		PostgresContainerID: engine.PostgresContainerID, PostgresBackupID: engine.PostgresBackupID,
		PortalBackupID: engine.PortalBackupID, AndroidBundleManifestSHA256: engine.AndroidBundleManifestSHA256,
		CandidateRunID: &candidate, StagingRunID: &stagingRun, StagingRunAttempt: &stagingAttempt,
		StagingReceiptSHA256: &stagingSHA, DeploymentRunID: &deploymentRun,
		DeploymentRunAttempt: &deploymentAttempt, PreviousReceiptSHA256: &previous,
		ProductionEngineReceiptSHA256:         engineSHA,
		ProductionEnginePreviousReceiptSHA256: engine.PreviousReceiptSHA256,
		RecordedAtUTC:                         config.Now().UTC().Truncate(time.Second).Format("2006-01-02T15:04:05Z"),
	}
}

func initializeState(config Config, manifestPath string, receiptPath string) error {
	if err := validateConfig(config); err != nil {
		return err
	}
	store, err := openStateStore(config.StateDir, config.OwnerUID)
	if err != nil {
		return err
	}
	lock, err := acquireLock(context.Background(), config.LockFile, config.OwnerUID)
	if err != nil {
		return failure("state_invalid")
	}
	defer lock.close()
	if current, _, err := store.loadCurrent(); err != nil || current != nil {
		return failure("state_already_initialized")
	}
	manifestContents, err := os.ReadFile(manifestPath)
	if err != nil {
		return failure("invalid_baseline")
	}
	manifest, err := parseReleaseManifest(manifestContents, 0)
	if err != nil {
		return failure("invalid_baseline")
	}
	receiptContents, err := readExternalReceipt(receiptPath, config.OwnerUID)
	if err != nil {
		return failure("invalid_baseline")
	}
	engine, err := parseEngineReceipt(receiptContents)
	if err != nil || !engineMatchesManifest(engine, manifest) {
		return failure("invalid_baseline")
	}
	manifestSHA, engineSHA := manifest.SHA256, sha256Bytes(receiptContents)
	if err := store.storeObject(store.manifestDir, manifestSHA, manifestContents); err != nil {
		return err
	}
	if err := store.storeObject(store.engineDir, engineSHA, receiptContents); err != nil {
		return err
	}
	baseline := auditReceipt{
		ReceiptVersion: 1, Environment: "production", Operation: "baseline", Repository: config.Repository,
		ManifestSHA256: manifestSHA, Version: manifest.Version, VersionCode: manifest.VersionCode,
		GitSHA: manifest.GitSHA, DatabaseSchemaVersion: manifest.DatabaseSchemaVersion,
		PortalImageDigest: manifest.PortalImageDigest, ServerImageDigest: manifest.ServerImageDigest,
		ProductionAPKFile: manifest.ProductionAPKFile, ProductionAPKSHA256: manifest.ProductionAPKSHA256,
		APKCertificateSHA256: manifest.APKCertificateSHA256,
		PortalContainerID:    engine.PortalContainerID, ServerContainerID: engine.ServerContainerID,
		PostgresContainerID:           engine.PostgresContainerID,
		ProductionEngineReceiptSHA256: engineSHA,
		RecordedAtUTC:                 config.Now().UTC().Truncate(time.Second).Format("2006-01-02T15:04:05Z"),
	}
	contents, _ := json.Marshal(baseline)
	sha := sha256Bytes(contents)
	if err := store.storeObject(store.auditDir, sha, contents); err != nil {
		return err
	}
	return store.writeCurrent(sha)
}

func runCommand(ctx context.Context, name string, arguments []string, environment []string) error {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Env = append([]string(nil), environment...)
	command.Stdin = nil
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Run()
}

func validateConfig(config Config) error {
	if config.Repository != officialRepository || config.ManagePath == "" || config.AndroidPath == "" || config.Environment == "" || config.PublicRoot == "" || config.StateDir == "" || config.LockFile == "" || config.PATH == "" || config.Now == nil || config.RunCommand == nil || config.RequestTimeout <= 0 || config.DeployTimeout <= 0 {
		return failure("invalid_configuration")
	}
	for _, value := range []string{config.ManagePath, config.AndroidPath, config.Environment, config.PublicRoot, config.StateDir, config.LockFile} {
		if !safeAbsolutePath(value) {
			return failure("invalid_configuration")
		}
	}
	return nil
}

func safeAbsolutePath(value string) bool {
	return filepath.IsAbs(value) && filepath.Clean(value) == value && !strings.Contains(value, "//")
}

func writeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

type extractedPayload struct {
	root  string
	files map[string]string
}

func readPayload(input io.Reader, incomingDir string) (extractedPayload, error) {
	root, err := os.MkdirTemp(incomingDir, ".request-")
	if err != nil {
		return extractedPayload{}, failure("state_invalid")
	}
	if err := os.Chmod(root, 0o700); err != nil {
		os.RemoveAll(root)
		return extractedPayload{}, failure("state_invalid")
	}
	result := extractedPayload{root: root, files: make(map[string]string)}
	reader := tar.NewReader(io.LimitReader(input, requestLimit+1))
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || header == nil || header.Typeflag != tar.TypeReg || header.Size <= 0 {
			os.RemoveAll(root)
			return extractedPayload{}, failure("invalid_request")
		}
		name := header.Name
		if !validPayloadName(name) || header.Size > payloadFileLimit(name) {
			os.RemoveAll(root)
			return extractedPayload{}, failure("invalid_request")
		}
		if _, duplicate := result.files[name]; duplicate {
			os.RemoveAll(root)
			return extractedPayload{}, failure("invalid_request")
		}
		total += header.Size
		if total > requestLimit {
			os.RemoveAll(root)
			return extractedPayload{}, failure("request_too_large")
		}
		target := filepath.Join(root, filepath.FromSlash(name))
		if !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			os.RemoveAll(root)
			return extractedPayload{}, failure("invalid_request")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			os.RemoveAll(root)
			return extractedPayload{}, failure("invalid_request")
		}
		file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			os.RemoveAll(root)
			return extractedPayload{}, failure("invalid_request")
		}
		written, copyErr := io.CopyN(file, reader, header.Size)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil || written != header.Size {
			os.RemoveAll(root)
			return extractedPayload{}, failure("invalid_request")
		}
		result.files[name] = target
	}
	if len(result.files) == 0 || len(result.files) > 7 || result.files["request.json"] == "" {
		os.RemoveAll(root)
		return extractedPayload{}, failure("invalid_request")
	}
	return result, nil
}

func validPayloadName(name string) bool {
	if name == "request.json" || name == "release-manifest.json" || name == "staging-receipt.json" || name == "bundle/bundle-manifest.json" {
		return true
	}
	if filepath.ToSlash(filepath.Clean(name)) != name || strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) != 5 || parts[0] != "bundle" || parts[1] != "downloads" || parts[2] != "android" || !strings.HasPrefix(parts[3], "v") || !versionPattern.MatchString(strings.TrimPrefix(parts[3], "v")) {
		return false
	}
	version := strings.TrimPrefix(parts[3], "v")
	apk := "speakup-v" + version + "-production-arm64.apk"
	return parts[4] == "release.json" || parts[4] == apk || parts[4] == apk+".sha256"
}

func payloadFileLimit(name string) int64 {
	if strings.HasSuffix(name, ".apk") {
		return apkLimit
	}
	return metadataLimit
}

func parseRequest(contents []byte) (request, error) {
	value, err := decodeStrictJSON(contents)
	if err != nil {
		return request{}, failure("invalid_request")
	}
	object, ok := value.(map[string]any)
	if !ok {
		return request{}, failure("invalid_request")
	}
	integer, integerOK := integerValue(object["protocol_version"])
	if !integerOK || integer != protocolVersion {
		return request{}, failure("invalid_request")
	}
	action, ok := object["action"].(string)
	if !ok {
		return request{}, failure("invalid_request")
	}
	parsed := request{Action: action}
	if action == "inspect" {
		if !hasExactKeys(object, "protocol_version", "action") {
			return request{}, failure("invalid_request")
		}
		return parsed, nil
	}
	if action == "release" {
		if !hasExactKeys(object,
			"protocol_version", "action", "repository", "deployment_run_id",
			"deployment_run_attempt", "expected_current_receipt_sha256",
			"manifest_sha256", "bundle_manifest_sha256", "version") {
			return request{}, failure("invalid_request")
		}
		parsed.Repository, ok = object["repository"].(string)
		if !ok || parsed.Repository != officialRepository {
			return request{}, failure("invalid_request")
		}
		parsed.DeploymentRunID, ok = positiveSafeInteger(object["deployment_run_id"])
		if !ok {
			return request{}, failure("invalid_request")
		}
		parsed.DeploymentRunAttempt, ok = positiveSafeInteger(object["deployment_run_attempt"])
		if !ok {
			return request{}, failure("invalid_request")
		}
		expected, expectedOK := object["expected_current_receipt_sha256"].(string)
		manifestSHA, manifestOK := object["manifest_sha256"].(string)
		bundleSHA, bundleOK := object["bundle_manifest_sha256"].(string)
		version, versionOK := object["version"].(string)
		if !expectedOK || !validNonzeroSHA256(expected) ||
			!manifestOK || !validNonzeroSHA256(manifestSHA) ||
			!bundleOK || !validNonzeroSHA256(bundleSHA) ||
			!versionOK || !versionPattern.MatchString(version) {
			return request{}, failure("invalid_request")
		}
		parsed.ExpectedCurrentReceiptSHA256 = &expected
		parsed.ManifestSHA256 = manifestSHA
		parsed.BundleManifestSHA256 = bundleSHA
		parsed.Version = version
		return parsed, nil
	}
	if action != "deploy" || !hasExactKeys(object,
		"protocol_version", "action", "repository", "candidate_run_id",
		"staging_run_id", "staging_run_attempt", "deployment_run_id",
		"deployment_run_attempt", "expected_current_receipt_sha256",
		"manifest_sha256", "staging_receipt_sha256", "bundle_manifest_sha256") {
		return request{}, failure("invalid_request")
	}
	parsed.Repository, ok = object["repository"].(string)
	if !ok || parsed.Repository != officialRepository {
		return request{}, failure("invalid_request")
	}
	integerFields := []struct {
		name   string
		target *int64
	}{
		{"candidate_run_id", &parsed.CandidateRunID},
		{"staging_run_id", &parsed.StagingRunID},
		{"staging_run_attempt", &parsed.StagingRunAttempt},
		{"deployment_run_id", &parsed.DeploymentRunID},
		{"deployment_run_attempt", &parsed.DeploymentRunAttempt},
	}
	for _, field := range integerFields {
		*field.target, ok = positiveSafeInteger(object[field.name])
		if !ok {
			return request{}, failure("invalid_request")
		}
	}
	expected, ok := object["expected_current_receipt_sha256"].(string)
	if !ok || !validNonzeroSHA256(expected) {
		return request{}, failure("invalid_request")
	}
	parsed.ExpectedCurrentReceiptSHA256 = &expected
	for _, field := range []struct {
		name   string
		target *string
	}{
		{"manifest_sha256", &parsed.ManifestSHA256},
		{"staging_receipt_sha256", &parsed.StagingReceiptSHA256},
		{"bundle_manifest_sha256", &parsed.BundleManifestSHA256},
	} {
		*field.target, ok = object[field.name].(string)
		if !ok || !validNonzeroSHA256(*field.target) {
			return request{}, failure("invalid_request")
		}
	}
	return parsed, nil
}

func parseReleaseManifest(contents []byte, candidateRunID int64) (releaseManifest, error) {
	value, err := decodeStrictJSON(contents)
	if err != nil {
		return releaseManifest{}, failure("invalid_manifest")
	}
	object, ok := value.(map[string]any)
	if !ok || !hasExactKeys(object,
		"manifest_version", "version", "git_sha", "version_code", "portal_image",
		"portal_image_digest", "server_image", "server_image_digest", "staging_apk_file",
		"staging_apk_sha256", "production_apk_file", "production_apk_size_bytes",
		"production_apk_sha256", "application_id", "minimum_android_api", "abis",
		"apk_certificate_sha256", "database_schema_version", "quality_run_url") {
		return releaseManifest{}, failure("invalid_manifest")
	}
	manifestVersion, ok := integerValue(object["manifest_version"])
	version, versionOK := object["version"].(string)
	gitSHA, gitOK := object["git_sha"].(string)
	versionCode, codeOK := positiveSafeInteger(object["version_code"])
	schema, schemaOK := positiveSafeInteger(object["database_schema_version"])
	portalDigest, portalOK := object["portal_image_digest"].(string)
	serverDigest, serverOK := object["server_image_digest"].(string)
	apkFile, apkFileOK := object["production_apk_file"].(string)
	apkSize, apkSizeOK := positiveSafeInteger(object["production_apk_size_bytes"])
	apkSHA, apkSHAOK := object["production_apk_sha256"].(string)
	certificate, certificateOK := object["apk_certificate_sha256"].(string)
	qualityURL, qualityOK := object["quality_run_url"].(string)
	abis, abisOK := object["abis"].([]any)
	if !ok || manifestVersion != 1 || !versionOK || !versionPattern.MatchString(version) ||
		!gitOK || !validNonzeroHex(gitSHA, gitSHAPattern) || !codeOK || !schemaOK ||
		object["portal_image"] != "ghcr.io/1024xengineer/xe3-esl-portal" || !portalOK || !validDigest(portalDigest) ||
		object["server_image"] != "ghcr.io/1024xengineer/xe3-esl-server" || !serverOK || !validDigest(serverDigest) ||
		object["staging_apk_file"] != "speakup-v"+version+"-staging-arm64.apk" || !stringIsNonzeroSHA256(object["staging_apk_sha256"]) ||
		!apkFileOK || apkFile != "speakup-v"+version+"-production-arm64.apk" || !apkSizeOK || !apkSHAOK || !validNonzeroSHA256(apkSHA) ||
		object["application_id"] != "com.xengineer.speakup" || object["minimum_android_api"] != json.Number("24") ||
		!abisOK || len(abis) != 1 || abis[0] != "arm64-v8a" || !certificateOK || !validNonzeroSHA256(certificate) || !qualityOK {
		return releaseManifest{}, failure("invalid_manifest")
	}
	expectedPrefix := "https://github.com/1024XEngineer/XE3-ESL/actions/runs/"
	if !strings.HasPrefix(qualityURL, expectedPrefix) {
		return releaseManifest{}, failure("invalid_manifest")
	}
	runID, parseErr := strconv.ParseInt(strings.TrimPrefix(qualityURL, expectedPrefix), 10, 64)
	if parseErr != nil || runID < 1 || (candidateRunID > 0 && runID != candidateRunID) {
		return releaseManifest{}, failure("invalid_manifest")
	}
	return releaseManifest{
		SHA256: sha256Bytes(contents), Version: version, VersionCode: versionCode, GitSHA: gitSHA,
		DatabaseSchemaVersion: schema, PortalImageDigest: portalDigest, ServerImageDigest: serverDigest,
		ProductionAPKFile: apkFile, ProductionAPKSize: apkSize, ProductionAPKSHA256: apkSHA,
		APKCertificateSHA256: certificate,
	}, nil
}

func parseStagingReceipt(contents []byte) (stagingReceipt, error) {
	value, err := decodeStrictJSON(contents)
	if err != nil {
		return stagingReceipt{}, failure("invalid_staging_receipt")
	}
	top, ok := value.(map[string]any)
	if !ok || !hasExactKeys(top, "protocol_version", "ok", "action", "receipt_sha256", "receipt") ||
		top["protocol_version"] != json.Number("1") || top["ok"] != true || top["action"] != "deploy" || !stringIsNonzeroSHA256(top["receipt_sha256"]) {
		return stagingReceipt{}, failure("invalid_staging_receipt")
	}
	receipt, ok := top["receipt"].(map[string]any)
	if !ok || !hasExactKeys(receipt,
		"receipt_version", "environment", "operation", "repository", "manifest_sha256", "version",
		"version_code", "git_sha", "database_schema_version", "portal_image_digest", "server_image_digest",
		"portal_container_id", "server_container_id", "postgres_container_id", "candidate_run_id",
		"deployment_run_id", "deployment_run_attempt", "previous_receipt_sha256", "rollback_target_receipt_sha256",
		"recorded_at_utc") || receipt["receipt_version"] != json.Number("2") || receipt["environment"] != "staging" ||
		receipt["operation"] != "deploy" || receipt["repository"] != officialRepository || receipt["rollback_target_receipt_sha256"] != nil {
		return stagingReceipt{}, failure("invalid_staging_receipt")
	}
	manifestSHA, a := receipt["manifest_sha256"].(string)
	version, b := receipt["version"].(string)
	versionCode, c := positiveSafeInteger(receipt["version_code"])
	gitSHA, d := receipt["git_sha"].(string)
	schema, e := positiveSafeInteger(receipt["database_schema_version"])
	portalDigest, f := receipt["portal_image_digest"].(string)
	serverDigest, g := receipt["server_image_digest"].(string)
	candidateRun, h := positiveSafeInteger(receipt["candidate_run_id"])
	deploymentRun, i := positiveSafeInteger(receipt["deployment_run_id"])
	deploymentAttempt, j := positiveSafeInteger(receipt["deployment_run_attempt"])
	if !a || !validNonzeroSHA256(manifestSHA) || !b || !versionPattern.MatchString(version) || !c || !d || !validNonzeroHex(gitSHA, gitSHAPattern) || !e || !f || !validDigest(portalDigest) || !g || !validDigest(serverDigest) || !h || !i || !j || !validContainerValue(receipt["portal_container_id"]) || !validContainerValue(receipt["server_container_id"]) || !validContainerValue(receipt["postgres_container_id"]) || !validOptionalSHA(receipt["previous_receipt_sha256"]) || !validTimestampValue(receipt["recorded_at_utc"]) {
		return stagingReceipt{}, failure("invalid_staging_receipt")
	}
	if receipt["portal_container_id"] == receipt["server_container_id"] || receipt["portal_container_id"] == receipt["postgres_container_id"] || receipt["server_container_id"] == receipt["postgres_container_id"] {
		return stagingReceipt{}, failure("invalid_staging_receipt")
	}
	return stagingReceipt{ManifestSHA256: manifestSHA, Version: version, VersionCode: versionCode, GitSHA: gitSHA, DatabaseSchemaVersion: schema, PortalImageDigest: portalDigest, ServerImageDigest: serverDigest, CandidateRunID: candidateRun, DeploymentRunID: deploymentRun, DeploymentRunAttempt: deploymentAttempt}, nil
}

func stagingMatches(staging stagingReceipt, manifest releaseManifest, req request) bool {
	return staging.ManifestSHA256 == manifest.SHA256 && staging.Version == manifest.Version && staging.VersionCode == manifest.VersionCode && staging.GitSHA == manifest.GitSHA && staging.DatabaseSchemaVersion == manifest.DatabaseSchemaVersion && staging.PortalImageDigest == manifest.PortalImageDigest && staging.ServerImageDigest == manifest.ServerImageDigest && staging.CandidateRunID == req.CandidateRunID && staging.DeploymentRunID == req.StagingRunID && staging.DeploymentRunAttempt == req.StagingRunAttempt
}

func validateBundle(root string, files map[string]string, manifest releaseManifest, expectedManifestSHA string) error {
	versionRoot := "bundle/downloads/android/v" + manifest.Version + "/"
	apkName := manifest.ProductionAPKFile
	expectedNames := []string{
		"request.json", "release-manifest.json", "staging-receipt.json", "bundle/bundle-manifest.json",
		versionRoot + "release.json", versionRoot + apkName, versionRoot + apkName + ".sha256",
	}
	actualNames := make([]string, 0, len(files))
	for name := range files {
		actualNames = append(actualNames, name)
	}
	sort.Strings(actualNames)
	sort.Strings(expectedNames)
	if strings.Join(actualNames, "\n") != strings.Join(expectedNames, "\n") {
		return failure("invalid_bundle")
	}
	bundleManifestPath := files["bundle/bundle-manifest.json"]
	bundleContents, err := os.ReadFile(bundleManifestPath)
	if err != nil || sha256Bytes(bundleContents) != expectedManifestSHA {
		return failure("invalid_bundle")
	}
	value, err := decodeStrictJSON(bundleContents)
	if err != nil {
		return failure("invalid_bundle")
	}
	object, ok := value.(map[string]any)
	if !ok || !hasExactKeys(object, "bundle_version", "version", "published_at", "release_manifest_sha256", "files") ||
		object["bundle_version"] != json.Number("1") || object["version"] != manifest.Version || object["release_manifest_sha256"] != manifest.SHA256 || !validTimestampValue(object["published_at"]) {
		return failure("invalid_bundle")
	}
	entries, ok := object["files"].([]any)
	if !ok || len(entries) != 3 {
		return failure("invalid_bundle")
	}
	expectedFiles := map[string]bool{
		strings.TrimPrefix(versionRoot+apkName, "bundle/"):           true,
		strings.TrimPrefix(versionRoot+apkName+".sha256", "bundle/"): true,
		strings.TrimPrefix(versionRoot+"release.json", "bundle/"):    true,
	}
	for _, rawEntry := range entries {
		entry, ok := rawEntry.(map[string]any)
		if !ok || !hasExactKeys(entry, "path", "size_bytes", "sha256") {
			return failure("invalid_bundle")
		}
		path, pathOK := entry["path"].(string)
		size, sizeOK := positiveSafeInteger(entry["size_bytes"])
		sha, shaOK := entry["sha256"].(string)
		if !pathOK || !expectedFiles[path] || !sizeOK || !shaOK || !validNonzeroSHA256(sha) {
			return failure("invalid_bundle")
		}
		delete(expectedFiles, path)
		file := files["bundle/"+path]
		info, statErr := os.Stat(file)
		fileSHA, hashErr := sha256File(file)
		if statErr != nil || hashErr != nil || info.Size() != size || fileSHA != sha {
			return failure("invalid_bundle")
		}
	}
	if len(expectedFiles) != 0 {
		return failure("invalid_bundle")
	}
	apkPath := files[versionRoot+apkName]
	apkInfo, err := os.Stat(apkPath)
	if err != nil || apkInfo.Size() != manifest.ProductionAPKSize {
		return failure("invalid_bundle")
	}
	apkSHA, err := sha256File(apkPath)
	if err != nil || apkSHA != manifest.ProductionAPKSHA256 {
		return failure("invalid_bundle")
	}
	checksum, err := os.ReadFile(files[versionRoot+apkName+".sha256"])
	if err != nil || string(checksum) != manifest.ProductionAPKSHA256+"  "+apkName+"\n" {
		return failure("invalid_bundle")
	}
	metadataContents, err := os.ReadFile(files[versionRoot+"release.json"])
	if err != nil {
		return failure("invalid_bundle")
	}
	metadataValue, err := decodeStrictJSON(metadataContents)
	if err != nil {
		return failure("invalid_bundle")
	}
	metadata, ok := metadataValue.(map[string]any)
	if !ok || !hasExactKeys(metadata, "metadata_version", "version", "version_code", "published_at", "file_name", "download_path", "size_bytes", "minimum_android_api", "abis", "apk_sha256", "apk_certificate_sha256") ||
		metadata["metadata_version"] != json.Number("1") || metadata["version"] != manifest.Version || metadata["version_code"] != json.Number(strconv.FormatInt(manifest.VersionCode, 10)) ||
		metadata["file_name"] != apkName || metadata["download_path"] != "/downloads/android/v"+manifest.Version+"/"+apkName || metadata["size_bytes"] != json.Number(strconv.FormatInt(manifest.ProductionAPKSize, 10)) ||
		metadata["minimum_android_api"] != json.Number("24") || metadata["apk_sha256"] != manifest.ProductionAPKSHA256 || metadata["apk_certificate_sha256"] != manifest.APKCertificateSHA256 || !validTimestampValue(metadata["published_at"]) {
		return failure("invalid_bundle")
	}
	abis, ok := metadata["abis"].([]any)
	if !ok || len(abis) != 1 || abis[0] != "arm64-v8a" {
		return failure("invalid_bundle")
	}
	_ = root
	return nil
}

func parseEngineReceipt(contents []byte) (engineReceipt, error) {
	value, err := decodeStrictJSON(contents)
	if err != nil {
		return engineReceipt{}, failure("invalid_engine_receipt")
	}
	object, ok := value.(map[string]any)
	if !ok || !hasExactKeys(object,
		"receipt_version", "environment", "operation", "manifest_sha256", "version", "version_code",
		"git_sha", "database_schema_version", "portal_image_digest", "server_image_digest",
		"production_apk_file", "production_apk_size_bytes", "production_apk_sha256", "apk_certificate_sha256",
		"portal_container_id", "server_container_id", "postgres_container_id", "nginx_config_sha256", "nginx_config",
		"postgres_backup_id", "portal_backup_id", "android_bundle_manifest_sha256", "previous_receipt_sha256",
		"rollback_target_receipt_sha256", "recorded_at_utc") || object["receipt_version"] != json.Number("1") || object["environment"] != "production" {
		return engineReceipt{}, failure("invalid_engine_receipt")
	}
	operation, opOK := object["operation"].(string)
	manifestSHA, a := object["manifest_sha256"].(string)
	version, b := object["version"].(string)
	versionCode, c := positiveSafeInteger(object["version_code"])
	gitSHA, d := object["git_sha"].(string)
	schema, e := positiveSafeInteger(object["database_schema_version"])
	portalDigest, f := object["portal_image_digest"].(string)
	serverDigest, g := object["server_image_digest"].(string)
	apkFile, h := object["production_apk_file"].(string)
	apkSize, i := positiveSafeInteger(object["production_apk_size_bytes"])
	apkSHA, j := object["production_apk_sha256"].(string)
	certificate, k := object["apk_certificate_sha256"].(string)
	portalID, l := object["portal_container_id"].(string)
	serverID, m := object["server_container_id"].(string)
	postgresID, n := object["postgres_container_id"].(string)
	recordedAt, o := object["recorded_at_utc"].(string)
	nginxConfig, p := object["nginx_config"].(string)
	nginxSHA, q := object["nginx_config_sha256"].(string)
	if !opOK || (operation != "baseline" && operation != "deploy" && operation != "rollback") || !a || !validNonzeroSHA256(manifestSHA) || !b || !versionPattern.MatchString(version) || !c || !d || !validNonzeroHex(gitSHA, gitSHAPattern) || !e || !f || !validDigest(portalDigest) || !g || !validDigest(serverDigest) || !h || apkFile != "speakup-v"+version+"-production-arm64.apk" || !i || !j || !validNonzeroSHA256(apkSHA) || !k || !validNonzeroSHA256(certificate) || !l || !validNonzeroHex(portalID, containerIDPattern) || !m || !validNonzeroHex(serverID, containerIDPattern) || !n || !validNonzeroHex(postgresID, containerIDPattern) || !o || !validUTCTimestamp(recordedAt) || !p || nginxConfig == "" || !q || !validNonzeroSHA256(nginxSHA) || sha256Bytes([]byte(nginxConfig)) != nginxSHA {
		return engineReceipt{}, failure("invalid_engine_receipt")
	}
	postgresBackup, ok := optionalString(object["postgres_backup_id"])
	if !ok {
		return engineReceipt{}, failure("invalid_engine_receipt")
	}
	portalBackup, ok := optionalString(object["portal_backup_id"])
	if !ok {
		return engineReceipt{}, failure("invalid_engine_receipt")
	}
	bundleSHA, ok := optionalSHAValue(object["android_bundle_manifest_sha256"])
	if !ok {
		return engineReceipt{}, failure("invalid_engine_receipt")
	}
	previousSHA, ok := optionalSHAValue(object["previous_receipt_sha256"])
	if !ok {
		return engineReceipt{}, failure("invalid_engine_receipt")
	}
	rollbackSHA, ok := optionalSHAValue(object["rollback_target_receipt_sha256"])
	if !ok {
		return engineReceipt{}, failure("invalid_engine_receipt")
	}
	if operation == "deploy" {
		if postgresBackup == nil || !postgresBackupPattern.MatchString(*postgresBackup) || portalBackup == nil || !portalBackupPattern.MatchString(*portalBackup) || bundleSHA == nil || previousSHA == nil || rollbackSHA != nil {
			return engineReceipt{}, failure("invalid_engine_receipt")
		}
	} else if operation == "baseline" {
		if postgresBackup != nil || portalBackup != nil || bundleSHA != nil || previousSHA != nil || rollbackSHA != nil {
			return engineReceipt{}, failure("invalid_engine_receipt")
		}
	}
	return engineReceipt{
		ManifestSHA256: manifestSHA, Version: version, VersionCode: versionCode, GitSHA: gitSHA,
		DatabaseSchemaVersion: schema, PortalImageDigest: portalDigest, ServerImageDigest: serverDigest,
		ProductionAPKFile: apkFile, ProductionAPKSize: apkSize, ProductionAPKSHA256: apkSHA,
		APKCertificateSHA256: certificate, PortalContainerID: portalID, ServerContainerID: serverID,
		PostgresContainerID: postgresID, PostgresBackupID: postgresBackup, PortalBackupID: portalBackup,
		AndroidBundleManifestSHA256: bundleSHA, PreviousReceiptSHA256: previousSHA,
		RecordedAtUTC: recordedAt, Operation: operation,
	}, nil
}

func engineMatchesManifest(engine engineReceipt, manifest releaseManifest) bool {
	return engine.ManifestSHA256 == manifest.SHA256 && engine.Version == manifest.Version && engine.VersionCode == manifest.VersionCode && engine.GitSHA == manifest.GitSHA && engine.DatabaseSchemaVersion == manifest.DatabaseSchemaVersion && engine.PortalImageDigest == manifest.PortalImageDigest && engine.ServerImageDigest == manifest.ServerImageDigest && engine.ProductionAPKFile == manifest.ProductionAPKFile && engine.ProductionAPKSize == manifest.ProductionAPKSize && engine.ProductionAPKSHA256 == manifest.ProductionAPKSHA256 && engine.APKCertificateSHA256 == manifest.APKCertificateSHA256
}

func engineMatchesAudit(engine engineReceipt, audit auditReceipt) bool {
	return engine.ManifestSHA256 == audit.ManifestSHA256 && engine.Version == audit.Version && engine.VersionCode == audit.VersionCode && engine.GitSHA == audit.GitSHA && engine.DatabaseSchemaVersion == audit.DatabaseSchemaVersion && engine.PortalImageDigest == audit.PortalImageDigest && engine.ServerImageDigest == audit.ServerImageDigest && engine.ProductionAPKFile == audit.ProductionAPKFile && engine.ProductionAPKSHA256 == audit.ProductionAPKSHA256 && engine.APKCertificateSHA256 == audit.APKCertificateSHA256 && engine.PortalContainerID == audit.PortalContainerID && engine.ServerContainerID == audit.ServerContainerID && engine.PostgresContainerID == audit.PostgresContainerID
}

func engineMatchesDeployment(engine engineReceipt, manifest releaseManifest, currentEngineSHA string, bundleSHA string) bool {
	return engine.Operation == "deploy" && engineMatchesManifest(engine, manifest) && engine.PreviousReceiptSHA256 != nil && *engine.PreviousReceiptSHA256 == currentEngineSHA && engine.AndroidBundleManifestSHA256 != nil && *engine.AndroidBundleManifestSHA256 == bundleSHA
}

type stateStore struct {
	root           string
	manifestDir    string
	engineDir      string
	auditDir       string
	incomingDir    string
	currentPath    string
	pendingJournal string
	pendingEngine  string
	ownerUID       uint32
}

func openStateStore(root string, ownerUID uint32) (*stateStore, error) {
	store := &stateStore{
		root:           root,
		manifestDir:    filepath.Join(root, "manifests"),
		engineDir:      filepath.Join(root, "engine-receipts"),
		auditDir:       filepath.Join(root, "audit-receipts"),
		incomingDir:    filepath.Join(root, "incoming"),
		currentPath:    filepath.Join(root, "current"),
		pendingJournal: filepath.Join(root, "pending.json"),
		pendingEngine:  filepath.Join(root, "pending-engine.json"),
		ownerUID:       ownerUID,
	}
	for _, directory := range []string{store.root, store.manifestDir, store.engineDir, store.auditDir, store.incomingDir} {
		if err := ensurePrivateDirectory(directory, ownerUID); err != nil {
			return nil, failure("state_invalid")
		}
	}
	return store, nil
}

func ensurePrivateDirectory(path string, ownerUID uint32) error {
	err := os.Mkdir(path, 0o700)
	if err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 || !ownedByUID(info, ownerUID) {
		return syscall.EPERM
	}
	return nil
}

func (store *stateStore) requireNoPending() error {
	for _, path := range []string{store.pendingJournal, store.pendingEngine} {
		if _, err := os.Lstat(path); err == nil || !errors.Is(err, os.ErrNotExist) {
			return failure("recovery_required")
		}
	}
	return nil
}

func (store *stateStore) loadCurrent() (*auditReceipt, string, error) {
	contents, err := readSecureFile(store.currentPath, 0o600, 65, store.ownerUID)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", failure("state_invalid")
	}
	sha := strings.TrimSuffix(string(contents), "\n")
	if len(contents) != 65 || !validNonzeroSHA256(sha) {
		return nil, "", failure("state_invalid")
	}
	auditContents, err := readSecureFile(filepath.Join(store.auditDir, sha+".json"), 0o444, metadataLimit, store.ownerUID)
	if err != nil || sha256Bytes(auditContents) != sha {
		return nil, "", failure("state_invalid")
	}
	receipt, err := parseAuditReceipt(auditContents)
	if err != nil {
		return nil, "", err
	}
	return &receipt, sha, nil
}

func (store *stateStore) loadEngine(sha string) (string, engineReceipt, error) {
	if !validNonzeroSHA256(sha) {
		return "", engineReceipt{}, failure("state_invalid")
	}
	path := filepath.Join(store.engineDir, sha+".json")
	contents, err := readSecureFile(path, 0o444, metadataLimit, store.ownerUID)
	if err != nil || sha256Bytes(contents) != sha {
		return "", engineReceipt{}, failure("state_invalid")
	}
	receipt, err := parseEngineReceipt(contents)
	return path, receipt, err
}

func (store *stateStore) storeObject(directory string, sha string, contents []byte) error {
	if !validNonzeroSHA256(sha) || sha256Bytes(contents) != sha {
		return failure("state_invalid")
	}
	path := filepath.Join(directory, sha+".json")
	if existing, err := readSecureFile(path, 0o444, int64(len(contents)+1), store.ownerUID); err == nil {
		if bytes.Equal(existing, contents) {
			return nil
		}
		return failure("state_invalid")
	} else if !errors.Is(err, os.ErrNotExist) {
		return failure("state_invalid")
	}
	if err := writeNoClobber(path, contents, 0o444); err != nil {
		return failure("state_invalid")
	}
	return nil
}

func (store *stateStore) writeCurrent(sha string) error {
	if !validNonzeroSHA256(sha) {
		return failure("state_invalid")
	}
	temporary, err := os.CreateTemp(store.root, ".current-")
	if err != nil {
		return failure("state_invalid")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return failure("state_invalid")
	}
	if _, err := temporary.WriteString(sha + "\n"); err != nil {
		temporary.Close()
		return failure("state_invalid")
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return failure("state_invalid")
	}
	if err := temporary.Close(); err != nil {
		return failure("state_invalid")
	}
	if err := os.Rename(temporaryPath, store.currentPath); err != nil {
		return failure("state_invalid")
	}
	directory, err := os.Open(store.root)
	if err != nil {
		return failure("state_invalid")
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return failure("state_invalid")
	}
	return nil
}

func parseAuditReceipt(contents []byte) (auditReceipt, error) {
	value, err := decodeStrictJSON(contents)
	if err != nil {
		return auditReceipt{}, failure("state_invalid")
	}
	object, ok := value.(map[string]any)
	if !ok || !hasExactKeys(object,
		"receipt_version", "environment", "operation", "repository", "manifest_sha256", "version", "version_code", "git_sha",
		"database_schema_version", "portal_image_digest", "server_image_digest", "production_apk_file", "production_apk_sha256",
		"apk_certificate_sha256", "portal_container_id", "server_container_id", "postgres_container_id", "postgres_backup_id",
		"portal_backup_id", "android_bundle_manifest_sha256", "candidate_run_id", "staging_run_id", "staging_run_attempt",
		"staging_receipt_sha256", "deployment_run_id", "deployment_run_attempt", "previous_receipt_sha256",
		"production_engine_receipt_sha256", "production_engine_previous_receipt_sha256", "recorded_at_utc") {
		return auditReceipt{}, failure("state_invalid")
	}
	var receipt auditReceipt
	if err := json.Unmarshal(contents, &receipt); err != nil || receipt.ReceiptVersion != 1 || receipt.Environment != "production" || (receipt.Operation != "baseline" && receipt.Operation != "deploy") || receipt.Repository != officialRepository || !validNonzeroSHA256(receipt.ManifestSHA256) || !versionPattern.MatchString(receipt.Version) || receipt.VersionCode < 1 || receipt.VersionCode > maxSafeInteger || !validNonzeroHex(receipt.GitSHA, gitSHAPattern) || receipt.DatabaseSchemaVersion < 1 || !validDigest(receipt.PortalImageDigest) || !validDigest(receipt.ServerImageDigest) || receipt.ProductionAPKFile != "speakup-v"+receipt.Version+"-production-arm64.apk" || !validNonzeroSHA256(receipt.ProductionAPKSHA256) || !validNonzeroSHA256(receipt.APKCertificateSHA256) || !validNonzeroHex(receipt.PortalContainerID, containerIDPattern) || !validNonzeroHex(receipt.ServerContainerID, containerIDPattern) || !validNonzeroHex(receipt.PostgresContainerID, containerIDPattern) || !validNonzeroSHA256(receipt.ProductionEngineReceiptSHA256) || !validUTCTimestamp(receipt.RecordedAtUTC) {
		return auditReceipt{}, failure("state_invalid")
	}
	if receipt.Operation == "baseline" {
		if receipt.CandidateRunID != nil || receipt.StagingRunID != nil || receipt.StagingRunAttempt != nil || receipt.StagingReceiptSHA256 != nil || receipt.DeploymentRunID != nil || receipt.DeploymentRunAttempt != nil || receipt.PreviousReceiptSHA256 != nil || receipt.ProductionEnginePreviousReceiptSHA256 != nil {
			return auditReceipt{}, failure("state_invalid")
		}
	} else if receipt.CandidateRunID == nil || receipt.StagingRunID == nil || receipt.StagingRunAttempt == nil || receipt.StagingReceiptSHA256 == nil || receipt.DeploymentRunID == nil || receipt.DeploymentRunAttempt == nil || receipt.PreviousReceiptSHA256 == nil || receipt.ProductionEnginePreviousReceiptSHA256 == nil || receipt.PostgresBackupID == nil || receipt.PortalBackupID == nil || receipt.AndroidBundleManifestSHA256 == nil || !validNonzeroSHA256(*receipt.StagingReceiptSHA256) || !validNonzeroSHA256(*receipt.PreviousReceiptSHA256) || !validNonzeroSHA256(*receipt.ProductionEnginePreviousReceiptSHA256) {
		return auditReceipt{}, failure("state_invalid")
	}
	return receipt, nil
}

type brokerLock struct{ file *os.File }

func acquireLock(ctx context.Context, path string, ownerUID uint32) (*brokerLock, error) {
	descriptor, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		syscall.Close(descriptor)
		return nil, syscall.EBADF
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByUID(info, ownerUID) {
		file.Close()
		return nil, syscall.EPERM
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		err = syscall.Flock(descriptor, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &brokerLock{file: file}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EINTR) {
			file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (lock *brokerLock) close() {
	if lock == nil || lock.file == nil {
		return
	}
	_ = syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN)
	_ = lock.file.Close()
}

func readSecureFile(path string, mode os.FileMode, limit int64, ownerUID uint32) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != mode || !ownedByUID(info, ownerUID) || info.Size() <= 0 || info.Size() > limit {
		return nil, syscall.EPERM
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(contents)) != info.Size() {
		return nil, syscall.EIO
	}
	return contents, nil
}

func readExternalReceipt(path string, ownerUID uint32) ([]byte, error) {
	if !safeAbsolutePath(path) {
		return nil, syscall.EINVAL
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o444 || !ownedByUID(info, ownerUID) || info.Size() <= 0 || info.Size() > metadataLimit {
		return nil, syscall.EPERM
	}
	return os.ReadFile(path)
}

func writeNoClobber(path string, contents []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, mode)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(contents); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	ok = true
	return nil
}

func ownedByUID(info os.FileInfo, ownerUID uint32) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == ownerUID
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
	if !ok || number.String() == "" || strings.ContainsAny(number.String(), ".eE+") {
		return 0, false
	}
	integer, err := strconv.ParseInt(number.String(), 10, 64)
	return integer, err == nil
}

func positiveSafeInteger(value any) (int64, bool) {
	integer, ok := integerValue(value)
	return integer, ok && integer >= 1 && integer <= maxSafeInteger
}

func validNonzeroSHA256(value string) bool { return validNonzeroHex(value, sha256Pattern) }
func stringIsNonzeroSHA256(value any) bool {
	text, ok := value.(string)
	return ok && validNonzeroSHA256(text)
}
func validNonzeroHex(value string, pattern *regexp.Regexp) bool {
	if !pattern.MatchString(value) {
		return false
	}
	return strings.Trim(value, "0") != ""
}
func validDigest(value string) bool {
	return digestPattern.MatchString(value) && validNonzeroSHA256(strings.TrimPrefix(value, "sha256:"))
}
func validContainerValue(value any) bool {
	text, ok := value.(string)
	return ok && validNonzeroHex(text, containerIDPattern)
}
func validOptionalSHA(value any) bool { return value == nil || stringIsNonzeroSHA256(value) }
func optionalSHAValue(value any) (*string, bool) {
	if value == nil {
		return nil, true
	}
	text, ok := value.(string)
	if !ok || !validNonzeroSHA256(text) {
		return nil, false
	}
	return &text, true
}
func optionalString(value any) (*string, bool) {
	if value == nil {
		return nil, true
	}
	text, ok := value.(string)
	if !ok || text == "" || strings.ContainsAny(text, "\r\n") {
		return nil, false
	}
	return &text, true
}
func validTimestampValue(value any) bool {
	text, ok := value.(string)
	return ok && validUTCTimestamp(text)
}
func validUTCTimestamp(value string) bool {
	parsed, err := time.Parse("2006-01-02T15:04:05Z", value)
	return err == nil && parsed.UTC().Format("2006-01-02T15:04:05Z") == value
}
func sha256Bytes(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
