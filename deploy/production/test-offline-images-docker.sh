#!/usr/bin/env bash

set -euo pipefail
umask 077

readonly production_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly loader="$production_directory/load-offline-images.sh"
readonly portal_repository='ghcr.io/1024xengineer/xe3-esl-portal'
readonly server_repository='ghcr.io/1024xengineer/xe3-esl-server'
readonly postgres_reference='postgres@sha256:7d2695c3aa88e792e8b3b233e7e4adb296a20412c6c0ca361e3edaaacfada108'
readonly source_url='https://github.com/1024XEngineer/XE3-ESL'

fail() {
  printf 'offline image Docker test: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

file_size() {
  local file=$1
  stat -c '%s' -- "$file" 2>/dev/null || stat -f '%z' "$file"
}

require_command docker
require_command jq
require_command node
require_command mktemp
readonly real_docker="$(command -v docker)"
"$real_docker" version >/dev/null 2>&1 || fail 'a reachable Docker Engine is required'
"$real_docker" buildx version >/dev/null 2>&1 || fail 'Docker Buildx is required'

readonly version="999.0.$$"
git_sha=$(printf '%040x' "$$")
readonly git_sha
readonly temporary_directory="$(
  mktemp -d "${TMPDIR:-/tmp}/xe3-offline-image-docker-test.XXXXXX"
)"
readonly bundle_directory="$temporary_directory/bundle"
readonly manifest_file="$temporary_directory/release-manifest.json"
readonly bundle_file="$bundle_directory/offline-image-bundle.json"
readonly fake_bin="$temporary_directory/fake-bin"
readonly portal_archive="$bundle_directory/portal-image.tar"
readonly server_archive="$bundle_directory/server-image.tar"
readonly portal_metadata="$bundle_directory/portal-build-metadata.json"
readonly server_metadata="$bundle_directory/server-build-metadata.json"

cleanup_image() {
  local reference=$1
  local identity
  local image_id

  identity="$(
    "$real_docker" image inspect \
      --format '{{index .Config.Labels "org.opencontainers.image.revision"}} {{index .Config.Labels "org.opencontainers.image.version"}} {{.Id}}' \
      "$reference" 2>/dev/null
  )" || return 0
  [[ "$identity" == "$git_sha $version "* ]] || return 0
  image_id=${identity##* }
  "$real_docker" image rm "$reference" >/dev/null 2>&1 || true
  if "$real_docker" image inspect "$image_id" >/dev/null 2>&1; then
    identity="$(
      "$real_docker" image inspect \
        --format '{{index .Config.Labels "org.opencontainers.image.revision"}} {{index .Config.Labels "org.opencontainers.image.version"}}' \
        "$image_id" 2>/dev/null
    )" || return 0
    if [[ "$identity" == "$git_sha $version" ]]; then
      "$real_docker" image rm "$image_id" >/dev/null 2>&1 || true
    fi
  fi
}

cleanup() {
  local status=$?

  trap - EXIT INT HUP TERM
  set +e
  cleanup_image "$portal_repository:$version"
  cleanup_image "$server_repository:$version"
  rm -rf -- "$temporary_directory"
  exit "$status"
}
trap cleanup EXIT INT HUP TERM

for repository in "$portal_repository" "$server_repository"; do
  if "$real_docker" image inspect "$repository:$version" >/dev/null 2>&1; then
    fail "fixture image reference already exists: $repository:$version"
  fi
done

mkdir -p \
  "$bundle_directory" \
  "$fake_bin" \
  "$temporary_directory/portal-context" \
  "$temporary_directory/server-context"
for name in portal server; do
  printf '%s\n' "$name fixture layer" > "$temporary_directory/$name-context/payload.txt"
  printf '%s\n' \
    'FROM scratch' \
    'COPY payload.txt /payload.txt' \
    > "$temporary_directory/$name-context/Dockerfile"
done

build_fixture() {
  local name=$1
  local repository=$2
  local archive=$3
  local metadata=$4

  SOURCE_DATE_EPOCH=1760000000 "$real_docker" buildx build \
    --file "$temporary_directory/$name-context/Dockerfile" \
    --platform linux/amd64 \
    --provenance=false \
    --sbom=false \
    --build-arg SOURCE_DATE_EPOCH=1760000000 \
    --label "org.opencontainers.image.source=$source_url" \
    --label "org.opencontainers.image.revision=$git_sha" \
    --label "org.opencontainers.image.version=$version" \
    --tag "$repository:$version" \
    --metadata-file "$metadata" \
    --output "type=docker,dest=$archive,rewrite-timestamp=true" \
    "$temporary_directory/$name-context" >/dev/null
}

