package identity

import (
	"bytes"
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"
)

var testArgon2idParams = Argon2idParams{
	MemoryKiB:   8,
	Iterations:  1,
	Parallelism: 1,
	SaltLength:  8,
	KeyLength:   16,
}

func TestArgon2idHashAndVerify(t *testing.T) {
	hasher, err := NewArgon2idHasher(
		testArgon2idParams,
		bytes.NewReader([]byte("12345678abcdefgh")),
		1,
	)
	if err != nil {
		t.Fatalf("new hasher: %v", err)
	}

	encoded, err := hasher.Hash(context.Background(), "correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=8,t=1,p=1$") {
		t.Fatalf("unexpected PHC encoding: %q", encoded)
	}
	parts := strings.Split(encoded, "$")
	for _, part := range parts[4:] {
		if strings.Contains(part, "=") || len(part)%4 == 1 {
			t.Fatalf("PHC segment is not unpadded RawStd Base64: %q", part)
		}
	}

	valid, needsRehash, err := hasher.Verify(
		context.Background(),
		"correct horse battery staple",
		encoded,
	)
	if err != nil || !valid || needsRehash {
		t.Fatalf("verify = %v, %v, %v", valid, needsRehash, err)
	}

	valid, _, err = hasher.Verify(
		context.Background(),
		"incorrect password value",
		encoded,
	)
	if err != nil || valid {
		t.Fatalf("incorrect password verified: %v, %v", valid, err)
	}

	parts[4] += "="
	if _, _, err := hasher.Verify(
		context.Background(),
		"correct horse battery staple",
		strings.Join(parts, "$"),
	); err == nil {
		t.Fatal("expected padded salt to be rejected")
	}
}

func TestArgon2idNeedsRehash(t *testing.T) {
	oldHasher, err := NewArgon2idHasher(
		testArgon2idParams,
		bytes.NewReader([]byte("12345678")),
		1,
	)
	if err != nil {
		t.Fatalf("new old hasher: %v", err)
	}
	encoded, err := oldHasher.Hash(
		context.Background(),
		"correct horse battery staple",
	)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	newParams := testArgon2idParams
	newParams.Iterations = 2
	newHasher, err := NewArgon2idHasher(
		newParams,
		bytes.NewReader([]byte("abcdefgh")),
		1,
	)
	if err != nil {
		t.Fatalf("new current hasher: %v", err)
	}
	valid, needsRehash, err := newHasher.Verify(
		context.Background(),
		"correct horse battery staple",
		encoded,
	)
	if err != nil || !valid || !needsRehash {
		t.Fatalf("verify = %v, %v, %v", valid, needsRehash, err)
	}
}

func TestArgon2idRejectsUnsafeStoredParameters(t *testing.T) {
	hasher, err := NewArgon2idHasher(
		testArgon2idParams,
		bytes.NewReader([]byte("12345678")),
		1,
	)
	if err != nil {
		t.Fatalf("new hasher: %v", err)
	}
	_, _, err = hasher.Verify(
		context.Background(),
		"correct horse battery staple",
		"$argon2id$v=19$m=4294967295,t=1,p=1$MTIzNDU2Nzg$MTIzNDU2Nzg5MGFiY2RlZg",
	)
	if err == nil {
		t.Fatal("expected unsafe parameters to be rejected")
	}
}

func TestValidatePasswordUsesCharacterLengthWithoutNormalization(t *testing.T) {
	if err := ValidatePassword("ééééééééééééééé"); err != nil {
		t.Fatalf("expected 15-character password to pass: %v", err)
	}
	if err := ValidatePassword("short password"); err == nil {
		t.Fatal("expected short password to fail")
	}
	if err := ValidatePassword(strings.Repeat("a", 129)); err == nil {
		t.Fatal("expected long password to fail")
	}
}

type blockingReader struct {
	started chan struct{}
	release chan struct{}
}

func (r *blockingReader) Read(buffer []byte) (int, error) {
	select {
	case r.started <- struct{}{}:
	default:
	}
	<-r.release
	for index := range buffer {
		buffer[index] = byte(index + 1)
	}
	return len(buffer), nil
}

func TestArgon2idAdmissionHonorsCancellationAndCapacity(t *testing.T) {
	random := &blockingReader{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	hasher, err := NewArgon2idHasherWithAdmission(
		testArgon2idParams,
		random,
		1,
		1,
		time.Second,
	)
	if err != nil {
		t.Fatalf("new hasher: %v", err)
	}
	firstDone := make(chan error, 1)
	go func() {
		_, err := hasher.Hash(context.Background(), "correct horse battery staple")
		firstDone <- err
	}()
	<-random.started

	queuedContext, cancel := context.WithCancel(context.Background())
	queuedDone := make(chan error, 1)
	go func() {
		_, err := hasher.Hash(queuedContext, "another correct password")
		queuedDone <- err
	}()
	deadline := time.Now().Add(time.Second)
	for len(hasher.admission) != 2 {
		if time.Now().After(deadline) {
			t.Fatal("queued hash did not enter admission")
		}
		runtime.Gosched()
	}
	if _, err := hasher.Hash(
		context.Background(),
		"capacity overflow password",
	); !errors.Is(err, ErrPasswordUnavailable) {
		t.Fatalf("queue-full error = %v", err)
	}
	cancel()
	if err := <-queuedDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("queued cancellation = %v", err)
	}

	close(random.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first hash: %v", err)
	}
	if len(hasher.admission) != 0 || len(hasher.semaphore) != 0 {
		t.Fatal("Argon2id admission slot leaked")
	}
}

func TestArgon2idAdmissionWaitIsFiniteAndReleasesQueueSlot(t *testing.T) {
	random := &blockingReader{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	hasher, err := NewArgon2idHasherWithAdmission(
		testArgon2idParams,
		random,
		1,
		1,
		20*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("new hasher: %v", err)
	}
	firstDone := make(chan error, 1)
	go func() {
		_, err := hasher.Hash(context.Background(), "correct horse battery staple")
		firstDone <- err
	}()
	<-random.started
	if _, err := hasher.Hash(
		context.Background(),
		"another correct password",
	); !errors.Is(err, ErrPasswordUnavailable) {
		t.Fatalf("admission timeout = %v", err)
	}
	if len(hasher.admission) != 1 {
		t.Fatal("timed-out queue slot was not released")
	}
	close(random.release)
	if err := <-firstDone; err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("first hash: %v", err)
	}
}

func BenchmarkArgon2idDefault(b *testing.B) {
	hasher, err := NewArgon2idHasher(
		DefaultArgon2idParams(),
		bytes.NewReader(bytes.Repeat([]byte("benchmark-salt-material"), b.N+1)),
		1,
	)
	if err != nil {
		b.Fatalf("new hasher: %v", err)
	}
	b.ReportAllocs()
	for range b.N {
		if _, err := hasher.Hash(
			context.Background(),
			"correct horse battery staple",
		); err != nil {
			b.Fatal(err)
		}
	}
}
