package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	protocolVersion      = 1
	officialRepository   = "1024XEngineer/XE3-ESL"
	productionManagePath = "/opt/xe3-speakup-staging-control/current/deploy/staging/manage.sh"
	productionRuntimeEnv = "/etc/speakup/staging-runtime.env"
	productionStateDir   = "/var/lib/speakup/staging-broker"
	productionPublicRoot = "/var/lib/speakup/staging-apk-public"
	productionLockFile   = "/run/lock/xe3-speakup-staging/broker.lock"
	productionHome       = "/var/lib/speakup/staging-runtime"
	productionPATH       = "/usr/local/bin:/usr/bin:/bin"
	runtimeUsername      = "speakup-staging-runtime"
	requestLimit         = 64 * 1024
)

type commandRunner func(context.Context, string, []string, []string) error

// Config contains only operator-controlled broker inputs. Production constructs
// it locally; request JSON cannot override any field.
type Config struct {
	Repository     string
	ManagePath     string
	RuntimeEnvFile string
	StateDir       string
	PublicRoot     string
	LockFile       string
	RequestTimeout time.Duration
	PublishTimeout time.Duration
	Timeout        time.Duration
	Home           string
	XDGRuntimeDir  string
	DockerHost     string
	PATH           string
	Now            func() time.Time
	RunCommand     commandRunner
	AfterEngine    func() error
	AfterPublish   func() error
}

type brokerError struct {
	code string
}

func (e *brokerError) Error() string { return e.code }

func failure(code string) error { return &brokerError{code: code} }

func errorCode(err error) string {
	var target *brokerError
	if errors.As(err, &target) {
		return target.code
	}
	return "internal_error"
}

type errorResponse struct {
	ProtocolVersion int    `json:"protocol_version"`
	OK              bool   `json:"ok"`
	Error           string `json:"error"`
}

type inspectResponse struct {
	ProtocolVersion      int            `json:"protocol_version"`
	OK                   bool           `json:"ok"`
	Action               string         `json:"action"`
	CurrentReceiptSHA256 *string        `json:"current_receipt_sha256"`
	Receipt              *brokerReceipt `json:"receipt"`
}

type mutationResponse struct {
	ProtocolVersion int           `json:"protocol_version"`
	OK              bool          `json:"ok"`
	Action          string        `json:"action"`
	ReceiptSHA256   string        `json:"receipt_sha256"`
	Receipt         brokerReceipt `json:"receipt"`
}

type publicationResponse struct {
	ProtocolVersion          int                `json:"protocol_version"`
	OK                       bool               `json:"ok"`
	Action                   string             `json:"action"`
	PublicationReceiptSHA256 string             `json:"publication_receipt_sha256"`
	RuntimeReceiptSHA256     string             `json:"runtime_receipt_sha256"`
	Receipt                  publicationReceipt `json:"receipt"`
}

func main() {
	config, err := productionConfig()
	if err != nil {
		writeJSON(os.Stdout, errorResponse{ProtocolVersion: protocolVersion, Error: errorCode(err)})
		os.Exit(1)
	}
	os.Exit(runWithDeferredTermination(func() int {
		return runCLI(os.Args, os.Getenv("SSH_ORIGINAL_COMMAND"), os.Stdin, os.Stdout, config)
	}))
}

func runWithDeferredTermination(run func() int) int {
	signals := make(chan os.Signal, 16)
	signal.Notify(signals, terminationSignals()...)
	status := run()
	signal.Stop(signals)
	select {
	case <-signals:
		return 1
	default:
		return status
	}
}

func productionConfig() (Config, error) {
	uid := os.Geteuid()
	if uid == 0 {
		return Config{}, failure("invalid_runtime_identity")
	}
	account, err := user.LookupId(strconv.Itoa(uid))
	if err != nil || account.Username != runtimeUsername {
		return Config{}, failure("invalid_runtime_identity")
	}

	xdgRuntimeDir := filepath.Join("/run/user", strconv.Itoa(uid))
	return Config{
		Repository:     officialRepository,
		ManagePath:     productionManagePath,
		RuntimeEnvFile: productionRuntimeEnv,
		StateDir:       productionStateDir,
		PublicRoot:     productionPublicRoot,
		LockFile:       productionLockFile,
		RequestTimeout: 15 * time.Second,
		PublishTimeout: 5 * time.Minute,
		Timeout:        15 * time.Minute,
		Home:           productionHome,
		XDGRuntimeDir:  xdgRuntimeDir,
		DockerHost:     "unix://" + filepath.Join(xdgRuntimeDir, "docker.sock"),
		PATH:           productionPATH,
		Now:            time.Now,
		RunCommand:     runCommand,
	}, nil
}

