#!/usr/bin/env bash

set -euo pipefail

temporary_current=""
created_version_directory=""
lock_directory=""

cleanup() {
  [[ -z "$temporary_current" ]] || rm -f -- "$temporary_current"
  [[ -z "$created_version_directory" ]] || rm -rf -- "$created_version_directory"
  [[ -z "$lock_directory" ]] || rmdir -- "$lock_directory" 2>/dev/null || true
}
trap cleanup EXIT

usage() {
  cat >&2 <<'EOF'
Usage:
  manage.sh validate --bundle DIRECTORY --root DIRECTORY
  manage.sh publish --bundle DIRECTORY --root DIRECTORY [--activate]
  manage.sh activate --root DIRECTORY --version X.Y.Z
EOF
}

fail() {
  printf 'android download: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

valid_absolute_path() {
  local value=$1
  [[ "$value" =~ ^/[A-Za-z0-9._/-]+$ ]] &&
    [[ "$value" != *//* ]] &&
    [[ "$value" != */../* ]] &&
    [[ "$value" != */./* ]] &&
    [[ "$value" != */.. ]] &&
    [[ "$value" != */. ]]
}

valid_version() {
  [[ "$1" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -- "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 -- "$1" | awk '{print $1}'
  else
    fail "sha256sum or shasum is required"
  fi
}

file_size() {
  local size
  if size=$(stat -c '%s' -- "$1" 2>/dev/null); then
    :
  elif size=$(stat -f '%z' "$1" 2>/dev/null); then
    :
  else
    fail "cannot inspect file size: $1"
  fi
  printf '%s\n' "$size"
}

require_regular_file() {
  local description=$1
  local file=$2
  [[ ! -L "$file" ]] || fail "$description cannot be a symlink: $file"
  [[ -f "$file" ]] || fail "$description is not a regular file: $file"
  [[ -r "$file" ]] || fail "$description is not readable: $file"
  [[ -s "$file" ]] || fail "$description is empty: $file"
}

require_real_directory() {
  local description=$1
  local directory=$2
  [[ ! -L "$directory" ]] || fail "$description cannot be a symlink: $directory"
  [[ -d "$directory" ]] || fail "$description is not a directory: $directory"
}

require_owned_public_directory() {
  local description=$1
  local directory=$2
  local owner mode

  require_real_directory "$description" "$directory"
  if owner=$(stat -c '%u' -- "$directory" 2>/dev/null); then
    mode=$(stat -c '%a' -- "$directory") ||
      fail "cannot inspect permissions for $description: $directory"
  elif owner=$(stat -f '%u' "$directory" 2>/dev/null); then
    mode=$(stat -f '%Lp' "$directory") ||
      fail "cannot inspect permissions for $description: $directory"
  else
    fail "cannot inspect ownership for $description: $directory"
  fi
  [[ "$owner" == "$(id -u)" ]] ||
    fail "$description must be owned by the current user: $directory"
  (( (8#$mode & 0022) == 0 )) ||
    fail "$description cannot be group or world writable: $directory"
}

assert_exact_entries() {
  local directory=$1
  shift
  local actual expected
  actual=$(find "$directory" -mindepth 1 -maxdepth 1 -print |
    sed "s|^$directory/||" | LC_ALL=C sort)
  expected=$(printf '%s\n' "$@" | LC_ALL=C sort)
  [[ "$actual" == "$expected" ]] || fail "unexpected entries in $directory"
}

validate_public_metadata() {
  local file=$1
  local expected_version=$2
  local expected_file="speakup-v${expected_version}-production-arm64.apk"
  local expected_path="/downloads/android/v${expected_version}/${expected_file}"

  require_regular_file "public release metadata" "$file"
  jq --exit-status --slurp \
    --arg version "$expected_version" \
    --arg file "$expected_file" \
    --arg path "$expected_path" '
      length == 1 and
      (.[0] |
        type == "object" and
        keys == [
          "abis",
          "apk_certificate_sha256",
          "apk_sha256",
          "download_path",
          "file_name",
          "metadata_version",
          "minimum_android_api",
          "published_at",
          "size_bytes",
          "version",
          "version_code"
        ] and
        .metadata_version == 1 and
        .version == $version and
        (.version_code | type == "number" and
          . >= 1 and . <= 9007199254740991 and . == floor) and
        (.published_at | type == "string" and
          test("^[0-9]{4}-(0[1-9]|1[0-2])-([012][0-9]|3[01])T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]Z$") and
          (. as $timestamp |
            try ((fromdateiso8601 | todateiso8601) == $timestamp) catch false)) and
        .file_name == $file and
        .download_path == $path and
        (.size_bytes | type == "number" and
          . >= 1 and . <= 9007199254740991 and . == floor) and
        .minimum_android_api == 24 and
        .abis == ["arm64-v8a"] and
        (.apk_sha256 | type == "string" and
          test("^[0-9a-f]{64}$") and test("[1-9a-f]")) and
        (.apk_certificate_sha256 | type == "string" and
          test("^[0-9a-f]{64}$") and test("[1-9a-f]"))
      )
    ' "$file" >/dev/null || fail "public release metadata is invalid: $file"
}

validate_version_directory() {
  local directory=$1
  local version=$2
  local apk_name="speakup-v${version}-production-arm64.apk"
  local apk="$directory/$apk_name"
  local checksum="$apk.sha256"
  local metadata="$directory/release.json"
  local expected_size expected_sha checksum_line

  require_owned_public_directory "version directory" "$directory"
  assert_exact_entries "$directory" "$apk_name" "$apk_name.sha256" release.json
  require_regular_file "versioned APK" "$apk"
  require_regular_file "APK checksum" "$checksum"
  validate_public_metadata "$metadata" "$version"

  expected_size=$(jq --raw-output '.size_bytes' "$metadata")
  expected_sha=$(jq --raw-output '.apk_sha256' "$metadata")
  [[ "$(file_size "$apk")" == "$expected_size" ]] ||
    fail "versioned APK size does not match public metadata"
  [[ "$(sha256_file "$apk")" == "$expected_sha" ]] ||
    fail "versioned APK SHA-256 does not match public metadata"
  IFS= read -r checksum_line <"$checksum" || fail "APK checksum is not newline terminated"
  [[ "$checksum_line" == "$expected_sha  $apk_name" ]] ||
    fail "APK checksum contents are invalid"
  [[ "$(wc -l <"$checksum" | tr -d ' ')" == 1 ]] ||
    fail "APK checksum must contain exactly one line"
}

validate_bundle() {
  local bundle=$1
  local manifest="$bundle/bundle-manifest.json"
  local apk_name metadata_path
  local entry_path entry_size entry_sha source

  require_command jq
  require_real_directory "download bundle" "$bundle"
  require_regular_file "bundle manifest" "$manifest"

  jq --exit-status --slurp '
    length == 1 and
    (.[0] |
      type == "object" and
      keys == [
        "bundle_version",
        "files",
        "published_at",
        "release_manifest_sha256",
        "version"
      ] and
      .bundle_version == 1 and
      (.version | type == "string" and
        test("^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$")) and
      (.published_at | type == "string" and
        test("^[0-9]{4}-(0[1-9]|1[0-2])-([012][0-9]|3[01])T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]Z$") and
        (. as $timestamp |
          try ((fromdateiso8601 | todateiso8601) == $timestamp) catch false)) and
      (.release_manifest_sha256 | type == "string" and
        test("^[0-9a-f]{64}$") and test("[1-9a-f]")) and
      (.files | type == "array" and length == 3 and
        all(.[];
          type == "object" and keys == ["path", "sha256", "size_bytes"] and
          (.size_bytes | type == "number" and
            . >= 1 and . <= 9007199254740991 and . == floor) and
          (.sha256 | type == "string" and
            test("^[0-9a-f]{64}$") and test("[1-9a-f]")))) and
      .files[0].path ==
        ("downloads/android/v" + .version + "/speakup-v" + .version +
          "-production-arm64.apk") and
      .files[1].path == (.files[0].path + ".sha256") and
      .files[2].path ==
        ("downloads/android/v" + .version + "/release.json")
    )
  ' "$manifest" >/dev/null || fail "bundle manifest is invalid"

  BUNDLE_VERSION=$(jq --raw-output '.version' "$manifest")
  apk_name="speakup-v${BUNDLE_VERSION}-production-arm64.apk"
  metadata_path="downloads/android/v${BUNDLE_VERSION}/release.json"

  require_real_directory "bundle downloads directory" "$bundle/downloads"
  require_real_directory "bundle Android directory" "$bundle/downloads/android"
  require_real_directory \
    "bundle version directory" "$bundle/downloads/android/v${BUNDLE_VERSION}"
  assert_exact_entries "$bundle" bundle-manifest.json downloads
  assert_exact_entries "$bundle/downloads" android
  assert_exact_entries "$bundle/downloads/android" "v${BUNDLE_VERSION}"
  assert_exact_entries \
    "$bundle/downloads/android/v${BUNDLE_VERSION}" \
    "$apk_name" "$apk_name.sha256" release.json

  while IFS=$'\t' read -r entry_path entry_size entry_sha; do
    source="$bundle/$entry_path"
    require_regular_file "bundle file" "$source"
    [[ "$(file_size "$source")" == "$entry_size" ]] ||
      fail "bundle file size does not match its manifest: $entry_path"
    [[ "$(sha256_file "$source")" == "$entry_sha" ]] ||
      fail "bundle file SHA-256 does not match its manifest: $entry_path"
  done < <(jq --raw-output '.files[] | [.path, .size_bytes, .sha256] | @tsv' "$manifest")

  validate_public_metadata "$bundle/$metadata_path" "$BUNDLE_VERSION"
  BUNDLE_PUBLISHED_AT=$(jq --raw-output '.published_at' "$manifest")
  [[ "$(jq --raw-output '.published_at' "$bundle/$metadata_path")" == \
    "$BUNDLE_PUBLISHED_AT" ]] ||
    fail "bundle and public metadata publication times do not match"
  validate_version_directory \
    "$bundle/downloads/android/v${BUNDLE_VERSION}" "$BUNDLE_VERSION"
  BUNDLE_MANIFEST_SHA256=$(sha256_file "$manifest")
  BUNDLE_APK_SIZE=$(jq --raw-output '.files[0].size_bytes' "$manifest")
  BUNDLE_APK_SHA256=$(jq --raw-output '.files[0].sha256' "$manifest")
  BUNDLE_CHECKSUM_SIZE=$(jq --raw-output '.files[1].size_bytes' "$manifest")
  BUNDLE_CHECKSUM_SHA256=$(jq --raw-output '.files[1].sha256' "$manifest")
  BUNDLE_METADATA_SIZE=$(jq --raw-output '.files[2].size_bytes' "$manifest")
  BUNDLE_METADATA_SHA256=$(jq --raw-output '.files[2].sha256' "$manifest")
}

verify_installed_bundle_bytes() {
  local directory=$1
  local apk="$directory/speakup-v${BUNDLE_VERSION}-production-arm64.apk"
  local checksum="$apk.sha256"
  local metadata="$directory/release.json"

  [[ "$(file_size "$apk")" == "$BUNDLE_APK_SIZE" &&
    "$(sha256_file "$apk")" == "$BUNDLE_APK_SHA256" ]] ||
    fail "installed APK does not match the validated bundle"
  [[ "$(file_size "$checksum")" == "$BUNDLE_CHECKSUM_SIZE" &&
    "$(sha256_file "$checksum")" == "$BUNDLE_CHECKSUM_SHA256" ]] ||
    fail "installed checksum does not match the validated bundle"
  [[ "$(file_size "$metadata")" == "$BUNDLE_METADATA_SIZE" &&
    "$(sha256_file "$metadata")" == "$BUNDLE_METADATA_SHA256" ]] ||
    fail "installed metadata does not match the validated bundle"
}

validate_root() {
  local root=$1
  valid_absolute_path "$root" || fail "--root must be a safe absolute path"
  require_owned_public_directory "public root" "$root"
  [[ -w "$root" ]] || fail "public root is not writable: $root"
  for directory in "$root/downloads" "$root/downloads/android"; do
    if [[ -e "$directory" || -L "$directory" ]]; then
      require_owned_public_directory "public download directory" "$directory"
    fi
  done
  if [[ -e "$root/downloads/android/release.json" ||
    -L "$root/downloads/android/release.json" ]]; then
    require_regular_file \
      "current public release metadata" "$root/downloads/android/release.json"
  fi
}

install_version_file() {
  local source=$1
  local destination=$2
  local temporary

  [[ ! -e "$destination" && ! -L "$destination" ]] ||
    fail "versioned file already exists and will not be overwritten: $destination"
  temporary=$(mktemp "$(dirname "$destination")/.publish.tmp.XXXXXX")
  install -m 0644 "$source" "$temporary"
  if ! ln -- "$temporary" "$destination"; then
    fail "versioned file already exists and will not be overwritten: $destination"
  fi
  rm -f -- "$temporary"
  require_regular_file "installed versioned file" "$destination"
}

acquire_lock() {
  local root=$1
  local candidate="$root/.android-download.lock"

  mkdir -m 0700 -- "$candidate" 2>/dev/null ||
    fail "another Android download operation is active"
  lock_directory=$candidate
}

activate_version() {
  local root=$1
  local version=$2
  local android_root="$root/downloads/android"
  local version_directory="$android_root/v${version}"
  local source="$version_directory/release.json"
  local current="$android_root/release.json"

  validate_version_directory "$version_directory" "$version"
  if [[ -e "$current" || -L "$current" ]]; then
    require_regular_file "current public release metadata" "$current"
  fi

  temporary_current=$(mktemp "$android_root/.release.json.tmp.XXXXXX")
  install -m 0644 "$source" "$temporary_current"
  [[ "$(sha256_file "$temporary_current")" == "$(sha256_file "$source")" ]] ||
    fail "temporary current metadata verification failed"
  mv -f -- "$temporary_current" "$current"
  temporary_current=""
  printf 'version=%s activated=true\n' "$version"
}

publish_bundle() {
  local bundle=$1
  local root=$2
  local activate=$3
  local android_root="$root/downloads/android"
  local source="$bundle/downloads/android/v${BUNDLE_VERSION}"
  local destination="$android_root/v${BUNDLE_VERSION}"

  [[ ! -e "$destination" && ! -L "$destination" ]] ||
    fail "version already exists and will not be overwritten: $destination"
  if [[ ! -e "$root/downloads" && ! -L "$root/downloads" ]]; then
    mkdir -m 0755 -- "$root/downloads"
  fi
  require_owned_public_directory "public downloads directory" "$root/downloads"
  if [[ ! -e "$android_root" && ! -L "$android_root" ]]; then
    mkdir -m 0755 -- "$android_root"
  fi
  require_owned_public_directory "public Android directory" "$android_root"
  mkdir -m 0755 -- "$destination" ||
    fail "version already exists and will not be overwritten: $destination"
  created_version_directory=$destination

  install_version_file \
    "$source/speakup-v${BUNDLE_VERSION}-production-arm64.apk" \
    "$destination/speakup-v${BUNDLE_VERSION}-production-arm64.apk"
  install_version_file \
    "$source/speakup-v${BUNDLE_VERSION}-production-arm64.apk.sha256" \
    "$destination/speakup-v${BUNDLE_VERSION}-production-arm64.apk.sha256"
  install_version_file \
    "$source/release.json" \
    "$destination/release.json"
  validate_version_directory "$destination" "$BUNDLE_VERSION"
  verify_installed_bundle_bytes "$destination"
  created_version_directory=""

  printf 'version=%s bundle_manifest_sha256=%s published=true\n' \
    "$BUNDLE_VERSION" "$BUNDLE_MANIFEST_SHA256"
  if [[ "$activate" == true ]]; then
    activate_version "$root" "$BUNDLE_VERSION"
  fi
}

main() {
  local command=${1:-}
  local bundle=""
  local root=""
  local version=""
  local activate=false

  [[ -n "$command" ]] || {
    usage
    exit 2
  }
  shift
  while (($# > 0)); do
    case "$1" in
      --bundle)
        (($# >= 2)) || fail "--bundle requires a value"
        bundle=$2
        shift 2
        ;;
      --root)
        (($# >= 2)) || fail "--root requires a value"
        root=$2
        shift 2
        ;;
      --version)
        (($# >= 2)) || fail "--version requires a value"
        version=$2
        shift 2
        ;;
      --activate)
        activate=true
        shift
        ;;
      *)
        fail "unknown argument: $1"
        ;;
    esac
  done

  [[ -n "$root" ]] || fail "--root is required"
  validate_root "$root"
  case "$command" in
    validate | publish)
      local initial_manifest_sha256
      [[ -n "$bundle" ]] || fail "$command requires --bundle"
      [[ -z "$version" ]] || fail "$command does not accept --version"
      [[ "$command" == publish || "$activate" == false ]] ||
        fail "validate does not accept --activate"
      valid_absolute_path "$bundle" || fail "--bundle must be a safe absolute path"
      validate_bundle "$bundle"
      if [[ "$command" == validate ]]; then
        printf 'version=%s bundle_manifest_sha256=%s validated=true\n' \
          "$BUNDLE_VERSION" "$BUNDLE_MANIFEST_SHA256"
      else
        initial_manifest_sha256=$BUNDLE_MANIFEST_SHA256
        acquire_lock "$root"
        validate_root "$root"
        validate_bundle "$bundle"
        [[ "$BUNDLE_MANIFEST_SHA256" == "$initial_manifest_sha256" ]] ||
          fail "bundle changed while waiting for the publish lock"
        publish_bundle "$bundle" "$root" "$activate"
      fi
      ;;
    activate)
      [[ -z "$bundle" ]] || fail "activate does not accept --bundle"
      [[ "$activate" == false ]] || fail "activate does not accept --activate"
      [[ -n "$version" ]] || fail "activate requires --version"
      valid_version "$version" || fail "--version is invalid"
      validate_version_directory "$root/downloads/android/v${version}" "$version"
      acquire_lock "$root"
      validate_root "$root"
      activate_version "$root" "$version"
      ;;
    *)
      usage
      exit 2
      ;;
  esac
}

main "$@"
