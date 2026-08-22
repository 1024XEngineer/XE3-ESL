import { createReadStream } from "node:fs";
import {
  chmod,
  copyFile,
  mkdir,
  open,
  readFile,
  readdir,
  rename,
  rm,
  stat,
} from "node:fs/promises";
import { createHash, randomUUID } from "node:crypto";
import { basename, isAbsolute, join, parse, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { backup, DatabaseSync } from "node:sqlite";

const backupDirectoryPattern = /^\d{8}T\d{9}Z$/;
const checksumPattern = /^[a-f0-9]{64}$/;
const metadataFileName = "metadata.json";
const databaseFileName = "portal.sqlite";
const checksumFileName = "portal.sqlite.sha256";

function fail(message) {
  throw new Error(message);
}

function requireSafeRoot(value, name) {
  if (!isAbsolute(value) || resolve(value) !== value || parse(value).root === value) {
    fail(`${name} must be an absolute non-root path`);
  }
  return value;
}

function requireNonEmpty(value, name) {
  if (typeof value !== "string" || value.length === 0) {
    fail(`${name} must not be empty`);
  }
  if (/[\u0000-\u001f\u007f]/u.test(value)) {
    fail(`${name} must not contain control characters`);
  }
  return value;
}

function requirePositiveInteger(value, name) {
  if (!/^[1-9]\d*$/u.test(String(value))) {
    fail(`${name} must be a positive integer`);
  }
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed)) {
    fail(`${name} is too large`);
  }
  return parsed;
}

function requireSafeDuration(value, multiplier, name) {
  const milliseconds = value * multiplier;
  if (!Number.isSafeInteger(milliseconds)) {
    fail(`${name} is too large`);
  }
  return milliseconds;
}

function requireDate(value, name) {
  const parsed = new Date(value);
  if (!Number.isFinite(parsed.getTime()) || parsed.toISOString() !== value) {
    fail(`${name} must be an ISO 8601 UTC timestamp`);
  }
  return parsed;
}

function backupId(date) {
  return date.toISOString().replace(/[-:.]/gu, "");
}

async function sha256(filePath) {
  const digest = createHash("sha256");
  await new Promise((resolvePromise, rejectPromise) => {
    const input = createReadStream(filePath);
    input.on("data", (chunk) => digest.update(chunk));
    input.on("error", rejectPromise);
    input.on("end", resolvePromise);
  });
  return digest.digest("hex");
}

async function writePrivateFile(filePath, content) {
  const handle = await open(filePath, "wx", 0o600);
  try {
    await handle.writeFile(content, "utf8");
    await handle.sync();
  } finally {
    await handle.close();
  }
}

function integrityResult(databasePath) {
  const database = new DatabaseSync(databasePath, { readOnly: true });
  try {
    const rows = database.prepare("PRAGMA integrity_check").all();
    if (
      rows.length !== 1 ||
      Object.values(rows[0]).length !== 1 ||
      String(Object.values(rows[0])[0]).toLowerCase() !== "ok"
    ) {
      fail(`SQLite integrity_check failed for ${basename(databasePath)}`);
    }
  } finally {
    database.close();
  }
}

function validateMetadata(metadata, directoryName) {
  if (!metadata || typeof metadata !== "object" || Array.isArray(metadata)) {
    fail(`${metadataFileName} must contain a JSON object`);
  }
  if (metadata.format_version !== 1) {
    fail(`${metadataFileName} has an unsupported format_version`);
  }
  const createdAt = requireDate(metadata.created_at, "metadata.created_at");
  if (backupId(createdAt) !== directoryName) {
    fail(`${metadataFileName} timestamp does not match its directory`);
  }
  requireNonEmpty(metadata.source, "metadata.source");
  requireNonEmpty(metadata.deployment_version, "metadata.deployment_version");
  if (!Number.isSafeInteger(metadata.size_bytes) || metadata.size_bytes <= 0) {
    fail("metadata.size_bytes must be a positive integer");
  }
  if (typeof metadata.sha256 !== "string" || !checksumPattern.test(metadata.sha256)) {
    fail("metadata.sha256 must be a lowercase SHA-256 digest");
  }
  return { createdAt, metadata };
}

