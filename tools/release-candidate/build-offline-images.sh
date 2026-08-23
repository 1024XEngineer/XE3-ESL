#!/usr/bin/env bash

set -euo pipefail
umask 077

readonly bundle_file_name='offline-image-bundle.json'
readonly platform='linux/amd64'
readonly source_url='https://github.com/1024XEngineer/XE3-ESL'
readonly portal_repository='ghcr.io/1024xengineer/xe3-esl-portal'
readonly server_repository='ghcr.io/1024xengineer/xe3-esl-server'

version=''
git_sha=''
output_directory_input=''
temporary_directory=''
output_parent=''
output_name=''
portal_archive_file=''
server_archive_file=''
portal_metadata_file=''
server_metadata_file=''

usage() {
  cat >&2 <<'EOF'
Usage: build-offline-images.sh \
  --version <X.Y.Z> \
  --git-sha <40-lowercase-hex-commit> \
  --output-dir <new-directory>
EOF
}

fail() {
  printf 'Offline image build error: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

set_argument() {
  local name=$1
  local value=$2

  [[ -n "$value" ]] || fail "--$name requires a non-empty value"
  case "$name" in
    version)
      [[ -z "$version" ]] || fail '--version may only be provided once'
      version=$value
      ;;
    git-sha)
      [[ -z "$git_sha" ]] || fail '--git-sha may only be provided once'
      git_sha=$value
      ;;
    output-dir)
      [[ -z "$output_directory_input" ]] ||
        fail '--output-dir may only be provided once'
      output_directory_input=$value
      ;;
    *) fail "unknown argument: --$name" ;;
  esac
}

cleanup() {
  local status=$?

  trap - EXIT INT HUP TERM
  set +e
  if [[ -n "$temporary_directory" ]]; then
    if [[ -n "$output_parent" && -n "$output_name" &&
      "${temporary_directory%/*}" == "$output_parent" &&
      "${temporary_directory##*/}" == ".${output_name}.tmp."* ]]; then
      rm -f \
        "$temporary_directory/$bundle_file_name" \
        "$temporary_directory/$portal_archive_file" \
        "$temporary_directory/$server_archive_file" \
        "$temporary_directory/$portal_metadata_file" \
        "$temporary_directory/$server_metadata_file" || status=1
      rmdir "$temporary_directory" || status=1
    else
      status=1
    fi
  fi
  exit "$status"
}

trap cleanup EXIT
trap 'exit 130' INT HUP TERM

