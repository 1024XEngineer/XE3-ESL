package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	minPasswordCharacters  = 15
	maxPasswordCharacters  = 128
	defaultHashConcurrency = 2
	maxArgon2MemoryKiB     = 256 * 1024
	maxArgon2Iterations    = 10
	maxArgon2Parallelism   = 8
)

var ErrInvalidPasswordHash = errors.New("identity: invalid password hash")

type Argon2idParams struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultArgon2idParams targets interactive login on the MS2 development
// machine. BenchmarkArgon2idDefault records the reproducible local cost.
func DefaultArgon2idParams() Argon2idParams {
	return Argon2idParams{
		MemoryKiB:   64 * 1024,
		Iterations:  3,
		Parallelism: 2,
		SaltLength:  16,
		KeyLength:   32,
	}
}

type Argon2idHasher struct {
	params    Argon2idParams
	random    io.Reader
	semaphore chan struct{}
}

func NewArgon2idHasher(
	params Argon2idParams,
	random io.Reader,
	maxConcurrent int,
) (*Argon2idHasher, error) {
	if err := validateArgon2idParams(params); err != nil {
		return nil, err
	}
	if random == nil {
		random = rand.Reader
	}
	if maxConcurrent < 1 {
		return nil, errors.New("identity: Argon2id concurrency must be positive")
	}
	return &Argon2idHasher{
		params:    params,
		random:    random,
		semaphore: make(chan struct{}, maxConcurrent),
	}, nil
}

// NewDefaultArgon2idHasher caps concurrent hashes at two, bounding this
// process's default Argon2id working set to approximately 128 MiB.
func NewDefaultArgon2idHasher() (*Argon2idHasher, error) {
	return NewArgon2idHasher(
		DefaultArgon2idParams(),
		rand.Reader,
		defaultHashConcurrency,
	)
}

func ValidatePassword(password string) error {
	if !utf8.ValidString(password) {
		return ErrInvalidRequest
	}
	characters := utf8.RuneCountInString(password)
	if characters < minPasswordCharacters || characters > maxPasswordCharacters {
		return ErrInvalidRequest
	}
	return nil
}

func (h *Argon2idHasher) Hash(password string) (string, error) {
	h.semaphore <- struct{}{}
	defer func() { <-h.semaphore }()

	salt := make([]byte, h.params.SaltLength)
	if _, err := io.ReadFull(h.random, salt); err != nil {
		return "", errors.New("identity: password salt generation failed")
	}
	hash := argon2.IDKey(
		[]byte(password),
		salt,
		h.params.Iterations,
		h.params.MemoryKiB,
		h.params.Parallelism,
		h.params.KeyLength,
	)
	return encodeArgon2id(h.params, salt, hash), nil
}

func (h *Argon2idHasher) Verify(
	password string,
	encodedHash string,
) (bool, bool, error) {
	params, salt, expected, err := parseArgon2id(encodedHash)
	if err != nil {
		return false, false, err
	}

	h.semaphore <- struct{}{}
	actual := argon2.IDKey(
		[]byte(password),
		salt,
		params.Iterations,
		params.MemoryKiB,
		params.Parallelism,
		uint32(len(expected)),
	)
	<-h.semaphore

	valid := subtle.ConstantTimeCompare(actual, expected) == 1
	return valid, valid && params != h.params, nil
}

func encodeArgon2id(params Argon2idParams, salt, hash []byte) string {
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		params.MemoryKiB,
		params.Iterations,
		params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	)
}

func parseArgon2id(encoded string) (Argon2idParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Argon2idParams{}, nil, nil, ErrInvalidPasswordHash
	}
	if parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return Argon2idParams{}, nil, nil, ErrInvalidPasswordHash
	}

	parameters := strings.Split(parts[3], ",")
	if len(parameters) != 3 {
		return Argon2idParams{}, nil, nil, ErrInvalidPasswordHash
	}
	memory, ok := parsePHCParameter(parameters[0], "m")
	if !ok {
		return Argon2idParams{}, nil, nil, ErrInvalidPasswordHash
	}
	iterations, ok := parsePHCParameter(parameters[1], "t")
	if !ok {
		return Argon2idParams{}, nil, nil, ErrInvalidPasswordHash
	}
	parallelism, ok := parsePHCParameter(parameters[2], "p")
	if !ok || parallelism > 255 {
		return Argon2idParams{}, nil, nil, ErrInvalidPasswordHash
	}
	params := Argon2idParams{
		MemoryKiB:   memory,
		Iterations:  iterations,
		Parallelism: uint8(parallelism),
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return Argon2idParams{}, nil, nil, ErrInvalidPasswordHash
	}
	hash, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return Argon2idParams{}, nil, nil, ErrInvalidPasswordHash
	}
	params.SaltLength = uint32(len(salt))
	params.KeyLength = uint32(len(hash))
	if err := validateArgon2idParams(params); err != nil {
		return Argon2idParams{}, nil, nil, ErrInvalidPasswordHash
	}
	return params, salt, hash, nil
}

func parsePHCParameter(value, name string) (uint32, bool) {
	prefix := name + "="
	if !strings.HasPrefix(value, prefix) {
		return 0, false
	}
	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, prefix), 10, 32)
	return uint32(parsed), err == nil
}

func validateArgon2idParams(params Argon2idParams) error {
	if params.MemoryKiB < 8*uint32(params.Parallelism) ||
		params.MemoryKiB > maxArgon2MemoryKiB ||
		params.Iterations < 1 ||
		params.Iterations > maxArgon2Iterations ||
		params.Parallelism < 1 ||
		params.Parallelism > maxArgon2Parallelism ||
		params.SaltLength < 8 ||
		params.SaltLength > 64 ||
		params.KeyLength < 16 ||
		params.KeyLength > 64 {
		return ErrInvalidPasswordHash
	}
	return nil
}
