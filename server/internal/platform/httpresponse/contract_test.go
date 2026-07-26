package httpresponse

import (
	"bufio"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/apperror"
)

func TestStatusForCategoryMapsEveryCategoryExplicitly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		category apperror.Category
		status   int
	}{
		{apperror.InvalidArgument, http.StatusBadRequest},
		{apperror.Unauthenticated, http.StatusUnauthorized},
		{apperror.PermissionDenied, http.StatusForbidden},
		{apperror.NotFound, http.StatusNotFound},
		{apperror.AlreadyExists, http.StatusConflict},
		{apperror.Conflict, http.StatusConflict},
		{apperror.FailedPrecondition, http.StatusPreconditionFailed},
		{apperror.ResourceExhausted, http.StatusTooManyRequests},
		{apperror.DeadlineExceeded, http.StatusGatewayTimeout},
		{apperror.Unimplemented, http.StatusNotImplemented},
		{apperror.Unavailable, http.StatusServiceUnavailable},
		{apperror.Internal, http.StatusInternalServerError},
	}

	for _, test := range tests {
		status, ok := statusForCategory(test.category)
		if !ok || status != test.status {
			t.Fatalf(
				"statusForCategory(%q) = %d, %v; want %d, true",
				test.category,
				status,
				ok,
				test.status,
			)
		}
	}

	if status, ok := statusForCategory(apperror.Category("future")); ok || status != 0 {
		t.Fatalf("unknown category mapped to %d, %v", status, ok)
	}
}

func TestCanonicalHTTPStatusByCodeMatchesAPIContract(t *testing.T) {
	t.Parallel()

	enumCodes, apiStatuses := readAPIErrorContract(t)
	if len(enumCodes) != len(apiStatuses) {
		t.Fatalf(
			"ErrorCode enum has %d entries but x-http-status-map has %d",
			len(enumCodes),
			len(apiStatuses),
		)
	}
	if len(canonicalHTTPStatusByCode) != len(apiStatuses) {
		t.Fatalf(
			"renderer has %d codes but API contract has %d",
			len(canonicalHTTPStatusByCode),
			len(apiStatuses),
		)
	}

	for code := range enumCodes {
		apiStatus, exists := apiStatuses[code]
		if !exists {
			t.Fatalf("ErrorCode %q has no x-http-status-map entry", code)
		}
		rendererStatus, exists := canonicalHTTPStatusByCode[code]
		if !exists {
			t.Fatalf("renderer does not recognize API ErrorCode %q", code)
		}
		if rendererStatus != apiStatus {
			t.Fatalf(
				"renderer maps %q to %d; API contract maps it to %d",
				code,
				rendererStatus,
				apiStatus,
			)
		}
	}

	for code := range canonicalHTTPStatusByCode {
		if _, exists := enumCodes[code]; !exists {
			t.Fatalf("renderer recognizes undeclared ErrorCode %q", code)
		}
	}
}

func readAPIErrorContract(t *testing.T) (map[string]struct{}, map[string]int) {
	t.Helper()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	contractPath := filepath.Join(
		filepath.Dir(sourceFile),
		"..",
		"..",
		"..",
		"..",
		"api",
		"common",
		"errors.yaml",
	)
	contract, err := os.Open(contractPath)
	if err != nil {
		t.Fatalf("open API error contract: %v", err)
	}
	defer contract.Close()

	enumCodes := make(map[string]struct{})
	statuses := make(map[string]int)
	inErrorCode := false
	section := ""

	scanner := bufio.NewScanner(contract)
	for scanner.Scan() {
		line := scanner.Text()
		switch line {
		case "    ErrorCode:":
			inErrorCode = true
			continue
		case "      enum:":
			if inErrorCode {
				section = "enum"
			}
			continue
		case "      x-http-status-map:":
			if inErrorCode {
				section = "status"
			}
			continue
		}

		if !inErrorCode {
			continue
		}
		if section == "enum" && strings.HasPrefix(line, "        - ") {
			enumCodes[strings.TrimSpace(strings.TrimPrefix(line, "        - "))] = struct{}{}
			continue
		}
		if section == "status" {
			if !strings.HasPrefix(line, "        ") {
				break
			}
			key, rawStatus, found := strings.Cut(strings.TrimSpace(line), ":")
			if !found {
				t.Fatalf("parse x-http-status-map line %q", line)
			}
			status, err := strconv.Atoi(strings.TrimSpace(rawStatus))
			if err != nil {
				t.Fatalf("parse status for %q: %v", key, err)
			}
			statuses[key] = status
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan API error contract: %v", err)
	}
	if len(enumCodes) == 0 || len(statuses) == 0 {
		t.Fatalf(
			"API ErrorCode contract was not found: enum=%d statuses=%d",
			len(enumCodes),
			len(statuses),
		)
	}
	return enumCodes, statuses
}
