#!/usr/bin/env node

import { createHash } from "node:crypto";
import {
  closeSync,
  linkSync,
  lstatSync,
  mkdtempSync,
  openSync,
  readSync,
  readFileSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

import { readArguments } from "./cli.mjs";
import {
  collectCandidateMetadata,
  collectReleaseMetadata,
} from "./metadata.mjs";

const sha256Pattern = /^[0-9a-f]{64}$/;
const imageDigestPattern = /^sha256:[0-9a-f]{64}$/;
const gitShaPattern = /^[0-9a-f]{40}$/;
const versionPattern = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const applicationIdPattern =
  /^[A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z][A-Za-z0-9_]*)+$/;
const androidReleaseContract = {
  application_id: "com.xengineer.speakup",
  minimum_android_api: 24,
  abis: ["arm64-v8a"],
};
const imagePattern =
  /^ghcr\.io\/[a-z0-9]+(?:[._-][a-z0-9]+)*\/[a-z0-9]+(?:[._/-][a-z0-9]+)*$/;
const repositoryPattern = /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/;
const offlineBundleKeys = [
  "bundle_version",
  "git_sha",
  "images",
  "platform",
  "source_date_epoch",
  "version",
];
const offlineImageKeys = [
  "archive_file",
  "archive_sha256",
  "archive_size_bytes",
  "build_metadata_file",
  "build_metadata_sha256",
  "digest",
  "name",
  "repository",
];
const offlineImages = [
  {
    name: "portal",
    repository: "ghcr.io/1024xengineer/xe3-esl-portal",
    environmentPrefix: "PORTAL",
  },
  {
    name: "server",
    repository: "ghcr.io/1024xengineer/xe3-esl-server",
    environmentPrefix: "SERVER",
  },
];

function required(input, name) {
  const value = input[name]?.trim();
  if (!value) throw new Error(`${name} is required`);
  return value;
}

function matches(value, pattern, name) {
  if (!pattern.test(value)) throw new Error(`${name} has an invalid value`);
  return value;
}

function nonzeroSha(value, pattern, name) {
  const validated = matches(value, pattern, name);
  const hexadecimal = validated.startsWith("sha256:")
    ? validated.slice("sha256:".length)
    : validated;
  if (/^0+$/.test(hexadecimal)) {
    throw new Error(`${name} cannot be a placeholder digest`);
  }
  return validated;
}

function qualityRunUrl(value, repository) {
  let url;
  try {
    url = new URL(value);
  } catch {
    throw new Error("QUALITY_RUN_URL has an invalid value");
  }
  const expectedPath = `/${repository}/actions/runs/`;
  if (
    url.protocol !== "https:" ||
    url.hostname !== "github.com" ||
    url.port ||
    url.username ||
    url.password ||
    url.search ||
    url.hash ||
    !url.pathname.toLowerCase().startsWith(expectedPath.toLowerCase()) ||
    !/^\d+$/.test(url.pathname.slice(expectedPath.length))
  ) {
    throw new Error("QUALITY_RUN_URL has an invalid value");
  }
  return value;
}

function certificateSha256(value) {
  const normalized = value.toLowerCase().replace(/[:\s]/g, "");
  return nonzeroSha(normalized, sha256Pattern, "APK_CERTIFICATE_SHA256");
}

function apkArtifact(filePath, expectedName, name) {
  const resolved = path.resolve(required({ filePath }, "filePath"));
  const status = statSync(resolved);
  if (!status.isFile() || status.size === 0) {
    throw new Error(`${name} must be a non-empty file`);
  }
  if (path.basename(resolved) !== expectedName) {
    throw new Error(`${name} must be named ${expectedName}`);
  }
  return {
    file: expectedName,
    size_bytes: status.size,
    sha256: createHash("sha256").update(readFileSync(resolved)).digest("hex"),
  };
}

function verifyApkHash(input, prefix, artifact) {
  const name = `${prefix}_APK_VERIFIED_SHA256`;
  if (!Object.hasOwn(input, name)) return;
  const expected = nonzeroSha(required(input, name), sha256Pattern, name);
  if (artifact.sha256 !== expected) {
    throw new Error(`${name} does not match the APK file`);
  }
}

