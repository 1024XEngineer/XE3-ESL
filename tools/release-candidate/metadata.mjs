#!/usr/bin/env node

import { execFileSync, spawnSync } from "node:child_process";
import { appendFileSync } from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

const stableTagPattern = /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const pubspecVersionPattern = /^version:\s*["']?([^+\s"']+)\+(\d+)["']?\s*$/gm;
const migrationPattern = /^server\/migrations\/(\d{6})_[^/]+\.up\.sql$/;

export function parseStableTag(tag) {
  const match = stableTagPattern.exec(tag);
  if (!match) {
    throw new Error(`Release tag must use vX.Y.Z: ${tag}`);
  }
  const version = match.slice(1).map(Number);
  if (!version.every(Number.isSafeInteger)) {
    throw new Error(`Release tag contains an unsupported numeric component: ${tag}`);
  }
  return version;
}

export function parsePubspecVersion(content) {
  const matches = [...content.matchAll(pubspecVersionPattern)];
  if (matches.length !== 1) {
    throw new Error(
      "mobile/pubspec.yaml must contain exactly one versionName+versionCode",
    );
  }
  const match = matches[0];
  return { version: match[1], versionCode: Number(match[2]) };
}

export function databaseSchemaVersion(files) {
  const versions = [];
  const seenVersions = new Set();
  for (const file of files.filter((candidate) => candidate.endsWith(".up.sql"))) {
    const match = migrationPattern.exec(file);
    if (!match) {
      throw new Error(`Invalid server migration filename: ${file}`);
    }
    const version = Number(match[1]);
    if (seenVersions.has(version)) {
      throw new Error(`Duplicate server migration version: ${match[1]}`);
    }
    seenVersions.add(version);
    versions.push(version);
  }
  if (versions.length === 0) {
    throw new Error("No server migration .up.sql files were found");
  }
  return Math.max(...versions);
}

function compareVersion(left, right) {
  for (let index = 0; index < 3; index += 1) {
    if (left[index] !== right[index]) return left[index] - right[index];
  }
  return 0;
}

function validateVersionCode(versionCode, source) {
  if (
    !Number.isSafeInteger(versionCode) ||
    versionCode < 1 ||
    versionCode > 2_100_000_000
  ) {
    throw new Error(`${source} versionCode must be between 1 and 2100000000`);
  }
}

