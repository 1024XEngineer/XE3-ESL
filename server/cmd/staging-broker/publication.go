package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const publicationStateLimit = 64 * 1024

type publicationReceipt struct {
	ReceiptVersion                   int     `json:"receipt_version"`
	Environment                      string  `json:"environment"`
	Operation                        string  `json:"operation"`
	Repository                       string  `json:"repository"`
	RuntimeReceiptSHA256             string  `json:"runtime_receipt_sha256"`
	ManifestSHA256                   string  `json:"manifest_sha256"`
	BundleManifestSHA256             string  `json:"bundle_manifest_sha256"`
	CandidateMetadataSHA256          string  `json:"candidate_metadata_sha256"`
	Version                          string  `json:"version"`
	VersionCode                      int64   `json:"version_code"`
	GitSHA                           string  `json:"git_sha"`
	CandidateRunID                   int64   `json:"candidate_run_id"`
	StagingAPKFile                   string  `json:"staging_apk_file"`
	StagingAPKSizeBytes              int64   `json:"staging_apk_size_bytes"`
	StagingAPKSHA256                 string  `json:"staging_apk_sha256"`
	APKCertificateSHA256             string  `json:"apk_certificate_sha256"`
	DeploymentRunID                  int64   `json:"deployment_run_id"`
	DeploymentRunAttempt             int64   `json:"deployment_run_attempt"`
	PreviousPublicationReceiptSHA256 *string `json:"previous_publication_receipt_sha256"`
	RecordedAtUTC                    string  `json:"recorded_at_utc"`
}

type publicationPending struct {
	PendingVersion                   int     `json:"pending_version"`
	Repository                       string  `json:"repository"`
	CandidateRunID                   int64   `json:"candidate_run_id"`
	DeploymentRunID                  int64   `json:"deployment_run_id"`
	DeploymentRunAttempt             int64   `json:"deployment_run_attempt"`
	RuntimeReceiptSHA256             string  `json:"runtime_receipt_sha256"`
	ManifestSHA256                   string  `json:"manifest_sha256"`
	BundleManifestSHA256             string  `json:"bundle_manifest_sha256"`
	CandidateMetadataSHA256          string  `json:"candidate_metadata_sha256"`
	StagingAPKSHA256                 string  `json:"staging_apk_sha256"`
	PreviousPublicationReceiptSHA256 *string `json:"previous_publication_receipt_sha256"`
	RecordedAtUTC                    string  `json:"recorded_at_utc"`
}

type validatedPublication struct {
	manifest           releaseManifest
	metadataContents   []byte
	metadataSHA256     string
	apkPath            string
	checksumPath       string
	metadataPath       string
	apkSize            int64
	immutableDirectory string
}

type publicationState struct {
	receiptDir  string
	currentPath string
	pendingPath string
}

