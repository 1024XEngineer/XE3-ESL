package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const maxReceiptChainLength = 10000

type brokerLock struct {
	file *os.File
}

func acquireBrokerLock(ctx context.Context, path string) (*brokerLock, error) {
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
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) {
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
	syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN)
	lock.file.Close()
}

type stateStore struct {
	root        string
	manifestDir string
	receiptDir  string
	engineDir   string
	currentPath string
	pendingPath string
	incomingDir string
}

type loadedReceipt struct {
	SHA256       string
	Receipt      brokerReceipt
	Manifest     releaseManifest
	ManifestPath string
}

func openStateStore(root string) (*stateStore, error) {
	manifestDir := filepath.Join(root, "manifests")
	receiptDir := filepath.Join(root, "receipts")
	engineDir := filepath.Join(root, "engine-receipts")
	incomingDir := filepath.Join(root, "incoming")
	for _, directory := range []string{root, manifestDir, receiptDir, engineDir, incomingDir} {
		if err := ensurePrivateDirectory(directory); err != nil {
			return nil, failure("state_invalid")
		}
	}
	return &stateStore{
		root:        root,
		manifestDir: manifestDir,
		receiptDir:  receiptDir,
		engineDir:   engineDir,
		currentPath: filepath.Join(root, "current"),
		pendingPath: filepath.Join(root, "pending.json"),
		incomingDir: incomingDir,
	}, nil
}

func ensurePrivateDirectory(path string) error {
	err := os.Mkdir(path, 0o700)
	if err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 || !ownedByCurrentUser(info) {
		return syscall.EPERM
	}
	return nil
}

func (store *stateStore) validateCurrentChain() ([]*loadedReceipt, error) {
	sha, err := store.readCurrentSHA()
	if err != nil {
		return nil, err
	}
	if sha == nil {
		entries, err := os.ReadDir(store.receiptDir)
		if err != nil || len(entries) != 0 {
			return nil, failure("state_invalid")
		}
		return nil, nil
	}
	return walkReceiptChain(*sha, store.loadReceipt)
}

func (store *stateStore) validateChainAt(sha *string) ([]*loadedReceipt, error) {
	if sha == nil {
		return nil, nil
	}
	return walkReceiptChain(*sha, store.loadReceipt)
}

func walkReceiptChain(head string, load func(string) (*loadedReceipt, error)) ([]*loadedReceipt, error) {
	seen := make(map[string]bool)
	chain := make([]*loadedReceipt, 0)
	next := head
	for {
		if len(chain) >= maxReceiptChainLength || !validNonzeroSHA256(next) || seen[next] {
			return nil, failure("state_invalid")
		}
		seen[next] = true
		loaded, err := load(next)
		if err != nil || loaded == nil || loaded.SHA256 != next {
			return nil, failure("state_invalid")
		}
		chain = append(chain, loaded)
		if loaded.Receipt.PreviousReceiptSHA256 == nil {
			break
		}
		next = *loaded.Receipt.PreviousReceiptSHA256
	}

	positions := make(map[string]int, len(chain))
	for index, loaded := range chain {
		positions[loaded.SHA256] = index
	}
	for index, loaded := range chain {
		receipt := loaded.Receipt
		if receipt.Operation == "deploy" {
			if receipt.RollbackTargetReceiptSHA256 != nil {
				return nil, failure("state_invalid")
			}
			continue
		}
		if receipt.Operation != "rollback" || receipt.PreviousReceiptSHA256 == nil || receipt.RollbackTargetReceiptSHA256 == nil {
			return nil, failure("state_invalid")
		}
		targetIndex, ok := positions[*receipt.RollbackTargetReceiptSHA256]
		targetReceipt := brokerReceipt{}
		if ok {
			targetReceipt = chain[targetIndex].Receipt
		}
		if !ok || targetIndex <= index || index+1 >= len(chain) ||
			targetReceipt.ManifestSHA256 != receipt.ManifestSHA256 ||
			targetReceipt.Version != receipt.Version ||
			targetReceipt.VersionCode != receipt.VersionCode ||
			targetReceipt.GitSHA != receipt.GitSHA ||
			targetReceipt.DatabaseSchemaVersion != receipt.DatabaseSchemaVersion ||
			targetReceipt.PortalImageDigest != receipt.PortalImageDigest ||
			targetReceipt.ServerImageDigest != receipt.ServerImageDigest ||
			targetReceipt.CandidateRunID != receipt.CandidateRunID ||
			chain[index+1].Receipt.DatabaseSchemaVersion != receipt.DatabaseSchemaVersion ||
			chain[index+1].Receipt.PostgresContainerID != receipt.PostgresContainerID {
			return nil, failure("state_invalid")
		}
	}
	if chain[len(chain)-1].Receipt.Operation != "deploy" || chain[len(chain)-1].Receipt.PreviousReceiptSHA256 != nil {
		return nil, failure("state_invalid")
	}
	return chain, nil
}