async function readMetadata(
  backupDirectory,
  directoryName = basename(backupDirectory),
) {
  let metadata;
  try {
    metadata = JSON.parse(
      await readFile(join(backupDirectory, metadataFileName), "utf8"),
    );
  } catch (error) {
    fail(
      `cannot read ${directoryName}/${metadataFileName}: ${
        error instanceof Error ? error.message : String(error)
      }`,
    );
  }
  return validateMetadata(metadata, directoryName);
}

async function verifyBackupFiles(
  backupDirectory,
  directoryName = basename(backupDirectory),
) {
  const { createdAt, metadata } = await readMetadata(
    backupDirectory,
    directoryName,
  );
  const databasePath = join(backupDirectory, databaseFileName);
  const databaseStat = await stat(databasePath);
  if (!databaseStat.isFile() || databaseStat.size !== metadata.size_bytes) {
    fail(`${databaseFileName} size does not match metadata`);
  }

  const actualSha256 = await sha256(databasePath);
  if (actualSha256 !== metadata.sha256) {
    fail(`${databaseFileName} SHA-256 does not match metadata`);
  }

  const checksum = await readFile(join(backupDirectory, checksumFileName), "utf8");
  if (checksum !== `${metadata.sha256}  ${databaseFileName}\n`) {
    fail(`${checksumFileName} does not match metadata`);
  }
  return { createdAt, databasePath, metadata };
}

async function finalizedBackupNames(backupRoot) {
  const entries = await readdir(backupRoot, { withFileTypes: true });
  return entries
    .filter((entry) => entry.isDirectory() && backupDirectoryPattern.test(entry.name))
    .map((entry) => entry.name)
    .sort();
}

async function pruneBackups({ backupRoot, retentionMilliseconds, now, keep }) {
  const cutoff = now.getTime() - retentionMilliseconds;
  for (const name of await finalizedBackupNames(backupRoot)) {
    if (name === keep) continue;
    const directory = join(backupRoot, name);
    const { createdAt } = await readMetadata(directory);
    if (createdAt.getTime() < cutoff) {
      await rm(directory, { recursive: true });
    }
  }
}

export async function createBackup({
  source,
  backupRoot,
  sourceId,
  deploymentVersion,
  retentionDays,
  now = new Date(),
}) {
  requireSafeRoot(backupRoot, "backupRoot");
  requireNonEmpty(sourceId, "sourceId");
  requireNonEmpty(deploymentVersion, "deploymentVersion");
  retentionDays = requirePositiveInteger(retentionDays, "retentionDays");
  const retentionMilliseconds = requireSafeDuration(
    retentionDays,
    24 * 60 * 60 * 1000,
    "retentionDays",
  );
  if (!(now instanceof Date) || !Number.isFinite(now.getTime())) {
    fail("now must be a valid Date");
  }

  const sourceStat = await stat(source);
  if (!sourceStat.isFile()) fail("source must be a regular SQLite database file");

  await mkdir(backupRoot, { recursive: true, mode: 0o700 });
  await chmod(backupRoot, 0o700);
  const id = backupId(now);
  const finalDirectory = join(backupRoot, id);
  const partialDirectory = join(
    backupRoot,
    `.partial-${id}-${randomUUID()}`,
  );
  await mkdir(partialDirectory, { mode: 0o700 });

  try {
    const databasePath = join(partialDirectory, databaseFileName);
    const sourceDatabase = new DatabaseSync(source, { readOnly: true });
    try {
      await backup(sourceDatabase, databasePath, { rate: 100 });
    } finally {
      sourceDatabase.close();
    }
    await chmod(databasePath, 0o600);
    integrityResult(databasePath);

    const databaseStat = await stat(databasePath);
    const digest = await sha256(databasePath);
    const metadata = {
      format_version: 1,
      created_at: now.toISOString(),
      source: sourceId,
      deployment_version: deploymentVersion,
      size_bytes: databaseStat.size,
      sha256: digest,
    };
    await writePrivateFile(
      join(partialDirectory, checksumFileName),
      `${digest}  ${databaseFileName}\n`,
    );
    await writePrivateFile(
      join(partialDirectory, metadataFileName),
      `${JSON.stringify(metadata, null, 2)}\n`,
    );
    await verifyBackupFiles(partialDirectory, id);
    await rename(partialDirectory, finalDirectory);
    await pruneBackups({
      backupRoot,
      retentionMilliseconds,
      now,
      keep: id,
    });
    return { id, ...metadata };
  } catch (error) {
    await rm(partialDirectory, { recursive: true, force: true });
    throw error;
  }
}

