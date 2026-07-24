package identity

import (
	"bytes"
	"strings"
	"testing"
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

	encoded, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=8,t=1,p=1$") {
		t.Fatalf("unexpected PHC encoding: %q", encoded)
	}

	valid, needsRehash, err := hasher.Verify(
		"correct horse battery staple",
		encoded,
	)
	if err != nil || !valid || needsRehash {
		t.Fatalf("verify = %v, %v, %v", valid, needsRehash, err)
	}

	valid, _, err = hasher.Verify("incorrect password value", encoded)
	if err != nil || valid {
		t.Fatalf("incorrect password verified: %v, %v", valid, err)
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
	encoded, err := oldHasher.Hash("correct horse battery staple")
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
		if _, err := hasher.Hash("correct horse battery staple"); err != nil {
			b.Fatal(err)
		}
	}
}
