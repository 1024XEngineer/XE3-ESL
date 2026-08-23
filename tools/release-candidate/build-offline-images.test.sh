#!/usr/bin/env bash

set -euo pipefail
umask 077

readonly script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly build_script="$script_directory/build-offline-images.sh"

fail() {
  printf 'Offline image build test: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

require_command git
require_command node
require_command mktemp
[[ -f "$build_script" ]] || fail "$build_script is required"

temporary_base=${TMPDIR:-/tmp}
temporary_base=${temporary_base%/}
temporary_directory_created="$(
  mktemp -d "$temporary_base/xe3-offline-images-test.XXXXXX"
)"
temporary_directory="$(cd "$temporary_directory_created" && pwd -P)"
readonly temporary_directory
readonly repository="$temporary_directory/repository"
readonly fake_bin="$temporary_directory/fake-bin"
readonly fake_counter="$temporary_directory/docker-counter"
readonly fake_log="$temporary_directory/docker.log"
readonly artifacts_parent="$temporary_directory/artifacts"

cleanup() {
  local status=$?

  trap - EXIT INT HUP TERM
  if [[ "$temporary_directory" == */xe3-offline-images-test.* ]]; then
    rm -rf "$temporary_directory"
  else
    status=1
  fi
  exit "$status"
}

trap cleanup EXIT INT HUP TERM

mkdir -p \
  "$repository/portal" \
  "$repository/server" \
  "$fake_bin" \
  "$artifacts_parent"
printf '%s\n' 'FROM scratch' >"$repository/portal/Dockerfile"
printf '%s\n' 'FROM scratch' >"$repository/server/Dockerfile"
git -C "$repository" init --initial-branch=main >/dev/null
git -C "$repository" config user.name 'Offline Build Test'
git -C "$repository" config user.email 'offline-build-test@example.invalid'
git -C "$repository" add portal/Dockerfile server/Dockerfile
GIT_AUTHOR_DATE='2026-08-22T12:34:56Z' \
GIT_COMMITTER_DATE='2026-08-22T12:34:56Z' \
  git -C "$repository" commit -m 'test release source' >/dev/null
readonly head_sha="$(git -C "$repository" rev-parse --verify 'HEAD^{commit}')"
readonly source_date_epoch="$(git -C "$repository" show -s --format=%ct HEAD)"
readonly version='1.2.3'

cat >"$fake_bin/docker" <<'EOF'
#!/usr/bin/env bash

set -euo pipefail

fail() {
  printf 'Fake Docker error: %s\n' "$*" >&2
  exit 64
}

