import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
  chmodSync,
  existsSync,
  lstatSync,
  mkdtempSync,
  mkdirSync,
  readFileSync,
  readlinkSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  createReleaseManifest,
  offlineBundleImageInput,
} from "./manifest.mjs";
import {
  generateOfflineManifest,
  validateQualityRun,
} from "./offline-manifest.mjs";
import {
  collectCandidateMetadata,
  collectReleaseMetadata,
  databaseSchemaVersion,
  parseStableTag,
} from "./metadata.mjs";

const manifestScript = fileURLToPath(new URL("./manifest.mjs", import.meta.url));
const metadataScript = fileURLToPath(new URL("./metadata.mjs", import.meta.url));
const offlineManifestScript = fileURLToPath(
  new URL("./offline-manifest.mjs", import.meta.url),
);
const releaseCandidateWorkflow = fileURLToPath(
  new URL("../../.github/workflows/release-candidate.yml", import.meta.url),
);
const stagingDeployWorkflow = fileURLToPath(
  new URL("../../.github/workflows/staging-deploy.yml", import.meta.url),
);
const officialUpstreamUrl = "https://github.com/1024XEngineer/XE3-ESL.git";
const officialRepository = "1024XEngineer/XE3-ESL";
const qualityWorkflowPath = ".github/workflows/quality.yml";
const githubApiVersion = "2026-03-10";
const realGit = process.env.PATH
  .split(path.delimiter)
  .map((directory) => path.join(directory, "git"))
  .find(existsSync);

function git(repo, ...arguments_) {
  return execFileSync("git", arguments_, {
    cwd: repo,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
}

function writeReleaseFiles(repo, version) {
  mkdirSync(path.join(repo, "mobile"), { recursive: true });
  mkdirSync(path.join(repo, "server", "migrations"), { recursive: true });
  writeFileSync(path.join(repo, "mobile", "pubspec.yaml"), `name: speakup\nversion: ${version}\n`);
  writeFileSync(path.join(repo, "server", "migrations", "000001_baseline.up.sql"), "SELECT 1;\n");
  writeFileSync(path.join(repo, "server", "migrations", "000007_current.up.sql"), "SELECT 1;\n");
}

function createRepository(version = "0.1.0+1") {
  const repo = mkdtempSync(path.join(tmpdir(), "speakup-release-"));
  git(repo, "init", "--initial-branch=main");
  git(repo, "config", "user.name", "Release Test");
  git(repo, "config", "user.email", "release-test@example.invalid");
  writeReleaseFiles(repo, version);
  git(repo, "add", ".");
  git(repo, "commit", "-m", "initial release");
  git(repo, "remote", "add", "origin", repo);
  return repo;
}

function commitVersion(repo, version, message = `prepare ${version}`) {
  writeReleaseFiles(repo, version);
  git(repo, "add", ".");
  git(repo, "commit", "-m", message);
  return git(repo, "rev-parse", "HEAD");
}

function configureOfficialUpstream(repo) {
  git(repo, "remote", "add", "upstream", officialUpstreamUrl);
  git(repo, "update-ref", "refs/remotes/upstream/main", "HEAD");
}

function officialGitProxy() {
  assert.ok(realGit, "git executable is required");
  const directory = mkdtempSync(path.join(tmpdir(), "speakup-git-proxy-"));
  const script = path.join(directory, "git");
  writeFileSync(script, `#!/bin/sh
if [ "$1" = ls-remote ] && [ "$4" = upstream ]; then
  exec "$SPEAKUP_TEST_REAL_GIT" "$1" "$2" "$3" \\
    "$SPEAKUP_TEST_REMOTE_REPO" "$5"
fi
exec "$SPEAKUP_TEST_REAL_GIT" "$@"
`);
  chmodSync(script, 0o755);
  return directory;
}

function withOfficialGitProxy(repo, callback) {
  const proxy = officialGitProxy();
  const previous = {
    PATH: process.env.PATH,
    realGit: process.env.SPEAKUP_TEST_REAL_GIT,
    remoteRepo: process.env.SPEAKUP_TEST_REMOTE_REPO,
  };
  process.env.PATH = `${proxy}:${process.env.PATH}`;
  process.env.SPEAKUP_TEST_REAL_GIT = realGit;
  process.env.SPEAKUP_TEST_REMOTE_REPO = repo;
  try {
    return callback();
  } finally {
    process.env.PATH = previous.PATH;
    if (previous.realGit === undefined) {
      delete process.env.SPEAKUP_TEST_REAL_GIT;
    } else {
      process.env.SPEAKUP_TEST_REAL_GIT = previous.realGit;
    }
    if (previous.remoteRepo === undefined) {
      delete process.env.SPEAKUP_TEST_REMOTE_REPO;
    } else {
      process.env.SPEAKUP_TEST_REMOTE_REPO = previous.remoteRepo;
    }
  }
}

async function withEnvironment(values, callback) {
  const previous = Object.fromEntries(
    Object.keys(values).map((name) => [name, process.env[name]]),
  );
  for (const [name, value] of Object.entries(values)) {
    process.env[name] = value;
  }
  try {
    return await callback();
  } finally {
    for (const [name, value] of Object.entries(previous)) {
      if (value === undefined) {
        delete process.env[name];
      } else {
        process.env[name] = value;
      }
    }
  }
}

async function withOfflineWrapperTools(repo, androidTools, extra, callback) {
  const gitProxy = officialGitProxy();
  return withEnvironment(
    {
      PATH: `${gitProxy}:${androidTools}:${process.env.PATH}`,
      SPEAKUP_TEST_REAL_GIT: realGit,
      SPEAKUP_TEST_REMOTE_REPO: repo,
      ...extra,
    },
    callback,
  );
}

function fakeAndroidTools(certificate, { failProduction = false } = {}) {
  const directory = mkdtempSync(path.join(tmpdir(), "speakup-android-tools-"));
  const tools = {
    aapt: `#!/bin/sh
cat <<'EOF'
package: name='com.xengineer.speakup' versionCode='1' versionName='0.1.0'
sdkVersion:'24'
native-code: 'arm64-v8a'
EOF
`,
    apksigner: `#!/bin/sh
last=''
for argument do last=$argument; done
${failProduction ? `case "$last" in
  *-production-arm64.apk) printf '%s\\n' 'production signature failure' >&2; exit 1 ;;
esac
` : ""}printf '%s\n' \\
  'Signer #1 certificate DN: CN=SpeakUp Release' \\
  'Signer #1 certificate SHA-256 digest: ${certificate}'
`,
    java: "#!/bin/sh\nexit 0\n",
  };
  for (const [name, content] of Object.entries(tools)) {
    const file = path.join(directory, name);
    writeFileSync(file, content);
    chmodSync(file, 0o755);
  }
  return directory;
}

function validManifestFixture() {
  const artifacts = mkdtempSync(path.join(tmpdir(), "speakup-artifacts-"));
  const stagingApk = path.join(artifacts, "speakup-v0.1.0-staging-arm64.apk");
  const productionApk = path.join(artifacts, "speakup-v0.1.0-production-arm64.apk");
  writeFileSync(stagingApk, "staging apk");
  writeFileSync(productionApk, "production apk");
  return {
    input: {
      PORTAL_IMAGE: "ghcr.io/1024xengineer/xe3-esl-portal",
      PORTAL_IMAGE_DIGEST: `sha256:${"a".repeat(64)}`,
      SERVER_IMAGE: "ghcr.io/1024xengineer/xe3-esl-server",
      SERVER_IMAGE_DIGEST: `sha256:${"b".repeat(64)}`,
      STAGING_APK_PATH: stagingApk,
      PRODUCTION_APK_PATH: productionApk,
      STAGING_APK_APPLICATION_ID: "com.xengineer.speakup",
      STAGING_APK_MINIMUM_ANDROID_API: "24",
      STAGING_APK_ABIS: "arm64-v8a",
      PRODUCTION_APK_APPLICATION_ID: "com.xengineer.speakup",
      PRODUCTION_APK_MINIMUM_ANDROID_API: "24",
      PRODUCTION_APK_ABIS: "arm64-v8a",
      APK_CERTIFICATE_SHA256: "c".repeat(64),
      GITHUB_REPOSITORY: "1024XEngineer/XE3-ESL",
      QUALITY_RUN_URL: "https://github.com/1024XEngineer/XE3-ESL/actions/runs/1",
    },
    metadata: {
      version: "0.1.0",
      version_code: "1",
      git_sha: "d".repeat(40),
      database_schema_version: "7",
    },
    stagingApk,
  };
}

function sha256File(file) {
  return createHash("sha256").update(readFileSync(file)).digest("hex");
}

function writeJson(file, value) {
  writeFileSync(file, `${JSON.stringify(value, null, 2)}\n`);
}

function offlineBundleFixture(version, gitSha) {
  const directory = mkdtempSync(path.join(tmpdir(), "speakup-offline-bundle-"));
  const definitions = [
    {
      name: "portal",
      repository: "ghcr.io/1024xengineer/xe3-esl-portal",
      digest: `sha256:${"e".repeat(64)}`,
    },
    {
      name: "server",
      repository: "ghcr.io/1024xengineer/xe3-esl-server",
      digest: `sha256:${"f".repeat(64)}`,
    },
  ];
  const artifacts = definitions.map(({ name, repository, digest }) => {
    const archiveFile = `speakup-${name}-v${version}-linux-amd64.tar`;
    const metadataFile =
      `speakup-${name}-v${version}-linux-amd64-build-metadata.json`;
    const archivePath = path.join(directory, archiveFile);
    const metadataPath = path.join(directory, metadataFile);
    writeFileSync(archivePath, `${name} Docker archive fixture\n`);
    writeJson(metadataPath, { "containerimage.digest": digest });
    return {
      name,
      repository,
      digest,
      archive_file: archiveFile,
      archive_size_bytes: readFileSync(archivePath).length,
      archive_sha256: sha256File(archivePath),
      build_metadata_file: metadataFile,
      build_metadata_sha256: sha256File(metadataPath),
      archivePath,
      metadataPath,
    };
  });
  const bundle = {
    bundle_version: 1,
    version,
    git_sha: gitSha,
    source_date_epoch: 1760000000,
    platform: "linux/amd64",
    images: artifacts.map(({
      archivePath: _archivePath,
      metadataPath: _metadataPath,
      ...image
    }) => image),
  };
  const bundlePath = path.join(directory, "offline-image-bundle.json");
  writeJson(bundlePath, bundle);
  return { artifacts, bundle, bundlePath, directory };
}

function qualityApiFixture(gitSha, runId = 32_456_953_876) {
  const workflowId = 319_521_814;
  const qualityRunUrl =
    `https://github.com/${officialRepository}/actions/runs/${runId}`;
  const workflowApiUrl =
    `https://api.github.com/repos/${officialRepository}/actions/workflows/quality.yml`;
  const runApiUrl =
    `https://api.github.com/repos/${officialRepository}/actions/runs/${runId}`;
  const workflow = {
    id: workflowId,
    name: "Quality",
    path: qualityWorkflowPath,
    state: "active",
  };
  const run = {
    id: runId,
    workflow_id: workflowId,
    path: qualityWorkflowPath,
    head_sha: gitSha,
    head_branch: "main",
    event: "push",
    status: "completed",
    conclusion: "success",
    url: runApiUrl,
    html_url: qualityRunUrl,
    repository: { full_name: officialRepository },
    head_repository: { full_name: officialRepository },
  };
  const calls = [];
  const fetchImplementation = async (url, options) => {
    calls.push({ url, options });
    const value = url === workflowApiUrl
      ? workflow
      : url === runApiUrl
        ? run
        : undefined;
    return {
      ok: value !== undefined,
      status: value === undefined ? 404 : 200,
      async json() {
        return structuredClone(value);
      },
    };
  };
  return {
    calls,
    fetchImplementation,
    qualityRunUrl,
    run,
    runApiUrl,
    workflow,
    workflowApiUrl,
  };
}

function manifestArguments(repo, input, output) {
  return [
    manifestScript,
    "--tag",
    "v0.1.0",
    "--main-ref",
    "main",
    "--staging-apk",
    input.STAGING_APK_PATH,
    "--production-apk",
    input.PRODUCTION_APK_PATH,
    "--output",
    output,
    "--repo",
    repo,
  ];
}

function candidateManifestArguments(repo, input, output) {
  return [
    manifestScript,
    "--candidate-sha",
    git(repo, "rev-parse", "HEAD"),
    "--main-ref",
    "main",
    "--staging-apk",
    input.STAGING_APK_PATH,
    "--production-apk",
    input.PRODUCTION_APK_PATH,
    "--output",
    output,
    "--repo",
    repo,
  ];
}

function offlineManifestArguments(
  repo,
  input,
  bundle,
  certificateFile,
  qualityRunUrl,
  output,
) {
  return [
    "--tag",
    "v0.1.0",
    "--staging-apk",
    input.STAGING_APK_PATH,
    "--production-apk",
    input.PRODUCTION_APK_PATH,
    "--offline-bundle",
    bundle.bundlePath,
    "--certificate-file",
    certificateFile,
    "--quality-run-url",
    qualityRunUrl,
    "--output",
    output,
    "--repo",
    repo,
  ];
}

function runManifestCli(repo, input, output) {
  return execFileSync(
    process.execPath,
    manifestArguments(repo, input, output),
    { env: { ...process.env, ...input }, stdio: "pipe" },
  );
}

test("accepts the first stable release and ignores prerelease tags", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0-alpha.1");
  git(repo, "tag", "v0.1.0");

  assert.deepEqual(
    collectReleaseMetadata({ repoDir: repo, tag: "v0.1.0", mainRef: "main" }),
    {
      tag: "v0.1.0",
      version: "0.1.0",
      version_code: "1",
      git_sha: git(repo, "rev-parse", "HEAD"),
      database_schema_version: "7",
    },
  );
});