func publishCandidate(store *stateStore, config Config, req request, payload publicationPayload, current *loadedReceipt) (any, error) {
	if current == nil || req.ExpectedCurrentReceiptSHA256 == nil || current.SHA256 != *req.ExpectedCurrentReceiptSHA256 ||
		current.Receipt.CandidateRunID != req.CandidateRunID || current.Receipt.ManifestSHA256 != req.ManifestSHA256 {
		return nil, failure("conflict")
	}
	validated, err := validatePublicationPayload(payload, req, current)
	if err != nil {
		return nil, err
	}
	state, err := openPublicationState(store.root)
	if err != nil {
		return nil, err
	}
	pending, err := state.loadPending()
	if err != nil {
		return nil, err
	}
	currentPublicationSHA, currentPublication, err := state.loadCurrent(pending != nil)
	if err != nil {
		return nil, err
	}
	if pending == nil && currentPublicationSHA != nil && currentPublication != nil &&
		publicationAlreadySucceeded(*currentPublication, req, current.SHA256, validated) {
		candidateRoot := filepath.Join(config.PublicRoot, filepath.FromSlash(strings.TrimSuffix(validated.immutableDirectory, "/")))
		if !publicFileMatches(filepath.Join(candidateRoot, validated.manifest.StagingAPKFile), validated.apkSize, validated.manifest.StagingAPKSHA256) ||
			!publicContentsMatch(filepath.Join(candidateRoot, validated.manifest.StagingAPKFile+".sha256"), []byte(validated.manifest.StagingAPKSHA256+"  "+validated.manifest.StagingAPKFile+"\n")) ||
			!publicContentsMatch(filepath.Join(candidateRoot, "candidate.json"), validated.metadataContents) ||
			!publicContentsMatch(filepath.Join(config.PublicRoot, "downloads", "android", "staging-candidate.json"), validated.metadataContents) {
			return nil, failure("state_invalid")
		}
		return publicationResponse{
			ProtocolVersion: publishProtocolVersion, OK: true, Action: "publish",
			PublicationReceiptSHA256: *currentPublicationSHA, RuntimeReceiptSHA256: current.SHA256,
			Receipt: *currentPublication,
		}, nil
	}
	if pending == nil {
		pending = &publicationPending{
			PendingVersion: 1, Repository: config.Repository, CandidateRunID: req.CandidateRunID,
			DeploymentRunID: req.DeploymentRunID, DeploymentRunAttempt: req.DeploymentRunAttempt,
			RuntimeReceiptSHA256: current.SHA256, ManifestSHA256: req.ManifestSHA256,
			BundleManifestSHA256: req.BundleManifestSHA256, CandidateMetadataSHA256: validated.metadataSHA256,
			StagingAPKSHA256:                 validated.manifest.StagingAPKSHA256,
			PreviousPublicationReceiptSHA256: currentPublicationSHA,
			RecordedAtUTC:                    config.Now().UTC().Truncate(time.Second).Format("2006-01-02T15:04:05Z"),
		}
		if err := state.savePending(*pending); err != nil {
			return nil, err
		}
	} else if !pending.matches(req, current.SHA256, validated) {
		return nil, failure("recovery_required")
	}

	receipt := newPublicationReceipt(*pending, validated)
	receiptContents, err := json.Marshal(receipt)
	if err != nil {
		return nil, failure("internal_error")
	}
	receiptSHA := sha256Bytes(receiptContents)
	if currentPublicationSHA == nil && pending.PreviousPublicationReceiptSHA256 != nil {
		return nil, failure("recovery_required")
	}
	if currentPublicationSHA != nil && *currentPublicationSHA != receiptSHA &&
		!equalOptionalStrings(currentPublicationSHA, pending.PreviousPublicationReceiptSHA256) {
		return nil, failure("recovery_required")
	}
	if currentPublication != nil && *currentPublicationSHA == receiptSHA && !reflect.DeepEqual(*currentPublication, receipt) {
		return nil, failure("state_invalid")
	}
	if currentPublicationSHA == nil {
		if err := state.validateOptionalOrphan(receiptSHA, receiptContents); err != nil {
			return nil, err
		}
	}

	if err := installImmutableCandidate(config.PublicRoot, payload, validated); err != nil {
		return nil, err
	}
	if err := activateCandidateMetadata(config.PublicRoot, validated.metadataContents); err != nil {
		return nil, err
	}
	if config.AfterPublish != nil {
		if err := config.AfterPublish(); err != nil {
			return nil, failure("operation_interrupted")
		}
	}
	if err := state.saveReceipt(receiptSHA, receiptContents); err != nil {
		return nil, err
	}
	if currentPublicationSHA == nil || *currentPublicationSHA != receiptSHA {
		if err := state.updateCurrent(pending.PreviousPublicationReceiptSHA256, receiptSHA); err != nil {
			return nil, err
		}
	}
	if err := state.clearPending(*pending); err != nil {
		return nil, err
	}
	return publicationResponse{
		ProtocolVersion: publishProtocolVersion, OK: true, Action: "publish",
		PublicationReceiptSHA256: receiptSHA, RuntimeReceiptSHA256: current.SHA256, Receipt: receipt,
	}, nil
}

