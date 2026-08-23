#!/usr/bin/env bash

set -euo pipefail
umask 077

readonly production_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly loader="$production_directory/load-offline-images.sh"
readonly version='0.1.1'
readonly git_sha='cccccccccccccccccccccccccccccccccccccccc'
readonly portal_repository='ghcr.io/1024xengineer/xe3-esl-portal'
readonly server_repository='ghcr.io/1024xengineer/xe3-esl-server'
readonly postgres_reference='postgres@sha256:7d2695c3aa88e792e8b3b233e7e4adb296a20412c6c0ca361e3edaaacfada108'

fail() {
  printf 'offline image contract test: %s\n' "$*" >&2
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
  local file=$1
  stat -c '%s' -- "$file" 2>/dev/null || stat -f '%z' "$file"
}

expect_failure() {
  local name=$1
  shift
  : > "$command_log"
  if "$@" > "$failure_output" 2>&1; then
    fail "$name unexpectedly succeeded"
  fi
}

expect_preflight_failure() {
  local name=$1
  shift
  expect_failure "$name" "$@"
  [[ ! -s "$command_log" ]] || fail "$name reached Docker before failing"
}

mutate_json() {
  local source=$1
  local destination=$2
  local operation=$3

  node - "$source" "$destination" "$operation" <<'NODE'
const fs = require("node:fs");

const [source, destination, operation] = process.argv.slice(2);
const value = JSON.parse(fs.readFileSync(source, "utf8"));
switch (operation) {
  case "traversal": value.images[0].archive_file = "../portal-image.tar"; break;
  case "symlink": value.images[0].archive_file = "portal-image-link.tar"; break;
  case "archive-sha": value.images[0].archive_sha256 = "f".repeat(64); break;
  case "server-archive-sha": value.images[1].archive_sha256 = "f".repeat(64); break;
  case "metadata-sha": value.images[0].build_metadata_sha256 = "e".repeat(64); break;
  case "size": value.images[0].archive_size_bytes += 1; break;
  case "version": value.version = "0.1.2"; break;
  case "git-sha": value.git_sha = "d".repeat(40); break;
  case "repository": value.images[0].repository = "ghcr.io/example/wrong"; break;
  case "digest": value.images[0].digest = `sha256:${"9".repeat(64)}`; break;
  case "platform": value.platform = "linux/arm64"; break;
  case "reverse-images": value.images.reverse(); break;
  case "extra-bundle-field": value.unexpected = true; break;
  case "extra-image-field": value.images[0].unexpected = true; break;
  case "manifest-version": value.manifest_version = 2; break;
  case "manifest-git-sha": value.git_sha = "d".repeat(40); break;
  case "manifest-repository": value.portal_image = "ghcr.io/example/wrong"; break;
  default: throw new Error(`unsupported mutation: ${operation}`);
}
fs.writeFileSync(destination, `${JSON.stringify(value, null, 2)}\n`);
NODE
}