[[ $# -ge 2 && "$1" == buildx && "$2" == build ]] ||
  fail 'expected docker buildx build'
shift 2

file=''
platform=''
pull=false
no_cache=false
provenance=false
sbom=false
source_date_epoch_build_arg=''
deployment_id_build_arg=''
source_label=''
revision_label=''
version_label=''
tag=''
metadata=''
output=''
context=''

while (($# > 0)); do
  case "$1" in
    --file | --platform | --build-arg | --label | --tag | --metadata-file | --output)
      [[ $# -ge 2 ]] || fail "$1 requires a value"
      case "$1" in
        --file) [[ -z "$file" ]] || fail 'duplicate --file'; file=$2 ;;
        --platform) [[ -z "$platform" ]] || fail 'duplicate --platform'; platform=$2 ;;
        --build-arg)
          case "$2" in
            SOURCE_DATE_EPOCH=*)
              [[ -z "$source_date_epoch_build_arg" ]] ||
                fail 'duplicate SOURCE_DATE_EPOCH build argument'
              source_date_epoch_build_arg=$2
              ;;
            NEXT_DEPLOYMENT_ID=*)
              [[ -z "$deployment_id_build_arg" ]] ||
                fail 'duplicate NEXT_DEPLOYMENT_ID build argument'
              deployment_id_build_arg=$2
              ;;
            *) fail "unexpected build argument: $2" ;;
          esac
          ;;
        --label)
          case "$2" in
            org.opencontainers.image.source=*)
              [[ -z "$source_label" ]] || fail 'duplicate source label'
              source_label=$2
              ;;
            org.opencontainers.image.revision=*)
              [[ -z "$revision_label" ]] || fail 'duplicate revision label'
              revision_label=$2
              ;;
            org.opencontainers.image.version=*)
              [[ -z "$version_label" ]] || fail 'duplicate version label'
              version_label=$2
              ;;
            *) fail "unexpected label: $2" ;;
          esac
          ;;
        --tag) [[ -z "$tag" ]] || fail 'duplicate --tag'; tag=$2 ;;
        --metadata-file)
          [[ -z "$metadata" ]] || fail 'duplicate --metadata-file'
          metadata=$2
          ;;
        --output) [[ -z "$output" ]] || fail 'duplicate --output'; output=$2 ;;
      esac
      shift 2
      ;;
    --pull)
      [[ "$pull" == false ]] || fail 'duplicate --pull'
      pull=true
      shift
      ;;
    --no-cache)
      [[ "$no_cache" == false ]] || fail 'duplicate --no-cache'
      no_cache=true
      shift
      ;;
    --provenance=false)
      [[ "$provenance" == false ]] || fail 'duplicate --provenance=false'
      provenance=true
      shift
      ;;
    --sbom=false)
      [[ "$sbom" == false ]] || fail 'duplicate --sbom=false'
      sbom=true
      shift
      ;;
    --*) fail "unexpected build flag: $1" ;;
    *)
      [[ -z "$context" && $# == 1 ]] || fail "unexpected build argument: $1"
      context=$1
      shift
      ;;
  esac
done

[[ "$platform" == linux/amd64 ]] || fail 'platform must be linux/amd64'
[[ "$pull" == true && "$no_cache" == true &&
  "$provenance" == true && "$sbom" == true ]] ||
  fail 'pull/no-cache/provenance/sbom flags are incomplete'
[[ "$source_date_epoch_build_arg" == \
  "SOURCE_DATE_EPOCH=$FAKE_EXPECTED_EPOCH" ]] ||
  fail 'SOURCE_DATE_EPOCH build argument is incorrect'
[[ "${SOURCE_DATE_EPOCH:-}" == "$FAKE_EXPECTED_EPOCH" ]] ||
  fail 'SOURCE_DATE_EPOCH environment is incorrect'
[[ "$source_label" == \
  'org.opencontainers.image.source=https://github.com/1024XEngineer/XE3-ESL' ]] ||
  fail 'OCI source label is incorrect'
[[ "$revision_label" == "org.opencontainers.image.revision=$FAKE_EXPECTED_SHA" ]] ||
  fail 'OCI revision label is incorrect'
[[ "$version_label" == "org.opencontainers.image.version=$FAKE_EXPECTED_VERSION" ]] ||
  fail 'OCI version label is incorrect'
[[ -n "$metadata" &&
  "$output" == type=docker,dest=*,rewrite-timestamp=true ]] ||
  fail 'Docker archive output or metadata file is missing'
archive=${output#type=docker,dest=}
archive=${archive%,rewrite-timestamp=true}
[[ "$archive" != "$output" && -n "$archive" ]] || fail 'archive destination is invalid'

case "$tag" in
  "ghcr.io/1024xengineer/xe3-esl-portal:$FAKE_EXPECTED_VERSION")
    name=portal
    digest="sha256:$(printf 'a%.0s' {1..64})"
    [[ "$deployment_id_build_arg" == \
      "NEXT_DEPLOYMENT_ID=$FAKE_EXPECTED_SHA" ]] ||
      fail 'Portal deployment ID build argument is incorrect'
    ;;
  "ghcr.io/1024xengineer/xe3-esl-server:$FAKE_EXPECTED_VERSION")
    name=server
    digest="sha256:$(printf 'b%.0s' {1..64})"
    [[ -z "$deployment_id_build_arg" ]] ||
      fail 'Server must not receive the Portal deployment ID build argument'
    ;;
  *) fail "unexpected image tag: $tag" ;;
esac
[[ "$context" == "$FAKE_REPOSITORY_ROOT/$name" ]] ||
  fail "$name build context is incorrect"
[[ "$file" == "$context/Dockerfile" ]] || fail "$name Dockerfile is incorrect"

count=0
if [[ -f "$FAKE_DOCKER_COUNTER" ]]; then
  count=$(<"$FAKE_DOCKER_COUNTER")
fi
count=$((count + 1))
printf '%s\n' "$count" >"$FAKE_DOCKER_COUNTER"
printf '%s\t%s\t%s\t%s\n' \
  "$count" "$name" "$SOURCE_DATE_EPOCH" "$tag" >>"$FAKE_DOCKER_LOG"

if [[ "${FAKE_DOCKER_MODE:-success}" == failure && "$count" == 2 ]]; then
  exit 70
