package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"time"
)

const pendingLimit = 16 * 1024

type pendingMutation struct {
	JournalVersion              int     `json:"journal_version"`
	Environment                 string  `json:"environment"`
	Operation                   string  `json:"operation"`
	Repository                  string  `json:"repository"`
	ManifestSHA256              string  `json:"manifest_sha256"`
	CandidateRunID              int64   `json:"candidate_run_id"`
	DeploymentRunID             int64   `json:"deployment_run_id"`
	DeploymentRunAttempt        int64   `json:"deployment_run_attempt"`
	PreviousReceiptSHA256       *string `json:"previous_receipt_sha256"`
	RollbackTargetReceiptSHA256 *string `json:"rollback_target_receipt_sha256"`
	RecordedAtUTC               string  `json:"recorded_at_utc"`
}

type recoveredMutation struct {
	Pending  pendingMutation
	Response mutationResponse
}

func newPendingMutation(
	config Config,
	request request,
	manifest releaseManifest,
	previous *string,
	rollbackTarget *string,
) pendingMutation {
	return pendingMutation{
		JournalVersion:              1,
		Environment:                 "staging",
		Operation:                   request.Action,
		Repository:                  config.Repository,
		ManifestSHA256:              manifest.SHA256,
		CandidateRunID:              request.CandidateRunID,
		DeploymentRunID:             request.DeploymentRunID,
		DeploymentRunAttempt:        request.DeploymentRunAttempt,
		PreviousReceiptSHA256:       previous,
		RollbackTargetReceiptSHA256: rollbackTarget,
		RecordedAtUTC:               config.Now().UTC().Truncate(time.Second).Format("2006-01-02T15:04:05Z"),
	}
}

func (pending pendingMutation) matches(request request) bool {
	if pending.Operation != request.Action ||
		pending.DeploymentRunID != request.DeploymentRunID ||
		pending.DeploymentRunAttempt != request.DeploymentRunAttempt ||
		!equalOptionalStrings(pending.PreviousReceiptSHA256, request.ExpectedCurrentReceiptSHA256) {
		return false
	}
	if pending.Operation == "deploy" {
		return pending.CandidateRunID == request.CandidateRunID &&
			pending.ManifestSHA256 == request.ManifestSHA256
	}
	return pending.RollbackTargetReceiptSHA256 != nil &&
		*pending.RollbackTargetReceiptSHA256 == request.TargetReceiptSHA256
}

func (store *stateStore) beginPending(pending pendingMutation) (string, error) {
	contents, err := json.Marshal(pending)
	if err != nil {
		return "", failure("internal_error")
	}
	if _, err := parsePendingMutation(contents); err != nil {
		return "", err
	}
	if _, err := readSecureFile(store.pendingPath, 0o600, pendingLimit); err == nil || !errors.Is(err, os.ErrNotExist) {
		return "", failure("recovery_required")
	}
	enginePath := store.engineReceiptPath(contents)
	if _, err := os.Lstat(enginePath); err == nil || !errors.Is(err, os.ErrNotExist) {
		return "", failure("state_invalid")
	}
	if err := writeNoClobber(store.root, store.pendingPath, contents, 0o600, false); err != nil {
		return "", failure("state_invalid")
	}
	return enginePath, nil
}

func (store *stateStore) loadPending() (*pendingMutation, []byte, error) {
	contents, err := readSecureFile(store.pendingPath, 0o600, pendingLimit)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, failure("state_invalid")
	}
	pending, err := parsePendingMutation(contents)
	if err != nil {
		return nil, nil, err
	}
	canonical, err := json.Marshal(pending)
	if err != nil || !bytes.Equal(canonical, contents) {
		return nil, nil, failure("state_invalid")
	}
	return &pending, contents, nil
}

func (store *stateStore) engineReceiptPath(pendingContents []byte) string {
	return filepath.Join(store.engineDir, sha256Bytes(pendingContents)+".json")
}

func (store *stateStore) syncEngineReceipt(path string) error {
	if filepath.Dir(path) != store.engineDir {
		return failure("state_invalid")
	}
	if err := syncSecureFile(path, 0o444); err != nil {
		return err
	}
	return syncDirectory(store.engineDir)
}

func (store *stateStore) clearPending() error {
	if err := os.Remove(store.pendingPath); err != nil {
		return failure("state_invalid")
	}
	if err := syncDirectory(store.root); err != nil {
		return failure("state_invalid")
	}
	return nil
}

