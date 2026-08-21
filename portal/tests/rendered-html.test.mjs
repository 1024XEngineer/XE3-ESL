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
  assert.match(html, /下一场重要的英文沟通/);
  assert.match(html, /一个有记忆、越用越懂你的 AI 口语老师/);
  assert.match(html, /portal-interview-practice\.png/);
  assert.match(html, /项目经历深挖/);
  assert.match(html, /你要面对什么/);
  assert.match(html, /assets\/practice-screen\/ielts\.webp/);
  assert.match(html, /assets\/practice-screen\/workplace\.webp/);
  assert.doesNotMatch(html, /不只是一场面试/);
  assert.doesNotMatch(html, /准备下一次口语考试/);
  assert.match(html, /portal-memory-chat\.jpg/);
  assert.match(html, /id="early-access"/);
  assert.doesNotMatch(html, /href="\/pages\/prototype\.html/);
});

test("renders the password-gated portal admin page", async () => {
  const response = await render("/admin");
  assert.equal(response.status, 200);
  assert.match(await response.text(), /首批体验 · 内部看板/);
});
