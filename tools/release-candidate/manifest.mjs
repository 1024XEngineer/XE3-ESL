#!/usr/bin/env node

import { createHash } from "node:crypto";
import {
  mkdtempSync,
  readFileSync,
  renameSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

import { collectReleaseMetadata } from "./metadata.mjs";

const sha256Pattern = /^[0-9a-f]{64}$/;
const imageDigestPattern = /^sha256:[0-9a-f]{64}$/;
const gitShaPattern = /^[0-9a-f]{40}$/;
const versionPattern = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const imagePattern =
  /^ghcr\.io\/[a-z0-9]+(?:[._-][a-z0-9]+)*\/[a-z0-9]+(?:[._/-][a-z0-9]+)*$/;
const repositoryPattern = /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/;

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
    sha256: createHash("sha256").update(readFileSync(resolved)).digest("hex"),
  };
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
    production_apk_sha256: productionApk.sha256,
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

function readArguments(arguments_) {
  const values = {};
  for (let index = 0; index < arguments_.length; index += 2) {
    const flag = arguments_[index];
    const value = arguments_[index + 1];
    if (!flag?.startsWith("--") || value === undefined) {
      throw new Error(
        "Usage: manifest.mjs --tag <vX.Y.Z> --main-ref <ref> " +
          "--staging-apk <file> --production-apk <file> --output <file>",
      );
    }
    values[flag.slice(2)] = value;
  }
  return values;
}

function main() {
  const arguments_ = readArguments(process.argv.slice(2));
  for (const name of ["tag", "main-ref", "staging-apk", "production-apk", "output"]) {
    if (!arguments_[name]) throw new Error(`--${name} is required`);
  }
  const repoDir = path.resolve(arguments_.repo ?? ".");
  const metadata = collectReleaseMetadata({
    repoDir,
    tag: arguments_.tag,
    mainRef: arguments_["main-ref"],
  });
  const manifest = createReleaseManifest(
    {
      ...process.env,
      STAGING_APK_PATH: arguments_["staging-apk"],
      PRODUCTION_APK_PATH: arguments_["production-apk"],
    },
    metadata,
  );
  const output = path.resolve(arguments_.output);
  const temporaryDirectory = mkdtempSync(`${output}.tmp-`);
  const temporaryOutput = path.join(temporaryDirectory, "release-manifest.json");
  try {
    writeFileSync(temporaryOutput, `${JSON.stringify(manifest, null, 2)}\n`, "utf8");
    renameSync(temporaryOutput, output);
  } finally {
    rmSync(temporaryDirectory, { force: true, recursive: true });
  }
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  try {
    main();
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}