test("accepts an untagged Candidate from the exact main checkout", () => {
  const repo = createRepository();
  const candidateSha = git(repo, "rev-parse", "HEAD");
  const tagsBefore = git(repo, "tag", "--list");

  assert.deepEqual(
    collectCandidateMetadata({
      repoDir: repo,
      candidateSha,
      mainRef: "main",
    }),
    {
      version: "0.1.0",
      version_code: "1",
      git_sha: candidateSha,
      database_schema_version: "7",
    },
  );
  assert.equal(git(repo, "tag", "--list"), tagsBefore);
});

test("accepts a Candidate newer than the complete stable release history", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  const candidateSha = commitVersion(repo, "0.1.1+2");

  assert.equal(
    collectCandidateMetadata({ repoDir: repo, candidateSha, mainRef: "main" })
      .version,
    "0.1.1",
  );
});

test("rejects Candidate checkout drift, side history, and reused stable identity", () => {
  const drifted = createRepository();
  const oldSha = git(drifted, "rev-parse", "HEAD");
  assert.throws(
    () => collectCandidateMetadata({
      repoDir: drifted,
      candidateSha: "not-a-full-sha",
      mainRef: "main",
    }),
    /full lowercase Git commit SHA/,
  );
  commitVersion(drifted, "0.1.1+2");
  assert.throws(
    () => collectCandidateMetadata({
      repoDir: drifted,
      candidateSha: oldSha,
      mainRef: "main",
    }),
    /does not match Candidate/,
  );

  const sideHistory = createRepository();
  git(sideHistory, "checkout", "-b", "feature");
  const sideSha = commitVersion(sideHistory, "0.1.1+2");
  assert.throws(
    () => collectCandidateMetadata({
      repoDir: sideHistory,
      candidateSha: sideSha,
      mainRef: "main",
    }),
    /not contained in main/,
  );

  const tagged = createRepository();
  const taggedSha = git(tagged, "rev-parse", "HEAD");
  git(tagged, "tag", "v0.1.0");
  assert.throws(
    () => collectCandidateMetadata({
      repoDir: tagged,
      candidateSha: taggedSha,
      mainRef: "main",
    }),
    /stable Tag already exists: v0\.1\.0/,
  );
});