create_image_archive() {
  local name=$1
  local repository=$2
  local archive=$3
  local variant=${4:-valid}
  local root="$temporary_directory/archive-root-$name-$archive_number"
  local digest_file="$temporary_directory/archive-digest-$archive_number"
  local extra_member=''

  archive_number=$((archive_number + 1))
  node - \
    "$root" \
    "$repository" \
    "$version" \
    "$git_sha" \
    "$variant" \
    "$digest_file" <<'NODE'
const { createHash } = require("node:crypto");
const { mkdirSync, symlinkSync, writeFileSync } = require("node:fs");
const path = require("node:path");

const [root, repository, version, gitSha, variant, digestFile] =
  process.argv.slice(2);
const json = (value) => Buffer.from(`${JSON.stringify(value)}\n`);
const sha256 = (value) => createHash("sha256").update(value).digest("hex");
const blobs = path.join(root, "blobs", "sha256");
mkdirSync(blobs, { recursive: true });
const labels = {
  "org.opencontainers.image.source": variant === "bad-source"
    ? "https://github.com/example/wrong"
    : "https://github.com/1024XEngineer/XE3-ESL",
  "org.opencontainers.image.revision": variant === "bad-revision"
    ? "d".repeat(40)
    : gitSha,
  "org.opencontainers.image.version": variant === "bad-version"
    ? "0.1.2"
    : version,
};
const config = json({
  architecture: variant === "bad-architecture" ? "arm64" : "amd64",
  os: "linux",
  config: { Labels: labels },
  rootfs: { type: "layers", diff_ids: [] },
  history: [],
});
const configDigest = sha256(config);
writeFileSync(path.join(blobs, configDigest), config);
const manifest = json({
  schemaVersion: 2,
  mediaType: "application/vnd.docker.distribution.manifest.v2+json",
  config: {
    mediaType: "application/vnd.docker.container.image.v1+json",
    digest: `sha256:${configDigest}`,
    size: config.length,
  },
  layers: [],
});
const manifestDigest = sha256(manifest);
writeFileSync(path.join(blobs, manifestDigest), manifest);
const descriptor = {
  mediaType: "application/vnd.docker.distribution.manifest.v2+json",
  digest: `sha256:${manifestDigest}`,
  size: manifest.length,
  platform: { architecture: "amd64", os: "linux" },
};
const descriptors = [descriptor];
if (variant === "extra-image") descriptors.push({ ...descriptor });
if (variant === "descriptor-digest") {
  descriptors[0] = { ...descriptor, digest: `sha256:${"f".repeat(64)}` };
}
writeFileSync(path.join(root, "index.json"), json({
  schemaVersion: 2,
  mediaType: "application/vnd.oci.image.index.v1+json",
  manifests: descriptors,
}));
const tags = [`${repository}:${version}`];
if (variant === "extra-tag") tags.push(`${repository}:unexpected`);
writeFileSync(path.join(root, "manifest.json"), json([{
  Config: `blobs/sha256/${configDigest}`,
  RepoTags: tags,
  Layers: [],
}]));
writeFileSync(path.join(root, "oci-layout"), json({ imageLayoutVersion: "1.0.0" }));
if (variant === "extra-path") writeFileSync(path.join(root, "unexpected"), "x\n");
if (variant === "symlink-member") {
  symlinkSync("index.json", path.join(root, "unexpected-link"));
}
writeFileSync(digestFile, `sha256:${manifestDigest}\n`);
NODE
  case "$variant" in
    extra-path) extra_member=unexpected ;;
    symlink-member) extra_member=unexpected-link ;;
  esac
  if [[ -n "$extra_member" ]]; then
    tar -cf "$archive" -C "$root" index.json manifest.json oci-layout blobs "$extra_member"
  else
    tar -cf "$archive" -C "$root" index.json manifest.json oci-layout blobs
  fi
  tr -d '[:space:]' < "$digest_file"
}

write_contract() {
  local manifest=$1
  local bundle=$2

  export \
    version git_sha portal_repository server_repository portal_digest server_digest \
    portal_archive_size server_archive_size portal_archive_sha server_archive_sha \
    portal_metadata_sha server_metadata_sha
  node - "$manifest" "$bundle" <<'NODE'
const fs = require("node:fs");
const [manifestFile, bundleFile] = process.argv.slice(2);
const value = (name) => process.env[name];
const version = value("version");
const gitSha = value("git_sha");
const portalDigest = value("portal_digest");
const serverDigest = value("server_digest");
fs.writeFileSync(manifestFile, `${JSON.stringify({
  manifest_version: 1,
  version,
  version_code: 2,
  git_sha: gitSha,
  portal_image: value("portal_repository"),
  portal_image_digest: portalDigest,
  server_image: value("server_repository"),
  server_image_digest: serverDigest,
  staging_apk_file: `speakup-v${version}-staging-arm64.apk`,
  staging_apk_sha256: "d".repeat(64),
  production_apk_file: `speakup-v${version}-production-arm64.apk`,
  production_apk_size_bytes: 123456,
  production_apk_sha256: "e".repeat(64),
  application_id: "com.xengineer.speakup",
  minimum_android_api: 24,
  abis: ["arm64-v8a"],
  apk_certificate_sha256: "f".repeat(64),
  database_schema_version: 7,
  quality_run_url: "https://github.com/1024XEngineer/XE3-ESL/actions/runs/123456",
}, null, 2)}\n`);
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
fs.writeFileSync(bundleFile, `${JSON.stringify({
  bundle_version: 1,
  version,
  git_sha: gitSha,
  source_date_epoch: 1760000000,
  platform: "linux/amd64",
  images: [
    image("portal", value("portal_repository"), portalDigest),
    image("server", value("server_repository"), serverDigest),
  ],
}, null, 2)}\n`);
NODE
}

