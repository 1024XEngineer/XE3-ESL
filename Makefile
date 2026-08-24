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
	check-flutter-coverage \
	check-android-release-guard \
	check-go \
	check-go-coverage \
	check-go-format \
	check-go-vet \
	check-go-test \
	check-oss-live \
	check-kodo-live \
	check-resume-ocr-live \
	check-api \
	check-api-dependencies \
	check-api-contracts \
	check-release-candidate \
	check-production-backup \
	check-production-rehearsal \
	check-offline-release \
	check-android-download \
	check-tls-lifecycle \
	check-observability \
	check-production-deploy \
	check-production-nginx \
	check-staging-deploy \
	check-staging-nginx \
	dev-android \
	dev-ios-simulator \
	build-android-release-staging \
	build-android-release-production \
	verify-android-release-staging \
	verify-android-release-production \
	test-ios-simulator-scenes

help:
	@printf '%s\n' \
		'SpeakUp quality checks:' \
		'  make check          Run Flutter, Go, and API checks' \
		'  make check-flutter  Run Flutter dependency, format, analysis, and test checks' \
		'  make check-flutter-coverage  Run Flutter checks and write mobile/coverage/lcov.info' \
		'  make check-android-release-guard  Verify release signing fails closed' \
		'  make check-go       Run Go format, vet, and test checks' \
		'  make check-go-coverage  Run Go checks and write server/coverage.out' \
		'  make check-oss-live Run the real OSS lifecycle test with exported OSS_* variables' \
		'  make check-kodo-live Run the real Kodo lifecycle test with exported QINIU_* variables' \
		'  make check-resume-ocr-live Run the PaddleOCR hosted API test with an explicit PDF' \
		'  make check-api      Validate OpenAPI, JSON Schema, and contract fixtures' \
		'  make check-release-candidate  Validate release metadata and manifest tooling' \
		'  make check-production-backup  Exercise PostgreSQL backup and isolated restore' \
		'  make check-production-rehearsal  Validate the isolated schema 7 to 9 rehearsal' \
		'  make check-offline-release  Validate offline image build/load contracts' \
		'  make check-android-download  Validate the public Android bundle and publish contract' \
		'  make check-tls-lifecycle  Validate TLS issuance and renewal contracts' \
		'  make check-observability  Validate monitoring, alert, and log rotation contracts' \
		'  make check-production-deploy  Validate the immutable Production contract' \
		'  make check-production-nginx  Run nginx -t against the Production template' \
		'  make check-staging-deploy  Validate Staging runtime, schema, lock, and receipt contracts' \
		'  make check-staging-nginx  Run nginx -t against the rendered Staging template' \
		'  make dev-android    Start the backend and run the App on an Android device' \
		'  make dev-ios-simulator  Start the backend on an iOS Simulator' \
		'  make build-android-release-staging  Build the signed staging arm64 APK' \
		'  make build-android-release-production  Build the signed production arm64 APK'
	@printf '%s\n' \
		'  make verify-android-release-{staging,production}  Verify a release APK' \
		'  make test-ios-simulator-scenes  Verify real scene and IELTS launch flows on an iOS Simulator'

check: check-flutter check-go check-api

check-flutter: check-flutter-test check-android-release-guard

check-flutter-dependencies:
	cd mobile && flutter pub get --enforce-lockfile

check-flutter-format: check-flutter-dependencies
	cd mobile && dart format --output=none --set-exit-if-changed lib test

check-flutter-analyze: check-flutter-format
	cd mobile && flutter analyze --no-pub

check-flutter-test: check-flutter-analyze
	cd mobile && flutter test --no-pub

check-flutter-coverage: check-flutter-analyze check-android-release-guard
	cd mobile && flutter test --no-pub --coverage