function exactObject(value, expectedKeys, name) {
  if (
    value === null ||
    typeof value !== "object" ||
    Array.isArray(value) ||
    JSON.stringify(Object.keys(value).sort()) !== JSON.stringify(expectedKeys)
  ) {
    throw new Error(`${name} must contain the exact field set`);
  }
  return value;
}

function nonemptyRegularFile(file, name) {
  let status;
  try {
    status = lstatSync(file);
  } catch {
    throw new Error(`${name} must be a non-empty regular file`);
  }
  if (status.isSymbolicLink() || !status.isFile() || status.size < 1) {
    throw new Error(`${name} must be a non-empty regular file`);
  }
  return status;
}

function sha256File(file) {
  const descriptor = openSync(file, "r");
  const hash = createHash("sha256");
  const buffer = Buffer.allocUnsafe(1024 * 1024);
  try {
    for (;;) {
      const bytesRead = readSync(descriptor, buffer, 0, buffer.length, null);
      if (bytesRead === 0) break;
      hash.update(buffer.subarray(0, bytesRead));
    }
  } finally {
    closeSync(descriptor);
  }
  return hash.digest("hex");
}

function readJson(file, name) {
  try {
    return JSON.parse(readFileSync(file, "utf8"));
  } catch {
    throw new Error(`${name} must contain valid JSON`);
  }
}

export function offlineBundleImageInput(filePath, releaseMetadata) {
  const bundlePath = path.resolve(filePath);
  nonemptyRegularFile(bundlePath, "offline bundle");
  const bundle = exactObject(
    readJson(bundlePath, "offline bundle"),
    offlineBundleKeys,
    "offline bundle",
  );
  if (bundle.bundle_version !== 1) {
    throw new Error("offline bundle version must be 1");
  }
  const bundleVersion = matches(bundle.version, versionPattern, "offline bundle version");
  if (bundleVersion !== releaseMetadata.version) {
    throw new Error("offline bundle version does not match release metadata");
  }
  const bundleGitSha = nonzeroSha(
    bundle.git_sha,
    gitShaPattern,
    "offline bundle git_sha",
  );
  if (bundleGitSha !== releaseMetadata.git_sha) {
    throw new Error("offline bundle git_sha does not match release metadata");
  }
  if (
    !Number.isSafeInteger(bundle.source_date_epoch) ||
    bundle.source_date_epoch < 1
  ) {
    throw new Error("offline bundle source_date_epoch must be a positive integer");
  }
  if (bundle.platform !== "linux/amd64") {
    throw new Error("offline bundle platform must be linux/amd64");
  }
  if (!Array.isArray(bundle.images) || bundle.images.length !== offlineImages.length) {
    throw new Error("offline bundle must contain ordered portal and server images");
  }

  const bundleDirectory = path.dirname(bundlePath);
  const imageInput = {};
  for (const [index, expected] of offlineImages.entries()) {
    const image = exactObject(
      bundle.images[index],
      offlineImageKeys,
      `offline ${expected.name} image`,
    );
    if (image.name !== expected.name || image.repository !== expected.repository) {
      throw new Error(`offline ${expected.name} image identity is invalid`);
    }
    const digest = nonzeroSha(
      image.digest,
      imageDigestPattern,
      `offline ${expected.name} image digest`,
    );
    const archiveFile =
      `speakup-${expected.name}-v${bundleVersion}-linux-amd64.tar`;
    const metadataFile =
      `speakup-${expected.name}-v${bundleVersion}-linux-amd64-build-metadata.json`;
    if (
      image.archive_file !== archiveFile ||
      image.build_metadata_file !== metadataFile
    ) {
      throw new Error(`offline ${expected.name} image file names are invalid`);
    }
    if (!Number.isSafeInteger(image.archive_size_bytes) || image.archive_size_bytes < 1) {
      throw new Error(`offline ${expected.name} archive size is invalid`);
    }
    const archiveSha256 = nonzeroSha(
      image.archive_sha256,
      sha256Pattern,
      `offline ${expected.name} archive SHA-256`,
    );
    const metadataSha256 = nonzeroSha(
      image.build_metadata_sha256,
      sha256Pattern,
      `offline ${expected.name} metadata SHA-256`,
    );
    const archivePath = path.join(bundleDirectory, archiveFile);
    const metadataPath = path.join(bundleDirectory, metadataFile);
    const archiveStatus = nonemptyRegularFile(
      archivePath,
      `offline ${expected.name} archive`,
    );
    nonemptyRegularFile(metadataPath, `offline ${expected.name} metadata`);
    if (archiveStatus.size !== image.archive_size_bytes) {
      throw new Error(`offline ${expected.name} archive size does not match`);
    }
    if (sha256File(archivePath) !== archiveSha256) {
      throw new Error(`offline ${expected.name} archive SHA-256 does not match`);
    }
    if (sha256File(metadataPath) !== metadataSha256) {
      throw new Error(`offline ${expected.name} metadata SHA-256 does not match`);
    }
    const buildMetadata = readJson(metadataPath, `offline ${expected.name} metadata`);
    if (buildMetadata?.["containerimage.digest"] !== digest) {
      throw new Error(`offline ${expected.name} metadata digest does not match`);
    }
    imageInput[`${expected.environmentPrefix}_IMAGE`] = expected.repository;
    imageInput[`${expected.environmentPrefix}_IMAGE_DIGEST`] = digest;
  }
  return imageInput;
}

