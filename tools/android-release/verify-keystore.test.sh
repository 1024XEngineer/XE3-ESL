#!/usr/bin/env bash

set -euo pipefail

readonly script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly temporary_directory="$(mktemp -d)"
readonly fake_bin="$temporary_directory/bin"
readonly certificate_sha256="cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
trap 'rm -rf "$temporary_directory"' EXIT

mkdir -p "$fake_bin"
printf '%s\n' 'fixture keystore' > "$temporary_directory/release.keystore"
cat > "$fake_bin/keytool" <<EOF
#!/usr/bin/env bash
cat <<'REPORT'
Alias name: speakup-release
Entry type: PrivateKeyEntry
Certificate fingerprints:
         SHA256: CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC:CC
REPORT
EOF
chmod +x "$fake_bin/keytool"

report="$(
  PATH="$fake_bin:$PATH" \
    JAVA_HOME="$temporary_directory" \
    SPEAKUP_ANDROID_KEY_ALIAS=speakup-release \
    SPEAKUP_ANDROID_STORE_PASSWORD=fixture-password \
    SPEAKUP_ANDROID_CERT_SHA256="$certificate_sha256" \
    "$script_directory/verify-keystore.sh" \
      "$temporary_directory/release.keystore"
)"
grep -Fqx "certificateSha256=$certificate_sha256" <<< "$report"

if PATH="$fake_bin:$PATH" \
  JAVA_HOME="$temporary_directory" \
  SPEAKUP_ANDROID_KEY_ALIAS=speakup-release \
  SPEAKUP_ANDROID_STORE_PASSWORD=fixture-password \
  SPEAKUP_ANDROID_CERT_SHA256="${certificate_sha256%?}d" \
  "$script_directory/verify-keystore.sh" \
    "$temporary_directory/release.keystore" \
    > "$temporary_directory/rejected.out" 2>&1; then
  printf 'A mismatched approved certificate was accepted.\n' >&2
  exit 1
fi
grep -Fq \
  'Android release keystore certificate does not match the approved certificate.' \
  "$temporary_directory/rejected.out"

printf '%s\n' 'Android release keystore verifier tests passed'
