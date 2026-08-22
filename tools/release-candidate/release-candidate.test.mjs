import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { existsSync, mkdtempSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createReleaseManifest } from "./manifest.mjs";
import {
  collectReleaseMetadata,
  databaseSchemaVersion,
  parseStableTag,
} from "./metadata.mjs";

const manifestScript = fileURLToPath(new URL("./manifest.mjs", import.meta.url));

function git(repo, ...arguments_) {
  return execFileSync("git", arguments_, {
    cwd: repo,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
}

function writeReleaseFiles(repo, version) {
  mkdirSync(path.join(repo, "mobile"), { recursive: true });
  mkdirSync(path.join(repo, "server", "migrations"), { recursive: true });
  writeFileSync(path.join(repo, "mobile", "pubspec.yaml"), `name: speakup\nversion: ${version}\n`);
  writeFileSync(path.join(repo, "server", "migrations", "000001_baseline.up.sql"), "SELECT 1;\n");
  writeFileSync(path.join(repo, "server", "migrations", "000007_current.up.sql"), "SELECT 1;\n");
}

function createRepository(version = "0.1.0+1") {
  const repo = mkdtempSync(path.join(tmpdir(), "speakup-release-"));
  git(repo, "init", "--initial-branch=main");
  git(repo, "config", "user.name", "Release Test");
  git(repo, "config", "user.email", "release-test@example.invalid");
  writeReleaseFiles(repo, version);
  git(repo, "add", ".");
  git(repo, "commit", "-m", "initial release");
  git(repo, "remote", "add", "origin", repo);
  return repo;
}

function validManifestFixture() {
  const artifacts = mkdtempSync(path.join(tmpdir(), "speakup-artifacts-"));
  const stagingApk = path.join(artifacts, "speakup-v0.1.0-staging-arm64.apk");
  const productionApk = path.join(artifacts, "speakup-v0.1.0-production-arm64.apk");
  writeFileSync(stagingApk, "staging apk");
  writeFileSync(productionApk, "production apk");
  return {
    input: {
      PORTAL_IMAGE: "ghcr.io/1024xengineer/xe3-esl-portal",
      PORTAL_IMAGE_DIGEST: `sha256:${"a".repeat(64)}`,
      SERVER_IMAGE: "ghcr.io/1024xengineer/xe3-esl-server",
      SERVER_IMAGE_DIGEST: `sha256:${"b".repeat(64)}`,
      STAGING_APK_PATH: stagingApk,
      PRODUCTION_APK_PATH: productionApk,
      STAGING_APK_APPLICATION_ID: "com.xengineer.speakup",
      STAGING_APK_MINIMUM_ANDROID_API: "24",
      STAGING_APK_ABIS: "arm64-v8a",
      PRODUCTION_APK_APPLICATION_ID: "com.xengineer.speakup",
      PRODUCTION_APK_MINIMUM_ANDROID_API: "24",
      PRODUCTION_APK_ABIS: "arm64-v8a",
      APK_CERTIFICATE_SHA256: "c".repeat(64),
      GITHUB_REPOSITORY: "1024XEngineer/XE3-ESL",
      QUALITY_RUN_URL: "https://github.com/1024XEngineer/XE3-ESL/actions/runs/1",
    },
    metadata: {
      version: "0.1.0",
      version_code: "1",
      git_sha: "d".repeat(40),
      database_schema_version: "7",
    },
    stagingApk,
  };
}

test("accepts the first stable release and ignores prerelease tags", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0-alpha.1");
  git(repo, "tag", "v0.1.0");

  assert.deepEqual(
    collectReleaseMetadata({ repoDir: repo, tag: "v0.1.0", mainRef: "main" }),
    {
      tag: "v0.1.0",
      version: "0.1.0",
      version_code: "1",
      git_sha: git(repo, "rev-parse", "HEAD"),
      database_schema_version: "7",
    },
  );
});

test("rejects a release tag that is not contained in main", () => {
  const repo = createRepository();
  git(repo, "checkout", "-b", "feature");
  writeReleaseFiles(repo, "0.1.1+2");
  git(repo, "add", ".");
  git(repo, "commit", "-m", "feature release");
  git(repo, "tag", "v0.1.1");

  assert.throws(
    () => collectReleaseMetadata({ repoDir: repo, tag: "v0.1.1", mainRef: "main" }),
    /not contained in main/,
  );
});

test("rejects a tag checkout that does not match HEAD", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  writeReleaseFiles(repo, "0.1.1+2");
  git(repo, "add", ".");
  git(repo, "commit", "-m", "unreleased change");

  assert.throws(
    () => collectReleaseMetadata({ repoDir: repo, tag: "v0.1.0", mainRef: "main" }),
    /does not match release tag/,
  );
});