func (store *stateStore) loadReceipt(sha string) (*loadedReceipt, error) {
	if !validNonzeroSHA256(sha) {
		return nil, failure("state_invalid")
	}
	contents, err := readSecureFile(store.receiptPath(sha), 0o444, storedReceiptLimit)
	if err != nil || sha256Bytes(contents) != sha {
		return nil, failure("state_invalid")
	}
	receipt, err := parseBrokerReceipt(contents)
	if err != nil {
		return nil, err
	}
	manifest, manifestPath, err := store.loadManifest(receipt.ManifestSHA256, receipt.CandidateRunID)
	if err != nil {
		return nil, err
	}
	if receipt.Version != manifest.Version ||
		receipt.VersionCode != manifest.VersionCode ||
		receipt.GitSHA != manifest.GitSHA ||
		receipt.DatabaseSchemaVersion != manifest.DatabaseSchemaVersion ||
		receipt.PortalImageDigest != manifest.PortalImageDigest ||
		receipt.ServerImageDigest != manifest.ServerImageDigest {
		return nil, failure("state_invalid")
	}
	return &loadedReceipt{
		SHA256:       sha,
		Receipt:      receipt,
		Manifest:     manifest,
		ManifestPath: manifestPath,
	}, nil
}

func (store *stateStore) loadManifest(sha string, candidateRunID int64) (releaseManifest, string, error) {
	if !validNonzeroSHA256(sha) {
		return releaseManifest{}, "", failure("state_invalid")
	}
	path := store.manifestPath(sha)
	contents, err := readSecureFile(path, 0o444, manifestLimit)
	if err != nil || sha256Bytes(contents) != sha {
		return releaseManifest{}, "", failure("state_invalid")
	}
	manifest, err := validateManifest(contents, candidateRunID)
	if err != nil || manifest.SHA256 != sha {
		return releaseManifest{}, "", failure("state_invalid")
	}
	return manifest, path, nil
}

func (store *stateStore) saveManifest(sha string, contents []byte) (string, error) {
	if !validNonzeroSHA256(sha) || sha256Bytes(contents) != sha {
		return "", failure("invalid_request")
	}
	path := store.manifestPath(sha)
	if err := writeReadonlyNoClobber(store.manifestDir, path, contents, true); err != nil {
		return "", failure("state_invalid")
	}
	return path, nil
}

func (store *stateStore) saveReceipt(sha string, contents []byte, allowIdentical bool) error {
	if !validNonzeroSHA256(sha) || sha256Bytes(contents) != sha {
		return failure("internal_error")
	}
	if err := writeNoClobber(store.receiptDir, store.receiptPath(sha), contents, 0o444, allowIdentical); err != nil {
		return failure("state_invalid")
	}
	return nil
}

func (store *stateStore) readCurrentSHA() (*string, error) {
	contents, err := readSecureFile(store.currentPath, 0o600, 64)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || len(contents) != 64 {
		return nil, failure("state_invalid")
	}
	sha := string(contents)
	if !validNonzeroSHA256(sha) {
		return nil, failure("state_invalid")
	}
	return &sha, nil
}

func (store *stateStore) updateCurrent(expected *string, next string) error {
	if !validNonzeroSHA256(next) {
		return failure("internal_error")
	}
	current, err := store.readCurrentSHA()
	if err != nil {
		return err
	}
	if !equalOptionalStrings(current, expected) {
		return failure("conflict")
	}

	temporary, err := os.CreateTemp(store.root, ".current-")
	if err != nil {
		return failure("state_invalid")
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return failure("state_invalid")
	}
	if _, err := io.WriteString(temporary, next); err != nil {
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
	removeTemporary = false
	if err := syncDirectory(store.root); err != nil {
		return failure("state_invalid")
	}
	return nil
}

func (store *stateStore) manifestPath(sha string) string {
	return filepath.Join(store.manifestDir, sha+".json")
}

func (store *stateStore) receiptPath(sha string) string {
	return filepath.Join(store.receiptDir, sha+".json")
}

func writeReadonlyNoClobber(directory string, destination string, contents []byte, allowIdentical bool) error {
	return writeNoClobber(directory, destination, contents, 0o444, allowIdentical)
}

func writeNoClobber(directory string, destination string, contents []byte, mode os.FileMode, allowIdentical bool) error {
	temporary, err := os.CreateTemp(directory, ".object-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Link(temporaryPath, destination); err != nil {
		if allowIdentical {
			existing, readErr := readSecureFile(destination, mode, len(contents))
			if readErr == nil && bytes.Equal(existing, contents) {
				return nil
			}
		}
		return err
	}
	return syncDirectory(directory)
}

func readSecureFile(path string, expectedMode os.FileMode, limit int) ([]byte, error) {
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		syscall.Close(descriptor)
		return nil, syscall.EBADF
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != expectedMode || !ownedByCurrentUser(info) || info.Size() < 0 || info.Size() > int64(limit) {
		return nil, syscall.EPERM
	}
	contents, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil || len(contents) > limit {
		return nil, syscall.EFBIG
	}
	return contents, nil
}

func syncSecureFile(path string, expectedMode os.FileMode) error {
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		syscall.Close(descriptor)
		return syscall.EBADF
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != expectedMode || !ownedByCurrentUser(info) {
		return syscall.EPERM
	}
	return file.Sync()
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func equalOptionalStrings(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
