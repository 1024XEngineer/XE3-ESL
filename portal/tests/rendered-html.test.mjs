import assert from "node:assert/strict";
import test from "node:test";

async function render(path = "/") {
  const workerUrl = new URL("../dist/server/index.js", import.meta.url);
  workerUrl.searchParams.set("test", `${process.pid}-${Date.now()}`);
  const { default: worker } = await import(workerUrl.href);
  return worker.fetch(
    new Request(`http://localhost${path}`, { headers: { accept: "text/html" } }),
    {
      ASSETS: { fetch: async () => new Response("Not found", { status: 404 }) },
    },
    { passThroughOnException() {}, waitUntil() {} },
  );
}

test("renders the standalone SpeakUp portal", async () => {
  const response = await render();
  assert.equal(response.status, 200);

  const html = await response.text();
  assert.match(html, /下一场重要的英文沟通，先练一遍/);
  assert.match(
    html,
    /越用越懂你的 AI 口语老师，围绕真实任务陪你准备、开口和复盘/,
  );
  assert.match(html, /Android 版本准备中/);
  assert.match(html, /正式 APK 就绪后开放下载/);
  assert.match(html, /href="#method"[^>]*>怎么练<\/a>/);
  assert.match(
    html,
    /<section class="release-method" id="method"[^>]*><div class="release-section-heading"><h2 id="method-title">/,
  );
  assert.match(html, /href="#memory"[^>]*>长期记忆<\/a>/);
  assert.match(
    html,
    /<section class="release-memory" id="memory"[^>]*><div><h2 id="memory-title">/,
  );
  assert.match(html, /portal-interview-practice\.png/);
  assert.match(html, /speak-up-wordmark-black\.png/);
  assert.match(html, /speakup-mascot-blue\.png/);
  assert.doesNotMatch(html, /href="\/download\/android"/);
  assert.doesNotMatch(html, /常见问题|唯一官方下载|制品信息完整/);
  assert.doesNotMatch(html, /SHA-256|签名证书|ABI/);
  assert.doesNotMatch(html, /\/downloads\/.*\.apk/);
});

test("renders an honest Android download preparing state", async () => {
  const response = await render("/download/android");
  assert.equal(response.status, 200);

  const html = await response.text();
  assert.match(html, /SpeakUp for Android/);
  assert.match(html, /当前还没有可公开下载和验证的 APK/);
  assert.match(html, /APK 文件 SHA-256/);
  assert.match(html, /签名证书 SHA-256/);
  assert.match(html, /更新日志/);
  assert.match(html, /隐私与权限/);
  assert.match(html, /问题反馈/);
  assert.match(html, /独立更新日志尚未提供/);
  assert.match(html, /正式隐私说明与权限清单尚未提供/);
  assert.match(html, /正式反馈入口尚未提供/);
  assert.doesNotMatch(html, /开放下载前公开/);
  assert.doesNotMatch(html, /安装步骤与更新记录/);
  assert.match(html, /安装未知应用/);
  assert.doesNotMatch(html, /\.apk(?:"|\?)/);
  assert.doesNotMatch(html, /versionName:\s*\d/);
});

test("renders the password-gated portal admin page", async () => {
  const response = await render("/admin");
  assert.equal(response.status, 200);
  assert.match(await response.text(), /首批体验 · 内部看板/);
});
