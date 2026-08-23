#!/usr/bin/env bash

set -euo pipefail

if [[ "$#" != "1" ]]; then
  printf 'Usage: %s <unsigned-apk>\n' "$0" >&2
  exit 2
fi

apk="$1"
keystore="${SPEAKUP_ANDROID_KEYSTORE_PATH:-}"
alias_name="${SPEAKUP_ANDROID_KEY_ALIAS:-}"
script_dir="$(cd "$(dirname "$0")" && pwd)"
repo_dir="$(cd "$script_dir/../.." && pwd)"

if [[ ! -s "$apk" ]]; then
  printf 'Unsigned Android APK is not a readable, non-empty file: %s\n' "$apk" >&2
  exit 1
fi
if [[ ! -s "$keystore" ]]; then
  printf '%s\n' 'SPEAKUP_ANDROID_KEYSTORE_PATH must name a readable, non-empty keystore.' >&2
  exit 1
fi
if [[ -z "$alias_name" ]]; then
  printf '%s\n' 'SPEAKUP_ANDROID_KEY_ALIAS is required.' >&2
  exit 1
fi
if [[ -z "${SPEAKUP_ANDROID_STORE_PASSWORD:-}" ]]; then
  printf '%s\n' 'SPEAKUP_ANDROID_STORE_PASSWORD is required.' >&2
  exit 1
fi
if [[ -z "${SPEAKUP_ANDROID_KEY_PASSWORD:-}" ]]; then
  printf '%s\n' 'SPEAKUP_ANDROID_KEY_PASSWORD is required.' >&2
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

zipalign="$(find_android_tool zipalign)"
apksigner="$(find_android_tool apksigner)"
ensure_java_runtime

if "$apksigner" verify "$apk" >/dev/null 2>&1; then
  printf '%s\n' 'Release APK must be unsigned before explicit signing.' >&2
  exit 1
fi

umask 077
signing_directory="$(mktemp -d "$(dirname "$apk")/.speakup-apk-signing.XXXXXX")"
aligned_apk="$signing_directory/aligned.apk"
signed_apk="$signing_directory/signed.apk"
cleanup_signing_directory() {
  rm -f "$aligned_apk" "$signed_apk" "$signed_apk.idsig"
  rmdir "$signing_directory" 2>/dev/null || true
}
trap cleanup_signing_directory EXIT

"$zipalign" -P 16 -f 4 "$apk" "$aligned_apk" >/dev/null
"$zipalign" -c -P 16 4 "$aligned_apk" >/dev/null
"$apksigner" sign \
  --ks "$keystore" \
  --ks-key-alias "$alias_name" \
  --ks-pass env:SPEAKUP_ANDROID_STORE_PASSWORD \
  --key-pass env:SPEAKUP_ANDROID_KEY_PASSWORD \
  --v4-signing-enabled false \
  --out "$signed_apk" \
  "$aligned_apk"
"$apksigner" verify --verbose "$signed_apk" >/dev/null
"$zipalign" -c -P 16 4 "$signed_apk" >/dev/null

chmod 0644 "$signed_apk"
mv -f "$signed_apk" "$apk"
cleanup_signing_directory
trap - EXIT

printf 'signedApk=%s\n' "$apk"