test("rejects non-stable tags and tag/versionName mismatches", () => {
  assert.throws(() => parseStableTag("v0.1.0-rc.1"), /must use vX.Y.Z/);

  const repo = createRepository();
  git(repo, "tag", "v0.2.0");
  assert.throws(
    () => collectReleaseMetadata({ repoDir: repo, tag: "v0.2.0", mainRef: "main" }),
    /does not match versionName/,
  );
});

test("rejects a versionCode that does not increase", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  writeReleaseFiles(repo, "0.1.1+1");
  git(repo, "add", ".");
  git(repo, "commit", "-m", "next release");
  git(repo, "tag", "v0.1.1");

  assert.throws(
    () => collectReleaseMetadata({ repoDir: repo, tag: "v0.1.1", mainRef: "main" }),
    /versionCode 1 must be greater than 1/,
  );
});

test("accepts an increased versionCode and permits rebuilding an older stable tag", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  writeReleaseFiles(repo, "0.1.1+2");
  git(repo, "add", ".");
  git(repo, "commit", "-m", "next release");
  git(repo, "tag", "v0.1.1");

  assert.equal(
    collectReleaseMetadata({ repoDir: repo, tag: "v0.1.1", mainRef: "main" })
      .version_code,
    "2",
  );

  git(repo, "checkout", "--detach", "v0.1.0");
  assert.equal(
    collectReleaseMetadata({ repoDir: repo, tag: "v0.1.0", mainRef: "main" }).version,
    "0.1.0",
  );
});

test("rejects shallow repositories", () => {
  const source = createRepository();
  git(source, "tag", "v0.1.0");
  const cloneParent = mkdtempSync(path.join(tmpdir(), "speakup-shallow-parent-"));
  const shallow = path.join(cloneParent, "repo");
  execFileSync(
    "git",
    ["clone", "--depth", "1", "--branch", "v0.1.0", `file://${source}`, shallow],
    { stdio: "ignore" },
  );

  assert.throws(
    () => collectReleaseMetadata({ repoDir: shallow, tag: "v0.1.0", mainRef: "main" }),
    /complete Git history/,
  );
});

test("rejects an incomplete local stable Tag set", () => {
  const source = createRepository("0.1.0+100");
  git(source, "tag", "v0.1.0");
  writeReleaseFiles(source, "0.2.0+1");
  git(source, "add", ".");
  git(source, "commit", "-m", "invalid lower version code");
  git(source, "tag", "v0.2.0");

  const cloneParent = mkdtempSync(path.join(tmpdir(), "speakup-no-tags-parent-"));
  const clone = path.join(cloneParent, "repo");
  execFileSync("git", ["clone", "--no-tags", source, clone], { stdio: "ignore" });
  git(clone, "fetch", "origin", "refs/tags/v0.2.0:refs/tags/v0.2.0");
  git(clone, "checkout", "--detach", "v0.2.0");

  assert.throws(
    () => collectReleaseMetadata({ repoDir: clone, tag: "v0.2.0", mainRef: "main" }),
    /stable release tags do not match origin/,
  );
});