write_portal_variant_contract() {
  local archive=$1
  local digest=$2
  local label=$3
  local manifest="$temporary_directory/$label-release-manifest.json"
  local bundle="$temporary_directory/$label-bundle.json"
  local metadata="$bundle_directory/$label-build-metadata.json"

  printf '{"containerimage.digest":"%s"}\n' "$digest" > "$metadata"
  node - \
    "$manifest_file" "$bundle_file" "$manifest" "$bundle" \
    "${archive##*/}" "$(file_size "$archive")" "$(sha256_file "$archive")" \
    "${metadata##*/}" "$(sha256_file "$metadata")" "$digest" <<'NODE'
const fs = require("node:fs");
const [
  sourceManifest, sourceBundle, destinationManifest, destinationBundle,
  archiveFile, archiveSize, archiveSha, metadataFile, metadataSha, digest,
] = process.argv.slice(2);
const manifest = JSON.parse(fs.readFileSync(sourceManifest, "utf8"));
const bundle = JSON.parse(fs.readFileSync(sourceBundle, "utf8"));
manifest.portal_image_digest = digest;
Object.assign(bundle.images[0], {
  digest,
  archive_file: archiveFile,
  archive_size_bytes: Number(archiveSize),
  archive_sha256: archiveSha,
  build_metadata_file: metadataFile,
  build_metadata_sha256: metadataSha,
});
fs.writeFileSync(destinationManifest, `${JSON.stringify(manifest, null, 2)}\n`);
fs.writeFileSync(destinationBundle, `${JSON.stringify(bundle, null, 2)}\n`);
NODE
  printf '%s\t%s\n' "$manifest" "$bundle"
}

temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/xe3-offline-image-test.XXXXXX")
readonly temporary_directory
trap 'rm -rf -- "$temporary_directory"' EXIT INT TERM
readonly bundle_directory="$temporary_directory/bundle"
readonly fake_bin="$temporary_directory/fake-bin"
readonly command_log="$temporary_directory/docker.log"
readonly failure_output="$temporary_directory/failure.out"
readonly manifest_file="$temporary_directory/release-manifest.json"
readonly bundle_file="$temporary_directory/offline-image-bundle.json"
readonly portal_archive="$bundle_directory/portal-image.tar"
readonly server_archive="$bundle_directory/server-image.tar"
readonly portal_metadata="$bundle_directory/portal-build-metadata.json"
readonly server_metadata="$bundle_directory/server-build-metadata.json"

mkdir -p "$bundle_directory" "$fake_bin"
archive_number=0
portal_digest=$(create_image_archive portal "$portal_repository" "$portal_archive")
server_digest=$(create_image_archive server "$server_repository" "$server_archive")
printf '{"containerimage.digest":"%s"}\n' "$portal_digest" > "$portal_metadata"
printf '{"containerimage.digest":"%s"}\n' "$server_digest" > "$server_metadata"
portal_archive_size=$(file_size "$portal_archive")
server_archive_size=$(file_size "$server_archive")
portal_archive_sha=$(sha256_file "$portal_archive")
server_archive_sha=$(sha256_file "$server_archive")
portal_metadata_sha=$(sha256_file "$portal_metadata")
server_metadata_sha=$(sha256_file "$server_metadata")
write_contract "$manifest_file" "$bundle_file"

export \
  PORTAL_REFERENCE="$portal_repository@$portal_digest" \
  SERVER_REFERENCE="$server_repository@$server_digest" \
  POSTGRES_REFERENCE="$postgres_reference"
cat > "$fake_bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" == 6 && "$1" == image && "$2" == load && "$3" == --platform &&
      "$4" == linux/amd64 && "$5" == -i ]]; then
  printf 'load:%s\n' "$6" >> "$COMMAND_LOG"
  [[ "${TEST_LOAD_FAILURE:-0}" != 1 ]]
  exit
fi

if [[ "$#" == 3 && "$1" == image && "$2" == inspect ]]; then
  reference=$3
  printf 'inspect:%s\n' "$reference" >> "$COMMAND_LOG"
  if [[ "$reference" == "$POSTGRES_REFERENCE" ]]; then
    [[ "${TEST_POSTGRES_IMAGE:-valid}" != missing ]] || exit 1
    repo_digest=$reference
    architecture=amd64
    case "${TEST_POSTGRES_IMAGE:-valid}" in
      valid) ;;
      wrong-digest) repo_digest="postgres@sha256:$(printf '9%.0s' {1..64})" ;;
      wrong-architecture) architecture=arm64 ;;
      *) exit 2 ;;
    esac
    node - "$repo_digest" "$architecture" <<'NODE'
