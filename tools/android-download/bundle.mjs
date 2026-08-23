#!/usr/bin/env node

import { createHash } from "node:crypto";
import {
  constants,
  lstatSync,
  fstatSync,
  mkdirSync,
  mkdtempSync,
  openSync,
  closeSync,
  readFileSync,
  renameSync,
  rmSync,
  statSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

const versionPattern = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const sha256Pattern = /^[0-9a-f]{64}$/;
const imageDigestPattern = /^sha256:[0-9a-f]{64}$/;
const gitShaPattern = /^[0-9a-f]{40}$/;
const canonicalUtcPattern =
  /^\d{4}-(0[1-9]|1[0-2])-([012]\d|3[01])T([01]\d|2[0-3]):[0-5]\d:[0-5]\dZ$/;

const releaseManifestKeys = [
  "abis",
  "apk_certificate_sha256",
  "application_id",
  "database_schema_version",
  "git_sha",
  "manifest_version",
  "minimum_android_api",
  "portal_image",
  "portal_image_digest",
  "production_apk_file",
  "production_apk_sha256",
  "production_apk_size_bytes",
  "quality_run_url",
  "server_image",
  "server_image_digest",
  "staging_apk_file",
  "staging_apk_sha256",
  "version",
  "version_code",
];

function fail(message) {
  throw new Error(message);
}

function exactKeys(value, expected, name) {
  if (
    value === null ||
    typeof value !== "object" ||
    Array.isArray(value) ||
    JSON.stringify(Object.keys(value).sort()) !== JSON.stringify([...expected].sort())
  ) {
    fail(`${name} has an invalid schema`);
  }
}

function positiveSafeInteger(value, name) {
  if (!Number.isSafeInteger(value) || value < 1) {
    fail(`${name} must be a positive safe integer`);
  }
}

function nonzeroSha(value, pattern, name) {
  if (typeof value !== "string" || !pattern.test(value)) {
    fail(`${name} has an invalid value`);
  }
  const hexadecimal = value.startsWith("sha256:") ? value.slice(7) : value;
  if (/^0+$/.test(hexadecimal)) fail(`${name} cannot be a placeholder digest`);
}

function readRegularFileSnapshot(file, name) {
  let status;
  try {
    status = lstatSync(file);
  } catch (error) {
    if (error && error.code !== "ENOENT") throw error;
    fail(`${name} does not exist: ${file}`);
  }
  if (status.isSymbolicLink()) {
    fail(`${name} cannot be a symlink`);
  }

  let descriptor;
  try {
    descriptor = openSync(
      file,
      constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0),
    );
  } catch (error) {
    if (error && error.code === "ELOOP") fail(`${name} cannot be a symlink`);
    fail(`${name} cannot be opened as a regular file`);
  }

  try {
    status = fstatSync(descriptor);
    if (!status.isFile() || status.size === 0) {
      fail(`${name} must be a non-empty regular file`);
    }
    const bytes = readFileSync(descriptor);
    if (bytes.length !== status.size) {
      fail(`${name} changed while it was being read`);
    }
    return { bytes, size: status.size };
  } finally {
    closeSync(descriptor);
  }
}

