#!/usr/bin/env bash

set -euo pipefail

if [[ "$#" -lt "1" || "$#" -gt "2" ]]; then
  printf 'Usage: SPEAKUP_ANDROID_CERT_SHA256=<fingerprint> %s <apk> [pubspec]\n' "$0" >&2
  exit 2
fi

apk="$1"
expected_certificate_sha256="${SPEAKUP_ANDROID_CERT_SHA256:-}"
script_dir="$(cd "$(dirname "$0")" && pwd)"
repo_dir="$(cd "$script_dir/../.." && pwd)"
pubspec="${2:-$repo_dir/mobile/pubspec.yaml}"

if [[ ! -f "$apk" ]]; then
  printf 'APK does not exist: %s\n' "$apk" >&2
  exit 1
fi
if [[ ! -f "$pubspec" ]]; then
  printf 'pubspec does not exist: %s\n' "$pubspec" >&2
  exit 1
fi
if [[ -z "$expected_certificate_sha256" ]]; then
  printf '%s\n' 'SPEAKUP_ANDROID_CERT_SHA256 is required.' >&2
  exit 1
fi

find_android_tool() {
  local tool_name="$1"
  local sdk_root="${ANDROID_SDK_ROOT:-${ANDROID_HOME:-}}"
  local local_properties="$repo_dir/mobile/android/local.properties"
  local candidate

  if command -v "$tool_name" >/dev/null 2>&1; then
    command -v "$tool_name"
    return
  fi
  if [[ -z "$sdk_root" && -f "$local_properties" ]]; then
    sdk_root="$(sed -n 's/^sdk.dir=//p' "$local_properties")"
  fi
  if [[ -z "$sdk_root" || ! -d "$sdk_root/build-tools" ]]; then
    printf 'Cannot locate Android SDK build-tools for %s.\n' "$tool_name" >&2
    return 1
  fi
  candidate="$(
    find "$sdk_root/build-tools" -type f -name "$tool_name" -print |
      LC_ALL=C sort |
      tail -n 1
  )"
  if [[ -z "$candidate" ]]; then
    printf 'Cannot locate Android SDK tool: %s.\n' "$tool_name" >&2
    return 1
  fi
  printf '%s\n' "$candidate"
}

ensure_java_runtime() {
  local flutter_config
  local flutter_jdk

  if java -version >/dev/null 2>&1; then
    return
  fi
  if ! command -v flutter >/dev/null 2>&1; then
    printf '%s\n' 'Java is required by apksigner.' >&2
    return 1
  fi
  flutter_config="$(flutter config --machine)"
  flutter_jdk="$(
    printf '%s\n' "$flutter_config" |
      sed -n 's/^[[:space:]]*"jdk-dir": "\([^"]*\)".*/\1/p'
  )"
  if [[ -z "$flutter_jdk" || ! -x "$flutter_jdk/bin/java" ]]; then
    printf '%s\n' 'Cannot locate the JDK configured for Flutter.' >&2
    return 1
  fi
  export JAVA_HOME="$flutter_jdk"
  export PATH="$JAVA_HOME/bin:$PATH"
}

aapt="$(find_android_tool aapt)"
apksigner="$(find_android_tool apksigner)"
badging="$("$aapt" dump badging "$apk")"

application_id="$(printf '%s\n' "$badging" | sed -n "s/^package: name='\([^']*\)'.*/\1/p")"
version_code="$(printf '%s\n' "$badging" | sed -n "s/^package: .* versionCode='\([^']*\)'.*/\1/p")"
version_name="$(printf '%s\n' "$badging" | sed -n "s/^package: .* versionName='\([^']*\)'.*/\1/p")"
minimum_android_api="$(printf '%s\n' "$badging" | sed -n "s/^sdkVersion:'\([^']*\)'.*/\1/p")"
native_code="$(printf '%s\n' "$badging" | sed -n '/^native-code:/p')"
pubspec_version="$(
  sed -n -E \
    "s/^version:[[:space:]]*['\"]?([^'\"[:space:]]+)['\"]?[[:space:]]*$/\1/p" \
    "$pubspec"
)"
if [[ ! "$pubspec_version" =~ ^([^+]+)\+([0-9]+)$ ]]; then
  printf '%s\n' 'pubspec version must use <versionName>+<versionCode>.' >&2
  exit 1
fi
expected_version_name="${BASH_REMATCH[1]}"
expected_version_code="${BASH_REMATCH[2]}"

[[ "$application_id" == "com.xengineer.speakup" ]] || {
  printf 'Unexpected applicationId: %s\n' "$application_id" >&2
  exit 1
}
[[ "$version_code" == "$expected_version_code" ]] || {
  printf 'Unexpected versionCode: %s\n' "$version_code" >&2
  exit 1
}
[[ "$version_name" == "$expected_version_name" ]] || {
  printf 'Unexpected versionName: %s\n' "$version_name" >&2
  exit 1
}
[[ "$minimum_android_api" == "24" ]] || {
  printf 'Unexpected minimum Android API: %s\n' "$minimum_android_api" >&2
  exit 1
}
[[ "$native_code" == "native-code: 'arm64-v8a'" ]] || {
  printf 'Unexpected native ABIs: %s\n' "$native_code" >&2
  exit 1
}

ensure_java_runtime
signature_report="$("$apksigner" verify --verbose --print-certs "$apk")"
if printf '%s\n' "$signature_report" | grep -Eq 'certificate DN:.*CN=Android Debug'; then
  printf '%s\n' 'APK uses an Android debug signing certificate.' >&2
  exit 1
fi
certificate_sha256="$(
  printf '%s\n' "$signature_report" |
    sed -n -E \
      's/^Signer (#[0-9]+|\([^)]*\)) certificate SHA-256 digest:[[:space:]]*(.*)$/\2/p' |
    tr '[:upper:]' '[:lower:]' |
    sed 's/[[:space:]:]//g' |
    LC_ALL=C sort -u
)"
expected_certificate_sha256="$(
  printf '%s' "$expected_certificate_sha256" |
    tr '[:upper:]' '[:lower:]' |
    tr -d '[:space:]:'
)"

[[ "$expected_certificate_sha256" =~ ^[0-9a-f]{64}$ ]] || {
  printf '%s\n' 'SPEAKUP_ANDROID_CERT_SHA256 must contain 64 hexadecimal digits.' >&2
  exit 1
}
[[ "$certificate_sha256" =~ ^[0-9a-f]{64}$ ]] || {
  printf '%s\n' \
    'APK signer certificate output is missing or contains multiple certificates.' >&2
  printf 'Observed signer certificate SHA-256 values: %s\n' \
    "${certificate_sha256:-<none>}" >&2
  exit 1
}
[[ "$certificate_sha256" == "$expected_certificate_sha256" ]] || {
  printf '%s\n' 'APK signing certificate SHA-256 does not match the approved certificate.' >&2
  printf 'Observed signer certificate SHA-256: %s\n' "$certificate_sha256" >&2
  exit 1
}

artifact_sha256="$(shasum -a 256 "$apk" | awk '{print $1}')"
printf '%s\n' \
  "applicationId=$application_id" \
  "versionName=$version_name" \
  "versionCode=$version_code" \
  "minimumAndroidApi=$minimum_android_api" \
  'abi=arm64-v8a' \
  'signature=verified' \
  "certificateSha256=$certificate_sha256" \
  "artifactSha256=$artifact_sha256"
