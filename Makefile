SHELL := /bin/bash

.DEFAULT_GOAL := help

.PHONY: \
	help \
	check \
	check-flutter \
	check-flutter-dependencies \
	check-flutter-format \
	check-flutter-analyze \
	check-flutter-test \
	check-go \
	check-go-format \
	check-go-vet \
	check-go-test \
	check-api \
	check-api-dependencies \
	check-api-contracts \
	check-smoke

help:
	@printf '%s\n' \
		'SpeakUp quality checks:' \
		'  make check          Run Flutter, Go, API, and deterministic smoke checks' \
		'  make check-flutter  Run Flutter dependency, format, analysis, and test checks' \
		'  make check-go       Run Go format, vet, and test checks' \
		'  make check-api      Validate OpenAPI, JSON Schema, and contract fixtures' \
		'  make check-smoke    Run the deterministic Mock main flow'

check: check-flutter check-go check-api check-smoke

check-flutter: check-flutter-test

check-flutter-dependencies:
	cd mobile && flutter pub get --enforce-lockfile

check-flutter-format: check-flutter-dependencies
	cd mobile && dart format --output=none --set-exit-if-changed lib test

check-flutter-analyze: check-flutter-format
	cd mobile && flutter analyze --no-pub

check-flutter-test: check-flutter-analyze
	cd mobile && flutter test --no-pub

check-go: check-go-test

check-go-format:
	@set -euo pipefail; \
	unformatted="$$(find server -type f -name '*.go' -print0 | xargs -0 gofmt -l)"; \
	if [[ -n "$$unformatted" ]]; then \
		printf '%s\n' 'Go files need formatting:' "$$unformatted"; \
		exit 1; \
	fi

check-go-vet: check-go-format
	cd server && go vet ./...

check-go-test: check-go-vet
	cd server && go test -count=1 ./...

check-api: check-api-contracts

check-api-dependencies:
	cd api && npm ci

check-api-contracts: check-api-dependencies
	cd api && npm run check

check-smoke:
	@set -euo pipefail; \
	available_tests="$$(cd server && go test -list '^TestDeterministicMainFlow$$' ./internal/smoke)"; \
	if ! grep -qx 'TestDeterministicMainFlow' <<< "$$available_tests"; then \
		printf '%s\n' 'Deterministic smoke entrypoint is missing.'; \
		exit 1; \
	fi
	cd server && go test -count=1 -run '^TestDeterministicMainFlow$$' ./internal/smoke
