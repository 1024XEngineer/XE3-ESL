package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvUpwardsSetsMissingValuesOnly(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "server", "cmd")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, ".env"),
		[]byte(`
# local config
DATABASE_URL=postgres://local
SERVER_PORT=8080
QUOTED="hello world"
INLINE=value # comment
export EXPORTED=yes
`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	t.Setenv("SERVER_PORT", "9090")
	unsetForTest(t, "DATABASE_URL")
	unsetForTest(t, "QUOTED")
	unsetForTest(t, "INLINE")
	unsetForTest(t, "EXPORTED")
	if err := os.Chdir(child); err != nil {
		t.Fatal(err)
	}

	if err := LoadDotEnvUpwards(); err != nil {
		t.Fatalf("LoadDotEnvUpwards() error = %v", err)
	}

	if got := os.Getenv("DATABASE_URL"); got != "postgres://local" {
		t.Fatalf("DATABASE_URL = %q", got)
	}
	if got := os.Getenv("SERVER_PORT"); got != "9090" {
		t.Fatalf("SERVER_PORT = %q, want existing value", got)
	}
	if got := os.Getenv("QUOTED"); got != "hello world" {
		t.Fatalf("QUOTED = %q", got)
	}
	if got := os.Getenv("INLINE"); got != "value" {
		t.Fatalf("INLINE = %q", got)
	}
	if got := os.Getenv("EXPORTED"); got != "yes" {
		t.Fatalf("EXPORTED = %q", got)
	}
}

func TestLoadDotEnvUpwardsAllowsMissingFile(t *testing.T) {
	dir := t.TempDir()
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := LoadDotEnvUpwards(); err != nil {
		t.Fatalf("LoadDotEnvUpwards() error = %v", err)
	}
}

func unsetForTest(t *testing.T, key string) {
	t.Helper()
	previous, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		var err error
		if existed {
			err = os.Setenv(key, previous)
		} else {
			err = os.Unsetenv(key)
		}
		if err != nil {
			t.Fatalf("restore %s: %v", key, err)
		}
	})
}