fi
if [[ "${FAKE_DOCKER_MODE:-success}" == invalid-metadata &&
  "$count" == 1 ]]; then
  digest='invalid'
fi

printf 'fake docker archive: %s build %s\n' "$tag" "$count" >"$archive"
printf '{"containerimage.digest":"%s","fake.build.number":%s}\n' \
  "$digest" "$count" >"$metadata"
EOF
chmod 0700 "$fake_bin/docker"

reset_fake_docker() {
  rm -f "$fake_counter" "$fake_log"
}

run_build() {
  local mode=$1
  shift

  (
    cd "$repository"
    env \
      PATH="$fake_bin:$PATH" \
      FAKE_DOCKER_MODE="$mode" \
      FAKE_DOCKER_COUNTER="$fake_counter" \
      FAKE_DOCKER_LOG="$fake_log" \
      FAKE_EXPECTED_EPOCH="$source_date_epoch" \
      FAKE_EXPECTED_SHA="$head_sha" \
      FAKE_EXPECTED_VERSION="$version" \
      FAKE_REPOSITORY_ROOT="$repository" \
      bash "$build_script" "$@"
  )
}

failure_number=0
expect_failure() {
  local name=$1
  local mode=$2
  local output=$3
  shift 3
  local failure_log

  failure_number=$((failure_number + 1))
  failure_log="$temporary_directory/failure-$failure_number.log"
  reset_fake_docker
  if run_build "$mode" "$@" >"$failure_log" 2>&1; then
    fail "$name unexpectedly succeeded"
  fi
  [[ ! -e "$output" && ! -L "$output" ]] ||
    fail "$name published an output directory after failure"
}

missing_output="$artifacts_parent/missing-arguments"
expect_failure 'missing arguments' success "$missing_output"
[[ ! -e "$fake_counter" ]] || fail 'missing arguments invoked Docker'

invalid_version_output="$artifacts_parent/invalid-version"
expect_failure \
  'invalid version' \
  success \
  "$invalid_version_output" \
  --version v1.2.3 \
  --git-sha "$head_sha" \
  --output-dir "$invalid_version_output"
[[ ! -e "$fake_counter" ]] || fail 'invalid version invoked Docker'

unknown_argument_output="$artifacts_parent/unknown-argument"
expect_failure \
  'unknown argument' \
  success \
  "$unknown_argument_output" \
  --version "$version" \
  --git-sha "$head_sha" \
  --output-dir "$unknown_argument_output" \
  --unexpected value
[[ ! -e "$fake_counter" ]] || fail 'unknown argument invoked Docker'

dirty_output="$artifacts_parent/dirty-worktree"
printf '%s\n' dirty >"$repository/untracked.txt"
expect_failure \
  'dirty worktree' \
  success \
  "$dirty_output" \
  --version "$version" \
  --git-sha "$head_sha" \
  --output-dir "$dirty_output"
rm "$repository/untracked.txt"
[[ ! -e "$fake_counter" ]] || fail 'dirty worktree invoked Docker'

sha_mismatch_output="$artifacts_parent/sha-mismatch"
expect_failure \
  'HEAD SHA mismatch' \
  success \
  "$sha_mismatch_output" \
  --version "$version" \
  --git-sha "$(printf '0%.0s' {1..40})" \
  --output-dir "$sha_mismatch_output"
[[ ! -e "$fake_counter" ]] || fail 'HEAD SHA mismatch invoked Docker'

inside_repository_output="$repository/offline-release-output"
expect_failure \
  'output inside Git worktree' \
  success \
  "$inside_repository_output" \
  --version "$version" \
  --git-sha "$head_sha" \
  --output-dir "$inside_repository_output"
[[ ! -e "$fake_counter" ]] || fail 'in-worktree output invoked Docker'

invalid_metadata_output="$artifacts_parent/invalid-metadata"
expect_failure \
  'invalid Docker metadata digest' \
  invalid-metadata \
  "$invalid_metadata_output" \
  --version "$version" \
  --git-sha "$head_sha" \
  --output-dir "$invalid_metadata_output"
if [[ ! -f "$fake_counter" ]]; then
  cat "$temporary_directory/failure-$failure_number.log" >&2
  fail 'invalid metadata did not invoke Docker'
fi
[[ "$(<"$fake_counter")" == 1 ]] ||
  fail 'invalid metadata did not stop after the Portal build'