check-android-release-guard: check-flutter-dependencies
	@set -euo pipefail; \
	output_file="$$(mktemp)"; \
	trap 'rm -f "$$output_file"' EXIT; \
	if ( \
		cd mobile && \
		env -u SPEAKUP_ANDROID_EXTERNAL_SIGNING \
			flutter build apk --release --flavor production \
				--target-platform android-arm64 \
	) >"$$output_file" 2>&1; then \
		printf '%s\n' 'Release build bypassed the explicit signing pipeline.' >&2; \
		exit 1; \
	fi; \
	if ! grep -Fq 'Android release APKs must use the explicit Makefile signing targets.' "$$output_file"; then \
		printf '%s\n' 'Release build failed before the signing guard was reached.' >&2; \
		cat "$$output_file" >&2; \
		exit 1; \
	fi; \
	gradle_java_home="$${JAVA_HOME:-$$( \
		flutter config --machine | \
			sed -n 's/^[[:space:]]*"jdk-dir": "\([^"]*\)".*/\1/p' \
	)}"; \
	if [[ ! -x "$$gradle_java_home/bin/java" ]]; then \
		printf '%s\n' 'Cannot locate the JDK configured for Flutter.' >&2; \
		exit 1; \
	fi; \
	for task in \
		app:assembleStaging \
		app:assembleProduction \
		app:bundleStaging \
		app:bundleProduction; do \
		: >"$$output_file"; \
		if ( \
			cd mobile/android && \
			env -u SPEAKUP_ANDROID_EXTERNAL_SIGNING \
				JAVA_HOME="$$gradle_java_home" \
				PATH="$$gradle_java_home/bin:$$PATH" \
				./gradlew --dry-run "$$task" \
		) >"$$output_file" 2>&1; then \
			printf 'Gradle task bypassed the explicit signing pipeline: %s\n' \
				"$$task" >&2; \
			exit 1; \
		fi; \
		if ! grep -Fq \
			'Android release APKs must use the explicit Makefile signing targets.' \
			"$$output_file"; then \
			printf 'Gradle task failed before the signing guard: %s\n' \
				"$$task" >&2; \
			cat "$$output_file" >&2; \
			exit 1; \
		fi; \
	done
	./tools/android-release/sign.test.sh

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

# discipline: Coverage gates use executed/not-executed statements, so merge
# repeated cross-package counters by OR before publishing the profile.
check-go-coverage: check-go-vet
	@set -euo pipefail; \
	cd server; \
	cover_packages="$$(go list ./... | awk ' \
		index($$0, "/server/test/") == 0 { \
			if (count++ > 0) printf ","; \
			printf "%s", $$0; \
		} \
	')"; \
	go test -count=1 -covermode=atomic \
		-coverpkg="$$cover_packages" \
		-coverprofile=coverage.raw.out ./... 2>&1 | \
		sed -E 's/(coverage: [^[:space:]]+ of statements) in .*/\1/'; \
	{ \
		printf '%s\n' 'mode: atomic'; \
		awk ' \
		NR > 1 { \
			statements[$$1] = $$2; \
			if ($$3 > 0) covered[$$1] = 1; \
		} \
		END { \
			for (block in statements) \
				print block, statements[block], covered[block] + 0; \
		} \
		' coverage.raw.out | LC_ALL=C sort; \
	} > coverage.out; \
	rm coverage.raw.out
	cd server && go tool cover -func=coverage.out > coverage.txt && tail -n 1 coverage.txt

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
	required=(OSS_ENABLED OBJECT_STORAGE_PROVIDER QINIU_KODO_S3_REGION QINIU_KODO_S3_ENDPOINT QINIU_KODO_S3_BUCKET QINIU_KODO_SERVER_SIDE_ENCRYPTION QINIU_ACCESS_KEY QINIU_SECRET_KEY KODO_LIVE_TEST); \
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

check-release-candidate:
	./tools/android-release/verify-keystore.test.sh
	./tools/android-release/sign.test.sh
	./tools/android-release/verify.test.sh
	node --test tools/release-candidate/*.test.mjs

check-production-backup:
	./deploy/production/test-backup.sh

check-production-rehearsal:
	./deploy/production/test-rehearsal.sh

check-offline-release:
	./tools/release-candidate/build-offline-images.test.sh
	./deploy/production/test-offline-images.sh
	./deploy/production/test-offline-images-docker.sh
	./deploy/production/test-offline-compose.sh

check-android-download:
	node --test tools/android-download/*.test.mjs
	./deploy/android-download/test.sh

check-tls-lifecycle:
	./deploy/tls/test.sh
	./deploy/tls/test-nginx.sh

check-observability:
	./deploy/observability/test.sh
	./deploy/observability/test-nginx.sh

check-production-deploy:
	./deploy/production/test.sh

check-production-nginx:
	./deploy/production/test-nginx.sh

check-staging-deploy:
	./deploy/staging/test.sh

check-staging-nginx:
	./deploy/staging/test-nginx.sh

dev-android:
	./tools/android-dev/run.sh

dev-ios-simulator:
	./tools/ios-simulator-dev/run.sh

build-android-release-staging:
	./tools/android-release/verify-keystore.sh \
		"$${SPEAKUP_ANDROID_KEYSTORE_PATH:-}"
	cd mobile && SPEAKUP_ANDROID_EXTERNAL_SIGNING=true flutter build apk \
		--release \
		--flavor staging \
		--target-platform android-arm64 \
		--dart-define=SPEAKUP_API_BASE_URL=https://staging-api.speak-up.top
	./tools/android-release/sign.sh \
		mobile/build/app/outputs/flutter-apk/app-staging-release.apk
	./tools/android-release/verify.sh \
		mobile/build/app/outputs/flutter-apk/app-staging-release.apk

build-android-release-production:
	./tools/android-release/verify-keystore.sh \
		"$${SPEAKUP_ANDROID_KEYSTORE_PATH:-}"
	cd mobile && SPEAKUP_ANDROID_EXTERNAL_SIGNING=true flutter build apk \
		--release \
		--flavor production \
		--target-platform android-arm64 \
		--dart-define=SPEAKUP_API_BASE_URL=https://api.speak-up.top
	./tools/android-release/sign.sh \
		mobile/build/app/outputs/flutter-apk/app-production-release.apk
	./tools/android-release/verify.sh \
		mobile/build/app/outputs/flutter-apk/app-production-release.apk

verify-android-release-staging:
	./tools/android-release/verify.sh \
		mobile/build/app/outputs/flutter-apk/app-staging-release.apk

verify-android-release-production:
	./tools/android-release/verify.sh \
		mobile/build/app/outputs/flutter-apk/app-production-release.apk

test-ios-simulator-scenes:
	./tools/ios-simulator-dev/run.sh test \
		integration_test/real_agent_e2e_test.dart \
		--plain-name 'real iOS IELTS Part 1 creates a Practice Session'