test("rejects Candidate version and versionCode regressions", () => {
  const versionRegression = createRepository("0.2.0+2");
  git(versionRegression, "tag", "v0.2.0");
  const lowerVersionSha = commitVersion(versionRegression, "0.1.9+3");
  assert.throws(
    () => collectCandidateMetadata({
      repoDir: versionRegression,
      candidateSha: lowerVersionSha,
      mainRef: "main",
    }),
    /Release version v0\.1\.9 must be newer than v0\.2\.0/,
  );

  const codeRegression = createRepository("0.1.0+2");
  git(codeRegression, "tag", "v0.1.0");
  const lowerCodeSha = commitVersion(codeRegression, "0.1.1+2");
  assert.throws(
    () => collectCandidateMetadata({
      repoDir: codeRegression,
      candidateSha: lowerCodeSha,
      mainRef: "main",
    }),
    /versionCode 2 must be greater than 2/,
  );
});

test("rejects malformed Candidate versions and incomplete Tag history", () => {
  const prerelease = createRepository("0.1.0-rc.1+1");
  const prereleaseSha = git(prerelease, "rev-parse", "HEAD");
  assert.throws(
    () => collectCandidateMetadata({
      repoDir: prerelease,
      candidateSha: prereleaseSha,
      mainRef: "main",
    }),
    /must use vX\.Y\.Z/,
  );

  const source = createRepository();
  git(source, "tag", "v0.1.0");
  const candidateSha = commitVersion(source, "0.1.1+2");
  const cloneParent = mkdtempSync(path.join(tmpdir(), "speakup-candidate-no-tags-"));
  const clone = path.join(cloneParent, "repo");
  execFileSync("git", ["clone", "--no-tags", source, clone], { stdio: "ignore" });
  assert.throws(
    () => collectCandidateMetadata({
      repoDir: clone,
      candidateSha,
      mainRef: "main",
    }),
    /stable release tags do not match origin/,
  );
});

test("rejects an existing stable Tag outside main history", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  git(repo, "checkout", "-b", "orphaned-release");
  commitVersion(repo, "9.0.0+9");
  git(repo, "tag", "v9.0.0");
  git(repo, "checkout", "main");
  const candidateSha = commitVersion(repo, "0.1.1+2");

  assert.throws(
    () => collectCandidateMetadata({
      repoDir: repo,
      candidateSha,
      mainRef: "main",
    }),
    /Previous release tag v9\.0\.0 is not contained in main/,
  );
});

test("validates stable tags against an explicitly selected remote", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  git(repo, "remote", "remove", "origin");
  git(repo, "remote", "add", "mirror", repo);

  assert.equal(
    collectReleaseMetadata({
      repoDir: repo,
      tag: "v0.1.0",
      mainRef: "main",
      tagRemote: "mirror",
    }).version,
    "0.1.0",
  );
});

test("validates the official upstream URL and live main commit", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  configureOfficialUpstream(repo);

  assert.equal(
    withOfficialGitProxy(repo, () => collectReleaseMetadata({
      repoDir: repo,
      tag: "v0.1.0",
      mainRef: "refs/remotes/upstream/main",
      tagRemote: "upstream",
    })).git_sha,
    git(repo, "rev-parse", "HEAD"),
  );
});

test("rejects a non-official upstream URL and stale upstream main", () => {
  const wrongRemote = createRepository();
  git(wrongRemote, "tag", "v0.1.0");
  git(wrongRemote, "remote", "add", "upstream", wrongRemote);
  assert.throws(
    () => collectReleaseMetadata({
      repoDir: wrongRemote,
      tag: "v0.1.0",
      mainRef: "main",
      tagRemote: "upstream",
    }),
    /upstream must use https:\/\/github\.com\/1024XEngineer\/XE3-ESL\.git/,
  );

  const staleMain = createRepository();
  git(staleMain, "tag", "v0.1.0");
  configureOfficialUpstream(staleMain);
  writeReleaseFiles(staleMain, "0.1.1+2");
  git(staleMain, "add", ".");
  git(staleMain, "commit", "-m", "advance live main");
  git(staleMain, "checkout", "--detach", "v0.1.0");
  assert.throws(
    () => withOfficialGitProxy(staleMain, () => collectReleaseMetadata({
      repoDir: staleMain,
      tag: "v0.1.0",
      mainRef: "refs/remotes/upstream/main",
      tagRemote: "upstream",
    })),
    /does not match live upstream main/,
  );

  const rewritten = createRepository();
  git(rewritten, "tag", "v0.1.0");
  configureOfficialUpstream(rewritten);
  git(
    rewritten,
    "config",
    `url.file://${rewritten}.insteadOf`,
    officialUpstreamUrl,
  );
  assert.throws(
    () => collectReleaseMetadata({
      repoDir: rewritten,
      tag: "v0.1.0",
      mainRef: "refs/remotes/upstream/main",
      tagRemote: "upstream",
    }),
    /Git URL rewrites must not affect the official upstream URL/,
  );
});

test("rejects unsafe stable Tag remote names", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");

  for (const tagRemote of [
    "",
    "-upstream",
    "team/upstream",
    "../upstream",
    ".",
    "..",
  ]) {
    assert.throws(
      () => collectReleaseMetadata({
        repoDir: repo,
        tag: "v0.1.0",
        mainRef: "main",
        tagRemote,
      }),
      /tagRemote must be a plain Git remote name/,
      tagRemote,
    );
  }
});

test("metadata and manifest CLIs reject unknown and duplicate options", () => {
  const cases = [
    {
      script: metadataScript,
      arguments_: ["--tag-remtoe", "upstream"],
      pattern: /Unknown option: --tag-remtoe/,
    },
    {
      script: metadataScript,
      arguments_: ["--tag", "v0.1.0", "--tag", "v0.1.0"],
      pattern: /Option may only be provided once: --tag/,
    },
    {
      script: manifestScript,
      arguments_: ["--offline-bunlde", "bundle.json"],
      pattern: /Unknown option: --offline-bunlde/,
    },
    {
      script: manifestScript,
      arguments_: ["--offline-bundle", "bundle.json"],
      pattern: /Unknown option: --offline-bundle/,
    },
    {
      script: manifestScript,
      arguments_: ["--output", "one.json", "--output", "two.json"],
      pattern: /Option may only be provided once: --output/,
    },
    {
      script: offlineManifestScript,
      arguments_: ["--quality-run-urll", "https://example.invalid"],
      pattern: /Unknown option: --quality-run-urll/,
    },
    {
      script: offlineManifestScript,
      arguments_: ["--repo", "one", "--repo", "two"],
      pattern: /Option may only be provided once: --repo/,
    },
    {
      script: offlineManifestScript,
      arguments_: ["--github-api-url", "https://example.invalid"],
      pattern: /Unknown option: --github-api-url/,
    },
  ];

  for (const scenario of cases) {
    assert.throws(
      () => execFileSync(
        process.execPath,
        [scenario.script, ...scenario.arguments_],
        { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] },
      ),
      (error) => {
        assert.match(error.stderr, scenario.pattern);
        return true;
      },
    );
  }
});