function git(repoDir, args) {
  return execFileSync("git", args, {
    cwd: repoDir,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
}

function isAncestor(repoDir, commit, mainRef) {
  return spawnSync("git", ["merge-base", "--is-ancestor", commit, mainRef], {
    cwd: repoDir,
    stdio: "ignore",
  }).status === 0;
}

function stableTagRefs(repoDir) {
  const local = new Map(
    git(repoDir, ["tag", "--list", "v*"])
      .split("\n")
      .filter((tag) => stableTagPattern.test(tag))
      .map((tag) => [
        tag,
        git(repoDir, ["rev-parse", "--verify", `refs/tags/${tag}`]),
      ]),
  );
  const remote = new Map(
    git(repoDir, ["ls-remote", "--tags", "--refs", "origin", "refs/tags/v*"])
      .split("\n")
      .filter(Boolean)
      .map((line) => line.split("\t"))
      .map(([object, ref]) => [ref.replace("refs/tags/", ""), object])
      .filter(([tag]) => stableTagPattern.test(tag)),
  );
  if (
    local.size !== remote.size ||
    [...remote].some(([tag, object]) => local.get(tag) !== object)
  ) {
    throw new Error("Local stable release tags do not match origin");
  }
  return [...local.keys()];
}

export function collectReleaseMetadata({ repoDir, tag, mainRef }) {
  if (git(repoDir, ["rev-parse", "--is-shallow-repository"]) === "true") {
    throw new Error("Release validation requires the complete Git history");
  }
  parseStableTag(tag);
  const tagCommit = git(repoDir, [
    "rev-parse",
    "--verify",
    `refs/tags/${tag}^{commit}`,
  ]);
  const headCommit = git(repoDir, ["rev-parse", "--verify", "HEAD^{commit}"]);
  if (headCommit !== tagCommit) {
    throw new Error(`HEAD ${headCommit} does not match release tag ${tag}`);
  }
  const mainCommit = git(repoDir, ["rev-parse", "--verify", `${mainRef}^{commit}`]);
  if (!isAncestor(repoDir, tagCommit, mainCommit)) {
    throw new Error(`Release tag ${tag} is not contained in ${mainRef}`);
  }
  const mainHistory = git(repoDir, [
    "rev-list",
    "--first-parent",
    "--reverse",
    mainCommit,
  ]).split("\n");
  const firstParentCommits = new Set(mainHistory);
  if (!firstParentCommits.has(tagCommit)) {
    throw new Error(`Release tag ${tag} is not on the first-parent history of ${mainRef}`);
  }
  const allStableTags = stableTagRefs(repoDir);

  const pubspec = git(repoDir, ["show", `${tagCommit}:mobile/pubspec.yaml`]);
  const current = parsePubspecVersion(pubspec);
  if (`v${current.version}` !== tag) {
    throw new Error(`Release tag ${tag} does not match versionName ${current.version}`);
  }
  validateVersionCode(current.versionCode, "Current release");

  const releasesOnMain = allStableTags
    .map((candidate) => ({
      tag: candidate,
      commit: git(repoDir, [
        "rev-parse",
        "--verify",
        `refs/tags/${candidate}^{commit}`,
      ]),
      version: parseStableTag(candidate),
    }))
    .filter((candidate) => isAncestor(repoDir, candidate.commit, mainCommit));

  const sideHistoryTag = releasesOnMain.find(
    (candidate) => !firstParentCommits.has(candidate.commit),
  );
  if (sideHistoryTag) {
    throw new Error(
      `Previous release tag ${sideHistoryTag.tag} is not on the first-parent history of ${mainRef}`,
    );
  }
  const historyOrder = new Map(
    mainHistory.map((commit, index) => [commit, index]),
  );
  const releases = releasesOnMain.map((candidate) => {
    let version = current;
    if (candidate.tag !== tag) {
      version = parsePubspecVersion(
        git(repoDir, [
          "show",
          `refs/tags/${candidate.tag}^{commit}:mobile/pubspec.yaml`,
        ]),
      );
    }
    if (`v${version.version}` !== candidate.tag) {
      throw new Error(
        `Previous release tag ${candidate.tag} does not match versionName ${version.version}`,
      );
    }
    validateVersionCode(version.versionCode, candidate.tag);
    return { ...candidate, versionCode: version.versionCode };
  }).sort(
    (left, right) => historyOrder.get(left.commit) - historyOrder.get(right.commit),
  );
  for (let index = 1; index < releases.length; index += 1) {
    const previous = releases[index - 1];
    const release = releases[index];
    if (release.commit === previous.commit) {
      throw new Error(`Stable release Tags ${previous.tag} and ${release.tag} share a commit`);
    }
    if (compareVersion(release.version, previous.version) <= 0) {
      throw new Error(`Release version ${release.tag} must be newer than ${previous.tag}`);
    }
    if (release.versionCode <= previous.versionCode) {
      throw new Error(
        `versionCode ${release.versionCode} must be greater than ` +
          `${previous.versionCode} from ${previous.tag}`,
      );
    }
  }

  const migrationFiles = git(repoDir, [
    "ls-tree",
    "-r",
    "--name-only",
    tagCommit,
    "--",
    "server/migrations",
  ]).split("\n");

  return {
    tag,
    version: current.version,
    version_code: String(current.versionCode),
    git_sha: tagCommit,
    database_schema_version: String(databaseSchemaVersion(migrationFiles)),
  };
}

function readArguments(arguments_) {
  const values = {};
  for (let index = 0; index < arguments_.length; index += 2) {
    const flag = arguments_[index];
    const value = arguments_[index + 1];
    if (!flag?.startsWith("--") || value === undefined) {
      throw new Error("Usage: metadata.mjs --tag <vX.Y.Z> --main-ref <ref> [--repo <path>]");
    }
    values[flag.slice(2)] = value;
  }
  return values;
}

function main() {
  const arguments_ = readArguments(process.argv.slice(2));
  if (!arguments_.tag || !arguments_["main-ref"]) {
    throw new Error("Both --tag and --main-ref are required");
  }
  const metadata = collectReleaseMetadata({
    repoDir: path.resolve(arguments_.repo ?? "."),
    tag: arguments_.tag,
    mainRef: arguments_["main-ref"],
  });
  const output = Object.entries(metadata)
    .map(([key, value]) => `${key}=${value}`)
    .join("\n");
  console.log(output);
  if (process.env.GITHUB_OUTPUT) {
    appendFileSync(process.env.GITHUB_OUTPUT, `${output}\n`, "utf8");
  }
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  try {
    main();
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}