while (($# > 0)); do
  [[ "$1" == --* ]] || {
    usage
    fail "unexpected positional argument: $1"
  }
  [[ $# -ge 2 ]] || {
    usage
    fail "$1 requires a value"
  }
  set_argument "${1#--}" "$2"
  shift 2
done

[[ -n "$version" && -n "$git_sha" && -n "$output_directory_input" ]] || {
  usage
  fail '--version, --git-sha, and --output-dir are all required'
}
[[ "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] ||
  fail '--version must use X.Y.Z without a v prefix or prerelease suffix'
[[ "$git_sha" =~ ^[a-f0-9]{40}$ ]] ||
  fail '--git-sha must be a full lowercase 40-character Git SHA'
[[ "$output_directory_input" != *$'\n'* &&
  "$output_directory_input" != *$'\r'* &&
  "$output_directory_input" != *,* ]] ||
  fail '--output-dir must not contain commas or newlines'
portal_archive_file="speakup-portal-v${version}-linux-amd64.tar"
server_archive_file="speakup-server-v${version}-linux-amd64.tar"
portal_metadata_file="speakup-portal-v${version}-linux-amd64-build-metadata.json"
server_metadata_file="speakup-server-v${version}-linux-amd64-build-metadata.json"
readonly \
  portal_archive_file \
  server_archive_file \
  portal_metadata_file \
  server_metadata_file

require_command docker
require_command git
require_command mktemp
require_command node

repository_root_input="$(git rev-parse --show-toplevel 2>/dev/null)" ||
  fail 'the current directory must belong to a Git worktree'
repository_root="$(cd "$repository_root_input" && pwd -P)" ||
  fail 'cannot resolve the Git worktree root'
readonly repository_root
cd "$repository_root"

head_sha="$(git rev-parse --verify 'HEAD^{commit}' 2>/dev/null)" ||
  fail 'HEAD must resolve to a Git commit'
[[ "$head_sha" == "$git_sha" ]] ||
  fail "HEAD $head_sha does not match --git-sha $git_sha"
worktree_status="$(
  git status --porcelain=v1 --untracked-files=all --ignore-submodules=none
)" || fail 'cannot inspect the Git worktree status'
[[ -z "$worktree_status" ]] ||
  fail 'the Git worktree must be clean before building release images'

source_date_epoch="$(git show -s --format=%ct "$git_sha" 2>/dev/null)" ||
  fail 'cannot read the release commit timestamp'
[[ "$source_date_epoch" =~ ^[1-9][0-9]*$ ]] ||
  fail 'the release commit timestamp must be a positive integer'
readonly source_date_epoch

[[ -f "$repository_root/portal/Dockerfile" &&
  -f "$repository_root/server/Dockerfile" ]] ||
  fail 'portal/Dockerfile and server/Dockerfile are required'

output_directory="$(
  node -e 'process.stdout.write(require("node:path").resolve(process.argv[1]))' \
    "$output_directory_input"
)" || fail 'cannot resolve --output-dir'
output_parent_input="$(dirname "$output_directory")"
output_name="$(basename "$output_directory")"
[[ "$output_name" != / && "$output_name" != . && "$output_name" != .. ]] ||
  fail '--output-dir must name a new non-root directory'
mkdir -p "$output_parent_input"
output_parent="$(cd "$output_parent_input" && pwd -P)" ||
  fail 'cannot resolve the output parent directory'
output_directory="$output_parent/$output_name"
readonly output_parent output_name output_directory
[[ ! -e "$output_directory" && ! -L "$output_directory" ]] ||
  fail '--output-dir must not already exist'
case "$output_directory/" in
  "$repository_root/"*)
    fail '--output-dir must be outside the Git worktree'
    ;;
esac

temporary_directory="$(
  mktemp -d "$output_parent/.${output_name}.tmp.XXXXXX"
)" || fail 'cannot create the temporary bundle directory'

metadata_digest() {
  local metadata=$1
  local digest

  if ! digest="$(node - "$metadata" <<'NODE'
const { readFileSync } = require("node:fs");

const file = process.argv[2];
const metadata = JSON.parse(readFileSync(file, "utf8"));
const digest = metadata?.["containerimage.digest"];
if (!/^sha256:[0-9a-f]{64}$/.test(digest ?? "")) {
  throw new Error(`${file} has an invalid containerimage.digest`);
}
process.stdout.write(digest);
NODE
  )"; then
    fail "cannot validate Docker build metadata: ${metadata##*/}"
  fi
  printf '%s\n' "$digest"
}

run_image_build() {
  local context=$1
  local repository=$2
  local archive=$3
  local metadata=$4
  local -a build_arguments=(
    --build-arg "SOURCE_DATE_EPOCH=$source_date_epoch"
  )

  if [[ "$repository" == "$portal_repository" ]]; then
    build_arguments+=(
      --build-arg "NEXT_DEPLOYMENT_ID=$git_sha"
    )
  fi

  SOURCE_DATE_EPOCH="$source_date_epoch" docker buildx build \
    --file "$context/Dockerfile" \
    --platform "$platform" \
    --pull \
    --no-cache \
    --provenance=false \
    --sbom=false \
    "${build_arguments[@]}" \
    --label "org.opencontainers.image.source=$source_url" \
    --label "org.opencontainers.image.revision=$git_sha" \
    --label "org.opencontainers.image.version=$version" \
    --tag "$repository:$version" \
    --metadata-file "$metadata" \
    --output "type=docker,dest=$archive,rewrite-timestamp=true" \
    "$context"
}

build_verified_image() {
  local name=$1
  local context=$2
  local repository=$3
  local archive_file=$4
  local metadata_file=$5
  local result_variable=$6
  local archive="$temporary_directory/$archive_file"
  local metadata="$temporary_directory/$metadata_file"
  local digest

  run_image_build "$context" "$repository" "$archive" "$metadata"
  digest="$(metadata_digest "$metadata")"
  [[ -s "$archive" && -s "$metadata" ]] ||
    fail "$name image build did not produce every required file"

  printf -v "$result_variable" '%s' "$digest"
}

portal_digest=''
server_digest=''
build_verified_image \
  portal \
  "$repository_root/portal" \
  "$portal_repository" \
  "$portal_archive_file" \
  "$portal_metadata_file" \
  portal_digest
build_verified_image \
  server \
  "$repository_root/server" \
  "$server_repository" \
  "$server_archive_file" \
  "$server_metadata_file" \
  server_digest

node - \
  "$temporary_directory" \
  "$version" \
  "$git_sha" \
  "$source_date_epoch" \
  "$portal_digest" \
  "$server_digest" \
  "$portal_archive_file" \
  "$server_archive_file" \
  "$portal_metadata_file" \
  "$server_metadata_file" <<'NODE'
const { createHash } = require("node:crypto");
const {
  readdirSync,
  readFileSync,
  statSync,
  writeFileSync,
} = require("node:fs");
const path = require("node:path");