build_fixture portal "$portal_repository" "$portal_archive" "$portal_metadata"
build_fixture server "$server_repository" "$server_archive" "$server_metadata"
portal_digest=$(jq --raw-output '."containerimage.digest"' "$portal_metadata")
server_digest=$(jq --raw-output '."containerimage.digest"' "$server_metadata")
readonly portal_digest server_digest

export \
  version git_sha portal_repository server_repository portal_digest server_digest \
  portal_archive_size="$(file_size "$portal_archive")" \
  server_archive_size="$(file_size "$server_archive")" \
  portal_archive_sha="$(sha256_file "$portal_archive")" \
  server_archive_sha="$(sha256_file "$server_archive")" \
  portal_metadata_sha="$(sha256_file "$portal_metadata")" \
  server_metadata_sha="$(sha256_file "$server_metadata")"
node - "$manifest_file" "$bundle_file" <<'NODE'
const fs = require("node:fs");
const [manifestFile, bundleFile] = process.argv.slice(2);
const value = (name) => process.env[name];
const version = value("version");
const gitSha = value("git_sha");
const image = (name, repository, digest) => ({
  name,
  repository,
  digest,
  archive_file: `${name}-image.tar`,
  archive_size_bytes: Number(value(`${name}_archive_size`)),
  archive_sha256: value(`${name}_archive_sha`),
  build_metadata_file: `${name}-build-metadata.json`,
  build_metadata_sha256: value(`${name}_metadata_sha`),
});
fs.writeFileSync(manifestFile, `${JSON.stringify({
  manifest_version: 1,
  version,
  version_code: 999000000,
  git_sha: gitSha,
  portal_image: value("portal_repository"),
  portal_image_digest: value("portal_digest"),
  server_image: value("server_repository"),
  server_image_digest: value("server_digest"),
  staging_apk_file: `speakup-v${version}-staging-arm64.apk`,
  staging_apk_sha256: "d".repeat(64),
  production_apk_file: `speakup-v${version}-production-arm64.apk`,
  production_apk_size_bytes: 1,
  production_apk_sha256: "e".repeat(64),
  application_id: "com.xengineer.speakup",
  minimum_android_api: 24,
  abis: ["arm64-v8a"],
  apk_certificate_sha256: "f".repeat(64),
  database_schema_version: 1,
  quality_run_url: "https://github.com/1024XEngineer/XE3-ESL/actions/runs/1",
}, null, 2)}\n`);
fs.writeFileSync(bundleFile, `${JSON.stringify({
  bundle_version: 1,
  version,
  git_sha: gitSha,
  source_date_epoch: 1760000000,
  platform: "linux/amd64",
  images: [
    image("portal", value("portal_repository"), value("portal_digest")),
    image("server", value("server_repository"), value("server_digest")),
  ],
}, null, 2)}\n`);
NODE

export REAL_DOCKER="$real_docker" POSTGRES_REFERENCE="$postgres_reference"
cat > "$fake_bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" == 3 && "$1" == image && "$2" == inspect &&
      "$3" == "$POSTGRES_REFERENCE" ]]; then
  node - "$POSTGRES_REFERENCE" <<'NODE'
const reference = process.argv[2];
process.stdout.write(`${JSON.stringify([{
  RepoDigests: [reference],
  Os: "linux",
  Architecture: "amd64",
  Config: { Labels: {} },
}])}\n`);
NODE
  exit
fi
exec "$REAL_DOCKER" "$@"
EOF
chmod 0700 "$fake_bin/docker"

output="$(
  PATH="$fake_bin:$PATH" "$loader" \
    --manifest "$manifest_file" \
    --bundle "$bundle_file" \
    --directory "$bundle_directory"
)"
[[ "$output" == "offline_images_loaded=true version=$version git_sha=$git_sha platform=linux/amd64 source_date_epoch=1760000000" ]] ||
  fail 'real Docker loader output is invalid'

for reference in \
  "$portal_repository@$portal_digest" \
  "$server_repository@$server_digest"; do
  "$real_docker" image inspect "$reference" >/dev/null ||
    fail "real Docker did not preserve the exact RepoDigest: $reference"
done

printf '%s\n' 'Offline image real Docker round-trip passed'
