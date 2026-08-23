#!/usr/bin/env bash

set -euo pipefail

if [[ "$#" != "1" ]]; then
  printf 'Usage: %s <keystore>\n' "$0" >&2
  exit 2
fi

keystore="$1"
alias_name="${SPEAKUP_ANDROID_KEY_ALIAS:-}"
expected_certificate_sha256="${SPEAKUP_ANDROID_CERT_SHA256:-}"

if [[ ! -s "$keystore" ]]; then
  printf 'Android release keystore is not a readable, non-empty file.\n' >&2
  exit 1
fi
if [[ -z "$alias_name" ]]; then
  printf 'SPEAKUP_ANDROID_KEY_ALIAS is required.\n' >&2
  exit 1
fi
if [[ -z "${SPEAKUP_ANDROID_STORE_PASSWORD:-}" ]]; then
  printf 'SPEAKUP_ANDROID_STORE_PASSWORD is required.\n' >&2
  exit 1
fi
if [[ -z "$expected_certificate_sha256" ]]; then
  printf 'SPEAKUP_ANDROID_CERT_SHA256 is required.\n' >&2
  exit 1
fi

if [[ -n "${JAVA_HOME:-}" && -x "$JAVA_HOME/bin/keytool" ]]; then
  keytool="$JAVA_HOME/bin/keytool"
elif command -v keytool >/dev/null 2>&1; then
  keytool="$(command -v keytool)"
else
  printf 'Cannot locate keytool.\n' >&2
  exit 1
fi

key_report="$(
  "$keytool" \
    -J-Duser.language=en \
    -J-Duser.country=US \
    -list \
    -v \
    -keystore "$keystore" \
    -alias "$alias_name" \
    -storepass:env SPEAKUP_ANDROID_STORE_PASSWORD
)"
if ! grep -Fq 'Entry type: PrivateKeyEntry' <<< "$key_report"; then
  printf 'Android release alias is not a private key entry.\n' >&2
  exit 1
fi

certificate_sha256="$(
  sed -n 's/^[[:space:]]*SHA256: //p' <<< "$key_report" |
    head -n 1 |
    tr '[:upper:]' '[:lower:]' |
    tr -d '[:space:]:'
)"
expected_certificate_sha256="$(
  printf '%s' "$expected_certificate_sha256" |
    tr '[:upper:]' '[:lower:]' |
    tr -d '[:space:]:'
)"

[[ "$certificate_sha256" =~ ^[0-9a-f]{64}$ ]] || {
  printf 'Cannot read a valid SHA-256 certificate fingerprint from the keystore.\n' >&2
  exit 1
}
[[ "$expected_certificate_sha256" =~ ^[0-9a-f]{64}$ ]] || {
  printf 'SPEAKUP_ANDROID_CERT_SHA256 must contain 64 hexadecimal digits.\n' >&2
  exit 1
}
[[ "$certificate_sha256" == "$expected_certificate_sha256" ]] || {
  printf 'Android release keystore certificate does not match the approved certificate.\n' >&2
  exit 1
}

printf 'certificateSha256=%s\n' "$certificate_sha256"
