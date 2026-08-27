import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  androidReleaseNotesIndexPath,
  androidReleaseNotesPath,
  loadAndroidReleaseHistory,
  matchAndroidReleaseNotes,
  matchAndroidReleaseNotesHistory,
  parseAndroidReleaseNotes,
  parseAndroidReleaseNotesIndex,
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

function validLegacyNotes() {
  return {
    release_notes_version: 1,
    locale: "zh-CN",
    version: "0.1.4",
    published_at: "2026-08-23T20:16:29Z",
    changes: [
      {
        type: "fix",
        text: "修复部分情况下实时语音识别无法返回文字结果的问题。",
      },
    ],
  };
}

function validIndex() {
  return {
    release_notes_index_version: 1,
    locale: "zh-CN",
    versions: ["0.1.7", "0.1.4"],
  };
}

function jsonResponse(value, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "content-type": "application/json" },
  });
}

test("publishes an honest Android release history", () => {
  const index = JSON.parse(
    readFileSync(
      new URL(
        "../public/release-notes/android/index.zh-CN.json",
        import.meta.url,
      ),
      "utf8",
    ),
  );
  const notes = index.versions.map((version) =>
    JSON.parse(
      readFileSync(
        new URL(
          `../public/release-notes/android/v${version}.zh-CN.json`,
          import.meta.url,
        ),
        "utf8",
      ),
    ),
  );

  assert.equal(parseAndroidReleaseNotesIndex(index), index);
  assert.deepEqual(index.versions, ["0.1.9", "0.1.8", "0.1.7", "0.1.4"]);
  const currentRelease = {
    version: notes[0].version,
    published_at: notes[0].published_at,
  };
  assert.deepEqual(
    matchAndroidReleaseNotesHistory(currentRelease, index, notes),
    notes,
  );
  assert.deepEqual(notes[0].changes, [
    {
      type: "feature",
      text: "新增“关于 SpeakUp”页面，可查看当前安装版本、检查更新、访问产品官网和开源许可。",
    },
    {
      type: "improvement",
      text: "精简个人页入口与视觉层级，让记忆、能力、关于和退出登录更容易辨认。",
    },
    {
      type: "improvement",
      text: "优化产品官网首页展示，直接呈现练习与复盘流程，并简化客户端下载入口。",
    },
  ]);
  assert.deepEqual(notes[1].changes, [
    {
      type: "feature",
      text: "在日常、职场、面试和 IELTS 练习中增加逐句纠错、润色与原声播放。",
    },
    {
      type: "fix",
      text: "修复 IELTS Speaking Part 2 说明阶段提前播放题目或出现双路语音的问题。",
    },
    {
      type: "fix",
      text: "修复未录到声音后重新录音可能无法发送或取消的问题。",
    },
    {
      type: "improvement",
      text: "优化 Android 底部导航选中状态、首页快捷入口布局和用户消息样式。",
    },
  ]);
  assert.deepEqual(notes[2].changes, [
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
  assert.deepEqual(notes[3], validLegacyNotes());
});

test("parses strict Android release notes and their ordered index", () => {
  const notes = validNotes();
  const index = validIndex();

  assert.equal(parseAndroidReleaseNotes(notes), notes);
  assert.equal(parseAndroidReleaseNotesIndex(index), index);
  assert.equal(
    androidReleaseNotesIndexPath,
    "/release-notes/android/index.zh-CN.json",
  );
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

test("rejects malformed or unordered release note indexes", () => {
  const invalidCases = [
    { ...validIndex(), unexpected: true },
    { ...validIndex(), release_notes_index_version: 2 },
    { ...validIndex(), locale: "en-US" },
    { ...validIndex(), versions: [] },
    { ...validIndex(), versions: ["0.1.7", "0.1.7"] },
    { ...validIndex(), versions: ["0.1.4", "0.1.7"] },
    { ...validIndex(), versions: ["v0.1.7", "0.1.4"] },
  ];

  for (const index of invalidCases) {
    assert.throws(
      () => parseAndroidReleaseNotesIndex(index),
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

test("matches the current release to a complete descending history", () => {
  const notes = [validNotes(), validLegacyNotes()];
  assert.deepEqual(
    matchAndroidReleaseNotesHistory(release, validIndex(), notes),
    notes,
  );

  const futureIndex = {
    ...validIndex(),
    versions: ["0.1.8", "0.1.7", "0.1.4"],
  };
  assert.deepEqual(
    matchAndroidReleaseNotesHistory(release, futureIndex, notes),
    notes,
  );

  assert.throws(
    () =>
      matchAndroidReleaseNotesHistory(release, validIndex(), [
        validNotes(),
        { ...validLegacyNotes(), version: "0.1.3" },
      ]),
    /Android release notes are invalid/,
  );
  assert.throws(
    () =>
      matchAndroidReleaseNotesHistory(release, validIndex(), [
        validNotes(),
        {
          ...validLegacyNotes(),
          published_at: "2026-08-25T15:05:30Z",
        },
      ]),
    /Android release notes are invalid/,
  );
});

test("loads visible release notes in parallel without exposing a future version", async () => {
  const requests = [];
  const noteResolvers = new Map();
  const futureIndex = {
    ...validIndex(),
    versions: ["0.1.8", "0.1.7", "0.1.4"],
  };
  const loading = loadAndroidReleaseHistory(release, async (url, options) => {
    requests.push({ url, options });
    if (url === androidReleaseNotesIndexPath) return jsonResponse(futureIndex);
    if (url === androidReleaseNotesPath("0.1.7")) {
      return new Promise((resolve) => {
        noteResolvers.set(url, () => resolve(jsonResponse(validNotes())));
      });
    }
    if (url === androidReleaseNotesPath("0.1.4")) {
      return new Promise((resolve) => {
        noteResolvers.set(url, () => resolve(jsonResponse(validLegacyNotes())));
      });
    }
    throw new Error(`Unexpected release note request: ${url}`);
  });

  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(noteResolvers.size, 2);
  for (const resolve of noteResolvers.values()) resolve();

  const ready = await loading;
  assert.equal(ready.status, "ready");
  assert.deepEqual(ready.notes, [validNotes(), validLegacyNotes()]);
  assert.deepEqual(requests, [
    {
      url: "/release-notes/android/index.zh-CN.json",
      options: {
        cache: "no-store",
        headers: { accept: "application/json" },
      },
    },
    {
      url: "/release-notes/android/v0.1.7.zh-CN.json",
      options: {
        cache: "force-cache",
        headers: { accept: "application/json" },
      },
    },
    {
      url: "/release-notes/android/v0.1.4.zh-CN.json",
      options: {
        cache: "force-cache",
        headers: { accept: "application/json" },
      },
    },
  ]);
});

test("reports unavailable instead of rendering an incomplete history", async () => {
  const missingIndex = await loadAndroidReleaseHistory(release, async () =>
    new Response("Not found", { status: 404 }),
  );
  assert.deepEqual(missingIndex, { status: "unavailable" });

  const missingHistory = await loadAndroidReleaseHistory(
    release,
    async (url) => {
      if (url === androidReleaseNotesIndexPath) return jsonResponse(validIndex());
      if (url === androidReleaseNotesPath("0.1.7")) {
        return jsonResponse(validNotes());
      }
      return new Response("Not found", { status: 404 });
    },
  );
  assert.deepEqual(missingHistory, { status: "unavailable" });

  const currentMissing = await loadAndroidReleaseHistory(
    { ...release, version: "0.1.6" },
    async () => jsonResponse(validIndex()),
  );
  assert.deepEqual(currentMissing, { status: "unavailable" });
});
