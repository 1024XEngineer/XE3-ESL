#!/usr/bin/env node

import process from "node:process";
import { benchmarkDatabaseConfig } from "./lib/database.mjs";

const [repoDirectory, databaseName, field] = process.argv.slice(2);
if (!repoDirectory || !databaseName || !["url", "owner"].includes(field)) {
  process.stderr.write(
    "usage: database_config.mjs REPO_DIR DATABASE_NAME url|owner\n",
  );
  process.exit(1);
}

benchmarkDatabaseConfig(repoDirectory, databaseName)
  .then((config) => {
    process.stdout.write(
      field === "url" ? config.databaseURL : config.owner,
    );
  })
  .catch((error) => {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  });