test("rejects a release Tag on merged side history", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  git(repo, "checkout", "-b", "feature");
  writeReleaseFiles(repo, "0.2.0+2");
  git(repo, "add", ".");
  git(repo, "commit", "-m", "side release");
  git(repo, "tag", "v0.2.0");
  git(repo, "checkout", "main");
  git(repo, "merge", "--no-ff", "feature", "-m", "merge feature");
  git(repo, "checkout", "--detach", "v0.2.0");

  assert.throws(
    () => collectReleaseMetadata({ repoDir: repo, tag: "v0.2.0", mainRef: "main" }),
    /not on the first-parent history/,
  );
});

test("rejects release ordering that regresses later on main", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  writeReleaseFiles(repo, "0.3.0+2");
  git(repo, "add", ".");
  git(repo, "commit", "-m", "backdated future release");
  const backdatedCommit = git(repo, "rev-parse", "HEAD");
  writeReleaseFiles(repo, "0.2.0+3");
  git(repo, "add", ".");
  git(repo, "commit", "-m", "existing later release");
  git(repo, "tag", "v0.2.0");
  git(repo, "tag", "v0.3.0", backdatedCommit);
  git(repo, "checkout", "--detach", "v0.3.0");

  assert.throws(
    () => collectReleaseMetadata({ repoDir: repo, tag: "v0.3.0", mainRef: "main" }),
    /Release version v0\.2\.0 must be newer than v0\.3\.0/,
  );
});

test("rejects missing, malformed, and duplicate migration versions", () => {
  assert.throws(() => databaseSchemaVersion([]), /No server migration/);
  assert.throws(
    () => databaseSchemaVersion(["server/migrations/not_numbered.up.sql"]),
    /Invalid server migration filename/,
  );
  assert.throws(
    () => databaseSchemaVersion([
      "server/migrations/000001_first.up.sql",
      "server/migrations/000001_second.up.sql",
    ]),
    /Duplicate server migration version/,
  );
  assert.equal(
    databaseSchemaVersion([
      "server/migrations/000001_first.up.sql",
      "server/migrations/999999_ignored.down.sql",
      "server/migrations/embed.go",
    ]),
    1,
  );
});

test("derives the manifest from validated build artifacts", () => {
  const { input, metadata } = validManifestFixture();
  const manifest = createReleaseManifest(input, metadata);

  assert.equal(
    manifest.staging_apk_sha256,
    createHash("sha256").update("staging apk").digest("hex"),
  );
  assert.equal(
    manifest.production_apk_sha256,
    createHash("sha256").update("production apk").digest("hex"),
  );
  assert.equal(manifest.database_schema_version, 7);
  assert.equal(manifest.portal_image_digest, input.PORTAL_IMAGE_DIGEST);
  assert.equal(
    manifest.production_apk_size_bytes,
    Buffer.byteLength("production apk"),
  );
  assert.equal(manifest.application_id, "com.xengineer.speakup");
  assert.equal(manifest.minimum_android_api, 24);
  assert.deepEqual(manifest.abis, ["arm64-v8a"]);
});

test("writes the validated manifest atomically through the CLI", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  const { input } = validManifestFixture();
  const output = path.join(repo, "release-manifest.json");
  execFileSync(
    process.execPath,
    [
      manifestScript,
      "--tag",
      "v0.1.0",
      "--main-ref",
      "main",
      "--staging-apk",
      input.STAGING_APK_PATH,
      "--production-apk",
      input.PRODUCTION_APK_PATH,
      "--output",
      output,
      "--repo",
      repo,
    ],
    { env: { ...process.env, ...input }, stdio: "pipe" },
  );

  const manifest = JSON.parse(readFileSync(output, "utf8"));
  assert.equal(manifest.version, "0.1.0");
  assert.equal(manifest.git_sha, git(repo, "rev-parse", "HEAD"));

  const rejectedOutput = path.join(repo, "rejected-manifest.json");
  assert.throws(
    () => execFileSync(
      process.execPath,
      [
        manifestScript,
        "--tag",
        "v0.1.0",
        "--main-ref",
        "main",
        "--staging-apk",
        input.STAGING_APK_PATH,
        "--production-apk",
        input.PRODUCTION_APK_PATH,
        "--output",
        rejectedOutput,
        "--repo",
        repo,
      ],
      {
        env: { ...process.env, ...input, PORTAL_IMAGE_DIGEST: "sha256:pending" },
        stdio: "pipe",
      },
    ),
  );
  assert.equal(existsSync(rejectedOutput), false);
});