func publicationAlreadySucceeded(receipt publicationReceipt, req request, runtimeSHA string, publication validatedPublication) bool {
	manifest := publication.manifest
	return receipt.Operation == "publish" && receipt.RuntimeReceiptSHA256 == runtimeSHA &&
		receipt.ManifestSHA256 == req.ManifestSHA256 && receipt.BundleManifestSHA256 == req.BundleManifestSHA256 &&
		receipt.CandidateMetadataSHA256 == publication.metadataSHA256 && receipt.Version == manifest.Version &&
		receipt.VersionCode == manifest.VersionCode && receipt.GitSHA == manifest.GitSHA &&
		receipt.CandidateRunID == req.CandidateRunID && receipt.StagingAPKFile == manifest.StagingAPKFile &&
		receipt.StagingAPKSizeBytes == publication.apkSize && receipt.StagingAPKSHA256 == manifest.StagingAPKSHA256 &&
		receipt.APKCertificateSHA256 == manifest.APKCertificateSHA256 && receipt.DeploymentRunID == req.DeploymentRunID &&
		receipt.DeploymentRunAttempt <= req.DeploymentRunAttempt
}

func (pending publicationPending) matches(req request, runtimeSHA string, publication validatedPublication) bool {
	return pending.PendingVersion == 1 && pending.Repository == officialRepository &&
		pending.CandidateRunID == req.CandidateRunID && pending.DeploymentRunID == req.DeploymentRunID &&
		req.DeploymentRunAttempt >= pending.DeploymentRunAttempt && pending.RuntimeReceiptSHA256 == runtimeSHA &&
		pending.ManifestSHA256 == req.ManifestSHA256 && pending.BundleManifestSHA256 == req.BundleManifestSHA256 &&
		pending.CandidateMetadataSHA256 == publication.metadataSHA256 &&
		pending.StagingAPKSHA256 == publication.manifest.StagingAPKSHA256 && validUTCTimestamp(pending.RecordedAtUTC)
}

func newPublicationReceipt(pending publicationPending, publication validatedPublication) publicationReceipt {
	manifest := publication.manifest
	return publicationReceipt{
		ReceiptVersion: 1, Environment: "staging", Operation: "publish", Repository: officialRepository,
		RuntimeReceiptSHA256: pending.RuntimeReceiptSHA256, ManifestSHA256: pending.ManifestSHA256,
		BundleManifestSHA256: pending.BundleManifestSHA256, CandidateMetadataSHA256: pending.CandidateMetadataSHA256,
		Version: manifest.Version, VersionCode: manifest.VersionCode, GitSHA: manifest.GitSHA,
		CandidateRunID: pending.CandidateRunID, StagingAPKFile: manifest.StagingAPKFile,
		StagingAPKSizeBytes: publication.apkSize, StagingAPKSHA256: manifest.StagingAPKSHA256,
		APKCertificateSHA256: manifest.APKCertificateSHA256, DeploymentRunID: pending.DeploymentRunID,
		DeploymentRunAttempt:             pending.DeploymentRunAttempt,
		PreviousPublicationReceiptSHA256: pending.PreviousPublicationReceiptSHA256, RecordedAtUTC: pending.RecordedAtUTC,
	}
}