test("metadata and manifest CLIs require exactly one release identity", () => {
  const sharedManifestArguments = [
    "--main-ref",
    "main",
    "--staging-apk",
    "staging.apk",
    "--production-apk",
    "production.apk",
    "--output",
    "release-manifest.json",
  ];
  const cases = [
    {
      script: metadataScript,
      arguments_: ["--main-ref", "main"],
    },
    {
      script: metadataScript,
      arguments_: [
        "--tag",
        "v0.1.0",
        "--candidate-sha",
        "a".repeat(40),
        "--main-ref",
        "main",
      ],
    },
    {
      script: manifestScript,
      arguments_: sharedManifestArguments,
    },
    {
      script: manifestScript,
      arguments_: [
        "--tag",
        "v0.1.0",
        "--candidate-sha",
        "a".repeat(40),
        ...sharedManifestArguments,
      ],
    },
  ];

  for (const scenario of cases) {
    assert.throws(
      () => execFileSync(
        process.execPath,
        [scenario.script, ...scenario.arguments_],
        { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] },
      ),
      (error) => {
        assert.match(error.stderr, /Exactly one of --tag or --candidate-sha is required/);
        return true;
      },
    );
  }
});

test("Release Candidate workflow is automatic, official-main-only, and Tag-free", () => {
  const workflow = readFileSync(releaseCandidateWorkflow, "utf8");
  assert.match(workflow, /on:\n  push:\n    branches:\n      - main/);
  assert.match(workflow, /CANDIDATE_REPOSITORY.*github\.repository/);
  assert.match(workflow, /1024XEngineer\/XE3-ESL/);
  assert.match(workflow, /CANDIDATE_REF.*github\.ref/);
  assert.match(workflow, /refs\/heads\/main/);
  assert.match(workflow, /CANDIDATE_EVENT" != "push"/);
  assert.match(workflow, /--candidate-sha "\$GITHUB_SHA"/);
  assert.doesNotMatch(workflow, /workflow_dispatch/);
  assert.doesNotMatch(workflow, /push:\n\s+tags:/);
  assert.doesNotMatch(workflow, /GITHUB_REF_NAME/);
  assert.doesNotMatch(workflow, /--tag "\$GITHUB_REF_NAME"/);
  assert.doesNotMatch(workflow, /contents:\s*write/);
  assert.doesNotMatch(workflow, /deploy\/(staging|production)\/manage\.sh/);
});

test("Staging deploy accepts only a successful official Candidate artifact", () => {
  const workflow = readFileSync(stagingDeployWorkflow, "utf8");
  assert.match(workflow, /workflow_run:\n\s+workflows:\n\s+- Release Candidate/);
  assert.match(workflow, /github\.event\.workflow_run\.conclusion == 'success'/);
  assert.match(workflow, /CANDIDATE_HEAD_REPOSITORY.*head_repository\.full_name/);
  assert.match(workflow, /CANDIDATE_EVENT.*workflow_run\.event/);
  assert.match(workflow, /CANDIDATE_HEAD_BRANCH.*workflow_run\.head_branch/);
  assert.match(workflow, /CANDIDATE_EVENT" != "push"/);
  assert.match(workflow, /CANDIDATE_HEAD_BRANCH" != "main"/);
  assert.doesNotMatch(workflow, /workflow_dispatch/);
  assert.match(workflow, /environment:\n\s+name: staging/);
  assert.match(workflow, /group: staging-deployment\n\s+cancel-in-progress: false/);
  assert.match(workflow, /listWorkflowRunArtifacts/);
  assert.match(workflow, /manifests\.length !== 1/);
  assert.match(workflow, /name:.*manifest_artifact/);
  assert.match(workflow, /name:.*android_artifact/);
  assert.match(workflow, /run-id:.*candidate_run_id/);
  assert.match(workflow, /github-token:.*github\.token/);
  assert.match(workflow, /actions\/checkout@/);
  assert.match(workflow, /ref:.*candidate_sha/);
  assert.match(workflow, /StrictHostKeyChecking=yes/);
  assert.match(workflow, /action:\s*"inspect"/);
  assert.match(workflow, /action:\s*"deploy"/);
  assert.match(workflow, /action:\s*"publish"/);
  assert.match(workflow, /--profile staging/);
  assert.match(workflow, /expected_runtime_receipt_sha256/);
  assert.match(
    workflow,
    /\.receipt\.deployment_run_attempt[\s\S]*<= \$deployment_run_attempt/,
  );
  assert.match(workflow, /STAGING_PORTAL_BASIC_AUTH:.*secrets\.STAGING_PORTAL_BASIC_AUTH/);
  assert.match(workflow, /--user "\$STAGING_PORTAL_BASIC_AUTH"/);
  assert.match(workflow, /downloads\/android\/staging-candidate\.json/);
  assert.match(workflow, /cmp --silent "\$CANDIDATE_METADATA" "\$public_metadata"/);
  assert.match(workflow, /\.manifest_sha256 == \$manifest_sha256/);
  assert.match(workflow, /\.apk_sha256 == \$manifest\[0\]\.staging_apk_sha256/);
  assert.match(workflow, /"https:\/\/staging\.speak-up\.top\$download_path"/);
  assert.match(workflow, /sha256sum "\$public_apk"/);
  assert.match(workflow, /keys == \[[\s\S]*"publication_receipt_sha256"/);
  assert.match(workflow, /candidate_metadata_sha256 == \$candidate_metadata_sha256/);
  assert.match(workflow, /staging_apk_file == \$manifest\[0\]\.staging_apk_file/);
  assert.match(workflow, /staging_apk_sha256 == \$manifest\[0\]\.staging_apk_sha256/);
  assert.match(workflow, /embedded_sha256=.*jq -cj '\.receipt'/);
  assert.match(workflow, /staging-publication-\$\{\{ github\.run_id \}\}/);
  assert.match(workflow, /expected_current_receipt_sha256/);
  assert.doesNotMatch(workflow, /already deployed to Staging/);
  assert.match(workflow, /https:\/\/staging-api\.speak-up\.top\/health/);
  assert.match(workflow, /staging-deployment-\$\{\{ github\.run_id \}\}/);
  assert.doesNotMatch(workflow, /deploy\/production/);
});

test("rejects a release tag that is not contained in main", () => {
  const repo = createRepository();
  git(repo, "checkout", "-b", "feature");
  writeReleaseFiles(repo, "0.1.1+2");
  git(repo, "add", ".");
  git(repo, "commit", "-m", "feature release");
  git(repo, "tag", "v0.1.1");

  assert.throws(
    () => collectReleaseMetadata({ repoDir: repo, tag: "v0.1.1", mainRef: "main" }),
    /not contained in main/,
  );
});

test("rejects a tag checkout that does not match HEAD", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  writeReleaseFiles(repo, "0.1.1+2");
  git(repo, "add", ".");
  git(repo, "commit", "-m", "unreleased change");

  assert.throws(
    () => collectReleaseMetadata({ repoDir: repo, tag: "v0.1.0", mainRef: "main" }),
    /does not match release tag/,
  );
});

test("rejects non-stable tags and tag/versionName mismatches", () => {
  assert.throws(() => parseStableTag("v0.1.0-rc.1"), /must use vX.Y.Z/);

  const repo = createRepository();
  git(repo, "tag", "v0.2.0");
  assert.throws(
    () => collectReleaseMetadata({ repoDir: repo, tag: "v0.2.0", mainRef: "main" }),
    /does not match versionName/,
  );
});

test("rejects a versionCode that does not increase", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  writeReleaseFiles(repo, "0.1.1+1");
  git(repo, "add", ".");
  git(repo, "commit", "-m", "next release");
  git(repo, "tag", "v0.1.1");

  assert.throws(
    () => collectReleaseMetadata({ repoDir: repo, tag: "v0.1.1", mainRef: "main" }),
    /versionCode 1 must be greater than 1/,
  );
});

test("accepts an increased versionCode and permits rebuilding an older stable tag", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  writeReleaseFiles(repo, "0.1.1+2");
  git(repo, "add", ".");
  git(repo, "commit", "-m", "next release");
  git(repo, "tag", "v0.1.1");

  assert.equal(
    collectReleaseMetadata({ repoDir: repo, tag: "v0.1.1", mainRef: "main" })
      .version_code,
    "2",
  );

  git(repo, "checkout", "--detach", "v0.1.0");
  assert.equal(
    collectReleaseMetadata({ repoDir: repo, tag: "v0.1.0", mainRef: "main" }).version,
    "0.1.0",
  );
});

test("rejects shallow repositories", () => {
  const source = createRepository();
  git(source, "tag", "v0.1.0");
  const cloneParent = mkdtempSync(path.join(tmpdir(), "speakup-shallow-parent-"));
  const shallow = path.join(cloneParent, "repo");
  execFileSync(
    "git",
    ["clone", "--depth", "1", "--branch", "v0.1.0", `file://${source}`, shallow],
    { stdio: "ignore" },
  );

  assert.throws(
    () => collectReleaseMetadata({ repoDir: shallow, tag: "v0.1.0", mainRef: "main" }),
    /complete Git history/,
  );
});

test("rejects an incomplete local stable Tag set", () => {
  const source = createRepository("0.1.0+100");
  git(source, "tag", "v0.1.0");
  writeReleaseFiles(source, "0.2.0+1");
  git(source, "add", ".");
  git(source, "commit", "-m", "invalid lower version code");
  git(source, "tag", "v0.2.0");

  const cloneParent = mkdtempSync(path.join(tmpdir(), "speakup-no-tags-parent-"));
  const clone = path.join(cloneParent, "repo");
  execFileSync("git", ["clone", "--no-tags", source, clone], { stdio: "ignore" });
  git(clone, "fetch", "origin", "refs/tags/v0.2.0:refs/tags/v0.2.0");
  git(clone, "checkout", "--detach", "v0.2.0");

  assert.throws(
    () => collectReleaseMetadata({ repoDir: clone, tag: "v0.2.0", mainRef: "main" }),
    /stable release tags do not match origin/,
  );
});

test("rejects a release Tag on merged side history", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  git(repo, "checkout", "-b", "feature");
  writeReleaseFiles(repo, "0.2.0+2");
  git(repo, "add", ".");
  git(repo, "commit", "-m", "side release");
  git(repo, "tag", "v0.2.0");
  git(repo, "checkout", "main");
  git(repo, "merge", "--no-ff", "feature", "-m", "merge feature");
  git(repo, "checkout", "--detach", "v0.2.0");

  assert.throws(
    () => collectReleaseMetadata({ repoDir: repo, tag: "v0.2.0", mainRef: "main" }),
    /not on the first-parent history/,
  );
});