test("rejects placeholder or malformed manifest values", () => {
  assert.throws(
    () => createReleaseManifest({}, {
      version: "pending",
      version_code: "1",
      git_sha: "c".repeat(40),
      database_schema_version: "7",
    }),
    /version has an invalid value/,
  );
});

test("rejects invalid APKs, image references, digests, and quality run URLs", () => {
  const { input, metadata, stagingApk } = validManifestFixture();
  writeFileSync(stagingApk, "");
  assert.throws(() => createReleaseManifest(input, metadata), /non-empty file/);

  writeFileSync(stagingApk, "staging apk");
  assert.throws(
    () => createReleaseManifest(
      { ...input, PORTAL_IMAGE_DIGEST: "sha256:pending" },
      metadata,
    ),
    /PORTAL_IMAGE_DIGEST has an invalid value/,
  );
  assert.throws(
    () => createReleaseManifest(
      { ...input, PORTAL_IMAGE: "ghcr.io/1024xengineer//" },
      metadata,
    ),
    /PORTAL_IMAGE has an invalid value/,
  );
  assert.throws(
    () => createReleaseManifest(
      { ...input, PORTAL_IMAGE_DIGEST: `sha256:${"0".repeat(64)}` },
      metadata,
    ),
    /cannot be a placeholder digest/,
  );
  assert.throws(
    () => createReleaseManifest(
      {
        ...input,
        QUALITY_RUN_URL: "https://github.com/another/repo/actions/runs/1",
      },
      metadata,
    ),
    /QUALITY_RUN_URL has an invalid value/,
  );
});

test("rejects missing, invalid, or inconsistent Android APK metadata", () => {
  const { input, metadata } = validManifestFixture();
  assert.throws(
    () => createReleaseManifest(
      { ...input, PRODUCTION_APK_APPLICATION_ID: "" },
      metadata,
    ),
    /PRODUCTION_APK_APPLICATION_ID is required/,
  );
  assert.throws(
    () => createReleaseManifest(
      { ...input, PRODUCTION_APK_MINIMUM_ANDROID_API: "0" },
      metadata,
    ),
    /must be a positive integer/,
  );
  assert.throws(
    () => createReleaseManifest(
      { ...input, PRODUCTION_APK_ABIS: "arm64-v8a,arm64-v8a" },
      metadata,
    ),
    /PRODUCTION_APK_ABIS has an invalid value/,
  );
  assert.throws(
    () => createReleaseManifest(
      { ...input, STAGING_APK_APPLICATION_ID: "com.xengineer.staging" },
      metadata,
    ),
    /Staging and Production APK metadata do not match/,
  );
  for (const forged of [
    {
      STAGING_APK_APPLICATION_ID: "com.example.fake",
      PRODUCTION_APK_APPLICATION_ID: "com.example.fake",
    },
    {
      STAGING_APK_MINIMUM_ANDROID_API: "25",
      PRODUCTION_APK_MINIMUM_ANDROID_API: "25",
    },
    {
      STAGING_APK_ABIS: "x86",
      PRODUCTION_APK_ABIS: "x86",
    },
  ]) {
    assert.throws(
      () => createReleaseManifest({ ...input, ...forged }, metadata),
      /Android APK metadata does not match the release contract/,
    );
  }
});