func validatePublicationPayload(payload publicationPayload, req request, current *loadedReceipt) (validatedPublication, error) {
	manifestPath := payload.files["release-manifest.json"]
	manifestContents, err := os.ReadFile(manifestPath)
	if err != nil || sha256Bytes(manifestContents) != req.ManifestSHA256 {
		return validatedPublication{}, failure("invalid_request")
	}
	manifest, err := validateManifest(manifestContents, req.CandidateRunID)
	if err != nil || manifest.SHA256 != current.Receipt.ManifestSHA256 || manifest.Version != current.Receipt.Version ||
		manifest.VersionCode != current.Receipt.VersionCode || manifest.GitSHA != current.Receipt.GitSHA {
		return validatedPublication{}, failure("invalid_request")
	}
	prefix := "downloads/android/candidates/" + strconv.FormatInt(req.CandidateRunID, 10) + "/"
	apkRelative := prefix + manifest.StagingAPKFile
	checksumRelative := apkRelative + ".sha256"
	metadataRelative := prefix + "candidate.json"
	expectedFiles := map[string]bool{
		"request.json": true, "release-manifest.json": true, "bundle/bundle-manifest.json": true,
		"bundle/" + apkRelative: true, "bundle/" + checksumRelative: true, "bundle/" + metadataRelative: true,
	}
	if len(payload.files) != len(expectedFiles) {
		return validatedPublication{}, failure("invalid_bundle")
	}
	for name := range payload.files {
		if !expectedFiles[name] {
			return validatedPublication{}, failure("invalid_bundle")
		}
	}
	bundleContents, err := os.ReadFile(payload.files["bundle/bundle-manifest.json"])
	if err != nil || sha256Bytes(bundleContents) != req.BundleManifestSHA256 {
		return validatedPublication{}, failure("invalid_bundle")
	}
	apkSize, apkSHA, err := secureFileSizeAndSHA(payload.files["bundle/"+apkRelative], 0o600, publishAPKLimit)
	if err != nil || apkSHA != manifest.StagingAPKSHA256 {
		return validatedPublication{}, failure("invalid_bundle")
	}
	metadataContents, err := os.ReadFile(payload.files["bundle/"+metadataRelative])
	if err != nil || !validateCandidateMetadata(metadataContents, manifest, req, metadataRelative, apkSize) {
		return validatedPublication{}, failure("invalid_bundle")
	}
	checksumContents, err := os.ReadFile(payload.files["bundle/"+checksumRelative])
	if err != nil || string(checksumContents) != manifest.StagingAPKSHA256+"  "+manifest.StagingAPKFile+"\n" {
		return validatedPublication{}, failure("invalid_bundle")
	}
	fileHashes := map[string]struct {
		size int64
		sha  string
	}{
		apkRelative:      {size: apkSize, sha: apkSHA},
		checksumRelative: {size: int64(len(checksumContents)), sha: sha256Bytes(checksumContents)},
		metadataRelative: {size: int64(len(metadataContents)), sha: sha256Bytes(metadataContents)},
	}
	if !validatePublicationBundleManifest(bundleContents, manifest, req, fileHashes) {
		return validatedPublication{}, failure("invalid_bundle")
	}
	return validatedPublication{
		manifest: manifest, metadataContents: metadataContents,
		metadataSHA256: sha256Bytes(metadataContents), apkPath: payload.files["bundle/"+apkRelative],
		checksumPath: payload.files["bundle/"+checksumRelative], metadataPath: payload.files["bundle/"+metadataRelative],
		apkSize: apkSize, immutableDirectory: prefix,
	}, nil
}

func validatePublicationBundleManifest(contents []byte, manifest releaseManifest, req request, expected map[string]struct {
	size int64
	sha  string
}) bool {
	value, err := decodeStrictJSON(contents)
	if err != nil {
		return false
	}
	object, ok := value.(map[string]any)
	if !ok || !hasExactKeys(object, "bundle_version", "environment", "version", "git_sha", "candidate_run_id", "release_manifest_sha256", "files") {
		return false
	}
	bundleVersion, versionOK := integerValue(object["bundle_version"])
	candidateRunID, runOK := positiveSafeInteger(object["candidate_run_id"])
	files, filesOK := object["files"].([]any)
	if !versionOK || bundleVersion != 1 || object["environment"] != "staging" || object["version"] != manifest.Version ||
		object["git_sha"] != manifest.GitSHA || !runOK || candidateRunID != req.CandidateRunID ||
		object["release_manifest_sha256"] != manifest.SHA256 || !filesOK || len(files) != len(expected) {
		return false
	}
	seen := make(map[string]bool, len(files))
	for _, item := range files {
		entry, ok := item.(map[string]any)
		if !ok || !hasExactKeys(entry, "path", "size_bytes", "sha256") {
			return false
		}
		path, pathOK := entry["path"].(string)
		size, sizeOK := positiveSafeInteger(entry["size_bytes"])
		sha, shaOK := entry["sha256"].(string)
		expectedFile, found := expected[path]
		if !pathOK || !sizeOK || !shaOK || !found || seen[path] || size != expectedFile.size || sha != expectedFile.sha {
			return false
		}
		seen[path] = true
	}
	return len(seen) == len(expected)
}

