import assert from "node:assert/strict";
import test from "node:test";

import { homeAndroidReleaseView } from "../lib/home-android-release.mjs";

const release = {
  version: "0.1.2",
  file_name: "speakup-v0.1.2-production-arm64.apk",
  download_path:
    "/downloads/android/v0.1.2/speakup-v0.1.2-production-arm64.apk",
};

test("ready homepage exposes one exact validated versioned APK action", () => {
  const view = homeAndroidReleaseView({ status: "ready", release });

  assert.equal(view.status, "ready");
  assert.deepEqual(view.action, {
    kind: "download",
    href: release.download_path,
    download: release.file_name,
    label: "下载客户端",
  });
  assert.equal(view.supportLine, "当前支持 Android 7.0 及以上");
  assert.doesNotMatch(
    `${view.action.label} ${view.supportLine}`,
    /APK|v0\.1\.2/,
  );
});

test("preparing homepage keeps the APK action disabled", () => {
  const view = homeAndroidReleaseView({ status: "preparing" });

  assert.equal(view.status, "preparing");
  assert.deepEqual(view.action, {
    kind: "disabled",
    label: "客户端准备中",
  });
  assert.match(view.supportLine, /正式版本就绪后开放下载/);
  assert.doesNotMatch(JSON.stringify(view), /\.apk(?:"|\\)/i);
});

test("unavailable homepage fails closed without guessing an APK URL", () => {
  const view = homeAndroidReleaseView({ status: "unavailable" });

  assert.equal(view.status, "unavailable");
  assert.deepEqual(view.action, {
    kind: "disabled",
    label: "下载暂不可用",
  });
  assert.match(view.supportLine, /无法验证/);
  assert.doesNotMatch(JSON.stringify(view), /\/downloads\/.*\.apk/i);
});

test("unknown release states cannot silently look like preparing", () => {
  assert.throws(
    () => homeAndroidReleaseView({ status: "unknown" }),
    /Unknown Android release state/,
  );
});