const [
  directory,
  version,
  gitSha,
  sourceDateEpochInput,
  portalDigest,
  serverDigest,
  portalArchiveFile,
  serverArchiveFile,
  portalMetadataFile,
  serverMetadataFile,
] = process.argv.slice(2);
const sourceDateEpoch = Number(sourceDateEpochInput);
const digestPattern = /^sha256:[0-9a-f]{64}$/;
const sha256Pattern = /^[0-9a-f]{64}$/;
const topLevelKeys = [
  "bundle_version",
  "version",
  "git_sha",
  "source_date_epoch",
  "platform",
  "images",
];
const imageKeys = [
  "name",
  "repository",
  "digest",
  "archive_file",
  "archive_size_bytes",
  "archive_sha256",
  "build_metadata_file",
  "build_metadata_sha256",
];

if (!Number.isSafeInteger(sourceDateEpoch) || sourceDateEpoch < 1) {
  throw new Error("source_date_epoch must be a positive safe integer");
}

function sha256(file) {
  return createHash("sha256").update(readFileSync(file)).digest("hex");
}

function assertFileName(file) {
  if (path.basename(file) !== file || file.includes("/") || file.includes("\\")) {
    throw new Error(`bundle file name must not contain a path: ${file}`);
  }
}

function image(name, repository, digest, archiveFile, metadataFile) {
  assertFileName(archiveFile);
  assertFileName(metadataFile);
  if (!digestPattern.test(digest)) {
    throw new Error(`${name} digest is invalid`);
  }
  const archivePath = path.join(directory, archiveFile);
  const metadataPath = path.join(directory, metadataFile);
  const archiveStatus = statSync(archivePath);
  const metadataStatus = statSync(metadataPath);
  if (!archiveStatus.isFile() || archiveStatus.size < 1 ||
      !metadataStatus.isFile() || metadataStatus.size < 1) {
    throw new Error(`${name} build artifacts must be non-empty regular files`);
  }
  const metadata = JSON.parse(readFileSync(metadataPath, "utf8"));
  if (metadata?.["containerimage.digest"] !== digest) {
    throw new Error(`${name} build metadata does not match its image digest`);
  }
  const entry = {
    name,
    repository,
    digest,
    archive_file: archiveFile,
    archive_size_bytes: archiveStatus.size,
    archive_sha256: sha256(archivePath),
    build_metadata_file: metadataFile,
    build_metadata_sha256: sha256(metadataPath),
  };
  if (
    JSON.stringify(Object.keys(entry)) !== JSON.stringify(imageKeys) ||
    !sha256Pattern.test(entry.archive_sha256) ||
    !sha256Pattern.test(entry.build_metadata_sha256)
  ) {
    throw new Error(`${name} manifest entry violates the bundle schema`);
  }
  return entry;
}

const manifest = {
  bundle_version: 1,
  version,
  git_sha: gitSha,
  source_date_epoch: sourceDateEpoch,
  platform: "linux/amd64",
  images: [
    image(
      "portal",
      "ghcr.io/1024xengineer/xe3-esl-portal",
      portalDigest,
      portalArchiveFile,
      portalMetadataFile,
    ),
    image(
      "server",
      "ghcr.io/1024xengineer/xe3-esl-server",
      serverDigest,
      serverArchiveFile,
      serverMetadataFile,
    ),
  ],
};
if (
  JSON.stringify(Object.keys(manifest)) !== JSON.stringify(topLevelKeys) ||
  manifest.images.length !== 2 ||
  manifest.images[0].name !== "portal" ||
  manifest.images[1].name !== "server"
) {
  throw new Error("offline image bundle violates the strict schema");
}

const manifestPath = path.join(directory, "offline-image-bundle.json");
writeFileSync(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`, "utf8");
const expectedFiles = [
  "offline-image-bundle.json",
  portalArchiveFile,
  serverArchiveFile,
  portalMetadataFile,
  serverMetadataFile,
].sort();
const actualFiles = readdirSync(directory).sort();
if (JSON.stringify(actualFiles) !== JSON.stringify(expectedFiles)) {
  throw new Error("temporary bundle directory contains unexpected files");
}
JSON.parse(readFileSync(manifestPath, "utf8"));
NODE

[[ ! -e "$output_directory" && ! -L "$output_directory" ]] ||
  fail '--output-dir appeared before atomic publication'
node -e \
  'require("node:fs").renameSync(process.argv[1], process.argv[2])' \
  "$temporary_directory" \
  "$output_directory"
temporary_directory=''

printf 'offline_image_bundle=%s\n' "$output_directory/$bundle_file_name"