func validateCandidateMetadata(contents []byte, manifest releaseManifest, req request, metadataRelative string, apkSize int64) bool {
	value, err := decodeStrictJSON(contents)
	if err != nil {
		return false
	}
	object, ok := value.(map[string]any)
	if !ok || !hasExactKeys(object,
		"candidate_metadata_version", "environment", "version", "version_code", "git_sha", "candidate_run_id",
		"manifest_sha256", "file_name", "download_path", "size_bytes", "minimum_android_api", "abis",
		"apk_sha256", "apk_certificate_sha256",
	) {
		return false
	}
	metadataVersion, metadataOK := integerValue(object["candidate_metadata_version"])
	versionCode, versionOK := positiveSafeInteger(object["version_code"])
	candidateRunID, runOK := positiveSafeInteger(object["candidate_run_id"])
	size, sizeOK := positiveSafeInteger(object["size_bytes"])
	minimumAPI, apiOK := integerValue(object["minimum_android_api"])
	abis, abisOK := object["abis"].([]any)
	return metadataOK && metadataVersion == 1 && object["environment"] == "staging" && object["version"] == manifest.Version &&
		versionOK && versionCode == manifest.VersionCode && object["git_sha"] == manifest.GitSHA && runOK && candidateRunID == req.CandidateRunID &&
		object["manifest_sha256"] == manifest.SHA256 && object["file_name"] == manifest.StagingAPKFile &&
		object["download_path"] == "/"+strings.TrimSuffix(metadataRelative, "candidate.json")+manifest.StagingAPKFile &&
		sizeOK && size == apkSize && apiOK && minimumAPI == 24 && abisOK && len(abis) == 1 && abis[0] == "arm64-v8a" &&
		object["apk_sha256"] == manifest.StagingAPKSHA256 && object["apk_certificate_sha256"] == manifest.APKCertificateSHA256
}

func installImmutableCandidate(publicRoot string, payload publicationPayload, publication validatedPublication) error {
	androidRoot := filepath.Join(publicRoot, "downloads", "android")
	candidatesRoot := filepath.Join(androidRoot, "candidates")
	for _, directory := range []string{publicRoot, filepath.Join(publicRoot, "downloads"), androidRoot, candidatesRoot} {
		if err := ensureOwnedDirectory(directory, 0o755); err != nil {
			return failure("publication_failed")
		}
	}
	candidateRoot := filepath.Join(publicRoot, filepath.FromSlash(strings.TrimSuffix(publication.immutableDirectory, "/")))
	if info, err := os.Lstat(candidateRoot); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o755 || !ownedByCurrentUser(info) ||
			!publicFileMatches(filepath.Join(candidateRoot, publication.manifest.StagingAPKFile), publication.apkSize, publication.manifest.StagingAPKSHA256) ||
			!publicContentsMatch(filepath.Join(candidateRoot, publication.manifest.StagingAPKFile+".sha256"), []byte(publication.manifest.StagingAPKSHA256+"  "+publication.manifest.StagingAPKFile+"\n")) ||
			!publicContentsMatch(filepath.Join(candidateRoot, "candidate.json"), publication.metadataContents) {
			return failure("publication_conflict")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return failure("publication_failed")
	}
	temporary, err := os.MkdirTemp(candidatesRoot, ".candidate-")
	if err != nil {
		return failure("publication_failed")
	}
	defer os.RemoveAll(temporary)
	files := []struct {
		source string
		name   string
		size   int64
		sha    string
	}{
		{publication.apkPath, publication.manifest.StagingAPKFile, publication.apkSize, publication.manifest.StagingAPKSHA256},
		{publication.checksumPath, publication.manifest.StagingAPKFile + ".sha256", int64(len(publication.manifest.StagingAPKSHA256) + 2 + len(publication.manifest.StagingAPKFile) + 1), ""},
		{publication.metadataPath, "candidate.json", int64(len(publication.metadataContents)), publication.metadataSHA256},
	}
	for _, file := range files {
		if err := copyPublicFile(file.source, filepath.Join(temporary, file.name), file.size, file.sha); err != nil {
			return failure("publication_failed")
		}
	}
	if err := os.Chmod(temporary, 0o755); err != nil || syncDirectory(temporary) != nil || os.Rename(temporary, candidateRoot) != nil || syncDirectory(candidatesRoot) != nil {
		return failure("publication_failed")
	}
	return nil
}

