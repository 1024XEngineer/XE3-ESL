import assert from "node:assert/strict";
import test from "node:test";
import { finalizeReleaseNotes } from "./notes.mjs";

const notes = {
  release_notes_version: 1,
  locale: "zh-CN",
  version: "0.1.8",
  published_at: "2026-08-27T10:00:00Z",
  changes: [
    { type: "fix", text: "修复报告生成失败的问题。" },
    { type: "improvement", text: "缩短报告等待时间。" },
  ],
};
const index = {
  release_notes_index_version: 1,
  locale: "zh-CN",
  versions: ["0.1.8", "0.1.7"],
};

test("renders deterministic release notes for the current indexed version", () => {
  const result = finalizeReleaseNotes({ version: "0.1.8", notes, index });
  assert.equal(result.publishedAt, notes.published_at);
  assert.match(result.body, /^# SpeakUp Android v0\.1\.8/m);
  assert.match(result.body, /## 体验优化\n\n- 缩短报告等待时间。/);
  assert.match(result.body, /## 问题修复\n\n- 修复报告生成失败的问题。/);
  assert.ok(result.body.indexOf("体验优化") < result.body.indexOf("问题修复"));
});

test("rejects notes that are not the current indexed version", () => {
  assert.throws(
    () =>
      finalizeReleaseNotes({
        version: "0.1.8",
        notes,
        index: { ...index, versions: ["0.1.9", "0.1.8"] },
      }),
    /do not match the Candidate version/,
  );
});
