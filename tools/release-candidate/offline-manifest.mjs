#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
  chmodSync,
  closeSync,
  constants,
  fstatSync,
  lstatSync,
  mkdtempSync,
  openSync,
  readFileSync,
  readSync,
  rmSync,
  writeSync,
} from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { readArguments } from "./cli.mjs";
import {
  createReleaseManifest,
  offlineBundleImageInput,
  writeReleaseManifest,
} from "./manifest.mjs";
import { collectReleaseMetadata } from "./metadata.mjs";

const verifierScript = fileURLToPath(
  new URL("../android-release/verify.sh", import.meta.url),
);
const officialRepository = "1024XEngineer/XE3-ESL";
const upstreamMainRef = "refs/remotes/upstream/main";
const qualityWorkflowPath = ".github/workflows/quality.yml";
const githubApiBase = "https://api.github.com";
const githubApiVersion = "2026-03-10";
const reportKeys = [
  "abi",
  "applicationId",
  "artifactSha256",
  "certificateSha256",
  "minimumAndroidApi",
  "signature",
  "versionCode",
  "versionName",
];
const sha256Pattern = /^[0-9a-f]{64}$/;
const gitShaPattern = /^[0-9a-f]{40}$/;

function required(input, name) {
  const value = input[name]?.trim();
  if (!value) throw new Error(`--${name} is required`);
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

function approvedCertificate(fileInput) {
  const file = path.resolve(fileInput);
  nonemptyRegularFile(file, "certificate fingerprint file");
  const fingerprint = readFileSync(file, "utf8")
    .toLowerCase()
    .replace(/[:\s]/g, "");
  if (!sha256Pattern.test(fingerprint) || /^0+$/.test(fingerprint)) {
    throw new Error("certificate fingerprint file has an invalid SHA-256 value");
  }
  return fingerprint;
}

function snapshotArtifact(sourceInput, expectedName, directory, name) {
  const source = path.resolve(sourceInput);
  if (path.basename(source) !== expectedName) {
    throw new Error(`${name} must be named ${expectedName}`);
  }
  nonemptyRegularFile(source, name);
  const destination = path.join(directory, expectedName);
  let sourceDescriptor;
  let destinationDescriptor;
  try {
    sourceDescriptor = openSync(
      source,
      constants.O_RDONLY | constants.O_NONBLOCK | (constants.O_NOFOLLOW ?? 0),
    );
    const before = fstatSync(sourceDescriptor);
    if (!before.isFile() || before.size < 1) {
      throw new Error(`${name} must be a non-empty regular file`);
    }
    destinationDescriptor = openSync(
      destination,
      constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL,
      0o400,
    );
    const buffer = Buffer.allocUnsafe(1024 * 1024);
    let copied = 0;
    for (;;) {
      const bytesRead = readSync(
        sourceDescriptor,
        buffer,
        0,
        buffer.length,
        null,
      );
      if (bytesRead === 0) break;
      let written = 0;
      while (written < bytesRead) {
        written += writeSync(
          destinationDescriptor,
          buffer,
          written,
          bytesRead - written,
          null,
        );
      }
      copied += bytesRead;
    }
    const after = fstatSync(sourceDescriptor);
    if (
      copied !== before.size ||
      after.size !== before.size ||
      after.mtimeMs !== before.mtimeMs ||
      after.ctimeMs !== before.ctimeMs
    ) {
      throw new Error(`${name} changed while it was being copied`);
    }
  } finally {
    if (destinationDescriptor !== undefined) closeSync(destinationDescriptor);
    if (sourceDescriptor !== undefined) closeSync(sourceDescriptor);
  }
  nonemptyRegularFile(destination, `${name} snapshot`);
  return destination;
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

function parseVerifierReport(output, name) {
  const report = {};
  const lines = output.trimEnd().split("\n");
  for (const line of lines) {
    const separator = line.indexOf("=");
    if (separator < 1) throw new Error(`${name} verifier report is malformed`);
    const key = line.slice(0, separator);
    const value = line.slice(separator + 1);
    if (!reportKeys.includes(key) || Object.hasOwn(report, key) || !value) {
      throw new Error(`${name} verifier report is malformed`);
    }
    report[key] = value;
  }
  if (
    JSON.stringify(Object.keys(report).sort()) !== JSON.stringify(reportKeys)
  ) {
    throw new Error(`${name} verifier report has an invalid field set`);
  }
  return report;
}

function verifyArtifact(name, apk, pubspec, certificate, metadata) {
  const result = spawnSync(verifierScript, [apk, pubspec], {
    encoding: "utf8",
    env: {
      ...process.env,
      SPEAKUP_ANDROID_CERT_SHA256: certificate,
    },
    maxBuffer: 10 * 1024 * 1024,
    stdio: ["ignore", "pipe", "pipe"],
  });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    const detail = result.stderr.trim();
    throw new Error(
      `${name} APK verification failed${detail ? `: ${detail}` : ""}`,
    );
  }
  const report = parseVerifierReport(result.stdout, name);
  const expected = {
    applicationId: "com.xengineer.speakup",
    versionName: metadata.version,
    versionCode: metadata.version_code,
    minimumAndroidApi: "24",
    abi: "arm64-v8a",
    signature: "verified",
    certificateSha256: certificate,
  };
  for (const [key, value] of Object.entries(expected)) {
    if (report[key] !== value) {
      throw new Error(`${name} verifier report has an unexpected ${key}`);
    }
  }
  if (
    !sha256Pattern.test(report.artifactSha256) ||
    /^0+$/.test(report.artifactSha256) ||
    report.artifactSha256 !== sha256File(apk)
  ) {
    throw new Error(`${name} verifier report does not match the APK snapshot`);
  }
  return report;
}

async function githubJson(fetchImplementation, url, name) {
  if (typeof fetchImplementation !== "function") {
    throw new Error("GitHub API fetch implementation is unavailable");
  }
  let response;
  try {
    response = await fetchImplementation(url, {
      method: "GET",
      headers: {
        Accept: "application/vnd.github+json",
        "User-Agent": "SpeakUp-Release-Candidate",
        "X-GitHub-Api-Version": githubApiVersion,
      },
      redirect: "error",
      signal: AbortSignal.timeout(15_000),
    });
  } catch (error) {
    const detail = error instanceof Error ? `: ${error.message}` : "";
    throw new Error(`${name} request failed${detail}`);
  }
  if (!response || response.ok !== true) {
    const status = Number.isInteger(response?.status)
      ? response.status
      : "unknown";
    throw new Error(`${name} request failed with HTTP ${status}`);
  }
  let value;
  try {
    value = await response.json();
  } catch {
    throw new Error(`${name} response must contain valid JSON`);
  }
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${name} response must be a JSON object`);
  }
  return value;
}

export async function validateQualityRun(
  qualityRunUrlInput,
  releaseGitSha,
  fetchImplementation = globalThis.fetch,
) {
  const qualityRunUrl = required(
    { "quality-run-url": qualityRunUrlInput },
    "quality-run-url",
  );
  if (!gitShaPattern.test(releaseGitSha)) {
    throw new Error("release Git SHA is invalid");
  }
  const runUrlPattern = new RegExp(
    `^https://github\\.com/${officialRepository.replace("/", "\\/")}` +
      "/actions/runs/([1-9]\\d*)$",
  );
  const runUrlMatch = runUrlPattern.exec(qualityRunUrl);
  const runId = Number(runUrlMatch?.[1]);
  if (!runUrlMatch || !Number.isSafeInteger(runId)) {
    throw new Error("quality run URL must identify an official workflow run");
  }

  const workflowApiUrl =
    `${githubApiBase}/repos/${officialRepository}/actions/workflows/quality.yml`;
  const workflow = await githubJson(
    fetchImplementation,
    workflowApiUrl,
    "Quality workflow",
  );
  if (
    !Number.isSafeInteger(workflow.id) ||
    workflow.id < 1 ||
    workflow.name !== "Quality" ||
    workflow.path !== qualityWorkflowPath ||
    workflow.state !== "active"
  ) {
    throw new Error("Quality workflow response does not match the release contract");
  }

  const runApiUrl =
    `${githubApiBase}/repos/${officialRepository}/actions/runs/${runId}`;
  const run = await githubJson(
    fetchImplementation,
    runApiUrl,
    "Quality workflow run",
  );
  if (
    run.id !== runId ||
    run.url !== runApiUrl ||
    run.html_url !== qualityRunUrl ||
    run.workflow_id !== workflow.id ||
    run.repository?.full_name !== officialRepository ||
    run.head_repository?.full_name !== officialRepository ||
    run.path !== qualityWorkflowPath ||
    run.head_sha !== releaseGitSha ||
    run.head_branch !== "main" ||
    !["push", "workflow_dispatch"].includes(run.event) ||
    run.status !== "completed" ||
    run.conclusion !== "success"
  ) {
    throw new Error("Quality workflow run does not match the release contract");
  }
  return qualityRunUrl;
}

