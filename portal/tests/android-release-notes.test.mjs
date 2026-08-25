import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  androidReleaseNotesPath,
  loadAndroidReleaseNotes,
  matchAndroidReleaseNotes,
  parseAndroidReleaseNotes,
} from "../lib/android-release-notes.mjs";

const release = {
  version: "0.1.7",
  published_at: "2026-08-25T14:05:30Z",
};

function validNotes() {
  return {
    release_notes_version: 1,
    locale: "zh-CN",
    version: "0.1.7",
    published_at: "2026-08-25T14:05:30Z",
    changes: [
      {
        type: "fix",
        text: "修复断网后实时语音无法再次录音的问题。",
      },
      {
        type: "fix",
        text: "修复语音反馈结果异常时可能重复失败的问题。",
      },
      {
        type: "improvement",
        text: "优化语音评测与文字反馈的重试等待，减少报告长时间卡住。",
      },
      {
        type: "fix",
        text: "修复进入历史练习时可能自动触发新回复的问题。",
      },
    ],
  };
}

test("publishes an honest v0.1.7 user-facing release note", () => {
  const notes = JSON.parse(
    readFileSync(
      new URL(
        "../public/release-notes/android/v0.1.7.zh-CN.json",
        import.meta.url,
      ),
      "utf8",
    ),
  );

  assert.equal(matchAndroidReleaseNotes(release, notes), notes);
  assert.deepEqual(notes.changes, [
    {
      type: "fix",
      text: "修复断网后实时语音无法再次录音的问题。",
    },
    {
      type: "fix",
      text: "修复语音反馈结果异常时可能重复失败的问题。",
    },
    {
      type: "improvement",
      text: "优化语音评测与文字反馈的重试等待，减少报告长时间卡住。",
    },
    {
      type: "fix",
      text: "修复进入历史练习时可能自动触发新回复的问题。",
    },
  ]);
});

test("parses strict, single-version Android release notes", () => {
  const notes = validNotes();

  assert.equal(parseAndroidReleaseNotes(notes), notes);
  assert.equal(
    androidReleaseNotesPath("0.1.7"),
    "/release-notes/android/v0.1.7.zh-CN.json",
  );
  assert.equal(matchAndroidReleaseNotes(release, notes), notes);
});

test("rejects malformed Android release notes", () => {
  const invalidCases = [
    { ...validNotes(), unexpected: true },
    { ...validNotes(), locale: "en-US" },
    { ...validNotes(), published_at: "2026-02-30T10:00:00Z" },
    {
      ...validNotes(),
      changes: [{ type: "internal", text: "重构内部模块。" }],
    },
    {
      ...validNotes(),
      changes: [{ type: "fix", text: " 修复问题。" }],
    },
    {
      ...validNotes(),
      changes: [
        ...validNotes().changes,
        {
          type: "improvement",
          text: "修复断网后实时语音无法再次录音的问题。",
        },
      ],
    },
  ];

  for (const notes of invalidCases) {
    assert.throws(
      () => parseAndroidReleaseNotes(notes),
      /Android release notes are invalid/,
    );
  }
});

test("requires current notes to match production version and publication time", () => {
  assert.throws(
    () => matchAndroidReleaseNotes({ ...release, version: "0.1.6" }, validNotes()),
    /Android release notes are invalid/,
  );
  assert.throws(
    () =>
      matchAndroidReleaseNotes(
        { ...release, published_at: "2026-08-25T14:06:00Z" },
        validNotes(),
      ),
    /Android release notes are invalid/,
  );
});

test("loads only the versioned notes for the active production release", async () => {
  const requests = [];
  const ready = await loadAndroidReleaseNotes(release, async (url, options) => {
    requests.push({ url, options });
    return new Response(JSON.stringify(validNotes()), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  });

  assert.equal(ready.status, "ready");
  assert.deepEqual(requests, [
    {
      url: "/release-notes/android/v0.1.7.zh-CN.json",
      options: {
        cache: "force-cache",
        headers: { accept: "application/json" },
      },
    },
  ]);

  const unavailable = await loadAndroidReleaseNotes(release, async () =>
    new Response("Not found", { status: 404 }),
  );
  assert.deepEqual(unavailable, { status: "unavailable" });
});