func recoverPending(
	ctx context.Context,
	store *stateStore,
	config Config,
	request request,
) (*recoveredMutation, error) {
	pending, pendingContents, err := store.loadPending()
	if err != nil || pending == nil {
		return nil, err
	}
	currentSHA, err := store.readCurrentSHA()
	if err != nil {
		return nil, err
	}
	chain, err := store.validateChainAt(currentSHA)
	if err != nil {
		return nil, err
	}
	enginePath := store.engineReceiptPath(pendingContents)
	if _, statErr := os.Lstat(enginePath); statErr == nil {
		response, err := finalizePending(store, *pending, true)
		if err != nil {
			return nil, err
		}
		return &recoveredMutation{Pending: *pending, Response: response}, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, failure("recovery_required")
	}
	if !pending.matches(request) ||
		!equalOptionalStrings(currentSHA, pending.PreviousReceiptSHA256) ||
		len(chain) >= maxReceiptChainLength {
		return nil, failure("recovery_required")
	}

	switch pending.Operation {
	case "deploy":
		_, manifestPath, err := store.loadManifest(pending.ManifestSHA256, pending.CandidateRunID)
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
		response, err := finalizePending(store, *pending, false)
		if err != nil {
			return nil, err
		}
		return &recoveredMutation{Pending: *pending, Response: response}, nil
	case "rollback":
		if len(chain) == 0 {
			return nil, failure("recovery_required")
		}
		var target *loadedReceipt
		for _, candidate := range chain[1:] {
			if pending.RollbackTargetReceiptSHA256 != nil && candidate.SHA256 == *pending.RollbackTargetReceiptSHA256 {
				target = candidate
				break
			}
		}
		if target == nil || target.Manifest.SHA256 != pending.ManifestSHA256 ||
			target.Receipt.CandidateRunID != pending.CandidateRunID ||
			target.Receipt.DatabaseSchemaVersion != chain[0].Receipt.DatabaseSchemaVersion {
			return nil, failure("recovery_required")
		}
		if err := runEngine(ctx, config, store, enginePath, []string{
			"rollback",
			"--manifest", target.ManifestPath,
			"--current-manifest", chain[0].ManifestPath,
			"--runtime-env-file", config.RuntimeEnvFile,
		}); err != nil {
			return nil, err
		}
		if config.AfterEngine != nil {
			if err := config.AfterEngine(); err != nil {
				return nil, failure("operation_interrupted")
			}
		}
		response, err := finalizePending(store, *pending, false)
		if err != nil {
			return nil, err
		}
		return &recoveredMutation{Pending: *pending, Response: response}, nil
	default:
		return nil, failure("state_invalid")
	}
}

func finalizePending(store *stateStore, expected pendingMutation, isRecovery bool) (mutationResponse, error) {
	pending, pendingContents, err := store.loadPending()
	if err != nil || pending == nil || !reflect.DeepEqual(*pending, expected) {
		return mutationResponse{}, failure("state_invalid")
	}
	manifest, _, err := store.loadManifest(pending.ManifestSHA256, pending.CandidateRunID)
	if err != nil {
		return mutationResponse{}, err
	}
	enginePath := store.engineReceiptPath(pendingContents)
	if err := store.syncEngineReceipt(enginePath); err != nil {
		if isRecovery {
			return mutationResponse{}, failure("recovery_required")
		}
		return mutationResponse{}, failure("operation_failed")
	}
	engine, err := loadEngineReceipt(enginePath, manifest)
	if err != nil {
		if isRecovery {
			return mutationResponse{}, failure("recovery_required")
		}
		return mutationResponse{}, err
	}
	receipt := newBrokerReceipt(*pending, manifest, engine)
	receiptContents, err := json.Marshal(receipt)
	if err != nil {
		return mutationResponse{}, failure("internal_error")
	}
	receiptSHA := sha256Bytes(receiptContents)
	currentSHA, err := store.readCurrentSHA()
	if err != nil {
		return mutationResponse{}, err
	}

	var previousChain []*loadedReceipt
	switch {
	case equalOptionalStrings(currentSHA, pending.PreviousReceiptSHA256):
		previousChain, err = store.validateChainAt(currentSHA)
		if err != nil {
			return mutationResponse{}, err
		}
		if err := validatePendingAgainstPreviousChain(*pending, manifest, engine, previousChain); err != nil {
			return mutationResponse{}, pendingFinalizeError(err, isRecovery)
		}
		if err := store.saveReceipt(receiptSHA, receiptContents, true); err != nil {
			return mutationResponse{}, err
		}
		if err := store.updateCurrent(pending.PreviousReceiptSHA256, receiptSHA); err != nil {
			return mutationResponse{}, err
		}
	case currentSHA != nil && *currentSHA == receiptSHA:
		completedChain, chainErr := store.validateChainAt(currentSHA)
		if chainErr != nil || len(completedChain) == 0 {
			return mutationResponse{}, failure("state_invalid")
		}
		completedContents, marshalErr := json.Marshal(completedChain[0].Receipt)
		if marshalErr != nil || !bytes.Equal(completedContents, receiptContents) {
			return mutationResponse{}, failure("state_invalid")
		}
		previousChain = completedChain[1:]
		if err := validatePendingAgainstPreviousChain(*pending, manifest, engine, previousChain); err != nil {
			return mutationResponse{}, pendingFinalizeError(err, isRecovery)
		}
	default:
		return mutationResponse{}, failure("recovery_required")
	}

	completedSHA := receiptSHA
	if _, err := store.validateChainAt(&completedSHA); err != nil {
		return mutationResponse{}, err
	}
	if err := store.clearPending(); err != nil {
		return mutationResponse{}, err
	}
	return mutationResponse{
		ProtocolVersion: protocolVersion,
		OK:              true,
		Action:          pending.Operation,
		ReceiptSHA256:   receiptSHA,
		Receipt:         receipt,
	}, nil
}