test("rejects release ordering that regresses later on main", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  writeReleaseFiles(repo, "0.3.0+2");
  git(repo, "add", ".");
  git(repo, "commit", "-m", "backdated future release");
  const backdatedCommit = git(repo, "rev-parse", "HEAD");
  writeReleaseFiles(repo, "0.2.0+3");
  git(repo, "add", ".");
  git(repo, "commit", "-m", "existing later release");
  git(repo, "tag", "v0.2.0");
  git(repo, "tag", "v0.3.0", backdatedCommit);
  git(repo, "checkout", "--detach", "v0.3.0");

  assert.throws(
    () => collectReleaseMetadata({ repoDir: repo, tag: "v0.3.0", mainRef: "main" }),
    /Release version v0\.2\.0 must be newer than v0\.3\.0/,
  );
});

test("rejects missing, malformed, and duplicate migration versions", () => {
  assert.throws(() => databaseSchemaVersion([]), /No server migration/);
  assert.throws(
    () => databaseSchemaVersion(["server/migrations/not_numbered.up.sql"]),
    /Invalid server migration filename/,
  );
  assert.throws(
    () => databaseSchemaVersion([
      "server/migrations/000001_first.up.sql",
      "server/migrations/000001_second.up.sql",
    ]),
    /Duplicate server migration version/,
  );
  assert.equal(
    databaseSchemaVersion([
      "server/migrations/000001_first.up.sql",
      "server/migrations/999999_ignored.down.sql",
      "server/migrations/embed.go",
    ]),
    1,
  );
});

test("derives the manifest from validated build artifacts", () => {
  const { input, metadata } = validManifestFixture();
  const manifest = createReleaseManifest(input, metadata);

  assert.equal(
    manifest.staging_apk_sha256,
    createHash("sha256").update("staging apk").digest("hex"),
  );
  assert.equal(
    manifest.production_apk_sha256,
    createHash("sha256").update("production apk").digest("hex"),
  );
  assert.equal(manifest.database_schema_version, 7);
  assert.equal(manifest.portal_image_digest, input.PORTAL_IMAGE_DIGEST);
  assert.equal(
    manifest.production_apk_size_bytes,
    Buffer.byteLength("production apk"),
  );
  assert.equal(manifest.application_id, "com.xengineer.speakup");
  assert.equal(manifest.minimum_android_api, 24);
  assert.deepEqual(manifest.abis, ["arm64-v8a"]);
});

test("writes the validated manifest atomically through the CLI", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  const { input } = validManifestFixture();
  const output = path.join(repo, "release-manifest.json");
  execFileSync(
    process.execPath,
    [
      manifestScript,
      "--tag",
      "v0.1.0",
      "--main-ref",
      "main",
      "--staging-apk",
      input.STAGING_APK_PATH,
      "--production-apk",
      input.PRODUCTION_APK_PATH,
      "--output",
      output,
      "--repo",
      repo,
    ],
    { env: { ...process.env, ...input }, stdio: "pipe" },
  );

  const manifest = JSON.parse(readFileSync(output, "utf8"));
  assert.equal(manifest.version, "0.1.0");
  assert.equal(manifest.git_sha, git(repo, "rev-parse", "HEAD"));
  assert.equal(manifest.portal_image, input.PORTAL_IMAGE);
  assert.equal(manifest.portal_image_digest, input.PORTAL_IMAGE_DIGEST);
  assert.equal(manifest.server_image, input.SERVER_IMAGE);
  assert.equal(manifest.server_image_digest, input.SERVER_IMAGE_DIGEST);

  const rejectedOutput = path.join(repo, "rejected-manifest.json");
  assert.throws(
    () => execFileSync(
      process.execPath,
      [
        manifestScript,
        "--tag",
        "v0.1.0",
        "--main-ref",
        "main",
        "--staging-apk",
        input.STAGING_APK_PATH,
        "--production-apk",
        input.PRODUCTION_APK_PATH,
        "--output",
        rejectedOutput,
        "--repo",
        repo,
      ],
      {
        env: { ...process.env, ...input, PORTAL_IMAGE_DIGEST: "sha256:pending" },
        stdio: "pipe",
      },
    ),
  );
  assert.equal(existsSync(rejectedOutput), false);
});

test("writes the unchanged v1 manifest for an untagged Candidate", () => {
  const repo = createRepository();
  const { input } = validManifestFixture();
  const output = path.join(repo, "candidate-release-manifest.json");
  const tagsBefore = git(repo, "tag", "--list");

  execFileSync(
    process.execPath,
    candidateManifestArguments(repo, input, output),
    { env: { ...process.env, ...input }, stdio: "pipe" },
  );

  const manifest = JSON.parse(readFileSync(output, "utf8"));
  assert.equal(manifest.manifest_version, 1);
  assert.equal(manifest.version, "0.1.0");
  assert.equal(manifest.git_sha, git(repo, "rev-parse", "HEAD"));
  assert.equal(Object.hasOwn(manifest, "tag"), false);
  assert.equal(git(repo, "tag", "--list"), tagsBefore);
});

