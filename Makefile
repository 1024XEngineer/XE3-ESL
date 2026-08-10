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
	check-qiniu-llm-live \
	check-oss-live \
	check-kodo-live \
	check-resume-ocr-live \
	check-api \
	check-api-dependencies \
	check-api-contracts \
	check-smoke \
	dev-android \
	dev-ios-simulator \
	test-ios-simulator-scenes

help:
	@printf '%s\n' \
		'SpeakUp quality checks:' \
		'  make check          Run Flutter, Go, API, and deterministic smoke checks' \
		'  make check-flutter  Run Flutter dependency, format, analysis, and test checks' \
		'  make check-go       Run Go format, vet, and test checks' \
		'  make check-qiniu-llm-live Run the real Qiniu text generation smoke test' \
		'  make check-oss-live Run the real OSS lifecycle test with exported OSS_* variables' \
		'  make check-kodo-live Run the real Kodo lifecycle test with exported QINIU_* variables' \
		'  make check-resume-ocr-live Run the PaddleOCR hosted API test with an explicit PDF' \
		'  make check-api      Validate OpenAPI, JSON Schema, and contract fixtures' \
		'  make check-smoke    Run the deterministic Mock main flow' \
		'  make dev-android    Start the backend and run the App on an Android device' \
		'  make dev-ios-simulator  Start the backend and run without AvatarKit on an iOS Simulator'
	@printf '%s\n' \
		'  make test-ios-simulator-scenes  Verify real scene and IELTS launch flows on an iOS Simulator'

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

