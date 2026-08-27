#!/usr/bin/env node

import { lstatSync, readFileSync, writeFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
import {
  parseAndroidReleaseNotes,
  parseAndroidReleaseNotesIndex,
} from "../../portal/lib/android-release-notes.mjs";

const headings = new Map([
  ["feature", "新功能"],
  ["improvement", "体验优化"],
  ["fix", "问题修复"],
  ["compatibility", "兼容性"],
  ["known_limitation", "已知限制"],
]);

function fail(message) {
  throw new Error(message);
}

function readJson(file, name) {
  let status;
  try {
    status = lstatSync(file);
  } catch {
    fail(`${name} does not exist`);
  }
  if (status.isSymbolicLink() || !status.isFile() || status.size < 1) {
    fail(`${name} must be a non-empty regular file`);
  }
  try {
    return JSON.parse(readFileSync(file, "utf8"));
  } catch {
    fail(`${name} is not valid JSON`);
  }
}

export function finalizeReleaseNotes({ version, notes, index }) {
  const parsedNotes = parseAndroidReleaseNotes(notes);
  const parsedIndex = parseAndroidReleaseNotesIndex(index);
  if (parsedNotes.version !== version || parsedIndex.versions[0] !== version) {
    fail("current Android release notes do not match the Candidate version");
  }

  const changes = new Map();
  for (const change of parsedNotes.changes) {
    if (!changes.has(change.type)) changes.set(change.type, []);
    changes.get(change.type).push(change.text);
  }

  const lines = [
    `# SpeakUp Android v${version}`,
    "",
    `发布时间：${parsedNotes.published_at}`,
  ];
  for (const [type, heading] of headings) {
    const entries = changes.get(type);
    if (!entries) continue;
    lines.push("", `## ${heading}`, "", ...entries.map((text) => `- ${text}`));
  }
  lines.push("", "## 下载校验", "", "本 Release 附带正式签名 APK、SHA-256 校验文件和发布审计制品。", "");
  return { body: lines.join("\n"), publishedAt: parsedNotes.published_at };
}

function parseArguments(arguments_) {
  const values = new Map();
  for (let index = 0; index < arguments_.length; index += 2) {
    const name = arguments_[index];
    const value = arguments_[index + 1];
    if (!name?.startsWith("--") || value === undefined || values.has(name)) {
      fail("invalid arguments");
    }
    values.set(name, value);
  }
  const expected = ["--index", "--notes", "--output", "--version"];
  if (
    values.size !== expected.length ||
    expected.some((name) => !values.has(name))
  ) {
    fail("required arguments: --version --notes --index --output");
  }
  return values;
}

function main() {
  const values = parseArguments(process.argv.slice(2));
  const result = finalizeReleaseNotes({
    version: values.get("--version"),
    notes: readJson(values.get("--notes"), "release notes"),
    index: readJson(values.get("--index"), "release notes index"),
  });
  writeFileSync(values.get("--output"), result.body, { flag: "wx", mode: 0o600 });
  process.stdout.write(`${result.publishedAt}\n`);
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    main();
  } catch (error) {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  }
}