export async function checkLatestBackup({
  backupRoot,
  restoreRoot,
  maxAgeSeconds,
  now = new Date(),
}) {
  requireSafeRoot(backupRoot, "backupRoot");
  requireSafeRoot(restoreRoot, "restoreRoot");
  maxAgeSeconds = requirePositiveInteger(maxAgeSeconds, "maxAgeSeconds");
  const maxAgeMilliseconds = requireSafeDuration(
    maxAgeSeconds,
    1000,
    "maxAgeSeconds",
  );
  if (!(now instanceof Date) || !Number.isFinite(now.getTime())) {
    fail("now must be a valid Date");
  }

  const names = await finalizedBackupNames(backupRoot);
  if (names.length === 0) fail("no finalized Portal SQLite backup exists");
  const id = names.at(-1);
  const backupDirectory = join(backupRoot, id);
  const { createdAt, databasePath, metadata } =
    await verifyBackupFiles(backupDirectory);
  const ageMilliseconds = now.getTime() - createdAt.getTime();
  if (ageMilliseconds < 0) fail("latest backup timestamp is in the future");
  if (ageMilliseconds > maxAgeMilliseconds) {
    fail("latest Portal SQLite backup is stale");
  }

  await mkdir(restoreRoot, { recursive: true, mode: 0o700 });
  const isolatedDirectory = join(restoreRoot, `restore-${id}-${randomUUID()}`);
  await mkdir(isolatedDirectory, { mode: 0o700 });
  try {
    const restoredDatabase = join(isolatedDirectory, databaseFileName);
    await copyFile(databasePath, restoredDatabase);
    await chmod(restoredDatabase, 0o600);
    if ((await sha256(restoredDatabase)) !== metadata.sha256) {
      fail("isolated restore SHA-256 does not match metadata");
    }
    integrityResult(restoredDatabase);
  } finally {
    await rm(isolatedDirectory, { recursive: true, force: true });
  }

  return {
    id,
    created_at: metadata.created_at,
    size_bytes: metadata.size_bytes,
    sha256: metadata.sha256,
  };
}

function parseOptions(args, allowed) {
  const result = {};
  for (let index = 0; index < args.length; index += 2) {
    const option = args[index];
    const value = args[index + 1];
    if (!allowed.has(option) || value === undefined || value.startsWith("--")) {
      fail(`invalid or incomplete option: ${option ?? "(missing)"}`);
    }
    if (Object.hasOwn(result, option)) fail(`duplicate option: ${option}`);
    result[option] = value;
  }
  for (const option of allowed) {
    if (!Object.hasOwn(result, option)) fail(`missing required option: ${option}`);
  }
  return result;
}

async function main(args) {
  process.umask(0o077);
  const command = args[0];
  if (command === "backup") {
    const options = parseOptions(
      args.slice(1),
      new Set([
        "--source",
        "--backup-root",
        "--source-id",
        "--deployment-version",
        "--retention-days",
      ]),
    );
    const result = await createBackup({
      source: options["--source"],
      backupRoot: options["--backup-root"],
      sourceId: options["--source-id"],
      deploymentVersion: options["--deployment-version"],
      retentionDays: options["--retention-days"],
    });
    console.log(
      `Portal SQLite backup completed: ${result.id} ` +
        `(${result.size_bytes} bytes, SHA-256 ${result.sha256})`,
    );
    return;
  }

  if (command === "check") {
    const options = parseOptions(
      args.slice(1),
      new Set(["--backup-root", "--restore-root", "--max-age-seconds"]),
    );
    const result = await checkLatestBackup({
      backupRoot: options["--backup-root"],
      restoreRoot: options["--restore-root"],
      maxAgeSeconds: options["--max-age-seconds"],
    });
    console.log(
      `Portal SQLite restore check passed: ${result.id} ` +
        `(${result.size_bytes} bytes, SHA-256 ${result.sha256})`,
    );
    return;
  }

  fail("usage: portal-sqlite-backup.mjs <backup|check> [options]");
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main(process.argv.slice(2)).catch((error) => {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  });
}