check-qiniu-llm-live:
	@set -euo pipefail; \
	required=(TEXT_GENERATION_PROVIDER QINIU_AI_BASE_URL QINIU_AI_MODEL QINIU_AI_SPEECH_FEEDBACK_MODEL QINIU_AI_API_KEY QINIU_LLM_LIVE_TEST); \
	missing=(); \
	for name in "$${required[@]}"; do \
		if [[ -z "$${!name:-}" ]]; then missing+=("$$name"); fi; \
	done; \
	if (( $${#missing[@]} > 0 )); then \
		printf '%s\n' 'This target intentionally does not load or execute .env.'; \
		printf 'Export the required Qiniu AI variables before running this target. Missing:'; \
		printf ' %s' "$${missing[@]}"; \
		printf '\n'; \
		exit 1; \
	fi; \
	if [[ "$${TEXT_GENERATION_PROVIDER}" != "qiniu" ]]; then \
		printf '%s\n' 'Set and export TEXT_GENERATION_PROVIDER=qiniu.'; \
		exit 1; \
	fi; \
	if [[ "$${QINIU_LLM_LIVE_TEST}" != "1" ]]; then \
		printf '%s\n' 'Set and export QINIU_LLM_LIVE_TEST=1 to opt in to billable requests.'; \
		exit 1; \
	fi; \
	$(MAKE) --no-print-directory check-qiniu-llm-live-go

.PHONY: check-qiniu-llm-live-go
check-qiniu-llm-live-go:
	cd server && go test -count=1 -run '^TestLiveQiniuTextGeneration$$' ./internal/providers/qiniu

check-oss-live:
	@set -euo pipefail; \
	required=(OSS_ENABLED OSS_REGION OSS_ENDPOINT OSS_BUCKET OSS_ACCESS_KEY_ID OSS_ACCESS_KEY_SECRET OSS_AUDIO_PREFIX OSS_SIGNED_URL_TTL); \
	missing=(); \
	for name in "$${required[@]}"; do \
		if [[ -z "$${!name:-}" ]]; then missing+=("$$name"); fi; \
	done; \
	if (( $${#missing[@]} > 0 )); then \
		printf '%s\n' 'This target intentionally does not load or execute .env.'; \
		printf 'Export the required OSS variables before running this target. Missing:'; \
		printf ' %s' "$${missing[@]}"; \
		printf '\n'; \
		exit 1; \
	fi; \
	if [[ "$${OSS_ENABLED}" != "1" && \
	      "$${OSS_ENABLED}" != "true" && \
	      "$${OSS_ENABLED}" != "TRUE" && \
	      "$${OSS_ENABLED}" != "True" ]]; then \
		printf '%s\n' 'Set and export OSS_ENABLED=1 for the real OSS lifecycle test.'; \
		exit 1; \
	fi; \
	if [[ "$${OSS_LIVE_TEST:-0}" != "1" ]]; then \
		printf '%s\n' 'Set and export OSS_LIVE_TEST=1 to opt in to the real OSS lifecycle test.'; \
		exit 1; \
	fi; \
	$(MAKE) --no-print-directory check-oss-live-go

.PHONY: check-oss-live-go
check-oss-live-go:
	cd server && go test -count=1 -run '^TestLiveObjectLifecycle$$' ./internal/platform/objectstore/ossstore

check-kodo-live:
	@set -euo pipefail; \
	required=(OSS_ENABLED OBJECT_STORAGE_PROVIDER QINIU_KODO_BUCKET QINIU_KODO_DOMAIN QINIU_KODO_SERVER_SIDE_ENCRYPTION QINIU_ACCESS_KEY QINIU_SECRET_KEY KODO_LIVE_TEST); \
	missing=(); \
	for name in "$${required[@]}"; do \
		if [[ -z "$${!name:-}" ]]; then missing+=("$$name"); fi; \
	done; \
	if (( $${#missing[@]} > 0 )); then \
		printf '%s\n' 'This target intentionally does not load or execute .env.'; \
		printf 'Export the required Kodo variables before running this target. Missing:'; \
		printf ' %s' "$${missing[@]}"; \
		printf '\n'; \
		exit 1; \
	fi; \
	if [[ "$${OSS_ENABLED}" != "1" && "$${OSS_ENABLED}" != "true" ]]; then \
		printf '%s\n' 'Set and export OSS_ENABLED=1 for the real Kodo lifecycle test.'; \
		exit 1; \
	fi; \
	if [[ "$${OBJECT_STORAGE_PROVIDER}" != "qiniu_kodo" ]]; then \
		printf '%s\n' 'Set and export OBJECT_STORAGE_PROVIDER=qiniu_kodo.'; \
		exit 1; \
	fi; \
	if [[ "$${QINIU_KODO_SERVER_SIDE_ENCRYPTION}" != "1" && "$${QINIU_KODO_SERVER_SIDE_ENCRYPTION}" != "true" ]]; then \
		printf '%s\n' 'Enable Kodo server-side encryption, then attest it with QINIU_KODO_SERVER_SIDE_ENCRYPTION=1.'; \
		exit 1; \
	fi; \
	if [[ "$${KODO_LIVE_TEST}" != "1" ]]; then \
		printf '%s\n' 'Set and export KODO_LIVE_TEST=1 to opt in to the real Kodo lifecycle test.'; \
		exit 1; \
	fi; \
	$(MAKE) --no-print-directory check-kodo-live-go

.PHONY: check-kodo-live-go
check-kodo-live-go:
	cd server && go test -count=1 -run '^TestLiveKodoObjectLifecycle$$' ./internal/platform/objectstore/kodostore

check-resume-ocr-live:
	@set -euo pipefail; \
	required=(OSS_ENABLED OSS_REGION OSS_ENDPOINT OSS_BUCKET OSS_ACCESS_KEY_ID OSS_ACCESS_KEY_SECRET OSS_RESUME_PREFIX OSS_SIGNED_URL_TTL RESUME_OCR_ENABLED PADDLEOCR_ACCESS_TOKEN RESUME_OCR_LIVE_TEST RESUME_OCR_LIVE_TEST_PDF); \
	missing=(); \
	for name in "$${required[@]}"; do \
		if [[ -z "$${!name:-}" ]]; then missing+=("$$name"); fi; \
	done; \
	if (( $${#missing[@]} > 0 )); then \
		printf '%s\n' 'This live test intentionally does not load or execute .env.'; \
		printf 'Export the required variables before running this target. Missing:'; \
		printf ' %s' "$${missing[@]}"; \
		printf '\n'; \
		exit 1; \
	fi; \
	if [[ "$${OSS_ENABLED}" != "1" && "$${OSS_ENABLED}" != "true" ]]; then \
		printf '%s\n' 'Set and export OSS_ENABLED=1.'; \
		exit 1; \
	fi; \
	if [[ "$${RESUME_OCR_ENABLED}" != "1" && "$${RESUME_OCR_ENABLED}" != "true" ]]; then \
		printf '%s\n' 'Set and export RESUME_OCR_ENABLED=1.'; \
		exit 1; \
	fi; \
	if [[ "$${RESUME_OCR_LIVE_TEST}" != "1" ]]; then \
		printf '%s\n' 'Set and export RESUME_OCR_LIVE_TEST=1 to opt in to the hosted API call.'; \
		exit 1; \
	fi; \
	$(MAKE) --no-print-directory check-resume-ocr-live-go

.PHONY: check-resume-ocr-live-go
check-resume-ocr-live-go:
	cd server && go test -count=1 -run '^TestLiveRecognizePDF$$' ./internal/providers/paddleocr

check-api: check-api-contracts

check-api-dependencies:
	cd api && npm ci

check-api-contracts: check-api-dependencies
	cd api && npm run check

check-smoke:
	@set -euo pipefail; \
	available_tests="$$(cd server && go test -list '^TestDeterministicMainFlow$$' ./test/smoke)"; \
	if ! grep -qx 'TestDeterministicMainFlow' <<< "$$available_tests"; then \
		printf '%s\n' 'Deterministic smoke entrypoint is missing.'; \
		exit 1; \
	fi
	cd server && go test -count=1 -run '^TestDeterministicMainFlow$$' ./test/smoke

dev-android:
	./tools/android-dev/run.sh

dev-ios-simulator:
	./tools/ios-simulator-dev/run.sh

test-ios-simulator-scenes:
	./tools/ios-simulator-dev/run.sh test \
		integration_test/real_agent_e2e_test.dart \
		--plain-name 'real iOS IELTS Part 1 creates a Practice Session'
