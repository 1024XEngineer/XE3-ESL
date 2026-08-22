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
  assert.match(html, /下一场重要的英文沟通，先和 SpeakUp 练一遍/);
  assert.match(html, /有记忆的 AI 口语老师/);
  assert.match(html, /portal-interview-practice\.png/);
  assert.match(html, /href="\/download\/android"/);
  assert.match(html, /首个 APK 正在准备/);
  assert.doesNotMatch(html, /portal-memory-chat\.jpg/);
  assert.doesNotMatch(html, /assets\/practice-screen\//);
  assert.doesNotMatch(html, /latest\.apk/);
  assert.doesNotMatch(html, /id="early-access"/);
  assert.doesNotMatch(html, /href="\/pages\/prototype\.html/);
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
  assert.match(html, /安装未知应用/);
  assert.doesNotMatch(html, /\.apk(?:"|\?)/);
  assert.doesNotMatch(html, /versionName:\s*\d/);
});

test("renders the password-gated portal admin page", async () => {
  const response = await render("/admin");
  assert.equal(response.status, 200);
  assert.match(await response.text(), /首批体验 · 内部看板/);
});
