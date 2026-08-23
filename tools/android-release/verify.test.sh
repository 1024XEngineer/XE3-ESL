#!/usr/bin/env bash

set -euo pipefail

readonly script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly temporary_directory="$(mktemp -d)"
readonly fake_bin="$temporary_directory/bin"
readonly certificate_sha256="cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
trap 'rm -rf "$temporary_directory"' EXIT

mkdir -p "$fake_bin"
printf '%s\n' 'fixture apk' > "$temporary_directory/speakup.apk"
printf '%s\n' 'name: speakup' 'version: 0.1.0+1' > "$temporary_directory/pubspec.yaml"

cat > "$fake_bin/apksigner" <<EOF
#!/usr/bin/env bash
printf '%s\n' \
  'Signer #1 certificate SHA-256 digest: $certificate_sha256' \
  'Signer (minSdkVersion=24, maxSdkVersion=2147483647) certificate SHA-256 digest: $certificate_sha256'
EOF
cat > "$fake_bin/java" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$fake_bin/apksigner" "$fake_bin/java"

write_aapt() {
  local minimum_android_api=$1
  cat > "$fake_bin/aapt" <<EOF
#!/usr/bin/env bash
cat <<'BADGING'
package: name='com.xengineer.speakup' versionCode='1' versionName='0.1.0'
sdkVersion:'$minimum_android_api'
native-code: 'arm64-v8a'
BADGING
EOF
  chmod +x "$fake_bin/aapt"
}

write_aapt 24
report="$(
  PATH="$fake_bin:$PATH" \
    SPEAKUP_ANDROID_CERT_SHA256="$certificate_sha256" \
    "$script_directory/verify.sh" \
      "$temporary_directory/speakup.apk" \
      "$temporary_directory/pubspec.yaml"
)"
grep -Fqx 'applicationId=com.xengineer.speakup' <<< "$report"
grep -Fqx 'versionName=0.1.0' <<< "$report"
grep -Fqx 'versionCode=1' <<< "$report"
grep -Fqx 'minimumAndroidApi=24' <<< "$report"
grep -Fqx 'abi=arm64-v8a' <<< "$report"
grep -Fqx 'signature=verified' <<< "$report"
grep -Fqx "certificateSha256=$certificate_sha256" <<< "$report"
artifact_sha256="$(shasum -a 256 "$temporary_directory/speakup.apk" | awk '{print $1}')"
grep -Fqx "artifactSha256=$artifact_sha256" <<< "$report"

write_aapt 23
if PATH="$fake_bin:$PATH" \
  SPEAKUP_ANDROID_CERT_SHA256="$certificate_sha256" \
  "$script_directory/verify.sh" \
    "$temporary_directory/speakup.apk" \
    "$temporary_directory/pubspec.yaml" \
    > "$temporary_directory/rejected.out" 2>&1; then
  printf '%s\n' 'APK with an unexpected minimum Android API was accepted.' >&2
  exit 1
fi
grep -Fq 'Unexpected minimum Android API: 23' "$temporary_directory/rejected.out"

write_aapt 24
cat > "$fake_bin/apksigner" <<EOF
#!/usr/bin/env bash
printf '%s\n' \
  'Signer #1 certificate SHA-256 digest: $certificate_sha256' \
  'Signer (minSdkVersion=24, maxSdkVersion=2147483647) certificate SHA-256 digest: dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd'
EOF
chmod +x "$fake_bin/apksigner"
if PATH="$fake_bin:$PATH" \
  SPEAKUP_ANDROID_CERT_SHA256="$certificate_sha256" \
  "$script_directory/verify.sh" \
    "$temporary_directory/speakup.apk" \
    "$temporary_directory/pubspec.yaml" \
    > "$temporary_directory/multiple-signers.out" 2>&1; then
  printf '%s\n' 'APK with multiple signer certificates was accepted.' >&2
  exit 1
fi
grep -Fq \
  'APK signer certificate output is missing or contains multiple certificates.' \
  "$temporary_directory/multiple-signers.out"

printf '%s\n' 'Android release verifier tests passed'