func activateCandidateMetadata(publicRoot string, contents []byte) error {
	androidRoot := filepath.Join(publicRoot, "downloads", "android")
	temporary, err := os.CreateTemp(androidRoot, ".staging-candidate-")
	if err != nil {
		return failure("publication_failed")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(contents); err != nil || temporary.Chmod(0o644) != nil || temporary.Sync() != nil || temporary.Close() != nil {
		_ = temporary.Close()
		return failure("publication_failed")
	}
	if err := os.Rename(temporaryPath, filepath.Join(androidRoot, "staging-candidate.json")); err != nil || syncDirectory(androidRoot) != nil {
		return failure("publication_failed")
	}
	return nil
}

func ensureOwnedDirectory(path string, mode os.FileMode) error {
	err := os.Mkdir(path, mode)
	if err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != mode || !ownedByCurrentUser(info) {
		return syscall.EPERM
	}
	return nil
}

func copyPublicFile(source string, destination string, expectedSize int64, expectedSHA string) error {
	descriptor, err := syscall.Open(source, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	input := os.NewFile(uintptr(descriptor), source)
	if input == nil {
		syscall.Close(descriptor)
		return syscall.EBADF
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) || info.Size() != expectedSize {
		return syscall.EPERM
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(output, hash), input)
	if copyErr != nil || written != expectedSize || (expectedSHA != "" && hex.EncodeToString(hash.Sum(nil)) != expectedSHA) || output.Chmod(0o644) != nil || output.Sync() != nil || output.Close() != nil {
		_ = output.Close()
		return syscall.EIO
	}
	return nil
}

func secureFileSizeAndSHA(path string, mode os.FileMode, limit int64) (int64, string, error) {
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return 0, "", err
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		syscall.Close(descriptor)
		return 0, "", syscall.EBADF
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != mode || !ownedByCurrentUser(info) || info.Size() <= 0 || info.Size() > limit {
		return 0, "", syscall.EPERM
	}
	hash := sha256.New()
	written, err := io.Copy(hash, file)
	if err != nil || written != info.Size() {
		return 0, "", syscall.EIO
	}
	return written, hex.EncodeToString(hash.Sum(nil)), nil
}

func publicFileMatches(path string, expectedSize int64, expectedSHA string) bool {
	size, sha, err := secureFileSizeAndSHA(path, 0o644, publishAPKLimit)
	return err == nil && size == expectedSize && sha == expectedSHA
}

func publicContentsMatch(path string, expected []byte) bool {
	contents, err := readSecureFile(path, 0o644, publicationStateLimit)
	return err == nil && bytes.Equal(contents, expected)
}

func openPublicationState(root string) (*publicationState, error) {
	receiptDir := filepath.Join(root, "publication-receipts")
	if err := ensurePrivateDirectory(receiptDir); err != nil {
		return nil, failure("state_invalid")
	}
	return &publicationState{
		receiptDir: receiptDir, currentPath: filepath.Join(root, "publication-current"),
		pendingPath: filepath.Join(root, "publication-pending.json"),
	}, nil
}

func requireNoPublicationPending(root string) error {
	state, err := openPublicationState(root)
	if err != nil {
		return err
	}
	pending, err := state.loadPending()
	if err != nil {
		return err
	}
	if pending != nil {
		return failure("recovery_required")
	}
	return nil
}

func (state *publicationState) loadPending() (*publicationPending, error) {
	contents, err := readSecureFile(state.pendingPath, 0o600, publicationStateLimit)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, failure("state_invalid")
	}
	pending, err := parsePublicationPending(contents)
	if err != nil {
		return nil, err
	}
	canonical, _ := json.Marshal(pending)
	if !bytes.Equal(canonical, contents) {
		return nil, failure("state_invalid")
	}
	return &pending, nil
}

func (state *publicationState) savePending(pending publicationPending) error {
	contents, err := json.Marshal(pending)
	if err != nil || writeNoClobber(filepath.Dir(state.pendingPath), state.pendingPath, contents, 0o600, false) != nil {
		return failure("state_invalid")
	}
	return nil
}

func (state *publicationState) clearPending(expected publicationPending) error {
	pending, err := state.loadPending()
	if err != nil || pending == nil || !reflect.DeepEqual(*pending, expected) || os.Remove(state.pendingPath) != nil || syncDirectory(filepath.Dir(state.pendingPath)) != nil {
		return failure("state_invalid")
	}
	return nil
}

func (state *publicationState) loadCurrent(allowOrphans bool) (*string, *publicationReceipt, error) {
	contents, err := readSecureFile(state.currentPath, 0o600, 64)
	if errors.Is(err, os.ErrNotExist) {
		entries, readErr := os.ReadDir(state.receiptDir)
		if readErr != nil || (!allowOrphans && len(entries) != 0) {
			return nil, nil, failure("state_invalid")
		}
		return nil, nil, nil
	}
	if err != nil || len(contents) != 64 || !validNonzeroSHA256(string(contents)) {
		return nil, nil, failure("state_invalid")
	}
	sha := string(contents)
	receipt, err := state.validateChain(sha)
	if err != nil {
		return nil, nil, err
	}
	return &sha, receipt, nil
}

func (state *publicationState) validateChain(head string) (*publicationReceipt, error) {
	seen := make(map[string]bool)
	next := head
	var headReceipt *publicationReceipt
	for len(seen) < maxReceiptChainLength {
		if !validNonzeroSHA256(next) || seen[next] {
			return nil, failure("state_invalid")
		}
		seen[next] = true
		contents, err := readSecureFile(filepath.Join(state.receiptDir, next+".json"), 0o444, publicationStateLimit)
		if err != nil || sha256Bytes(contents) != next {
			return nil, failure("state_invalid")
		}
		receipt, err := parsePublicationReceipt(contents)
		if err != nil {
			return nil, err
		}
		if headReceipt == nil {
			copy := receipt
			headReceipt = &copy
		}
		if receipt.PreviousPublicationReceiptSHA256 == nil {
			return headReceipt, nil
		}
		next = *receipt.PreviousPublicationReceiptSHA256
	}
	return nil, failure("state_invalid")
}

func (state *publicationState) validateOptionalOrphan(expectedSHA string, expectedContents []byte) error {
	entries, err := os.ReadDir(state.receiptDir)
	if err != nil || len(entries) > 1 {
		return failure("state_invalid")
	}
	if len(entries) == 0 {
		return nil
	}
	if entries[0].IsDir() || entries[0].Name() != expectedSHA+".json" {
		return failure("state_invalid")
	}
	contents, err := readSecureFile(filepath.Join(state.receiptDir, entries[0].Name()), 0o444, publicationStateLimit)
	if err != nil || !bytes.Equal(contents, expectedContents) {
		return failure("state_invalid")
	}
	return nil
}

func (state *publicationState) saveReceipt(sha string, contents []byte) error {
	if !validNonzeroSHA256(sha) || sha256Bytes(contents) != sha || writeNoClobber(state.receiptDir, filepath.Join(state.receiptDir, sha+".json"), contents, 0o444, true) != nil {
		return failure("state_invalid")
	}
	return nil
}

func (state *publicationState) updateCurrent(expected *string, next string) error {
	current, _, err := state.loadCurrent(true)
	if err != nil || !equalOptionalStrings(current, expected) || !validNonzeroSHA256(next) {
		if err != nil {
			return err
		}
		return failure("conflict")
	}
	directory := filepath.Dir(state.currentPath)
	temporary, err := os.CreateTemp(directory, ".publication-current-")
	if err != nil {
		return failure("state_invalid")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := io.WriteString(temporary, next); err != nil || temporary.Chmod(0o600) != nil || temporary.Sync() != nil || temporary.Close() != nil || os.Rename(temporaryPath, state.currentPath) != nil || syncDirectory(directory) != nil {
		_ = temporary.Close()
		return failure("state_invalid")
	}
	return nil
}

func parsePublicationPending(contents []byte) (publicationPending, error) {
	value, err := decodeStrictJSON(contents)
	if err != nil {
		return publicationPending{}, failure("state_invalid")
	}
	object, ok := value.(map[string]any)
	if !ok || !hasExactKeys(object,
		"pending_version", "repository", "candidate_run_id", "deployment_run_id", "deployment_run_attempt",
		"runtime_receipt_sha256", "manifest_sha256", "bundle_manifest_sha256", "candidate_metadata_sha256",
		"staging_apk_sha256", "previous_publication_receipt_sha256", "recorded_at_utc",
	) {
		return publicationPending{}, failure("state_invalid")
	}
	var pending publicationPending
	version, versionOK := integerValue(object["pending_version"])
	previous, previousOK := optionalSHA256(object["previous_publication_receipt_sha256"])
	pending.CandidateRunID, ok = positiveSafeInteger(object["candidate_run_id"])
	if !versionOK || version != 1 || object["repository"] != officialRepository || !ok {
		return publicationPending{}, failure("state_invalid")
	}
	pending.DeploymentRunID, ok = positiveSafeInteger(object["deployment_run_id"])
	if !ok {
		return publicationPending{}, failure("state_invalid")
	}
	pending.DeploymentRunAttempt, ok = positiveSafeInteger(object["deployment_run_attempt"])
	if !ok || !previousOK {
		return publicationPending{}, failure("state_invalid")
	}
	shaFields := []struct {
		name   string
		target *string
	}{
		{"runtime_receipt_sha256", &pending.RuntimeReceiptSHA256}, {"manifest_sha256", &pending.ManifestSHA256},
		{"bundle_manifest_sha256", &pending.BundleManifestSHA256}, {"candidate_metadata_sha256", &pending.CandidateMetadataSHA256},
		{"staging_apk_sha256", &pending.StagingAPKSHA256},
	}
	for _, field := range shaFields {
		value, ok := object[field.name].(string)
		if !ok || !validNonzeroSHA256(value) {
			return publicationPending{}, failure("state_invalid")
		}
		*field.target = value
	}
	recorded, ok := object["recorded_at_utc"].(string)
	if !ok || !validUTCTimestamp(recorded) {
		return publicationPending{}, failure("state_invalid")
	}
	pending.PendingVersion = 1
	pending.Repository = officialRepository
	pending.PreviousPublicationReceiptSHA256 = previous
	pending.RecordedAtUTC = recorded
	return pending, nil
}

func parsePublicationReceipt(contents []byte) (publicationReceipt, error) {
	var receipt publicationReceipt
	value, err := decodeStrictJSON(contents)
	if err != nil {
		return receipt, failure("state_invalid")
	}
	object, ok := value.(map[string]any)
	if !ok || !hasExactKeys(object,
		"receipt_version", "environment", "operation", "repository", "runtime_receipt_sha256", "manifest_sha256",
		"bundle_manifest_sha256", "candidate_metadata_sha256", "version", "version_code", "git_sha", "candidate_run_id",
		"staging_apk_file", "staging_apk_size_bytes", "staging_apk_sha256", "apk_certificate_sha256",
		"deployment_run_id", "deployment_run_attempt", "previous_publication_receipt_sha256", "recorded_at_utc",
	) {
		return receipt, failure("state_invalid")
	}
	if err := json.Unmarshal(contents, &receipt); err != nil || receipt.ReceiptVersion != 1 || receipt.Environment != "staging" ||
		receipt.Operation != "publish" || receipt.Repository != officialRepository || !validNonzeroSHA256(receipt.RuntimeReceiptSHA256) ||
		!validNonzeroSHA256(receipt.ManifestSHA256) || !validNonzeroSHA256(receipt.BundleManifestSHA256) ||
		!validNonzeroSHA256(receipt.CandidateMetadataSHA256) || !versionPattern.MatchString(receipt.Version) || receipt.VersionCode < 1 || receipt.VersionCode > maxSafeInteger ||
		receipt.CandidateRunID < 1 || receipt.CandidateRunID > maxSafeInteger || receipt.DeploymentRunID < 1 || receipt.DeploymentRunID > maxSafeInteger || receipt.DeploymentRunAttempt < 1 || receipt.DeploymentRunAttempt > maxSafeInteger ||
		receipt.StagingAPKFile != "speakup-v"+receipt.Version+"-staging-arm64.apk" || receipt.StagingAPKSizeBytes < 1 ||
		!validNonzeroSHA256(receipt.StagingAPKSHA256) || !validNonzeroSHA256(receipt.APKCertificateSHA256) ||
		!validNonzeroHex(receipt.GitSHA, gitSHAPattern) || !validUTCTimestamp(receipt.RecordedAtUTC) {
		return publicationReceipt{}, failure("state_invalid")
	}
	if receipt.PreviousPublicationReceiptSHA256 != nil && !validNonzeroSHA256(*receipt.PreviousPublicationReceiptSHA256) {
		return publicationReceipt{}, failure("state_invalid")
	}
	canonical, _ := json.Marshal(receipt)
	if !bytes.Equal(canonical, contents) {
		return publicationReceipt{}, failure("state_invalid")
	}
	return receipt, nil
}