export async function generateOfflineManifest(
  argv = process.argv.slice(2),
  fetchImplementation = globalThis.fetch,
) {
  const usage =
    "Usage: offline-manifest.mjs --tag <vX.Y.Z> --staging-apk <file> " +
    "--production-apk <file> --offline-bundle <file> " +
    "--certificate-file <file> --quality-run-url <url> --output <new-file> " +
    "[--repo <path>]";
  const arguments_ = readArguments(
    argv,
    [
      "tag",
      "staging-apk",
      "production-apk",
      "offline-bundle",
      "certificate-file",
      "quality-run-url",
      "output",
      "repo",
    ],
    usage,
  );
  for (const name of [
    "tag",
    "staging-apk",
    "production-apk",
    "offline-bundle",
    "certificate-file",
    "quality-run-url",
    "output",
  ]) {
    required(arguments_, name);
  }

  const repoDir = path.resolve(arguments_.repo ?? ".");
  const metadata = collectReleaseMetadata({
    repoDir,
    tag: arguments_.tag,
    mainRef: upstreamMainRef,
    tagRemote: "upstream",
  });
  const certificate = approvedCertificate(arguments_["certificate-file"]);
  const qualityRunUrl = await validateQualityRun(
    arguments_["quality-run-url"],
    metadata.git_sha,
    fetchImplementation,
  );
  const temporaryDirectory = mkdtempSync(
    path.join(tmpdir(), "speakup-offline-manifest-"),
  );
  chmodSync(temporaryDirectory, 0o700);
  try {
    const stagingApk = snapshotArtifact(
      arguments_["staging-apk"],
      `speakup-v${metadata.version}-staging-arm64.apk`,
      temporaryDirectory,
      "staging APK",
    );
    const productionApk = snapshotArtifact(
      arguments_["production-apk"],
      `speakup-v${metadata.version}-production-arm64.apk`,
      temporaryDirectory,
      "production APK",
    );
    chmodSync(temporaryDirectory, 0o500);

    const pubspec = path.join(repoDir, "mobile", "pubspec.yaml");
    const stagingReport = verifyArtifact(
      "staging",
      stagingApk,
      pubspec,
      certificate,
      metadata,
    );
    const productionReport = verifyArtifact(
      "production",
      productionApk,
      pubspec,
      certificate,
      metadata,
    );
    const imageInput = offlineBundleImageInput(
      arguments_["offline-bundle"],
      metadata,
    );
    const manifest = createReleaseManifest(
      {
        ...imageInput,
        GITHUB_REPOSITORY: officialRepository,
        QUALITY_RUN_URL: qualityRunUrl,
        APK_CERTIFICATE_SHA256: certificate,
        STAGING_APK_APPLICATION_ID: stagingReport.applicationId,
        STAGING_APK_MINIMUM_ANDROID_API: stagingReport.minimumAndroidApi,
        STAGING_APK_ABIS: stagingReport.abi,
        STAGING_APK_VERIFIED_SHA256: stagingReport.artifactSha256,
        PRODUCTION_APK_APPLICATION_ID: productionReport.applicationId,
        PRODUCTION_APK_MINIMUM_ANDROID_API: productionReport.minimumAndroidApi,
        PRODUCTION_APK_ABIS: productionReport.abi,
        PRODUCTION_APK_VERIFIED_SHA256: productionReport.artifactSha256,
        STAGING_APK_PATH: stagingApk,
        PRODUCTION_APK_PATH: productionApk,
      },
      metadata,
    );
    writeReleaseManifest(arguments_.output, manifest);
    process.stdout.write(
      `offline_manifest=${path.resolve(arguments_.output)} version=${metadata.version} git_sha=${metadata.git_sha}\n`,
    );
  } finally {
    chmodSync(temporaryDirectory, 0o700);
    rmSync(temporaryDirectory, { force: true, recursive: true });
  }
}

if (
  process.argv[1] &&
  pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url
) {
  generateOfflineManifest().catch((error) => {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  });
}
