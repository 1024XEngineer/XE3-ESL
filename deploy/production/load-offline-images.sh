#!/usr/bin/env bash

set -euo pipefail
umask 077

readonly expected_platform='linux/amd64'
readonly expected_source='https://github.com/1024XEngineer/XE3-ESL'
readonly postgres_reference='postgres@sha256:7d2695c3aa88e792e8b3b233e7e4adb296a20412c6c0ca361e3edaaacfada108'

usage() {
  cat >&2 <<'EOF'
Usage:
  load-offline-images.sh --manifest FILE --bundle FILE --directory DIRECTORY
EOF
}

fail() {
  printf 'offline image contract: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

require_regular_file() {
  local description=$1
  local file=$2

  [[ ! -L "$file" ]] || fail "$description must not be a symbolic link: $file"
  [[ -f "$file" ]] || fail "$description is not a regular file: $file"
  [[ -r "$file" ]] || fail "$description is not readable: $file"
  [[ -s "$file" ]] || fail "$description is empty: $file"
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    fail 'sha256sum or shasum is required'
  fi
}

file_size() {
  local file=$1
  local size

  if size=$(stat -c '%s' -- "$file" 2>/dev/null); then
    :
  elif size=$(stat -f '%z' "$file" 2>/dev/null); then
    :
  else
    fail "cannot inspect file size: $file"
  fi
  printf '%s\n' "$size"
}

verify_blob() {
  local description=$1
  local file=$2
  local digest=$3
  local size=$4

  require_regular_file "$description" "$file"
  [[ "$(file_size "$file")" == "$size" ]] ||
    fail "$description size does not match its descriptor"
  [[ "sha256:$(sha256_file "$file")" == "$digest" ]] ||
    fail "$description digest does not match its descriptor"
}

verify_image_archive() {
  local name=$1
  local repository=$2
  local digest=$3
  local archive=$4
  local version=$5
  local git_sha=$6
  local validation_directory="$temporary_directory/$name-archive"
  local extraction_directory="$validation_directory/extracted"
  local members_file="$validation_directory/members"
  local member_types_file="$validation_directory/member-types"
  local regular_members_file="$validation_directory/regular-members"
  local expected_members_file="$validation_directory/expected-members"
  local index_file="$extraction_directory/index.json"
  local docker_manifest_file="$extraction_directory/manifest.json"
  local oci_layout_file="$extraction_directory/oci-layout"
  local manifest_file
  local manifest_size
  local config_digest
  local config_size
  local config_file
  local layer_contract_file="$validation_directory/layers.tsv"
  local layer_paths_file="$validation_directory/layer-paths"
  local member
  local type
  local layer_digest
  local layer_size
  local layer_path
  local member_count
  local member_type_count

  mkdir -p "$extraction_directory"
  LC_ALL=C tar -tf "$archive" > "$members_file" ||
    fail "$name image archive cannot be listed"
  LC_ALL=C tar -tvf "$archive" | cut -c 1 > "$member_types_file" ||
    fail "$name image archive member types cannot be inspected"
  member_count=$(wc -l < "$members_file" | tr -d '[:space:]')
  member_type_count=$(wc -l < "$member_types_file" | tr -d '[:space:]')
  [[ -s "$members_file" && "$member_count" == "$member_type_count" ]] ||
    fail "$name image archive has an invalid member list"
  [[ -z "$(LC_ALL=C sort "$members_file" | uniq -d)" ]] ||
    fail "$name image archive contains duplicate paths"

  exec 3< "$member_types_file"
  while IFS= read -r member; do
    IFS= read -r type <&3 || fail "$name image archive member types are incomplete"
    [[ -n "$member" && "$member" != *$'\r'* && "$member" != *$'\t'* &&
      "$member" != *'\\'* ]] ||
      fail "$name image archive contains an unsafe path"
    case "$member" in
      blobs/ | blobs/sha256/)
        [[ "$type" == d ]] ||
          fail "$name image archive directory has an invalid type"
        ;;
      index.json | manifest.json | oci-layout)
        [[ "$type" == - ]] ||
          fail "$name image archive file has an invalid type"
        ;;
      blobs/sha256/*)
        [[ "$type" == - &&
          "${member#blobs/sha256/}" =~ ^[0-9a-f]{64}$ ]] ||
          fail "$name image archive blob path or type is invalid"
        ;;
      *) fail "$name image archive contains an unexpected path: $member" ;;
    esac
  done < "$members_file"
  if IFS= read -r type <&3; then
    exec 3<&-
    fail "$name image archive member types are not one-to-one"
  fi
  exec 3<&-

  for member in index.json manifest.json oci-layout; do
    [[ "$(grep -Fxc "$member" "$members_file")" == 1 ]] ||
      fail "$name image archive must contain exactly one $member"
  done
  tar --no-same-owner --no-same-permissions -xf "$archive" -C "$extraction_directory" ||
    fail "$name image archive cannot be extracted safely"
  require_regular_file "$name OCI index" "$index_file"
  require_regular_file "$name Docker manifest" "$docker_manifest_file"
  require_regular_file "$name OCI layout" "$oci_layout_file"

  jq --exit-status '
      type == "object" and
      keys == ["imageLayoutVersion"] and
      .imageLayoutVersion == "1.0.0"
    ' "$oci_layout_file" >/dev/null ||
    fail "$name image archive has an invalid OCI layout"

  jq --exit-status --arg digest "$digest" '
      type == "object" and
      .schemaVersion == 2 and
      .mediaType == "application/vnd.oci.image.index.v1+json" and
      (.manifests | type == "array" and length == 1) and
      (.manifests[0] |
        type == "object" and
        .digest == $digest and
        (.size | type == "number" and . >= 1 and . == floor) and
        (.mediaType == "application/vnd.docker.distribution.manifest.v2+json" or
          .mediaType == "application/vnd.oci.image.manifest.v1+json") and
        .platform.os == "linux" and
        .platform.architecture == "amd64")
    ' "$index_file" >/dev/null ||
    fail "$name image archive has an invalid OCI index"
  manifest_size=$(jq --raw-output '.manifests[0].size' "$index_file")
  manifest_file="$extraction_directory/blobs/sha256/${digest#sha256:}"
  verify_blob "$name image manifest" "$manifest_file" "$digest" "$manifest_size"

  jq --exit-status '
      type == "object" and
      .schemaVersion == 2 and
      (.mediaType == "application/vnd.docker.distribution.manifest.v2+json" or
        .mediaType == "application/vnd.oci.image.manifest.v1+json") and
      (.config |
        type == "object" and
        (.digest | type == "string" and test("^sha256:[0-9a-f]{64}$")) and
        (.size | type == "number" and . >= 1 and . == floor) and
        (.mediaType == "application/vnd.docker.container.image.v1+json" or
          .mediaType == "application/vnd.oci.image.config.v1+json")) and
      (.layers | type == "array") and
      (all(.layers[];
        type == "object" and
        (.digest | type == "string" and test("^sha256:[0-9a-f]{64}$")) and
        (.size | type == "number" and . >= 0 and . == floor) and
        (.mediaType == "application/vnd.docker.image.rootfs.diff.tar.gzip" or
          .mediaType == "application/vnd.oci.image.layer.v1.tar+gzip")))
    ' "$manifest_file" >/dev/null ||
    fail "$name image archive has an invalid image manifest"
  config_digest=$(jq --raw-output '.config.digest' "$manifest_file")
  config_size=$(jq --raw-output '.config.size' "$manifest_file")
  config_file="$extraction_directory/blobs/sha256/${config_digest#sha256:}"
  verify_blob "$name image config" "$config_file" "$config_digest" "$config_size"
  jq --exit-status \
    --arg source "$expected_source" \
    --arg git_sha "$git_sha" \
    --arg version "$version" '
      type == "object" and
      .os == "linux" and
      .architecture == "amd64" and
      (.config.Labels | type == "object") and
      .config.Labels["org.opencontainers.image.source"] == $source and
      .config.Labels["org.opencontainers.image.revision"] == $git_sha and
      .config.Labels["org.opencontainers.image.version"] == $version
    ' "$config_file" >/dev/null ||
    fail "$name image archive has invalid platform or OCI labels"

  jq --raw-output '.layers[] | [.digest, (.size | tostring)] | @tsv' \
    "$manifest_file" > "$layer_contract_file"
  : > "$layer_paths_file"
  while IFS=$'\t' read -r layer_digest layer_size; do
    [[ -n "$layer_digest" && -n "$layer_size" ]] ||
      fail "$name image archive has an incomplete layer descriptor"
    layer_path="blobs/sha256/${layer_digest#sha256:}"
    verify_blob \
      "$name image layer" \
      "$extraction_directory/$layer_path" \
      "$layer_digest" \
      "$layer_size"
    printf '%s\n' "$layer_path" >> "$layer_paths_file"
  done < "$layer_contract_file"

  jq --exit-status \
    --arg reference "$repository:$version" \
    --arg config "blobs/sha256/${config_digest#sha256:}" \
    --rawfile layer_paths "$layer_paths_file" '
      ($layer_paths | split("\n") | map(select(length > 0))) as $layers |
      type == "array" and length == 1 and
      (.[0] |
        type == "object" and
        keys == ["Config", "Layers", "RepoTags"] and
        .Config == $config and
        .RepoTags == [$reference] and
        .Layers == $layers)
    ' "$docker_manifest_file" >/dev/null ||
    fail "$name image archive has an invalid Docker manifest or tag set"

  {
    printf '%s\n' index.json manifest.json oci-layout
    printf 'blobs/sha256/%s\n' "${digest#sha256:}" "${config_digest#sha256:}"
    cat "$layer_paths_file"
  } | LC_ALL=C sort -u > "$expected_members_file"
  grep -v '/$' "$members_file" | LC_ALL=C sort -u > "$regular_members_file"
  [[ "$(< "$regular_members_file")" == "$(< "$expected_members_file")" ]] ||
    fail "$name image archive contains unreferenced or missing files"
  rm -rf -- "$validation_directory"
}

verify_loaded_image() {
  local description=$1
  local reference=$2
  local inspect_file=$3
  local require_labels=$4
  local version=${5:-}
  local git_sha=${6:-}

  docker image inspect "$reference" > "$inspect_file" ||
    fail "$description is not inspectable as $reference"
  if [[ "$require_labels" == true ]]; then
    jq --exit-status --slurp \
      --arg reference "$reference" \
      --arg version "$version" \
      --arg git_sha "$git_sha" \
      --arg source "$expected_source" '
        length == 1 and
        (.[0] |
          type == "array" and length == 1 and
          (.[0] |
            type == "object" and
            (.RepoDigests | type == "array" and index($reference) != null) and
            .Os == "linux" and
            .Architecture == "amd64" and
            (.Config.Labels | type == "object") and
            .Config.Labels["org.opencontainers.image.source"] == $source and
            .Config.Labels["org.opencontainers.image.revision"] == $git_sha and
            .Config.Labels["org.opencontainers.image.version"] == $version))
      ' "$inspect_file" >/dev/null ||
      fail "$description identity does not match the offline release"
  else
    jq --exit-status --slurp \
      --arg reference "$reference" '
        length == 1 and
        (.[0] |
          type == "array" and length == 1 and
          (.[0] |
            type == "object" and
            (.RepoDigests | type == "array" and index($reference) != null) and
            .Os == "linux" and
            .Architecture == "amd64"))
      ' "$inspect_file" >/dev/null ||
      fail "$description is not the required local linux/amd64 image"
  fi
}

manifest_file=''
bundle_file=''
bundle_directory=''
while (($# > 0)); do
  case "$1" in
    --manifest | --bundle | --directory)
      (($# >= 2)) || {
        usage
        exit 2
      }
      case "$1" in
        --manifest)
          [[ -z "$manifest_file" ]] || fail '--manifest may only be provided once'
          manifest_file=$2
          ;;
        --bundle)
          [[ -z "$bundle_file" ]] || fail '--bundle may only be provided once'
          bundle_file=$2
          ;;
        --directory)
          [[ -z "$bundle_directory" ]] || fail '--directory may only be provided once'
          bundle_directory=$2
          ;;
      esac
      shift 2
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

[[ -n "$manifest_file" ]] || fail '--manifest is required'
[[ -n "$bundle_file" ]] || fail '--bundle is required'
[[ -n "$bundle_directory" ]] || fail '--directory is required'

require_command awk
require_command stat
require_command docker
require_command jq
require_command tar
require_regular_file 'release manifest' "$manifest_file"
require_regular_file 'offline image bundle' "$bundle_file"
[[ ! -L "$bundle_directory" ]] || \
  fail "bundle directory must not be a symbolic link: $bundle_directory"
[[ -d "$bundle_directory" && -r "$bundle_directory" && -x "$bundle_directory" ]] || \
  fail "bundle directory is not readable and searchable: $bundle_directory"

temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/xe3-offline-image-load.XXXXXX")
readonly temporary_directory
trap 'rm -rf -- "$temporary_directory"' EXIT INT TERM
readonly contract_file="$temporary_directory/contract.tsv"

jq --exit-status --slurp \
  --slurpfile manifest "$manifest_file" '
    def version:
      type == "string" and
      test("^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$");
    def git_sha:
      type == "string" and test("^[0-9a-f]{40}$") and test("[1-9a-f]");
    def image_digest:
      type == "string" and
      test("^sha256:[0-9a-f]{64}$") and
      (ltrimstr("sha256:") | test("[1-9a-f]"));
    def sha256:
      type == "string" and test("^[0-9a-f]{64}$") and test("[1-9a-f]");
    def positive_integer:
      type == "number" and
      . >= 1 and . <= 9007199254740991 and . == floor;
    def safe_basename:
      type == "string" and
      test("^[A-Za-z0-9][A-Za-z0-9._-]*$") and . != "." and . != "..";
    length == 1 and
    ($manifest | length == 1) and
    (.[0] as $bundle |
      $manifest[0] as $release |
      ($release | type == "object") and
      $release.manifest_version == 1 and
      ($release.version | version) and
      ($release.git_sha | git_sha) and
      $release.portal_image == "ghcr.io/1024xengineer/xe3-esl-portal" and
      ($release.portal_image_digest | image_digest) and
      $release.server_image == "ghcr.io/1024xengineer/xe3-esl-server" and
      ($release.server_image_digest | image_digest) and
      ($bundle | type == "object") and
      ($bundle | keys) == [
        "bundle_version", "git_sha", "images", "platform",
        "source_date_epoch", "version"
      ] and
      $bundle.bundle_version == 1 and
      ($bundle.version | version) and
      ($bundle.git_sha | git_sha) and
      ($bundle.source_date_epoch | positive_integer) and
      $bundle.platform == "linux/amd64" and
      ($bundle.images | type == "array" and length == 2) and
      ($bundle.images | map(.name)) == ["portal", "server"] and
      (all($bundle.images[];
        type == "object" and
        keys == [
          "archive_file", "archive_sha256", "archive_size_bytes",
          "build_metadata_file", "build_metadata_sha256", "digest",
          "name", "repository"
        ] and
        (.archive_file | safe_basename) and
        (.archive_size_bytes | positive_integer) and
        (.archive_sha256 | sha256) and
        (.build_metadata_file | safe_basename) and
        (.build_metadata_sha256 | sha256) and
        (.digest | image_digest)
      )) and
      $bundle.version == $release.version and
      $bundle.git_sha == $release.git_sha and
      $bundle.images[0].repository == $release.portal_image and
      $bundle.images[0].digest == $release.portal_image_digest and
      $bundle.images[1].repository == $release.server_image and
      $bundle.images[1].digest == $release.server_image_digest
    )
  ' "$bundle_file" >/dev/null || fail 'offline image bundle is invalid'

jq --raw-output '
    ([.version, .git_sha, (.source_date_epoch | tostring)] | @tsv),
    (.images[] | ([
      .name, .repository, .digest, .archive_file,
      (.archive_size_bytes | tostring), .archive_sha256,
      .build_metadata_file, .build_metadata_sha256
    ] | @tsv))
  ' "$bundle_file" > "$contract_file"

[[ "$(wc -l < "$contract_file" | tr -d '[:space:]')" == 3 ]] || \
  fail 'validated bundle did not produce the canonical image contract'

IFS=$'\t' read -r release_version release_git_sha source_date_epoch < "$contract_file"
IFS=$'\t' read -r \
  portal_name portal_repository portal_digest portal_archive portal_size \
  portal_archive_sha portal_metadata portal_metadata_sha \
  < <(sed -n '2p' "$contract_file")
IFS=$'\t' read -r \
  server_name server_repository server_digest server_archive server_size \
  server_archive_sha server_metadata server_metadata_sha \
  < <(sed -n '3p' "$contract_file")

image_names=("$portal_name" "$server_name")
image_repositories=("$portal_repository" "$server_repository")
image_digests=("$portal_digest" "$server_digest")
archive_files=("$portal_archive" "$server_archive")
archive_sizes=("$portal_size" "$server_size")
archive_hashes=("$portal_archive_sha" "$server_archive_sha")
metadata_files=("$portal_metadata" "$server_metadata")
metadata_hashes=("$portal_metadata_sha" "$server_metadata_sha")
archive_paths=()

# Verify every artifact before loading either image. Invalid bundles therefore
# cannot leave a partially loaded release on the host.
for index in 0 1; do
  archive_path="$bundle_directory/${archive_files[$index]}"
  metadata_path="$bundle_directory/${metadata_files[$index]}"
  require_regular_file "${image_names[$index]} image archive" "$archive_path"
  require_regular_file "${image_names[$index]} build metadata" "$metadata_path"
  [[ "$(file_size "$archive_path")" == "${archive_sizes[$index]}" ]] || \
    fail "${image_names[$index]} image archive size does not match the bundle"
  [[ "$(sha256_file "$archive_path")" == "${archive_hashes[$index]}" ]] || \
    fail "${image_names[$index]} image archive checksum does not match the bundle"
  [[ "$(sha256_file "$metadata_path")" == "${metadata_hashes[$index]}" ]] || \
    fail "${image_names[$index]} build metadata checksum does not match the bundle"
  jq --exit-status --slurp \
    --arg digest "${image_digests[$index]}" '
      length == 1 and
      (.[0] | type == "object" and .["containerimage.digest"] == $digest)
    ' "$metadata_path" >/dev/null || \
    fail "${image_names[$index]} build metadata does not match its image digest"
  archive_paths[$index]=$archive_path
  verify_image_archive \
    "${image_names[$index]}" \
    "${image_repositories[$index]}" \
    "${image_digests[$index]}" \
    "$archive_path" \
    "$release_version" \
    "$release_git_sha"
done

verify_loaded_image \
  'required Postgres image' \
  "$postgres_reference" \
  "$temporary_directory/postgres-inspect.json" \
  false

for index in 0 1; do
  name=${image_names[$index]}
  reference="${image_repositories[$index]}@${image_digests[$index]}"
  inspect_file="$temporary_directory/$name-inspect.json"

  docker image load --platform "$expected_platform" -i "${archive_paths[$index]}" \
    >/dev/null || fail "failed to load the $name image archive"
  verify_loaded_image \
    "loaded $name image" \
    "$reference" \
    "$inspect_file" \
    true \
    "$release_version" \
    "$release_git_sha"
done

printf '%s\n' \
  "offline_images_loaded=true version=$release_version git_sha=$release_git_sha platform=$expected_platform source_date_epoch=$source_date_epoch"
