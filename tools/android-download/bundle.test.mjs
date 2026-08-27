import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import {
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  renameSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import {
  buildAndroidDownloadBundle,
  buildStagingCandidateBundle,
} from "./bundle.mjs";

const publishedAt = "2026-08-23T12:34:56Z";
const nonzeroDigest = (character) => `sha256:${character.repeat(64)}`;
const sha256 = (value) => createHash("sha256").update(value).digest("hex");

function fixture() {
  const root = mkdtempSync(path.join(os.tmpdir(), "android-download-bundle-"));
  const apkName = "speakup-v0.1.0-production-arm64.apk";
  const apk = path.join(root, apkName);
  const apkBytes = Buffer.from("signed-production-apk-fixture\n");
  writeFileSync(apk, apkBytes);
  const stagingApkName = "speakup-v0.1.0-staging-arm64.apk";
  const stagingApk = path.join(root, stagingApkName);
  const stagingApkBytes = Buffer.from("signed-staging-apk-fixture\n");
  writeFileSync(stagingApk, stagingApkBytes);
  const manifest = {
    manifest_version: 1,
    version: "0.1.0",
    version_code: 1,
    git_sha: "c".repeat(40),
    portal_image: "ghcr.io/1024xengineer/xe3-esl-portal",
    portal_image_digest: nonzeroDigest("a"),
    server_image: "ghcr.io/1024xengineer/xe3-esl-server",
    server_image_digest: nonzeroDigest("b"),
    staging_apk_file: stagingApkName,
    staging_apk_sha256: sha256(stagingApkBytes),
    production_apk_file: apkName,
    production_apk_size_bytes: apkBytes.length,
    production_apk_sha256: sha256(apkBytes),
    application_id: "com.xengineer.speakup",
    minimum_android_api: 24,
    abis: ["arm64-v8a"],
    apk_certificate_sha256: "e".repeat(64),
    database_schema_version: 7,
    quality_run_url:
      "https://github.com/1024XEngineer/XE3-ESL/actions/runs/123456789",
  };
  const manifestPath = path.join(root, "release-manifest.json");
  writeFileSync(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`);
  return {
    apk,
    apkBytes,
    manifest,
    manifestPath,
    root,
    stagingApk,
    stagingApkBytes,
  };
}

function build(input, outputName = "public-bundle") {
  return buildAndroidDownloadBundle({
    manifestPath: input.manifestPath,
    productionApkPath: input.apk,
    publishedAt,
    outputPath: path.join(input.root, outputName),
  });
}

function rewriteManifest(input, mutate) {
  const changed = structuredClone(input.manifest);
  mutate(changed);
  writeFileSync(input.manifestPath, `${JSON.stringify(changed, null, 2)}\n`);
}

test("builds a deterministic strict public Android download bundle", () => {
  const input = fixture();
  try {
    const first = build(input, "bundle-one");
    const second = build(input, "bundle-two");
    const expectedDirectory = path.join(
      input.root,
      "bundle-one",
      "downloads",
      "android",
      "v0.1.0",
    );
    assert.deepEqual(readdirSync(expectedDirectory).sort(), [
      "release.json",
      "speakup-v0.1.0-production-arm64.apk",
      "speakup-v0.1.0-production-arm64.apk.sha256",
    ]);

    const metadata = JSON.parse(
      readFileSync(path.join(expectedDirectory, "release.json"), "utf8"),
    );
    assert.deepEqual(metadata, {
      metadata_version: 1,
      version: "0.1.0",
      version_code: 1,
      published_at: publishedAt,
      file_name: "speakup-v0.1.0-production-arm64.apk",
      download_path:
        "/downloads/android/v0.1.0/speakup-v0.1.0-production-arm64.apk",
      size_bytes: input.apkBytes.length,
      minimum_android_api: 24,
      abis: ["arm64-v8a"],
      apk_sha256: input.manifest.production_apk_sha256,
      apk_certificate_sha256: input.manifest.apk_certificate_sha256,
    });
    assert.equal(
      readFileSync(
        path.join(
          expectedDirectory,
          "speakup-v0.1.0-production-arm64.apk.sha256",
        ),
        "utf8",
      ),
      `${input.manifest.production_apk_sha256}  ${input.manifest.production_apk_file}\n`,
    );

    const bundleManifest = JSON.parse(
      readFileSync(path.join(input.root, "bundle-one", "bundle-manifest.json")),
    );
    assert.deepEqual(
      bundleManifest.files.map((entry) => entry.path),
      [
        "downloads/android/v0.1.0/speakup-v0.1.0-production-arm64.apk",
        "downloads/android/v0.1.0/speakup-v0.1.0-production-arm64.apk.sha256",
        "downloads/android/v0.1.0/release.json",
      ],
    );
    assert.equal(first.bundleManifestSha256, second.bundleManifestSha256);
    assert.equal(
      readFileSync(path.join(input.root, "bundle-one", "bundle-manifest.json"), "utf8"),
      readFileSync(path.join(input.root, "bundle-two", "bundle-manifest.json"), "utf8"),
    );
  } finally {
    rmSync(input.root, { recursive: true, force: true });
  }
});

test("builds a deterministic Staging candidate bundle bound to one Candidate run", () => {
  const input = fixture();
  try {
    const result = buildStagingCandidateBundle({
      manifestPath: input.manifestPath,
      stagingApkPath: input.stagingApk,
      candidateRunId: 123456789,
      outputPath: path.join(input.root, "staging-bundle"),
    });
    const candidateDirectory = path.join(
      input.root,
      "staging-bundle",
      "downloads",
      "android",
      "candidates",
      "123456789",
    );
    assert.deepEqual(readdirSync(candidateDirectory).sort(), [
      "candidate.json",
      "speakup-v0.1.0-staging-arm64.apk",
      "speakup-v0.1.0-staging-arm64.apk.sha256",
    ]);
    const metadata = JSON.parse(
      readFileSync(path.join(candidateDirectory, "candidate.json"), "utf8"),
    );
    assert.deepEqual(metadata, {
      candidate_metadata_version: 1,
      environment: "staging",
      version: "0.1.0",
      version_code: 1,
      git_sha: "c".repeat(40),
      candidate_run_id: 123456789,
      manifest_sha256: sha256(readFileSync(input.manifestPath)),
      file_name: "speakup-v0.1.0-staging-arm64.apk",
      download_path:
        "/downloads/android/candidates/123456789/speakup-v0.1.0-staging-arm64.apk",
      size_bytes: input.stagingApkBytes.length,
      minimum_android_api: 24,
      abis: ["arm64-v8a"],
      apk_sha256: input.manifest.staging_apk_sha256,
      apk_certificate_sha256: input.manifest.apk_certificate_sha256,
    });
    const bundleManifest = JSON.parse(
      readFileSync(
        path.join(input.root, "staging-bundle", "bundle-manifest.json"),
        "utf8",
      ),
    );
    assert.equal(bundleManifest.environment, "staging");
    assert.equal(bundleManifest.candidate_run_id, 123456789);
    assert.equal(bundleManifest.git_sha, input.manifest.git_sha);
    assert.deepEqual(
      bundleManifest.files.map((entry) => entry.path),
      [
        "downloads/android/candidates/123456789/speakup-v0.1.0-staging-arm64.apk",
        "downloads/android/candidates/123456789/speakup-v0.1.0-staging-arm64.apk.sha256",
        "downloads/android/candidates/123456789/candidate.json",
      ],
    );
    assert.equal(result.candidateRunId, 123456789);
  } finally {
    rmSync(input.root, { recursive: true, force: true });
  }
});

test("rejects a Staging candidate bundle for a different Candidate run", () => {
  const input = fixture();
  try {
    assert.throws(
      () =>
        buildStagingCandidateBundle({
          manifestPath: input.manifestPath,
          stagingApkPath: input.stagingApk,
          candidateRunId: 987654321,
          outputPath: path.join(input.root, "wrong-run-bundle"),
        }),
      /candidate run id does not match the release manifest/,
    );
  } finally {
    rmSync(input.root, { recursive: true, force: true });
  }
});

test("binds parsed release metadata and provenance hash to one manifest snapshot", () => {
  const input = fixture();
  const originalParse = JSON.parse;
  try {
    const originalManifestBytes = readFileSync(input.manifestPath);
    const replacement = structuredClone(input.manifest);
    replacement.git_sha = "f".repeat(40);
    const replacementBytes = Buffer.from(`${JSON.stringify(replacement, null, 2)}\n`);
    const replacementPath = path.join(input.root, "replacement-manifest.json");
    writeFileSync(replacementPath, replacementBytes);

    let replaced = false;
    JSON.parse = function parseAndReplacePath(value, ...arguments_) {
      const parsed = originalParse(value, ...arguments_);
      if (!replaced) {
        renameSync(replacementPath, input.manifestPath);
        replaced = true;
      }
      return parsed;
    };

    build(input);
    JSON.parse = originalParse;
    assert.equal(replaced, true);

    const bundleManifest = JSON.parse(
      readFileSync(
        path.join(input.root, "public-bundle", "bundle-manifest.json"),
        "utf8",
      ),
    );
    assert.equal(bundleManifest.release_manifest_sha256, sha256(originalManifestBytes));
    assert.notEqual(
      bundleManifest.release_manifest_sha256,
      sha256(replacementBytes),
    );
  } finally {
    JSON.parse = originalParse;
    rmSync(input.root, { recursive: true, force: true });
  }
});

test("rejects symlinked release manifest and APK inputs", () => {
  const input = fixture();
  try {
    const links = path.join(input.root, "links");
    mkdirSync(links);
    const manifestLink = path.join(links, "release-manifest.json");
    const apkLink = path.join(
      links,
      "speakup-v0.1.0-production-arm64.apk",
    );
    symlinkSync(input.manifestPath, manifestLink);
    symlinkSync(input.apk, apkLink);

    assert.throws(
      () =>
        buildAndroidDownloadBundle({
          manifestPath: manifestLink,
          productionApkPath: input.apk,
          publishedAt,
          outputPath: path.join(input.root, "manifest-link-bundle"),
        }),
      /release manifest cannot be a symlink/,
    );
    assert.throws(
      () =>
        buildAndroidDownloadBundle({
          manifestPath: input.manifestPath,
          productionApkPath: apkLink,
          publishedAt,
          outputPath: path.join(input.root, "apk-link-bundle"),
        }),
      /Production APK cannot be a symlink/,
    );
  } finally {
    rmSync(input.root, { recursive: true, force: true });
  }
});

test("never overwrites an existing output file, directory, or symlink", () => {
  for (const kind of ["file", "directory", "symlink"]) {
    const input = fixture();
    try {
      const output = path.join(input.root, "public-bundle");
      if (kind === "file") writeFileSync(output, "keep\n");
      if (kind === "directory") mkdirSync(output);
      if (kind === "symlink") symlinkSync(input.apk, output);
      assert.throws(() => build(input), /output already exists/);
      assert.ok(lstatSync(output));
    } finally {
      rmSync(input.root, { recursive: true, force: true });
    }
  }
});

test("rejects malformed manifests and mismatched APK evidence", () => {
  const cases = [
    ["extra field", (value) => (value.unknown = true), /invalid schema/],
    ["package", (value) => (value.application_id = "com.example.fake"), /Android contract/],
    ["ABI", (value) => (value.abis = ["x86_64"]), /Android contract/],
    ["certificate", (value) => (value.apk_certificate_sha256 = "0".repeat(64)), /placeholder/],
    ["size", (value) => (value.production_apk_size_bytes += 1), /size does not match/],
    ["hash", (value) => (value.production_apk_sha256 = "f".repeat(64)), /SHA-256/],
    ["name", (value) => (value.production_apk_file = "renamed.apk"), /APK name/],
  ];
  for (const [name, mutate, expected] of cases) {
    const input = fixture();
    try {
      rewriteManifest(input, mutate);
      assert.throws(() => build(input), expected, name);
    } finally {
      rmSync(input.root, { recursive: true, force: true });
    }
  }
});

test("rejects non-canonical or impossible publication timestamps", () => {
  const input = fixture();
  try {
    for (const value of [
      "2026-08-23T12:34:56+00:00",
      "2026-08-23T12:34:56.000Z",
      "2026-02-30T12:34:56Z",
    ]) {
      assert.throws(
        () =>
          buildAndroidDownloadBundle({
            manifestPath: input.manifestPath,
            productionApkPath: input.apk,
            publishedAt: value,
            outputPath: path.join(input.root, `bundle-${value.length}`),
          }),
        /published time/,
      );
    }
  } finally {
    rmSync(input.root, { recursive: true, force: true });
  }
});