func pendingFinalizeError(err error, recovery bool) error {
	if recovery && errorCode(err) == "operation_failed" {
		return failure("recovery_required")
	}
	return err
}

func validatePendingAgainstPreviousChain(
	pending pendingMutation,
	manifest releaseManifest,
	engine engineReceipt,
	chain []*loadedReceipt,
) error {
	if len(chain) >= maxReceiptChainLength {
		return failure("state_limit_reached")
	}
	if pending.PreviousReceiptSHA256 == nil {
		if len(chain) != 0 || pending.Operation != "deploy" {
			return failure("state_invalid")
		}
		return nil
	}
	if len(chain) == 0 || chain[0].SHA256 != *pending.PreviousReceiptSHA256 {
		return failure("state_invalid")
	}
	if pending.Operation == "deploy" {
		return nil
	}
	if pending.Operation != "rollback" || pending.RollbackTargetReceiptSHA256 == nil {
		return failure("state_invalid")
	}
	var target *loadedReceipt
	for _, candidate := range chain {
		if candidate.SHA256 == *pending.RollbackTargetReceiptSHA256 {
			target = candidate
			break
		}
	}
	if target == nil || target.Manifest.SHA256 != manifest.SHA256 ||
		target.Receipt.CandidateRunID != pending.CandidateRunID ||
		target.Receipt.DatabaseSchemaVersion != chain[0].Receipt.DatabaseSchemaVersion ||
		engine.PostgresContainerID != chain[0].Receipt.PostgresContainerID {
		return failure("operation_failed")
	}
	return nil
}

func parsePendingMutation(contents []byte) (pendingMutation, error) {
	value, err := decodeStrictJSON(contents)
	if err != nil {
		return pendingMutation{}, failure("state_invalid")
	}
	object, ok := value.(map[string]any)
	if !ok || !hasExactKeys(object,
		"journal_version",
		"environment",
		"operation",
		"repository",
		"manifest_sha256",
		"candidate_run_id",
		"deployment_run_id",
		"deployment_run_attempt",
		"previous_receipt_sha256",
		"rollback_target_receipt_sha256",
		"recorded_at_utc",
	) {
		return pendingMutation{}, failure("state_invalid")
	}
	version, ok := integerValue(object["journal_version"])
	if !ok || version != 1 || object["environment"] != "staging" || object["repository"] != officialRepository {
		return pendingMutation{}, failure("state_invalid")
	}
	operation, ok := object["operation"].(string)
	if !ok || (operation != "deploy" && operation != "rollback") {
		return pendingMutation{}, failure("state_invalid")
	}
	manifestSHA, ok := object["manifest_sha256"].(string)
	if !ok || !validNonzeroSHA256(manifestSHA) {
		return pendingMutation{}, failure("state_invalid")
	}
	candidateRunID, ok := positiveSafeInteger(object["candidate_run_id"])
	if !ok {
		return pendingMutation{}, failure("state_invalid")
	}
	deploymentRunID, ok := positiveSafeInteger(object["deployment_run_id"])
	if !ok {
		return pendingMutation{}, failure("state_invalid")
	}
	deploymentRunAttempt, ok := positiveSafeInteger(object["deployment_run_attempt"])
	if !ok {
		return pendingMutation{}, failure("state_invalid")
	}
	previous, ok := optionalSHA256(object["previous_receipt_sha256"])
	if !ok {
		return pendingMutation{}, failure("state_invalid")
	}
	rollbackTarget, ok := optionalSHA256(object["rollback_target_receipt_sha256"])
	if !ok || (operation == "deploy" && rollbackTarget != nil) ||
		(operation == "rollback" && (previous == nil || rollbackTarget == nil)) {
		return pendingMutation{}, failure("state_invalid")
	}
	recordedAt, ok := object["recorded_at_utc"].(string)
	if !ok || !validUTCTimestamp(recordedAt) {
		return pendingMutation{}, failure("state_invalid")
	}
	return pendingMutation{
		JournalVersion:              1,
		Environment:                 "staging",
		Operation:                   operation,
		Repository:                  officialRepository,
		ManifestSHA256:              manifestSHA,
		CandidateRunID:              candidateRunID,
		DeploymentRunID:             deploymentRunID,
		DeploymentRunAttempt:        deploymentRunAttempt,
		PreviousReceiptSHA256:       previous,
		RollbackTargetReceiptSHA256: rollbackTarget,
		RecordedAtUTC:               recordedAt,
	}, nil
}