test("refuses to replace an existing manifest file, directory, or symbolic link", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  const { input } = validManifestFixture();
  const existingFile = path.join(repo, "existing-manifest.json");
  const existingDirectory = path.join(repo, "existing-manifest-directory");
  const linkTarget = path.join(repo, "manifest-link-target.json");
  const existingLink = path.join(repo, "existing-manifest-link.json");
  writeFileSync(existingFile, "original manifest\n");
  mkdirSync(existingDirectory);
  writeFileSync(linkTarget, "link target\n");
  symlinkSync(linkTarget, existingLink);

  for (const output of [existingFile, existingDirectory, existingLink]) {
    let failure;
    try {
      runManifestCli(repo, input, output);
    } catch (error) {
      failure = error;
    }
    assert.ok(failure, `${output} was unexpectedly replaced`);
    assert.match(failure.stderr.toString(), /output already exists/);
  }

  assert.equal(readFileSync(existingFile, "utf8"), "original manifest\n");
  assert.equal(lstatSync(existingDirectory).isDirectory(), true);
  assert.equal(lstatSync(existingLink).isSymbolicLink(), true);
  assert.equal(readlinkSync(existingLink), linkTarget);
  assert.equal(readFileSync(linkTarget, "utf8"), "link target\n");
});

test("derives image references from a validated offline bundle", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  const gitSha = git(repo, "rev-parse", "HEAD");
  const { input, metadata } = validManifestFixture();
  const fixture = offlineBundleFixture("0.1.0", gitSha);

  input.PORTAL_IMAGE = "invalid-environment-value";
  input.PORTAL_IMAGE_DIGEST = "sha256:pending";
  input.SERVER_IMAGE = "invalid-environment-value";
  input.SERVER_IMAGE_DIGEST = "sha256:pending";
  metadata.git_sha = gitSha;
  const imageInput = offlineBundleImageInput(fixture.bundlePath, metadata);
  const manifest = createReleaseManifest({ ...input, ...imageInput }, metadata);

  assert.equal(manifest.manifest_version, 1);
  assert.equal(manifest.version, "0.1.0");
  assert.equal(manifest.git_sha, gitSha);
  assert.equal(
    manifest.portal_image,
    "ghcr.io/1024xengineer/xe3-esl-portal",
  );
  assert.equal(manifest.portal_image_digest, `sha256:${"e".repeat(64)}`);
  assert.equal(
    manifest.server_image,
    "ghcr.io/1024xengineer/xe3-esl-server",
  );
  assert.equal(manifest.server_image_digest, `sha256:${"f".repeat(64)}`);
  assert.equal(Object.hasOwn(manifest, "offline_bundle"), false);
});

test("offline manifest wrapper derives APK fields from fail-closed verifier reports", async () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  configureOfficialUpstream(repo);
  const gitSha = git(repo, "rev-parse", "HEAD");
  const { input, stagingApk } = validManifestFixture();
  const originalStagingSha = sha256File(stagingApk);
  const bundle = offlineBundleFixture("0.1.0", gitSha);
  const certificate = "c".repeat(64);
  const certificateFile = path.join(bundle.directory, "certificate-sha256.txt");
  const output = path.join(repo, "offline-wrapper-manifest.json");
  writeFileSync(certificateFile, `${certificate}\n`);
  const androidTools = fakeAndroidTools(certificate);
  const quality = qualityApiFixture(gitSha);

  await withOfflineWrapperTools(
    repo,
    androidTools,
    {
      STAGING_APK_APPLICATION_ID: "caller.must.not.win",
      PRODUCTION_APK_APPLICATION_ID: "caller.must.not.win",
      APK_CERTIFICATE_SHA256: "0".repeat(64),
      GITHUB_API_URL: "https://example.invalid/must-not-win",
    },
    () => generateOfflineManifest(
      offlineManifestArguments(
        repo,
        input,
        bundle,
        certificateFile,
        quality.qualityRunUrl,
        output,
      ),
      quality.fetchImplementation,
    ),
  );

  const manifest = JSON.parse(readFileSync(output, "utf8"));
  assert.equal(manifest.application_id, "com.xengineer.speakup");
  assert.equal(manifest.minimum_android_api, 24);
  assert.deepEqual(manifest.abis, ["arm64-v8a"]);
  assert.equal(manifest.apk_certificate_sha256, certificate);
  assert.equal(manifest.staging_apk_sha256, originalStagingSha);
  assert.equal(manifest.quality_run_url, quality.qualityRunUrl);
  assert.deepEqual(
    quality.calls.map(({ url }) => url),
    [quality.workflowApiUrl, quality.runApiUrl],
  );
  for (const { options } of quality.calls) {
    assert.equal(options.method, "GET");
    assert.equal(options.redirect, "error");
    assert.equal(options.headers.Accept, "application/vnd.github+json");
    assert.equal(
      options.headers["X-GitHub-Api-Version"],
      githubApiVersion,
    );
    assert.equal(options.headers["User-Agent"], "SpeakUp-Release-Candidate");
  }
});

test("accepts only the fixed official successful Quality workflow contract", async () => {
  const gitSha = "d".repeat(40);
  const fixture = qualityApiFixture(gitSha);
  fixture.run.event = "workflow_dispatch";

  assert.equal(
    await validateQualityRun(
      fixture.qualityRunUrl,
      gitSha,
      fixture.fetchImplementation,
    ),
    fixture.qualityRunUrl,
  );
  assert.deepEqual(
    fixture.calls.map(({ url }) => url),
    [fixture.workflowApiUrl, fixture.runApiUrl],
  );
});

test("rejects malformed or non-official Quality workflow run URLs", async () => {
  const gitSha = "d".repeat(40);
  const urls = [
    "http://github.com/1024XEngineer/XE3-ESL/actions/runs/1",
    "https://github.com/1024xengineer/XE3-ESL/actions/runs/1",
    "https://github.com/1024XEngineer/XE3-ESL/actions/runs/0",
    "https://github.com/1024XEngineer/XE3-ESL/actions/runs/01",
    "https://github.com/1024XEngineer/XE3-ESL/actions/runs/9007199254740992",
    "https://github.com/1024XEngineer/XE3-ESL/actions/runs/1?attempt=2",
    "https://github.com/1024XEngineer/XE3-ESL/actions/runs/1/attempts/2",
  ];

  for (const url of urls) {
    let called = false;
    await assert.rejects(
      validateQualityRun(url, gitSha, async () => {
        called = true;
      }),
      /quality run URL must identify an official workflow run/,
      url,
    );
    assert.equal(called, false, url);
  }
});