func runCLI(arguments []string, originalCommand string, input io.Reader, output io.Writer, config Config) int {
	if len(arguments) != 1 || originalCommand != "" {
		writeJSON(output, errorResponse{ProtocolVersion: protocolVersion, Error: "invalid_invocation"})
		return 1
	}

	response, err := execute(input, config)
	if err != nil {
		writeJSON(output, errorResponse{ProtocolVersion: protocolVersion, Error: errorCode(err)})
		return 1
	}
	if err := writeJSON(output, response); err != nil {
		return 1
	}
	return 0
}

func execute(input io.Reader, config Config) (any, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	store, err := openStateStore(config.StateDir)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()
	lock, err := acquireBrokerLock(ctx, config.LockFile)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, failure("operation_timeout")
		}
		return nil, failure("state_invalid")
	}
	defer lock.close()

	envelope, err := parseEnvelopeWithTimeouts(input, config.Repository, store.incomingDir, config.RequestTimeout, config.PublishTimeout)
	if err != nil {
		return nil, err
	}
	defer envelope.close()
	request := envelope.request
	if request.Action == "deploy" || request.Action == "rollback" {
		if err := requireNoPublicationPending(store.root); err != nil {
			return nil, err
		}
	}

	recovered, err := recoverPending(ctx, store, config, request)
	if err != nil {
		return nil, err
	}
	chain, err := store.validateCurrentChain()
	if err != nil {
		return nil, err
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, failure("operation_timeout")
	}
	if recovered != nil && recovered.Pending.matches(request) {
		return recovered.Response, nil
	}
	var current *loadedReceipt
	if len(chain) != 0 {
		current = chain[0]
	}
	switch request.Action {
	case "inspect":
		return inspect(current)
	case "deploy":
		return deploy(ctx, store, config, request, chain)
	case "rollback":
		return rollback(ctx, store, config, request, chain)
	case "publish":
		if envelope.payload == nil {
			return nil, failure("invalid_request")
		}
		return publishCandidate(store, config, request, *envelope.payload, current)
	default:
		return nil, failure("invalid_request")
	}
}

func inspect(current *loadedReceipt) (any, error) {
	if current == nil {
		return inspectResponse{
			ProtocolVersion: protocolVersion,
			OK:              true,
			Action:          "inspect",
		}, nil
	}
	sha := current.SHA256
	return inspectResponse{
		ProtocolVersion:      protocolVersion,
		OK:                   true,
		Action:               "inspect",
		CurrentReceiptSHA256: &sha,
		Receipt:              &current.Receipt,
	}, nil
}

func deploy(ctx context.Context, store *stateStore, config Config, request request, chain []*loadedReceipt) (any, error) {
	manifest, err := validateManifest(request.Manifest, request.CandidateRunID)
	if err != nil {
		return nil, err
	}
	if len(chain) >= maxReceiptChainLength {
		return nil, failure("state_limit_reached")
	}
	var current *loadedReceipt
	if len(chain) != 0 {
		current = chain[0]
	}
	if !expectedCurrentMatches(current, request.ExpectedCurrentReceiptSHA256) {
		return nil, failure("conflict")
	}
	if current != nil {
		if err := invokeManage(ctx, config, []string{
			"verify",
			"--manifest", current.ManifestPath,
			"--runtime-env-file", config.RuntimeEnvFile,
		}); err != nil {
			return nil, err
		}
	}

	manifestPath, err := store.saveManifest(request.ManifestSHA256, request.Manifest)
	if err != nil {
		return nil, err
	}
	var previous *string
	if current != nil {
		value := current.SHA256
		previous = &value
	}
	pending := newPendingMutation(config, request, manifest, previous, nil)
	enginePath, err := store.beginPending(pending)
	if err != nil {
		return nil, err
	}
	if err := runEngine(ctx, config, store, enginePath, []string{
		"deploy",
		"--manifest", manifestPath,
		"--runtime-env-file", config.RuntimeEnvFile,
	}); err != nil {
		return nil, err
	}
	if config.AfterEngine != nil {
		if err := config.AfterEngine(); err != nil {
			return nil, failure("operation_interrupted")
		}
	}
	response, err := finalizePending(store, pending, false)
	if err != nil {
		return nil, err
	}
	return response, nil
}