function androidApkMetadata(input, prefix) {
  const applicationId = matches(
    required(input, `${prefix}_APK_APPLICATION_ID`),
    applicationIdPattern,
    `${prefix}_APK_APPLICATION_ID`,
  );
  const minimumAndroidApi = Number(
    required(input, `${prefix}_APK_MINIMUM_ANDROID_API`),
  );
  if (!Number.isSafeInteger(minimumAndroidApi) || minimumAndroidApi < 1) {
    throw new Error(`${prefix}_APK_MINIMUM_ANDROID_API must be a positive integer`);
  }
  const abis = required(input, `${prefix}_APK_ABIS`).split(",");
  const supportedAbis = new Set(["armeabi-v7a", "arm64-v8a", "x86", "x86_64"]);
  if (
    abis.some((abi) => !supportedAbis.has(abi)) ||
    new Set(abis).size !== abis.length
  ) {
    throw new Error(`${prefix}_APK_ABIS has an invalid value`);
  }
  return {
    application_id: applicationId,
    minimum_android_api: minimumAndroidApi,
    abis,
  };
}

function matchingAndroidApkMetadata(input) {
  const staging = androidApkMetadata(input, "STAGING");
  const production = androidApkMetadata(input, "PRODUCTION");
  if (JSON.stringify(staging) !== JSON.stringify(production)) {
    throw new Error("Staging and Production APK metadata do not match");
  }
  if (JSON.stringify(production) !== JSON.stringify(androidReleaseContract)) {
    throw new Error("Android APK metadata does not match the release contract");
  }
  return production;
}