test("rejects mismatched Quality workflow and run response fields", async () => {
  const gitSha = "d".repeat(40);
  const cases = [
    {
      name: "workflow id",
      pattern: /Quality workflow response does not match/,
      mutate(fixture) { fixture.workflow.id = 0; },
    },
    {
      name: "workflow name",
      pattern: /Quality workflow response does not match/,
      mutate(fixture) { fixture.workflow.name = "Other"; },
    },
    {
      name: "workflow path",
      pattern: /Quality workflow response does not match/,
      mutate(fixture) { fixture.workflow.path = ".github/workflows/other.yml"; },
    },
    {
      name: "workflow state",
      pattern: /Quality workflow response does not match/,
      mutate(fixture) { fixture.workflow.state = "disabled_manually"; },
    },
    {
      name: "run id",
      pattern: /Quality workflow run does not match/,
      mutate(fixture) { fixture.run.id += 1; },
    },
    {
      name: "run API URL",
      pattern: /Quality workflow run does not match/,
      mutate(fixture) { fixture.run.url = "https://api.github.com/forged"; },
    },
    {
      name: "run browser URL",
      pattern: /Quality workflow run does not match/,
      mutate(fixture) { fixture.run.html_url += "?attempt=2"; },
    },
    {
      name: "workflow id binding",
      pattern: /Quality workflow run does not match/,
      mutate(fixture) { fixture.run.workflow_id += 1; },
    },
    {
      name: "repository",
      pattern: /Quality workflow run does not match/,
      mutate(fixture) { fixture.run.repository.full_name = "example/fork"; },
    },
    {
      name: "head repository",
      pattern: /Quality workflow run does not match/,
      mutate(fixture) { fixture.run.head_repository = null; },
    },
    {
      name: "run workflow path",
      pattern: /Quality workflow run does not match/,
      mutate(fixture) { fixture.run.path = `${qualityWorkflowPath}@main`; },
    },
    {
      name: "release SHA",
      pattern: /Quality workflow run does not match/,
      mutate(fixture) { fixture.run.head_sha = "e".repeat(40); },
    },
    {
      name: "release branch",
      pattern: /Quality workflow run does not match/,
      mutate(fixture) { fixture.run.head_branch = "dev"; },
    },
    {
      name: "event",
      pattern: /Quality workflow run does not match/,
      mutate(fixture) { fixture.run.event = "pull_request"; },
    },
    {
      name: "status",
      pattern: /Quality workflow run does not match/,
      mutate(fixture) { fixture.run.status = "in_progress"; },
    },
    {
      name: "conclusion",
      pattern: /Quality workflow run does not match/,
      mutate(fixture) { fixture.run.conclusion = "failure"; },
    },
  ];

  for (const scenario of cases) {
    const fixture = qualityApiFixture(gitSha);
    scenario.mutate(fixture);
    await assert.rejects(
      validateQualityRun(
        fixture.qualityRunUrl,
        gitSha,
        fixture.fetchImplementation,
      ),
      scenario.pattern,
      scenario.name,
    );
  }
});

test("fails closed on Quality workflow API and JSON errors", async () => {
  const gitSha = "d".repeat(40);
  const fixture = qualityApiFixture(gitSha);

  await assert.rejects(
    validateQualityRun(fixture.qualityRunUrl, gitSha, async () => {
      throw new Error("network unavailable");
    }),
    /Quality workflow request failed: network unavailable/,
  );
  await assert.rejects(
    validateQualityRun(fixture.qualityRunUrl, gitSha, async () => ({
      ok: false,
      status: 403,
    })),
    /Quality workflow request failed with HTTP 403/,
  );
  await assert.rejects(
    validateQualityRun(fixture.qualityRunUrl, gitSha, async () => ({
      ok: true,
      status: 200,
      async json() { throw new Error("invalid JSON"); },
    })),
    /Quality workflow response must contain valid JSON/,
  );

  let calls = 0;
  await assert.rejects(
    validateQualityRun(fixture.qualityRunUrl, gitSha, async (url, options) => {
      calls += 1;
      if (calls === 1) return fixture.fetchImplementation(url, options);
      return { ok: false, status: 502 };
    }),
    /Quality workflow run request failed with HTTP 502/,
  );
  assert.equal(calls, 2);
});

test("offline manifest wrapper does not write output when Quality validation fails", async () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  configureOfficialUpstream(repo);
  const gitSha = git(repo, "rev-parse", "HEAD");
  const { input } = validManifestFixture();
  const bundle = offlineBundleFixture("0.1.0", gitSha);
  const certificate = "c".repeat(64);
  const certificateFile = path.join(bundle.directory, "certificate-sha256.txt");
  const output = path.join(repo, "quality-rejected-offline-manifest.json");
  writeFileSync(certificateFile, `${certificate}\n`);
  const androidTools = fakeAndroidTools(certificate);
  const quality = qualityApiFixture(gitSha);
  quality.run.conclusion = "failure";

  await assert.rejects(
    withOfflineWrapperTools(
      repo,
      androidTools,
      {},
      () => generateOfflineManifest(
        offlineManifestArguments(
          repo,
          input,
          bundle,
          certificateFile,
          quality.qualityRunUrl,
          output,
        ),
        quality.fetchImplementation,
      ),
    ),
    /Quality workflow run does not match the release contract/,
  );
  assert.equal(existsSync(output), false);
});

test("offline manifest wrapper does not write output when APK verification fails", async () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  configureOfficialUpstream(repo);
  const gitSha = git(repo, "rev-parse", "HEAD");
  const { input } = validManifestFixture();
  const bundle = offlineBundleFixture("0.1.0", gitSha);
  const certificate = "c".repeat(64);
  const certificateFile = path.join(bundle.directory, "certificate-sha256.txt");
  const output = path.join(repo, "rejected-offline-wrapper-manifest.json");
  writeFileSync(certificateFile, `${certificate}\n`);
  const androidTools = fakeAndroidTools(certificate, { failProduction: true });
  const quality = qualityApiFixture(gitSha);

  await assert.rejects(
    withOfflineWrapperTools(
      repo,
      androidTools,
      {},
      () => generateOfflineManifest(
        offlineManifestArguments(
          repo,
          input,
          bundle,
          certificateFile,
          quality.qualityRunUrl,
          output,
        ),
        quality.fetchImplementation,
      ),
    ),
    /production APK verification failed/,
  );
  assert.equal(existsSync(output), false);
});

