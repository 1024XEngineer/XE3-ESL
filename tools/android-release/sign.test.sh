#!/usr/bin/env bash

set -euo pipefail

readonly script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly temporary_directory="$(mktemp -d)"
readonly fake_bin="$temporary_directory/bin"
trap 'rm -rf "$temporary_directory"' EXIT

mkdir -p "$fake_bin"
printf '%s\n' 'fixture keystore' > "$temporary_directory/release.jks"
printf '%s\n' 'unsigned fixture apk' > "$temporary_directory/speakup.apk"

cat > "$fake_bin/java" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
cat > "$fake_bin/zipalign" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == "-c" ]]; then
  [[ -s "${@: -1}" ]]
  exit
fi
cp "${@: -2:1}" "${@: -1}"
EOF
cat > "$fake_bin/apksigner" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
command="$1"
shift
if [[ "$command" == "verify" ]]; then
  grep -Fqx 'signed fixture apk' "${@: -1}"
  exit
fi
[[ "$command" == "sign" ]]
[[ " $* " == *" --ks-pass env:SPEAKUP_ANDROID_STORE_PASSWORD "* ]]
[[ " $* " == *" --key-pass env:SPEAKUP_ANDROID_KEY_PASSWORD "* ]]
[[ " $* " == *" --ks-key-alias speakup-release "* ]]
output=""
while [[ "$#" -gt 0 ]]; do
  if [[ "$1" == "--out" ]]; then
    output="$2"
    break
  fi
  shift
done
[[ -n "$output" ]]
printf '%s\n' 'signed fixture apk' > "$output"
EOF
chmod +x "$fake_bin/java" "$fake_bin/zipalign" "$fake_bin/apksigner"

report="$(
  PATH="$fake_bin:$PATH" \
    JAVA_HOME="$temporary_directory" \
    SPEAKUP_ANDROID_KEYSTORE_PATH="$temporary_directory/release.jks" \
    SPEAKUP_ANDROID_KEY_ALIAS=speakup-release \
    SPEAKUP_ANDROID_STORE_PASSWORD=fixture-store-password \
    SPEAKUP_ANDROID_KEY_PASSWORD=fixture-key-password \
    "$script_directory/sign.sh" "$temporary_directory/speakup.apk"
)"
grep -Fqx "signedApk=$temporary_directory/speakup.apk" <<< "$report"
grep -Fqx 'signed fixture apk' "$temporary_directory/speakup.apk"

if PATH="$fake_bin:$PATH" \
  JAVA_HOME="$temporary_directory" \
  SPEAKUP_ANDROID_KEYSTORE_PATH="$temporary_directory/release.jks" \
  SPEAKUP_ANDROID_KEY_ALIAS=speakup-release \
  SPEAKUP_ANDROID_STORE_PASSWORD=fixture-store-password \
  SPEAKUP_ANDROID_KEY_PASSWORD=fixture-key-password \
  "$script_directory/sign.sh" "$temporary_directory/speakup.apk" \
    > "$temporary_directory/already-signed.out" 2>&1; then
  printf '%s\n' 'An already signed APK was signed again.' >&2
  exit 1
fi
grep -Fq \
  'Release APK must be unsigned before explicit signing.' \
  "$temporary_directory/already-signed.out"

printf '%s\n' 'unsigned fixture apk' > "$temporary_directory/missing-secret.apk"
if PATH="$fake_bin:$PATH" \
  JAVA_HOME="$temporary_directory" \
  SPEAKUP_ANDROID_KEYSTORE_PATH="$temporary_directory/release.jks" \
  SPEAKUP_ANDROID_STORE_PASSWORD=fixture-store-password \
  SPEAKUP_ANDROID_KEY_PASSWORD=fixture-key-password \
  "$script_directory/sign.sh" "$temporary_directory/missing-secret.apk" \
    > "$temporary_directory/missing-secret.out" 2>&1; then
  printf '%s\n' 'Signing succeeded without an Android key alias.' >&2
  exit 1
fi
grep -Fq 'SPEAKUP_ANDROID_KEY_ALIAS is required.' "$temporary_directory/missing-secret.out"

printf '%s\n' 'Android explicit signer tests passed'
