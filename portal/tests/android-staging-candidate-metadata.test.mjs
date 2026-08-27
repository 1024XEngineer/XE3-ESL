import assert from "node:assert/strict";
import test from "node:test";

import {
  loadAndroidStagingCandidate,
  parseAndroidStagingCandidateMetadata,
} from "../lib/android-staging-candidate-metadata.mjs";

const validMetadata = {
  candidate_metadata_version: 1,
  environment: "staging",
  version: "0.1.8",
  version_code: 9,
  git_sha: "a".repeat(40),
  candidate_run_id: 33027113615,
  manifest_sha256: "b".repeat(64),
  file_name: "speakup-v0.1.8-staging-arm64.apk",
  download_path:
    "/downloads/android/candidates/33027113615/speakup-v0.1.8-staging-arm64.apk",
  size_bytes: 70033360,
  minimum_android_api: 24,
  abis: ["arm64-v8a"],
  apk_sha256: "c".repeat(64),
  apk_certificate_sha256: "d".repeat(64),
};

test("accepts only the exact Staging candidate contract", () => {
  assert.deepEqual(
    parseAndroidStagingCandidateMetadata(validMetadata),
    validMetadata,
  );

  const invalidValues = [
    { ...validMetadata, extra: true },
    { ...validMetadata, environment: "production" },
    { ...validMetadata, version: "01.1.8" },
    { ...validMetadata, version_code: 0 },
    { ...validMetadata, git_sha: "0".repeat(40) },
    { ...validMetadata, candidate_run_id: 0 },
    { ...validMetadata, manifest_sha256: "0".repeat(64) },
    { ...validMetadata, file_name: "speakup-v0.1.8-production-arm64.apk" },
    {
      ...validMetadata,
      download_path:
        "/downloads/android/candidates/1/speakup-v0.1.8-staging-arm64.apk",
    },
    { ...validMetadata, size_bytes: 0 },
    { ...validMetadata, minimum_android_api: 23 },
    { ...validMetadata, abis: ["arm64-v8a", "x86_64"] },
    { ...validMetadata, apk_sha256: "C".repeat(64) },
    { ...validMetadata, apk_certificate_sha256: "0".repeat(64) },
  ];
  for (const value of invalidValues) {
    assert.throws(
      () => parseAndroidStagingCandidateMetadata(value),
      /Android staging candidate metadata is invalid/,
    );
  }
});

test("loads only the Staging candidate pointer without a Production fallback", async () => {
  const requests = [];
  const fetcher = async (url, options) => {
    requests.push([url, options]);
    return Response.json(validMetadata);
  };

  assert.deepEqual(await loadAndroidStagingCandidate(fetcher), {
    status: "ready",
    release: validMetadata,
  });
  assert.deepEqual(requests, [
    [
      "/downloads/android/staging-candidate.json",
      { cache: "no-store", headers: { accept: "application/json" } },
    ],
  ]);
});

test("fails closed when the Staging candidate cannot be verified", async () => {
  assert.deepEqual(
    await loadAndroidStagingCandidate(async () =>
      new Response(null, { status: 404 }),
    ),
    { status: "preparing" },
  );

  const failures = [
    async () => {
      throw new Error("network unavailable");
    },
    async () => new Response(null, { status: 503 }),
    async () => new Response("not-json", { status: 200 }),
    async () => Response.json({ ...validMetadata, unexpected: true }),
  ];
  for (const fetcher of failures) {
    assert.deepEqual(await loadAndroidStagingCandidate(fetcher), {
      status: "unavailable",
    });
  }
});