test("rejects invalid offline bundles and artifacts", () => {
  const repo = createRepository();
  git(repo, "tag", "v0.1.0");
  const gitSha = git(repo, "rev-parse", "HEAD");
  const cases = [
    {
      name: "bundle version",
      pattern: /offline bundle version must be 1/,
      mutate(fixture) {
        fixture.bundle.bundle_version = 2;
        writeJson(fixture.bundlePath, fixture.bundle);
      },
    },
    {
      name: "release version mismatch",
      pattern: /offline bundle version does not match release metadata/,
      mutate(fixture) {
        fixture.bundle.version = "0.1.1";
        writeJson(fixture.bundlePath, fixture.bundle);
      },
    },
    {
      name: "release Git SHA mismatch",
      pattern: /offline bundle git_sha does not match release metadata/,
      mutate(fixture) {
        fixture.bundle.git_sha = "a".repeat(40);
        writeJson(fixture.bundlePath, fixture.bundle);
      },
    },
    {
      name: "platform",
      pattern: /offline bundle platform must be linux\/amd64/,
      mutate(fixture) {
        fixture.bundle.platform = "linux/arm64";
        writeJson(fixture.bundlePath, fixture.bundle);
      },
    },
    {
      name: "source date epoch",
      pattern: /source_date_epoch must be a positive integer/,
      mutate(fixture) {
        fixture.bundle.source_date_epoch = 0;
        writeJson(fixture.bundlePath, fixture.bundle);
      },
    },
    {
      name: "unknown bundle field",
      pattern: /offline bundle must contain the exact field set/,
      mutate(fixture) {
        fixture.bundle.unexpected = true;
        writeJson(fixture.bundlePath, fixture.bundle);
      },
    },
    {
      name: "unknown image field",
      pattern: /offline portal image must contain the exact field set/,
      mutate(fixture) {
        fixture.bundle.images[0].unexpected = true;
        writeJson(fixture.bundlePath, fixture.bundle);
      },
    },
    {
      name: "image order",
      pattern: /offline portal image identity is invalid/,
      mutate(fixture) {
        fixture.bundle.images.reverse();
        writeJson(fixture.bundlePath, fixture.bundle);
      },
    },
    {
      name: "repository",
      pattern: /offline portal image identity is invalid/,
      mutate(fixture) {
        fixture.bundle.images[0].repository = "ghcr.io/example/portal";
        writeJson(fixture.bundlePath, fixture.bundle);
      },
    },
    {
      name: "digest",
      pattern: /cannot be a placeholder digest/,
      mutate(fixture) {
        fixture.bundle.images[0].digest = `sha256:${"0".repeat(64)}`;
        writeJson(fixture.bundlePath, fixture.bundle);
      },
    },
    {
      name: "versioned file name",
      pattern: /offline portal image file names are invalid/,
      mutate(fixture) {
        fixture.bundle.images[0].archive_file = "portal.tar";
        writeJson(fixture.bundlePath, fixture.bundle);
      },
    },
    {
      name: "bundle symbolic link",
      pattern: /offline bundle must be a non-empty regular file/,
      mutate(fixture) {
        const link = path.join(fixture.directory, "offline-bundle-link.json");
        symlinkSync(fixture.bundlePath, link);
        return link;
      },
    },
    {
      name: "empty bundle",
      pattern: /offline bundle must be a non-empty regular file/,
      mutate(fixture) {
        writeFileSync(fixture.bundlePath, "");
      },
    },
    {
      name: "bundle directory",
      pattern: /offline bundle must be a non-empty regular file/,
      mutate(fixture) {
        rmSync(fixture.bundlePath);
        mkdirSync(fixture.bundlePath);
      },
    },
    {
      name: "archive symbolic link",
      pattern: /offline portal archive must be a non-empty regular file/,
      mutate(fixture) {
        const archive = fixture.artifacts[0].archivePath;
        const target = path.join(fixture.directory, "portal-archive-target.tar");
        writeFileSync(target, readFileSync(archive));
        rmSync(archive);
        symlinkSync(target, archive);
      },
    },
    {
      name: "empty archive",
      pattern: /offline portal archive must be a non-empty regular file/,
      mutate(fixture) {
        writeFileSync(fixture.artifacts[0].archivePath, "");
      },
    },
    {
      name: "archive size",
      pattern: /offline portal archive size does not match/,
      mutate(fixture) {
        fixture.bundle.images[0].archive_size_bytes += 1;
        writeJson(fixture.bundlePath, fixture.bundle);
      },
    },
    {
      name: "archive SHA-256",
      pattern: /offline portal archive SHA-256 does not match/,
      mutate(fixture) {
        fixture.bundle.images[0].archive_sha256 = "9".repeat(64);
        writeJson(fixture.bundlePath, fixture.bundle);
      },
    },
    {
      name: "server archive SHA-256",
      pattern: /offline server archive SHA-256 does not match/,
      mutate(fixture) {
        fixture.bundle.images[1].archive_sha256 = "9".repeat(64);
        writeJson(fixture.bundlePath, fixture.bundle);
      },
    },
    {
      name: "metadata symbolic link",
      pattern: /offline portal metadata must be a non-empty regular file/,
      mutate(fixture) {
        const metadata = fixture.artifacts[0].metadataPath;
        const target = path.join(fixture.directory, "portal-metadata-target.json");
        writeFileSync(target, readFileSync(metadata));
        rmSync(metadata);
        symlinkSync(target, metadata);
      },
    },
    {
      name: "metadata SHA-256",
      pattern: /offline portal metadata SHA-256 does not match/,
      mutate(fixture) {
        fixture.bundle.images[0].build_metadata_sha256 = "9".repeat(64);
        writeJson(fixture.bundlePath, fixture.bundle);
      },
    },
    {
      name: "metadata directory",
      pattern: /offline portal metadata must be a non-empty regular file/,
      mutate(fixture) {
        rmSync(fixture.artifacts[0].metadataPath);
        mkdirSync(fixture.artifacts[0].metadataPath);
      },
    },
    {
      name: "metadata digest",
      pattern: /offline portal metadata digest does not match/,
      mutate(fixture) {
        const metadata = fixture.artifacts[0].metadataPath;
        writeJson(metadata, {
          "containerimage.digest": `sha256:${"9".repeat(64)}`,
        });
        fixture.bundle.images[0].build_metadata_sha256 = sha256File(metadata);
        writeJson(fixture.bundlePath, fixture.bundle);
      },
    },
  ];

  for (const scenario of cases) {
    const fixture = offlineBundleFixture("0.1.0", gitSha);
    const selectedBundle = scenario.mutate(fixture) ?? fixture.bundlePath;
    assert.throws(
      () => offlineBundleImageInput(selectedBundle, {
        version: "0.1.0",
        git_sha: gitSha,
      }),
      scenario.pattern,
      scenario.name,
    );
  }
});

test("rejects placeholder or malformed manifest values", () => {
  assert.throws(
    () => createReleaseManifest({}, {
      version: "pending",
      version_code: "1",
      git_sha: "c".repeat(40),
      database_schema_version: "7",
    }),
    /version has an invalid value/,
  );
});

test("rejects invalid APKs, image references, digests, and quality run URLs", () => {
  const { input, metadata, stagingApk } = validManifestFixture();
  writeFileSync(stagingApk, "");
  assert.throws(() => createReleaseManifest(input, metadata), /non-empty file/);

  writeFileSync(stagingApk, "staging apk");
  assert.throws(
    () => createReleaseManifest(
      { ...input, PORTAL_IMAGE_DIGEST: "sha256:pending" },
      metadata,
    ),
    /PORTAL_IMAGE_DIGEST has an invalid value/,
  );
  assert.throws(
    () => createReleaseManifest(
      { ...input, PORTAL_IMAGE: "ghcr.io/1024xengineer//" },
      metadata,
    ),
    /PORTAL_IMAGE has an invalid value/,
  );
  assert.throws(
    () => createReleaseManifest(
      { ...input, PORTAL_IMAGE_DIGEST: `sha256:${"0".repeat(64)}` },
      metadata,
    ),
    /cannot be a placeholder digest/,
  );
  assert.throws(
    () => createReleaseManifest(
      {
        ...input,
        QUALITY_RUN_URL: "https://github.com/another/repo/actions/runs/1",
      },
      metadata,
    ),
    /QUALITY_RUN_URL has an invalid value/,
  );
  assert.throws(
    () => createReleaseManifest(
      { ...input, STAGING_APK_VERIFIED_SHA256: "e".repeat(64) },
      metadata,
    ),
    /STAGING_APK_VERIFIED_SHA256 does not match the APK file/,
  );
});

test("rejects missing, invalid, or inconsistent Android APK metadata", () => {
  const { input, metadata } = validManifestFixture();
  assert.throws(
    () => createReleaseManifest(
      { ...input, PRODUCTION_APK_APPLICATION_ID: "" },
      metadata,
    ),
    /PRODUCTION_APK_APPLICATION_ID is required/,
  );
  assert.throws(
    () => createReleaseManifest(
      { ...input, PRODUCTION_APK_MINIMUM_ANDROID_API: "0" },
      metadata,
    ),
    /must be a positive integer/,
  );
  assert.throws(
    () => createReleaseManifest(
      { ...input, PRODUCTION_APK_ABIS: "arm64-v8a,arm64-v8a" },
      metadata,
    ),
    /PRODUCTION_APK_ABIS has an invalid value/,
  );
  assert.throws(
    () => createReleaseManifest(
      { ...input, STAGING_APK_APPLICATION_ID: "com.xengineer.staging" },
      metadata,
    ),
    /Staging and Production APK metadata do not match/,
  );
  for (const forged of [
    {
      STAGING_APK_APPLICATION_ID: "com.example.fake",
      PRODUCTION_APK_APPLICATION_ID: "com.example.fake",
    },
    {
      STAGING_APK_MINIMUM_ANDROID_API: "25",
      PRODUCTION_APK_MINIMUM_ANDROID_API: "25",
    },
    {
      STAGING_APK_ABIS: "x86",
      PRODUCTION_APK_ABIS: "x86",
    },
  ]) {
    assert.throws(
      () => createReleaseManifest({ ...input, ...forged }, metadata),
      /Android APK metadata does not match the release contract/,
    );
  }
});
