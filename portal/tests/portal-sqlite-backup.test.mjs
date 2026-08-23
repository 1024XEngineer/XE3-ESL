import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import {
  access,
  mkdtemp,
  mkdir,
  readFile,
  readdir,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { DatabaseSync } from "node:sqlite";
import test from "node:test";

import {
  checkLatestBackup,
  createBackup,
} from "../ops/portal-sqlite-backup.mjs";

const portalDirectory = dirname(dirname(fileURLToPath(import.meta.url)));

async function createSourceDatabase(root) {
  const source = join(root, "portal.sqlite");
  const database = new DatabaseSync(source);
  database.exec(`
    PRAGMA journal_mode = WAL;
    PRAGMA wal_autocheckpoint = 0;
    CREATE TABLE waitlist (
      id INTEGER PRIMARY KEY,
      email TEXT NOT NULL
    );
    INSERT INTO waitlist (email) VALUES ('first@example.com');
    INSERT INTO waitlist (email) VALUES ('second@example.com');
  `);
  return { database, source };
}

async function withTemporaryDirectory(callback) {
  const directory = await mkdtemp(join(tmpdir(), "portal-sqlite-backup-"));
  try {
    await callback(directory);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
}

test("creates a private online backup and verifies an isolated restore", async () => {
  await withTemporaryDirectory(async (root) => {
    const { database, source } = await createSourceDatabase(root);
    const backupRoot = join(root, "backups");
    const restoreRoot = join(root, "restore");
    const now = new Date("2026-08-22T01:02:03.456Z");
    try {
      await access(`${source}-wal`);
      const result = await createBackup({
        source,
        backupRoot,
        sourceId: "docker-volume:test-portal-data/portal.sqlite",
        deploymentVersion: "v0.1.0-test",
        retentionDays: 14,
        now,
      });

      assert.equal(result.id, "20260822T010203456Z");
      const directory = join(backupRoot, result.id);
      const metadata = JSON.parse(
        await readFile(join(directory, "metadata.json"), "utf8"),
      );
      assert.deepEqual(metadata, {
        format_version: 1,
        created_at: now.toISOString(),
        source: "docker-volume:test-portal-data/portal.sqlite",
        deployment_version: "v0.1.0-test",
        size_bytes: result.size_bytes,
        sha256: result.sha256,
      });
      assert.equal((await stat(directory)).mode & 0o777, 0o700);
      assert.equal(
        (await stat(join(directory, "portal.sqlite"))).mode & 0o777,
        0o600,
      );
      assert.equal(
        await readFile(join(directory, "portal.sqlite.sha256"), "utf8"),
        `${result.sha256}  portal.sqlite\n`,
      );

      const snapshot = new DatabaseSync(join(directory, "portal.sqlite"), {
        readOnly: true,
      });
      try {
        assert.equal(
          snapshot.prepare("SELECT count(*) AS count FROM waitlist").get().count,
          2,
        );
      } finally {
        snapshot.close();
      }

      const check = await checkLatestBackup({
        backupRoot,
        restoreRoot,
        maxAgeSeconds: 120,
        now: new Date("2026-08-22T01:03:00.000Z"),
      });
      assert.equal(check.id, result.id);
      assert.deepEqual(await readdir(restoreRoot), []);
    } finally {
      database.close();
    }
  });
});

test("fails closed when the newest backup is stale or damaged", async () => {
  await withTemporaryDirectory(async (root) => {
    const { database, source } = await createSourceDatabase(root);
    const backupRoot = join(root, "backups");
    const restoreRoot = join(root, "restore");
    try {
      const result = await createBackup({
        source,
        backupRoot,
        sourceId: "docker-volume:test-portal-data/portal.sqlite",
        deploymentVersion: "v0.1.0-test",
        retentionDays: 14,
        now: new Date("2026-08-20T00:00:00.000Z"),
      });
      await assert.rejects(
        checkLatestBackup({
          backupRoot,
          restoreRoot,
          maxAgeSeconds: 60,
          now: new Date("2026-08-20T00:02:00.000Z"),
        }),
        /backup is stale/,
      );

      const databasePath = join(backupRoot, result.id, "portal.sqlite");
      const originalDatabase = await readFile(databasePath);
      const damagedDatabase = Buffer.from(originalDatabase);
      damagedDatabase[0] ^= 0xff;
      await writeFile(databasePath, damagedDatabase);
      await assert.rejects(
        checkLatestBackup({
          backupRoot,
          restoreRoot,
          maxAgeSeconds: 300,
          now: new Date("2026-08-20T00:01:00.000Z"),
        }),
        /SHA-256 does not match metadata/,
      );
      await writeFile(databasePath, originalDatabase);

      await writeFile(
        join(backupRoot, result.id, "portal.sqlite.sha256"),
        `${"0".repeat(64)}  portal.sqlite\n`,
      );
      await assert.rejects(
        checkLatestBackup({
          backupRoot,
          restoreRoot,
          maxAgeSeconds: 300,
          now: new Date("2026-08-20T00:01:00.000Z"),
        }),
        /does not match metadata/,
      );
    } finally {
      database.close();
    }
  });
});

test("ignores interrupted partial output and rejects a failed backup", async () => {
  await withTemporaryDirectory(async (root) => {
    const backupRoot = join(root, "backups");
    const restoreRoot = join(root, "restore");
    await mkdir(join(backupRoot, ".partial-20260822T010203456Z-interrupted"), {
      recursive: true,
    });
    await assert.rejects(
      checkLatestBackup({
        backupRoot,
        restoreRoot,
        maxAgeSeconds: 60,
        now: new Date("2026-08-22T01:03:00.000Z"),
      }),
      /no finalized Portal SQLite backup exists/,
    );

    const invalidSource = join(root, "invalid.sqlite");
    await writeFile(invalidSource, "not a SQLite database");
    await assert.rejects(
      createBackup({
        source: invalidSource,
        backupRoot,
        sourceId: "docker-volume:test-portal-data/portal.sqlite",
        deploymentVersion: "v0.1.0-test",
        retentionDays: 14,
        now: new Date("2026-08-22T02:00:00.000Z"),
      }),
    );
    assert.deepEqual(
      (await readdir(backupRoot)).filter((name) => !name.includes("interrupted")),
      [],
    );
  });
});

test("prunes only finalized backups older than the explicit retention", async () => {
  await withTemporaryDirectory(async (root) => {
    const { database, source } = await createSourceDatabase(root);
    const backupRoot = join(root, "backups");
    try {
      const oldBackup = await createBackup({
        source,
        backupRoot,
        sourceId: "docker-volume:test-portal-data/portal.sqlite",
        deploymentVersion: "v0.1.0-test",
        retentionDays: 30,
        now: new Date("2026-08-01T00:00:00.000Z"),
      });
      const interrupted = join(backupRoot, ".partial-interrupted-evidence");
      await mkdir(interrupted);
      const currentBackup = await createBackup({
        source,
        backupRoot,
        sourceId: "docker-volume:test-portal-data/portal.sqlite",
        deploymentVersion: "v0.1.1-test",
        retentionDays: 7,
        now: new Date("2026-08-22T00:00:00.000Z"),
      });

      await assert.rejects(access(join(backupRoot, oldBackup.id)));
      await access(join(backupRoot, currentBackup.id));
      await access(interrupted);
    } finally {
      database.close();
    }
  });
});

test("deployment units expose timer, failure, and isolated-check contracts", async () => {
  const deployDirectory = join(portalDirectory, "deploy");
  const wrapper = join(deployDirectory, "xe3-portal-sqlite-backup");
  execFileSync("bash", ["-n", wrapper]);

  const backupService = await readFile(
    join(deployDirectory, "xe3-portal-sqlite-backup.service"),
    "utf8",
  );
  const restoreService = await readFile(
    join(deployDirectory, "xe3-portal-sqlite-restore-check.service"),
    "utf8",
  );
  const timer = await readFile(
    join(deployDirectory, "xe3-portal-sqlite-backup.timer"),
    "utf8",
  );
  const wrapperContent = await readFile(wrapper, "utf8");

  assert.match(backupService, /^Type=oneshot$/m);
  assert.match(backupService, /EnvironmentFile=\/etc\/speakup\/portal-backup\.env/);
  assert.match(backupService, /flock --nonblock/);
  assert.match(
    backupService,
    /\/usr\/bin\/env PORTAL_BACKUP_ROOT=\/var\/lib\/speakup\/portal-backups .* backup$/m,
  );
  assert.match(
    restoreService,
    /\/usr\/bin\/env PORTAL_BACKUP_ROOT=\/var\/lib\/speakup\/portal-backups .* check$/m,
  );
  assert.match(restoreService, /xe3-portal-sqlite-backup check/);
  assert.match(timer, /^OnCalendar=daily$/m);
  assert.match(timer, /^Persistent=true$/m);
  assert.match(wrapperContent, /dst=\/source,readonly,volume-nocopy/);
  assert.match(wrapperContent, /dst=\/backups,readonly/);
  assert.match(wrapperContent, /type=volume,dst=\/restore,volume-nocopy/);
  assert.doesNotMatch(wrapperContent, /PORTAL_ADMIN_PASSWORD/);
});