function sha256Bytes(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

function parseJsonSnapshot(snapshot, name) {
  try {
    return JSON.parse(snapshot.bytes.toString("utf8"));
  } catch {
    fail(`${name} is not valid JSON`);
  }
}

function sha256File(file) {
  return sha256Bytes(readFileSync(file));
}

export function validateReleaseManifest(value) {
  exactKeys(value, releaseManifestKeys, "release manifest");
  if (value.manifest_version !== 1) fail("release manifest version must be 1");
  if (typeof value.version !== "string" || !versionPattern.test(value.version)) {
    fail("release manifest version is invalid");
  }
  positiveSafeInteger(value.version_code, "release manifest version_code");
  nonzeroSha(value.git_sha, gitShaPattern, "release manifest git_sha");
  if (value.portal_image !== "ghcr.io/1024xengineer/xe3-esl-portal") {
    fail("release manifest Portal image is invalid");
  }
  nonzeroSha(
    value.portal_image_digest,
    imageDigestPattern,
    "release manifest portal_image_digest",
  );
  if (value.server_image !== "ghcr.io/1024xengineer/xe3-esl-server") {
    fail("release manifest Server image is invalid");
  }
  nonzeroSha(
    value.server_image_digest,
    imageDigestPattern,
    "release manifest server_image_digest",
  );
  if (value.staging_apk_file !== `speakup-v${value.version}-staging-arm64.apk`) {
    fail("release manifest Staging APK name is invalid");
  }
  nonzeroSha(
    value.staging_apk_sha256,
    sha256Pattern,
    "release manifest staging_apk_sha256",
  );
  if (
    value.production_apk_file !==
    `speakup-v${value.version}-production-arm64.apk`
  ) {
    fail("release manifest Production APK name is invalid");
  }
  positiveSafeInteger(
    value.production_apk_size_bytes,
    "release manifest production_apk_size_bytes",
  );
  nonzeroSha(
    value.production_apk_sha256,
    sha256Pattern,
    "release manifest production_apk_sha256",
  );
  if (
    value.application_id !== "com.xengineer.speakup" ||
    value.minimum_android_api !== 24 ||
    JSON.stringify(value.abis) !== JSON.stringify(["arm64-v8a"])
  ) {
    fail("release manifest Android contract is invalid");
  }
  nonzeroSha(
    value.apk_certificate_sha256,
    sha256Pattern,
    "release manifest apk_certificate_sha256",
  );
  positiveSafeInteger(
    value.database_schema_version,
    "release manifest database_schema_version",
  );
  if (
    typeof value.quality_run_url !== "string" ||
    !/^https:\/\/github\.com\/1024XEngineer\/XE3-ESL\/actions\/runs\/[1-9]\d*$/.test(
      value.quality_run_url,
    )
  ) {
    fail("release manifest quality_run_url is invalid");
  }
  return value;
}

export function validatePublishedAt(value) {
  if (typeof value !== "string" || !canonicalUtcPattern.test(value)) {
    fail("published time must be canonical RFC3339 UTC without fractional seconds");
  }
  const parsed = new Date(value);
  if (
    Number.isNaN(parsed.valueOf()) ||
    parsed.toISOString().replace(".000Z", "Z") !== value
  ) {
    fail("published time is not a real UTC timestamp");
  }
  return value;
}

function writeJson(file, value) {
  writeFileSync(file, `${JSON.stringify(value, null, 2)}\n`, {
    encoding: "utf8",
    flag: "wx",
    mode: 0o644,
  });
}

function fileEntry(root, relativePath) {
  const file = path.join(root, relativePath);
  return {
    path: relativePath,
    size_bytes: statSync(file).size,
    sha256: sha256File(file),
  };
}

function outputMustNotExist(output) {
  try {
    lstatSync(output);
  } catch (error) {
    if (error && error.code === "ENOENT") return;
    throw error;
  }
  fail(`output already exists: ${output}`);
}

export function buildAndroidDownloadBundle({
  manifestPath,
  productionApkPath,
  publishedAt,
  outputPath,
}) {
  const manifestFile = path.resolve(manifestPath);
  const apkFile = path.resolve(productionApkPath);
  const output = path.resolve(outputPath);
  const outputParent = path.dirname(output);
  const outputName = path.basename(output);

  const manifestSnapshot = readRegularFileSnapshot(manifestFile, "release manifest");
  const releaseManifestSha256 = sha256Bytes(manifestSnapshot.bytes);
  const manifest = validateReleaseManifest(
    parseJsonSnapshot(manifestSnapshot, "release manifest"),
  );
  const apkSnapshot = readRegularFileSnapshot(apkFile, "Production APK");
  const timestamp = validatePublishedAt(publishedAt);

  if (path.basename(apkFile) !== manifest.production_apk_file) {
    fail(`Production APK must be named ${manifest.production_apk_file}`);
  }
  if (apkSnapshot.size !== manifest.production_apk_size_bytes) {
    fail("Production APK size does not match the release manifest");
  }
  const apkSha256 = sha256Bytes(apkSnapshot.bytes);
  if (apkSha256 !== manifest.production_apk_sha256) {
    fail("Production APK SHA-256 does not match the release manifest");
  }

  const parentStatus = lstatSync(outputParent);
  if (parentStatus.isSymbolicLink() || !parentStatus.isDirectory()) {
    fail("output parent must be a real directory");
  }
  outputMustNotExist(output);

  const lock = path.join(outputParent, `.${outputName}.lock`);
  let lockDescriptor;
  let lockCreated = false;
  let temporary;
  try {
    lockDescriptor = openSync(
      lock,
      constants.O_CREAT | constants.O_EXCL | constants.O_WRONLY,
      0o600,
    );
    lockCreated = true;
    temporary = mkdtempSync(path.join(outputParent, `.${outputName}.tmp-`));
    const versionDirectory = path.join(
      temporary,
      "downloads",
      "android",
      `v${manifest.version}`,
    );
    mkdirSync(versionDirectory, { recursive: true, mode: 0o755 });

    const apkRelativePath = path.posix.join(
      "downloads",
      "android",
      `v${manifest.version}`,
      manifest.production_apk_file,
    );
    const checksumRelativePath = `${apkRelativePath}.sha256`;
    const metadataRelativePath = path.posix.join(
      "downloads",
      "android",
      `v${manifest.version}`,
      "release.json",
    );

    writeFileSync(path.join(temporary, apkRelativePath), apkSnapshot.bytes, {
      flag: "wx",
      mode: 0o644,
    });
    writeFileSync(
      path.join(temporary, checksumRelativePath),
      `${apkSha256}  ${manifest.production_apk_file}\n`,
      { encoding: "utf8", flag: "wx", mode: 0o644 },
    );

    const publicMetadata = {
      metadata_version: 1,
      version: manifest.version,
      version_code: manifest.version_code,
      published_at: timestamp,
      file_name: manifest.production_apk_file,
      download_path: `/${apkRelativePath}`,
      size_bytes: manifest.production_apk_size_bytes,
      minimum_android_api: manifest.minimum_android_api,
      abis: manifest.abis,
      apk_sha256: manifest.production_apk_sha256,
      apk_certificate_sha256: manifest.apk_certificate_sha256,
    };
    writeJson(path.join(temporary, metadataRelativePath), publicMetadata);

    const files = [apkRelativePath, checksumRelativePath, metadataRelativePath].map(
      (relativePath) => fileEntry(temporary, relativePath),
    );
    writeJson(path.join(temporary, "bundle-manifest.json"), {
      bundle_version: 1,
      version: manifest.version,
      published_at: timestamp,
      release_manifest_sha256: releaseManifestSha256,
      files,
    });

    outputMustNotExist(output);
    renameSync(temporary, output);
    temporary = undefined;
    return {
      output,
      version: manifest.version,
      bundleManifestSha256: sha256File(path.join(output, "bundle-manifest.json")),
    };
  } finally {
    if (lockDescriptor !== undefined) closeSync(lockDescriptor);
    if (temporary) rmSync(temporary, { recursive: true, force: true });
    if (lockCreated) {
      try {
        unlinkSync(lock);
      } catch (error) {
        if (!error || error.code !== "ENOENT") throw error;
      }
    }
  }
}

function readArguments(arguments_) {
  const values = {};
  for (let index = 0; index < arguments_.length; index += 2) {
    const flag = arguments_[index];
    const value = arguments_[index + 1];
    if (!flag?.startsWith("--") || value === undefined || values[flag.slice(2)]) {
      fail(
        "Usage: bundle.mjs --manifest FILE --production-apk FILE " +
          "--published-at RFC3339_UTC --output DIRECTORY",
      );
    }
    values[flag.slice(2)] = value;
  }
  return values;
}

function main() {
  const arguments_ = readArguments(process.argv.slice(2));
  const allowed = ["manifest", "production-apk", "published-at", "output"];
  for (const name of allowed) {
    if (!arguments_[name]) fail(`--${name} is required`);
  }
  for (const name of Object.keys(arguments_)) {
    if (!allowed.includes(name)) fail(`unknown argument: --${name}`);
  }
  const result = buildAndroidDownloadBundle({
    manifestPath: arguments_.manifest,
    productionApkPath: arguments_["production-apk"],
    publishedAt: arguments_["published-at"],
    outputPath: arguments_.output,
  });
  process.stdout.write(
    `version=${result.version} bundle_manifest_sha256=${result.bundleManifestSha256} output=${result.output}\n`,
  );
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  try {
    main();
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}