const [repoDigest, architecture] = process.argv.slice(2);
process.stdout.write(`${JSON.stringify([{
  RepoDigests: [repoDigest], Os: "linux", Architecture: architecture,
  Config: { Labels: {} },
}])}\n`);
NODE
    exit
  fi

  case "$reference" in
    "$PORTAL_REFERENCE" | "$SERVER_REFERENCE") ;;
    *) exit 1 ;;
  esac
  repo_digest=$reference
  case "${TEST_REPO_DIGEST:-valid}" in
    valid) ;;
    missing) repo_digest='' ;;
    wrong) repo_digest="${reference%@*}@sha256:$(printf '9%.0s' {1..64})" ;;
    *) exit 2 ;;
  esac
  source_label='https://github.com/1024XEngineer/XE3-ESL'
  revision_label='cccccccccccccccccccccccccccccccccccccccc'
  version_label='0.1.1'
  case "${TEST_BAD_LABEL:-}" in
    '') ;;
    source) source_label='https://github.com/example/wrong' ;;
    revision) revision_label='dddddddddddddddddddddddddddddddddddddddd' ;;
    version) version_label='0.1.2' ;;
    *) exit 2 ;;
  esac
  node - \
    "$repo_digest" "${TEST_IMAGE_OS:-linux}" \
    "${TEST_IMAGE_ARCHITECTURE:-amd64}" "$source_label" \
    "$revision_label" "$version_label" <<'NODE'
const [repoDigest, os, architecture, source, revision, version] =
  process.argv.slice(2);
process.stdout.write(`${JSON.stringify([{
  RepoDigests: repoDigest ? [repoDigest] : [], Os: os, Architecture: architecture,
  Config: { Labels: {
    "org.opencontainers.image.source": source,
    "org.opencontainers.image.revision": revision,
    "org.opencontainers.image.version": version,
  } },
}])}\n`);
NODE
  exit
fi

exit 2
EOF
chmod 0700 "$fake_bin/docker"

export COMMAND_LOG="$command_log"
export PATH="$fake_bin:$PATH"

bash -n "$loader" "$0"

: > "$command_log"
success_output="$(
  "$loader" --manifest "$manifest_file" --bundle "$bundle_file" \
    --directory "$bundle_directory"
)"
[[ "$success_output" == "offline_images_loaded=true version=$version git_sha=$git_sha platform=linux/amd64 source_date_epoch=1760000000" ]] ||
  fail 'valid offline image bundle did not return the canonical success contract'
expected_log="$(
  printf '%s\n' \
    "inspect:$postgres_reference" \
    "load:$portal_archive" \
    "inspect:$portal_repository@$portal_digest" \
    "load:$server_archive" \
    "inspect:$server_repository@$server_digest"
)"
[[ "$(< "$command_log")" == "$expected_log" ]] ||
  fail 'valid bundle did not preflight Postgres and load exact application digests'

for mutation in \
  traversal archive-sha server-archive-sha metadata-sha size version git-sha \
  repository digest platform reverse-images extra-bundle-field extra-image-field; do
  invalid_bundle="$temporary_directory/$mutation-bundle.json"
  mutate_json "$bundle_file" "$invalid_bundle" "$mutation"
  expect_preflight_failure "$mutation bundle" \
    "$loader" --manifest "$manifest_file" --bundle "$invalid_bundle" \
      --directory "$bundle_directory"
done

ln -s "$portal_archive" "$bundle_directory/portal-image-link.tar"
symlink_bundle="$temporary_directory/symlink-bundle.json"
mutate_json "$bundle_file" "$symlink_bundle" symlink
expect_preflight_failure 'symbolic-link archive' \
  "$loader" --manifest "$manifest_file" --bundle "$symlink_bundle" \
    --directory "$bundle_directory"

for mutation in manifest-version manifest-git-sha manifest-repository; do
  invalid_manifest="$temporary_directory/$mutation-release-manifest.json"
  mutate_json "$manifest_file" "$invalid_manifest" "$mutation"
  expect_preflight_failure "$mutation" \
    "$loader" --manifest "$invalid_manifest" --bundle "$bundle_file" \
      --directory "$bundle_directory"
done

empty_manifest="$temporary_directory/empty-release-manifest.json"
empty_bundle="$temporary_directory/empty-bundle.json"
: > "$empty_manifest"
: > "$empty_bundle"
expect_preflight_failure 'empty release manifest' \
  "$loader" --manifest "$empty_manifest" --bundle "$bundle_file" \
    --directory "$bundle_directory"
expect_preflight_failure 'empty bundle' \
  "$loader" --manifest "$manifest_file" --bundle "$empty_bundle" \
    --directory "$bundle_directory"

