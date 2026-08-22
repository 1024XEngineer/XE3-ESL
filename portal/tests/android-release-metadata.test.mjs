import assert from "node:assert/strict";
import test from "node:test";

import {
  loadAndroidRelease,
  parseAndroidReleaseMetadata,
} from "../lib/android-release-metadata.mjs";

const validMetadata = {
  metadata_version: 1,
  version: "0.1.0",
  version_code: 1,
  published_at: "2026-08-23T12:34:56Z",
  file_name: "speakup-v0.1.0-production-arm64.apk",
  download_path:
    "/downloads/android/v0.1.0/speakup-v0.1.0-production-arm64.apk",
  size_bytes: 12345678,
  minimum_android_api: 24,
  abis: ["arm64-v8a"],
  apk_sha256: "a".repeat(64),
  apk_certificate_sha256: "b".repeat(64),
};

test("accepts only the exact versioned Android release contract", () => {
  assert.deepEqual(parseAndroidReleaseMetadata(validMetadata), validMetadata);

  const invalidValues = [
    { ...validMetadata, extra: true },
    { ...validMetadata, download_path: "https://example.com/app.apk" },
    { ...validMetadata, download_path: "/downloads/android/latest.apk" },
    { ...validMetadata, file_name: "speakup-v0.1.1-production-arm64.apk" },
    { ...validMetadata, published_at: "2026-02-30T12:34:56Z" },
    { ...validMetadata, published_at: "2026-08-23T12:34:56+00:00" },
    { ...validMetadata, size_bytes: 0 },
    { ...validMetadata, minimum_android_api: 23 },
    { ...validMetadata, abis: ["arm64-v8a", "x86_64"] },
    { ...validMetadata, apk_sha256: "A".repeat(64) },
    { ...validMetadata, apk_certificate_sha256: "0".repeat(64) },
  ];
  for (const value of invalidValues) {
    assert.throws(
      () => parseAndroidReleaseMetadata(value),
      /Android release metadata is invalid/,
    );
  }
});

test("maps a missing release to the honest preparing state", async () => {
  const fetcher = async () => new Response(null, { status: 404 });
  assert.deepEqual(await loadAndroidRelease(fetcher), { status: "preparing" });
});

test("returns ready only after strict metadata validation", async () => {
  const fetcher = async () => Response.json(validMetadata);
  assert.deepEqual(await loadAndroidRelease(fetcher), {
    status: "ready",
    release: validMetadata,
  });
});

test("maps network, HTTP, JSON, and schema failures to unavailable", async () => {
  const cases = [
    async () => {
      throw new Error("network unavailable");
    },
    async () => new Response(null, { status: 503 }),
    async () => new Response("not-json", { status: 200 }),
    async () => Response.json({ ...validMetadata, unexpected: true }),
  ];
  for (const fetcher of cases) {
    assert.deepEqual(await loadAndroidRelease(fetcher), {
      status: "unavailable",
    });
  }
});