build_failure_output="$artifacts_parent/build-failure"
expect_failure \
  'Server Docker build failure' \
  failure \
  "$build_failure_output" \
  --version "$version" \
  --git-sha "$head_sha" \
  --output-dir "$build_failure_output"
if [[ ! -f "$fake_counter" ]]; then
  cat "$temporary_directory/failure-$failure_number.log" >&2
  fail 'Docker failure test did not invoke Docker'
fi
[[ "$(<"$fake_counter")" == 2 ]] ||
  fail 'Docker failure did not stop after the failing build'

success_output="$artifacts_parent/success"
reset_fake_docker
success_log="$temporary_directory/success.log"
run_build \
  success \
  --version "$version" \
  --git-sha "$head_sha" \
  --output-dir "$success_output" >"$success_log"
[[ "$(<"$success_log")" == \
  "offline_image_bundle=$success_output/offline-image-bundle.json" ]] ||
  fail 'success output did not identify the published bundle manifest'
[[ "$(<"$fake_counter")" == 2 ]] ||
  fail 'success did not build Portal and Server exactly once each'
[[ "$(wc -l <"$fake_log" | tr -d '[:space:]')" == 2 ]] ||
  fail 'fake Docker did not record exactly two buildx invocations'

node - \
  "$success_output" \
  "$version" \
  "$head_sha" \
  "$source_date_epoch" <<'NODE'
const assert = require("node:assert/strict");
const { createHash } = require("node:crypto");
const { readdirSync, readFileSync, statSync } = require("node:fs");
const path = require("node:path");

const [directory, version, gitSha, sourceDateEpoch] = process.argv.slice(2);
const manifest = JSON.parse(
  readFileSync(path.join(directory, "offline-image-bundle.json"), "utf8"),
);
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
const expectedRepositories = [
  "ghcr.io/1024xengineer/xe3-esl-portal",
  "ghcr.io/1024xengineer/xe3-esl-server",
];
const expectedDigests = [
  `sha256:${"a".repeat(64)}`,
  `sha256:${"b".repeat(64)}`,
];

assert.deepEqual(Object.keys(manifest), topLevelKeys);
assert.equal(manifest.bundle_version, 1);
assert.equal(manifest.version, version);
assert.equal(manifest.git_sha, gitSha);
assert.equal(manifest.source_date_epoch, Number(sourceDateEpoch));
assert.equal(manifest.platform, "linux/amd64");
assert.equal(manifest.images.length, 2);
assert.deepEqual(manifest.images.map(({ name }) => name), ["portal", "server"]);

for (const [index, image] of manifest.images.entries()) {
  assert.deepEqual(Object.keys(image), imageKeys);
  assert.equal(image.repository, expectedRepositories[index]);
  assert.equal(image.digest, expectedDigests[index]);
  for (const file of [image.archive_file, image.build_metadata_file]) {
    assert.equal(path.basename(file), file);
    assert.equal(file.includes("/"), false);
    assert.equal(file.includes("\\"), false);
  }
  const archivePath = path.join(directory, image.archive_file);
  const metadataPath = path.join(directory, image.build_metadata_file);
  assert.equal(statSync(archivePath).isFile(), true);
  assert.equal(statSync(metadataPath).isFile(), true);
  assert.equal(image.archive_size_bytes, statSync(archivePath).size);
  assert.match(image.archive_sha256, /^[0-9a-f]{64}$/);
  assert.match(image.build_metadata_sha256, /^[0-9a-f]{64}$/);
  assert.equal(
    image.archive_sha256,
    createHash("sha256").update(readFileSync(archivePath)).digest("hex"),
  );
  assert.equal(
    image.build_metadata_sha256,
    createHash("sha256").update(readFileSync(metadataPath)).digest("hex"),
  );
  assert.equal(
    JSON.parse(readFileSync(metadataPath, "utf8"))["containerimage.digest"],
    image.digest,
  );
}

assert.deepEqual(readdirSync(directory).sort(), [
  "offline-image-bundle.json",
  `speakup-portal-v${version}-linux-amd64-build-metadata.json`,
  `speakup-portal-v${version}-linux-amd64.tar`,
  `speakup-server-v${version}-linux-amd64-build-metadata.json`,
  `speakup-server-v${version}-linux-amd64.tar`,
]);
NODE

if find "$artifacts_parent" -maxdepth 1 -name '.*.tmp.*' -print -quit |
  grep -q .; then
  fail 'a temporary bundle directory leaked after the tests'
fi

printf '%s\n' 'Offline image build tests passed'