cp "$server_archive" "$temporary_directory/server-image.original"
: > "$server_archive"
expect_preflight_failure 'empty Server archive' \
  "$loader" --manifest "$manifest_file" --bundle "$bundle_file" \
    --directory "$bundle_directory"
mv "$temporary_directory/server-image.original" "$server_archive"

cp "$server_metadata" "$temporary_directory/server-metadata.original"
: > "$server_metadata"
expect_preflight_failure 'empty Server metadata' \
  "$loader" --manifest "$manifest_file" --bundle "$bundle_file" \
    --directory "$bundle_directory"
mv "$temporary_directory/server-metadata.original" "$server_metadata"

wrong_metadata="$bundle_directory/wrong-digest-build-metadata.json"
printf '{"containerimage.digest":"sha256:%s"}\n' "$(printf '9%.0s' {1..64})" > "$wrong_metadata"
wrong_metadata_bundle="$temporary_directory/wrong-metadata-bundle.json"
node - "$bundle_file" "$wrong_metadata_bundle" "${wrong_metadata##*/}" \
  "$(sha256_file "$wrong_metadata")" <<'NODE'
const fs = require("node:fs");
const [source, destination, file, sha] = process.argv.slice(2);
const value = JSON.parse(fs.readFileSync(source, "utf8"));
value.images[0].build_metadata_file = file;
value.images[0].build_metadata_sha256 = sha;
fs.writeFileSync(destination, `${JSON.stringify(value, null, 2)}\n`);
NODE
expect_preflight_failure 'build metadata content digest mismatch' \
  "$loader" --manifest "$manifest_file" --bundle "$wrong_metadata_bundle" \
    --directory "$bundle_directory"

for variant in \
  descriptor-digest extra-image extra-tag extra-path symlink-member \
  bad-architecture bad-source bad-revision bad-version; do
  variant_archive="$bundle_directory/$variant-portal-image.tar"
  variant_digest=$(create_image_archive \
    "$variant-portal" "$portal_repository" "$variant_archive" "$variant")
  IFS=$'\t' read -r variant_manifest variant_bundle < <(
    write_portal_variant_contract "$variant_archive" "$variant_digest" "$variant"
  )
  expect_preflight_failure "$variant image archive" \
    "$loader" --manifest "$variant_manifest" --bundle "$variant_bundle" \
      --directory "$bundle_directory"
done

for postgres_failure in missing wrong-digest wrong-architecture; do
  expect_failure "Postgres $postgres_failure" \
    env TEST_POSTGRES_IMAGE="$postgres_failure" \
    "$loader" --manifest "$manifest_file" --bundle "$bundle_file" \
      --directory "$bundle_directory"
  [[ "$(wc -l < "$command_log" | tr -d '[:space:]')" == 1 &&
    "$(< "$command_log")" == "inspect:$postgres_reference" ]] ||
    fail "Postgres $postgres_failure reached an application image load"
done

expect_failure 'Portal load failure' \
  env TEST_LOAD_FAILURE=1 \
  "$loader" --manifest "$manifest_file" --bundle "$bundle_file" \
    --directory "$bundle_directory"
[[ "$(wc -l < "$command_log" | tr -d '[:space:]')" == 2 ]] ||
  fail 'Portal load failure did not stop before image inspection'

for runtime_failure in \
  'TEST_IMAGE_ARCHITECTURE=arm64' 'TEST_IMAGE_OS=windows' \
  'TEST_BAD_LABEL=source' 'TEST_BAD_LABEL=revision' 'TEST_BAD_LABEL=version' \
  'TEST_REPO_DIGEST=wrong' 'TEST_REPO_DIGEST=missing'; do
  name=${runtime_failure%%=*}
  value=${runtime_failure#*=}
  expect_failure "$runtime_failure" \
    env "$name=$value" \
    "$loader" --manifest "$manifest_file" --bundle "$bundle_file" \
      --directory "$bundle_directory"
  [[ "$(wc -l < "$command_log" | tr -d '[:space:]')" == 3 ]] ||
    fail "$runtime_failure did not stop after the first exact image inspection"
  grep -Fxq "inspect:$postgres_reference" "$command_log" ||
    fail "$runtime_failure skipped the Postgres preflight"
  grep -Fxq "load:$portal_archive" "$command_log" ||
    fail "$runtime_failure did not load the Portal archive"
  grep -Fxq "inspect:$portal_repository@$portal_digest" "$command_log" ||
    fail "$runtime_failure did not inspect the Portal digest"
done

printf '%s\n' 'Offline image loading contract tests passed'
