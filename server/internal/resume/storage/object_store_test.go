// 本文件验证 Resume 私有对象存储适配器的完整生命周期。
package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"
	"time"

	objectstorefake "github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore/fake"
)

// TestObjectStoreLifecycle 验证 Resume 适配器的保存、读取、签名和删除链路。
func TestObjectStoreLifecycle(t *testing.T) {
	store, err := objectstorefake.New("resume/v1", time.Minute)
	if err != nil {
		t.Fatalf("new fake store: %v", err)
	}
	storage, err := NewObjectStore(store)
	if err != nil {
		t.Fatalf("NewObjectStore: %v", err)
	}
	body := []byte("%PDF-1.4 test")
	sum := sha256.Sum256(body)
	checksum := hex.EncodeToString(sum[:])
	key := "resume/v1/user/document.pdf"
	if err := storage.Put(context.Background(), key, bytes.NewReader(body), int64(len(body)), checksum); err != nil {
		t.Fatalf("Put: %v", err)
	}
	reader, err := storage.Open(context.Background(), key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	read, _ := io.ReadAll(reader)
	_ = reader.Close()
	if !bytes.Equal(read, body) {
		t.Fatalf("read body = %q", read)
	}
	url, expiresAt, err := storage.SignedReadURL(context.Background(), key, time.Minute)
	if err != nil || url == "" || expiresAt.IsZero() {
		t.Fatalf("SignedReadURL = %q, %v, %v", url, expiresAt, err)
	}
	if err := storage.Delete(context.Background(), key); err != nil || store.Has(key) {
		t.Fatalf("Delete = %v, has = %v", err, store.Has(key))
	}
}

// TestObjectStoreRejectsChecksumMismatch 验证适配器不会保存摘要不一致的对象。
func TestObjectStoreRejectsChecksumMismatch(t *testing.T) {
	store, _ := objectstorefake.New("resume/v1", time.Minute)
	storage, _ := NewObjectStore(store)
	body := []byte("%PDF-1.4 test")
	if err := storage.Put(
		context.Background(),
		"resume/v1/user/document.pdf",
		bytes.NewReader(body),
		int64(len(body)),
		stringsRepeat("0", 64),
	); err == nil {
		t.Fatal("checksum mismatch was accepted")
	}
}

// stringsRepeat 避免测试依赖魔法长字符串。
func stringsRepeat(value string, count int) string {
	var buffer bytes.Buffer
	for range count {
		buffer.WriteString(value)
	}
	return buffer.String()
}