func rollback(ctx context.Context, store *stateStore, config Config, request request, chain []*loadedReceipt) (any, error) {
	if len(chain) == 0 || !expectedCurrentMatches(chain[0], request.ExpectedCurrentReceiptSHA256) {
		return nil, failure("conflict")
	}
	if len(chain) >= maxReceiptChainLength {
		return nil, failure("state_limit_reached")
	}
	current := chain[0]
	var target *loadedReceipt
	for _, candidate := range chain[1:] {
		if candidate.SHA256 == request.TargetReceiptSHA256 {
			target = candidate
			break
		}
	}
	if target == nil {
		return nil, failure("conflict")
	}
	if current.Receipt.DatabaseSchemaVersion != target.Receipt.DatabaseSchemaVersion {
		return nil, failure("conflict")
	}

	previous := current.SHA256
	targetSHA := target.SHA256
	rollbackRequest := request
	rollbackRequest.CandidateRunID = target.Receipt.CandidateRunID
	pending := newPendingMutation(config, rollbackRequest, target.Manifest, &previous, &targetSHA)
	enginePath, err := store.beginPending(pending)
	if err != nil {
		return nil, err
	}
	if err := runEngine(ctx, config, store, enginePath, []string{
		"rollback",
		"--manifest", target.ManifestPath,
		"--current-manifest", current.ManifestPath,
		"--runtime-env-file", config.RuntimeEnvFile,
	}); err != nil {
		return nil, err
	}
	if config.AfterEngine != nil {
		if err := config.AfterEngine(); err != nil {
			return nil, failure("operation_interrupted")
		}
	}
	response, err := finalizePending(store, pending, false)
	if err != nil {
		return nil, err
	}
	return response, nil
}

func runEngine(ctx context.Context, config Config, store *stateStore, receiptPath string, arguments []string) error {
	arguments = append(append([]string(nil), arguments...), "--receipt", receiptPath)
	if err := invokeManage(ctx, config, arguments); err != nil {
		return err
	}
	if err := store.syncEngineReceipt(receiptPath); err != nil {
		return failure("operation_failed")
	}
	return nil
}

func invokeManage(ctx context.Context, config Config, arguments []string) error {
	for _, argument := range arguments {
		if argument == "" {
			return failure("internal_error")
		}
	}
	environment := []string{
		"HOME=" + config.Home,
		"XDG_RUNTIME_DIR=" + config.XDGRuntimeDir,
		"DOCKER_HOST=" + config.DockerHost,
		"PATH=" + config.PATH,
	}
	if err := config.RunCommand(ctx, config.ManagePath, arguments, environment); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return failure("operation_timeout")
		}
		return failure("operation_failed")
	}
	return nil
}

func runCommand(ctx context.Context, name string, arguments []string, environment []string) error {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Env = append([]string(nil), environment...)
	command.Stdin = nil
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	configureCommandProcess(command)
	return command.Run()
}

func validateConfig(config Config) error {
	if config.Repository != officialRepository || config.RequestTimeout <= 0 || config.PublishTimeout <= 0 || config.Timeout <= 0 ||
		config.RequestTimeout > config.Timeout || config.PublishTimeout > config.Timeout || config.Now == nil || config.RunCommand == nil {
		return failure("internal_error")
	}
	for _, path := range []string{
		config.ManagePath,
		config.RuntimeEnvFile,
		config.StateDir,
		config.PublicRoot,
		config.LockFile,
		config.Home,
		config.XDGRuntimeDir,
	} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00\r\n") {
			return failure("internal_error")
		}
	}
	if config.StateDir == "/" || config.PublicRoot == "/" || config.Home == "/" || config.XDGRuntimeDir == "/" {
		return failure("internal_error")
	}
	if config.DockerHost != "unix://"+filepath.Join(config.XDGRuntimeDir, "docker.sock") {
		return failure("internal_error")
	}
	if config.PATH == "" || strings.ContainsAny(config.PATH, "\x00\r\n=") {
		return failure("internal_error")
	}
	return nil
}

func writeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func expectedCurrentMatches(current *loadedReceipt, expected *string) bool {
	if current == nil {
		return expected == nil
	}
	return expected != nil && current.SHA256 == *expected
}

func newBrokerReceipt(pending pendingMutation, manifest releaseManifest, engine engineReceipt) brokerReceipt {
	return brokerReceipt{
		ReceiptVersion:              2,
		Environment:                 "staging",
		Operation:                   pending.Operation,
		Repository:                  pending.Repository,
		ManifestSHA256:              manifest.SHA256,
		Version:                     manifest.Version,
		VersionCode:                 manifest.VersionCode,
		GitSHA:                      manifest.GitSHA,
		DatabaseSchemaVersion:       manifest.DatabaseSchemaVersion,
		PortalImageDigest:           manifest.PortalImageDigest,
		ServerImageDigest:           manifest.ServerImageDigest,
		PortalContainerID:           engine.PortalContainerID,
		ServerContainerID:           engine.ServerContainerID,
		PostgresContainerID:         engine.PostgresContainerID,
		CandidateRunID:              pending.CandidateRunID,
		DeploymentRunID:             pending.DeploymentRunID,
		DeploymentRunAttempt:        pending.DeploymentRunAttempt,
		PreviousReceiptSHA256:       pending.PreviousReceiptSHA256,
		RollbackTargetReceiptSHA256: pending.RollbackTargetReceiptSHA256,
		RecordedAtUTC:               pending.RecordedAtUTC,
	}
}
