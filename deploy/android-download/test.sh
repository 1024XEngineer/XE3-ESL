#!/usr/bin/env bash

set -euo pipefail

readonly script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly repository_directory="$(cd "$script_directory/../.." && pwd)"
readonly manage="$script_directory/manage.sh"
readonly bundle_builder="$repository_directory/tools/android-download/bundle.mjs"

fail() {
  printf 'android download test: %s\n' "$*" >&2
  exit 1
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

file_size() {
  stat -c '%s' "$1" 2>/dev/null || stat -f '%z' "$1"
}

expect_failure() {
  local name=$1
  shift
  if "$@" >"$temporary_directory/failure.out" 2>&1; then
    fail "$name unexpectedly succeeded"
  fi
}

build_bundle() {
  local version=$1
  local version_code=$2
  local destination=$3
  local apk="$temporary_directory/speakup-v${version}-production-arm64.apk"
  local manifest="$temporary_directory/release-${version}.json"
  local apk_sha apk_size

  printf 'signed-production-apk-%s\n' "$version" >"$apk"
  apk_sha=$(sha256_file "$apk")
  apk_size=$(file_size "$apk")
  jq --null-input \
    --arg version "$version" \
    --argjson version_code "$version_code" \
    --arg apk_file "speakup-v${version}-production-arm64.apk" \
    --arg apk_sha "$apk_sha" \
    --argjson apk_size "$apk_size" '
      {
        manifest_version: 1,
        version: $version,
        version_code: $version_code,
        git_sha: ("c" * 40),
        portal_image: "ghcr.io/1024xengineer/xe3-esl-portal",
        portal_image_digest: ("sha256:" + ("a" * 64)),
        server_image: "ghcr.io/1024xengineer/xe3-esl-server",
        server_image_digest: ("sha256:" + ("b" * 64)),
        staging_apk_file: ("speakup-v" + $version + "-staging-arm64.apk"),
        staging_apk_sha256: ("d" * 64),
        production_apk_file: $apk_file,
        production_apk_size_bytes: $apk_size,
        production_apk_sha256: $apk_sha,
        application_id: "com.xengineer.speakup",
        minimum_android_api: 24,
        abis: ["arm64-v8a"],
        apk_certificate_sha256: ("e" * 64),
        database_schema_version: 7,
        quality_run_url:
          "https://github.com/1024XEngineer/XE3-ESL/actions/runs/123456789"
      }
    ' >"$manifest"
  node "$bundle_builder" \
    --manifest "$manifest" \
    --production-apk "$apk" \
    --published-at "2026-08-23T12:34:56Z" \
    --output "$destination" \
    >/dev/null
}

temporary_directory=$(mktemp -d)
readonly temporary_directory
trap 'rm -rf "$temporary_directory"' EXIT

bash -n "$manage" "$0"
mkdir -p "$temporary_directory/public"
build_bundle 0.1.0 1 "$temporary_directory/bundle-0.1.0"
build_bundle 0.1.1 2 "$temporary_directory/bundle-0.1.1"

"$manage" validate \
  --bundle "$temporary_directory/bundle-0.1.0" \
  --root "$temporary_directory/public" \
  >"$temporary_directory/validate.out"
grep -Fq 'validated=true' "$temporary_directory/validate.out" ||
  fail "valid bundle was rejected"

cp -R "$temporary_directory/bundle-0.1.0" "$temporary_directory/tampered"
jq '.unexpected = true' \
  "$temporary_directory/tampered/downloads/android/v0.1.0/release.json" \
  >"$temporary_directory/tampered/release.tmp"
mv "$temporary_directory/tampered/release.tmp" \
  "$temporary_directory/tampered/downloads/android/v0.1.0/release.json"
tampered_metadata="$temporary_directory/tampered/downloads/android/v0.1.0/release.json"
tampered_size=$(file_size "$tampered_metadata")
tampered_sha=$(sha256_file "$tampered_metadata")
jq \
  --argjson size "$tampered_size" \
  --arg sha "$tampered_sha" \
  '.files[2].size_bytes = $size | .files[2].sha256 = $sha' \
  "$temporary_directory/tampered/bundle-manifest.json" \
  >"$temporary_directory/tampered/manifest.tmp"
mv "$temporary_directory/tampered/manifest.tmp" \
  "$temporary_directory/tampered/bundle-manifest.json"
expect_failure "extra public metadata field" \
  "$manage" publish \
    --bundle "$temporary_directory/tampered" \
    --root "$temporary_directory/public"
[[ ! -e "$temporary_directory/public/downloads" ]] ||
  fail "invalid bundle wrote to the public root"

cp -R "$temporary_directory/bundle-0.1.0" "$temporary_directory/time-tampered"
jq '.published_at = "2026-08-23T12:35:56Z"' \
  "$temporary_directory/time-tampered/downloads/android/v0.1.0/release.json" \
  >"$temporary_directory/time-tampered/release.tmp"
mv "$temporary_directory/time-tampered/release.tmp" \
  "$temporary_directory/time-tampered/downloads/android/v0.1.0/release.json"
time_metadata="$temporary_directory/time-tampered/downloads/android/v0.1.0/release.json"
time_size=$(file_size "$time_metadata")
time_sha=$(sha256_file "$time_metadata")
jq \
  --argjson size "$time_size" \
  --arg sha "$time_sha" \
  '.files[2].size_bytes = $size | .files[2].sha256 = $sha' \
  "$temporary_directory/time-tampered/bundle-manifest.json" \
  >"$temporary_directory/time-tampered/manifest.tmp"
mv "$temporary_directory/time-tampered/manifest.tmp" \
  "$temporary_directory/time-tampered/bundle-manifest.json"
expect_failure "inconsistent publication time" \
  "$manage" validate \
    --bundle "$temporary_directory/time-tampered" \
    --root "$temporary_directory/public"

mkdir "$temporary_directory/world-writable-root"
chmod 0777 "$temporary_directory/world-writable-root"
expect_failure "world-writable public root" \
  "$manage" publish \
    --bundle "$temporary_directory/bundle-0.1.0" \
    --root "$temporary_directory/world-writable-root"
[[ -z "$(find "$temporary_directory/world-writable-root" -mindepth 1 -print -quit)" ]] ||
  fail "world-writable root was modified"

mkdir -p "$temporary_directory/unsafe-nested-root/downloads/android"
chmod 0777 "$temporary_directory/unsafe-nested-root/downloads/android"
expect_failure "world-writable Android public directory" \
  "$manage" validate \
    --bundle "$temporary_directory/bundle-0.1.0" \
    --root "$temporary_directory/unsafe-nested-root"

mkdir "$temporary_directory/public/.android-download.lock"
expect_failure "existing publication lock" \
  "$manage" publish \
    --bundle "$temporary_directory/bundle-0.1.0" \
    --root "$temporary_directory/public"
[[ -d "$temporary_directory/public/.android-download.lock" ]] ||
  fail "a failed lock acquisition removed another operation's lock"
rmdir "$temporary_directory/public/.android-download.lock"
[[ ! -e "$temporary_directory/public/downloads" ]] ||
  fail "lock contention wrote to the public download tree"

"$manage" publish \
  --bundle "$temporary_directory/bundle-0.1.0" \
  --root "$temporary_directory/public" \
  --activate \
  >"$temporary_directory/publish-one.out"
grep -Fq 'published=true' "$temporary_directory/publish-one.out" ||
  fail "first version was not published"
grep -Fq 'activated=true' "$temporary_directory/publish-one.out" ||
  fail "first version was not activated"
cmp \
  "$temporary_directory/public/downloads/android/release.json" \
  "$temporary_directory/public/downloads/android/v0.1.0/release.json"

current_before=$(sha256_file "$temporary_directory/public/downloads/android/release.json")
expect_failure "existing version overwrite" \
  "$manage" publish \
    --bundle "$temporary_directory/bundle-0.1.0" \
    --root "$temporary_directory/public" \
    --activate
[[ "$(sha256_file "$temporary_directory/public/downloads/android/release.json")" == \
  "$current_before" ]] || fail "failed republish changed the current release"

"$manage" publish \
  --bundle "$temporary_directory/bundle-0.1.1" \
  --root "$temporary_directory/public" \
  --activate \
  >"$temporary_directory/publish-two.out"
[[ "$(jq --raw-output '.version' "$temporary_directory/public/downloads/android/release.json")" == \
  "0.1.1" ]] || fail "second version was not activated"

"$manage" activate \
  --root "$temporary_directory/public" \
  --version 0.1.0 \
  >"$temporary_directory/rollback.out"
[[ "$(jq --raw-output '.version' "$temporary_directory/public/downloads/android/release.json")" == \
  "0.1.0" ]] || fail "previous version was not reactivated"

printf 'tampered\n' >> \
  "$temporary_directory/public/downloads/android/v0.1.1/speakup-v0.1.1-production-arm64.apk"
expect_failure "tampered installed APK activation" \
  "$manage" activate \
    --root "$temporary_directory/public" \
    --version 0.1.1
[[ "$(jq --raw-output '.version' "$temporary_directory/public/downloads/android/release.json")" == \
  "0.1.0" ]] || fail "failed activation changed the current release"

mkdir -p "$temporary_directory/symlink-root/downloads/android" "$temporary_directory/outside"
symlink_target="$temporary_directory/symlink-root/downloads/android/v0.1.0"
ln -s "$temporary_directory/outside" "$symlink_target"
expect_failure "symlink version target" \
  "$manage" publish \
    --bundle "$temporary_directory/bundle-0.1.0" \
    --root "$temporary_directory/symlink-root"
[[ -L "$symlink_target" ]] || fail "symlink target was replaced"
[[ -z "$(find "$temporary_directory/outside" -mindepth 1 -print -quit)" ]] ||
  fail "publish followed a version-directory symlink"

printf '%s\n' 'Android download publish contract tests passed'