export function createReleaseManifest(input, metadata) {
  const version = matches(metadata.version, versionPattern, "version");
  const gitSha = nonzeroSha(metadata.git_sha, gitShaPattern, "git_sha");
  const versionCode = Number(metadata.version_code);
  const schemaVersion = Number(metadata.database_schema_version);
  if (!Number.isSafeInteger(versionCode) || versionCode < 1) {
    throw new Error("version_code must be a positive integer");
  }
  if (!Number.isSafeInteger(schemaVersion) || schemaVersion < 1) {
    throw new Error("database_schema_version must be a positive integer");
  }
  const repository = matches(
    required(input, "GITHUB_REPOSITORY"),
    repositoryPattern,
    "GITHUB_REPOSITORY",
  );

  const stagingApk = apkArtifact(
    input.STAGING_APK_PATH,
    `speakup-v${version}-staging-arm64.apk`,
    "STAGING_APK_PATH",
  );
  const productionApk = apkArtifact(
    input.PRODUCTION_APK_PATH,
    `speakup-v${version}-production-arm64.apk`,
    "PRODUCTION_APK_PATH",
  );
  verifyApkHash(input, "STAGING", stagingApk);
  verifyApkHash(input, "PRODUCTION", productionApk);
  const androidMetadata = matchingAndroidApkMetadata(input);

  return {
    manifest_version: 1,
    version,
    version_code: versionCode,
    git_sha: gitSha,
    portal_image: matches(required(input, "PORTAL_IMAGE"), imagePattern, "PORTAL_IMAGE"),
    portal_image_digest: nonzeroSha(
      required(input, "PORTAL_IMAGE_DIGEST"),
      imageDigestPattern,
      "PORTAL_IMAGE_DIGEST",
    ),
    server_image: matches(required(input, "SERVER_IMAGE"), imagePattern, "SERVER_IMAGE"),
    server_image_digest: nonzeroSha(
      required(input, "SERVER_IMAGE_DIGEST"),
      imageDigestPattern,
      "SERVER_IMAGE_DIGEST",
    ),
    staging_apk_file: stagingApk.file,
    staging_apk_sha256: stagingApk.sha256,
    production_apk_file: productionApk.file,
    production_apk_size_bytes: productionApk.size_bytes,
    production_apk_sha256: productionApk.sha256,
    ...androidMetadata,
    apk_certificate_sha256: certificateSha256(
      required(input, "APK_CERTIFICATE_SHA256"),
    ),
    database_schema_version: schemaVersion,
    quality_run_url: qualityRunUrl(
      required(input, "QUALITY_RUN_URL"),
      repository,
    ),
  };
}

export function writeReleaseManifest(outputInput, manifest) {
  const output = path.resolve(outputInput);
  const temporaryDirectory = mkdtempSync(`${output}.tmp-`);
  const temporaryOutput = path.join(temporaryDirectory, "release-manifest.json");
  try {
    writeFileSync(temporaryOutput, `${JSON.stringify(manifest, null, 2)}\n`, "utf8");
    try {
      linkSync(temporaryOutput, output);
    } catch (error) {
      if (error?.code === "EEXIST") {
        throw new Error(`output already exists: ${output}`);
      }
      throw error;
    }
  } finally {
    rmSync(temporaryDirectory, { force: true, recursive: true });
  }
}

function main() {
  const usage =
    "Usage: manifest.mjs (--tag <vX.Y.Z> | --candidate-sha <sha>) " +
    "--main-ref <ref> " +
    "--staging-apk <file> --production-apk <file> --output <file> " +
    "[--repo <path>] [--tag-remote <name>]";
  const arguments_ = readArguments(
    process.argv.slice(2),
    [
      "tag",
      "candidate-sha",
      "main-ref",
      "staging-apk",
      "production-apk",
      "output",
      "repo",
      "tag-remote",
    ],
    usage,
  );
  for (const name of ["main-ref", "staging-apk", "production-apk", "output"]) {
    if (!arguments_[name]) throw new Error(`--${name} is required`);
  }
  if (Boolean(arguments_.tag) === Boolean(arguments_["candidate-sha"])) {
    throw new Error("Exactly one of --tag or --candidate-sha is required");
  }
  const repoDir = path.resolve(arguments_.repo ?? ".");
  const common = {
    repoDir,
    mainRef: arguments_["main-ref"],
    tagRemote: arguments_["tag-remote"] ?? "origin",
  };
  const metadata = arguments_.tag
    ? collectReleaseMetadata({ ...common, tag: arguments_.tag })
    : collectCandidateMetadata({
        ...common,
        candidateSha: arguments_["candidate-sha"],
      });
  const manifest = createReleaseManifest(
    {
      ...process.env,
      STAGING_APK_PATH: arguments_["staging-apk"],
      PRODUCTION_APK_PATH: arguments_["production-apk"],
    },
    metadata,
  );
  writeReleaseManifest(arguments_.output, manifest);
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  try {
    main();
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}
