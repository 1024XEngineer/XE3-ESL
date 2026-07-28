package config

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const dotenvFilename = ".env"

// LoadDotEnvUpwards loads the nearest .env found in the current directory or
// one of its parents. Existing process environment variables win over .env.
func LoadDotEnvUpwards() error {
	path, ok, err := findDotEnvUpwards()
	if err != nil || !ok {
		return err
	}
	return loadDotEnvFile(path)
}

func findDotEnvUpwards() (string, bool, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", false, err
	}
	for {
		candidate := filepath.Join(dir, dotenvFilename)
		info, statErr := os.Stat(candidate)
		switch {
		case statErr == nil && !info.IsDir():
			return candidate, true, nil
		case statErr == nil:
			return "", false, errors.New(".env path is a directory")
		case !errors.Is(statErr, os.ErrNotExist):
			return "", false, statErr
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false, nil
		}
		dir = parent
	}
}

func loadDotEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := parseDotEnvLine(scanner.Text())
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func parseDotEnvLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")
	key, value, found := strings.Cut(line, "=")
	key = strings.TrimSpace(key)
	if !found || !validDotEnvKey(key) {
		return "", "", false
	}
	value = strings.TrimSpace(value)
	value = trimDotEnvComment(value)
	value = strings.TrimSpace(value)
	value = trimDotEnvQuotes(value)
	return key, value, true
}

func validDotEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for index, r := range key {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '_' {
			continue
		}
		if index > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func trimDotEnvComment(value string) string {
	var quote rune
	for index, r := range value {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '#':
			if index == 0 || value[index-1] == ' ' || value[index-1] == '\t' {
				return value[:index]
			}
		}
	}
	return value
}

func trimDotEnvQuotes(value string) string {
	if len(value) < 2 {
		return value
	}
	if value[0] == '"' && value[len(value)-1] == '"' ||
		value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1]
	}
	return value
}
