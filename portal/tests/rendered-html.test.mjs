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
  assert.doesNotMatch(html, /href="#method"[^>]*>怎么练<\/a>/);
  assert.match(
    html,
    /<section class="release-method" id="method"[^>]*><div class="release-section-heading"><h2 id="method-title">/,
  );
  assert.doesNotMatch(html, /href="#memory"[^>]*>长期记忆<\/a>/);
  assert.match(
    html,
    /<section class="release-memory" id="memory"[^>]*><div><h2 id="memory-title">/,
  );
  assert.match(html, /aria-roledescription="轮播图"/);
  assert.match(html, /interview-entry\.webp/);
  assert.match(html, /interview-chat\.webp/);
  assert.match(html, /ielts-review\.webp/);
  assert.match(html, /practice-progress\.webp/);
  assert.doesNotMatch(html, /aria-label="上一张产品截图"/);
  assert.doesNotMatch(html, /aria-label="下一张产品截图"/);
  assert.match(
    html,
    /<div class="hero-product-carousel__progress" aria-hidden="true"><span/,
  );
  assert.doesNotMatch(
    html,
    /<div class="hero-product-carousel__progress"[^>]*><button/,
  );
  assert.match(html, /aria-label="暂停自动播放"/);
  assert.match(html, /场景化专项练习/);
  assert.doesNotMatch(html, /portal-interview-practice\.png/);
  assert.match(html, /speak-up-wordmark-black\.png/);
  assert.match(html, /speakup-mark\.svg/);
  assert.doesNotMatch(html, /speakup-mascot-blue/);
  assert.match(html, /href="\/changelog"[^>]*>更新日志<\/a>/);
  assert.match(
    html,
    /href="https:\/\/github\.com\/1024XEngineer\/XE3-ESL"[^>]*target="_blank"[^>]*rel="noopener noreferrer"[^>]*>GitHub <span aria-hidden="true">↗<\/span><\/a>/,
  );
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
  assert.match(html, /href="\/changelog"[^>]*>查看正式版本更新记录/);
  assert.match(html, /正式隐私说明与权限清单尚未提供/);
  assert.match(html, /正式反馈入口尚未提供/);
  assert.doesNotMatch(html, /开放下载前公开/);
  assert.doesNotMatch(html, /安装步骤与更新记录/);
  assert.match(html, /安装未知应用/);
  assert.doesNotMatch(html, /\.apk(?:"|\?)/);
  assert.doesNotMatch(html, /versionName:\s*\d/);
});

test("renders the user-facing changelog without guessing a release", async () => {
  const response = await render("/changelog");
  assert.equal(response.status, 200);

  const html = await response.text();
  assert.match(html, /<title>更新日志 · SpeakUp<\/title>/);
  assert.match(html, /<h1>更新日志<\/h1>/);
  assert.match(html, /记录 SpeakUp 已正式发布的功能、体验优化与问题修复/);
  assert.match(html, /正在确认当前正式版本/);
  assert.match(html, /href="\/changelog"[^>]*aria-current="page"/);
  assert.doesNotMatch(html, /v0\.1\.4/);
  assert.doesNotMatch(html, /实时语音识别无法返回文字结果/);
});

test("renders the password-gated portal admin page", async () => {
  const response = await render("/admin");
  assert.equal(response.status, 200);
  assert.match(await response.text(), /首批体验 · 内部看板/);
});
