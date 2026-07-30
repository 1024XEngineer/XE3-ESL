import assert from "node:assert/strict";
import { mkdtemp, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { benchmarkDatabaseConfig } from "./database.mjs";

test("derives an isolated database URL without changing connection options", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "routing-database-"));
  await writeFile(
    path.join(directory, ".env"),
    "DATABASE_URL='postgres://app:secret@127.0.0.1:5432/main?sslmode=disable'\n",
  );

  const config = await benchmarkDatabaseConfig(
    directory,
    "agent_benchmark_123",
  );
  assert.equal(config.owner, "app");
  assert.equal(
    config.databaseURL,
    "postgres://app:secret@127.0.0.1:5432/agent_benchmark_123?sslmode=disable",
  );
});

test("rejects unsafe database names", async () => {
  await assert.rejects(
    benchmarkDatabaseConfig("/tmp", "main;drop database"),
    /invalid benchmark database name/,
  );
});
