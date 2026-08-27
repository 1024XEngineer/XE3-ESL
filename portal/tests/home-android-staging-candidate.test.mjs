import assert from "node:assert/strict";
import test from "node:test";

import { homeAndroidStagingCandidateView } from "../lib/home-android-staging-candidate.mjs";

const release = {
  version: "0.1.8",
  version_code: 9,
  file_name: "speakup-v0.1.8-staging-arm64.apk",
  download_path:
    "/downloads/android/candidates/33027113615/speakup-v0.1.8-staging-arm64.apk",
  apk_sha256: "abcdef12".padEnd(64, "3"),
};

test("ready Staging homepage identifies and downloads the exact candidate", () => {
  const view = homeAndroidStagingCandidateView({ status: "ready", release });

  assert.equal(view.status, "ready");
  assert.deepEqual(view.action, {
    kind: "download",
    href: release.download_path,
    download: release.file_name,
    label: "下载 Staging APK",
  });
  assert.match(view.supportLine, /Staging 候选环境/);
  assert.match(view.supportLine, /v0\.1\.8/);
  assert.match(view.supportLine, /versionCode 9/);
  assert.match(view.supportLine, /APK SHA-256 abcdef12…/);
});

test("unready Staging homepage never guesses an APK URL", () => {
  for (const status of ["preparing", "unavailable"]) {
    const view = homeAndroidStagingCandidateView({ status });
    assert.match(JSON.stringify(view), /Staging/);
    assert.doesNotMatch(JSON.stringify(view), /\/downloads\/.*\.apk/i);
  }
});

test("unknown Staging candidate states fail explicitly", () => {
  assert.throws(
    () => homeAndroidStagingCandidateView({ status: "unknown" }),
    /Unknown Android staging candidate state/,
  );
});
