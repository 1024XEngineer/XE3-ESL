import { readFile } from "node:fs/promises";
import path from "node:path";

function parseDotEnvValue(content, key) {
  for (const originalLine of content.split(/\r?\n/)) {
    const line = originalLine.trim().replace(/^export\s+/, "");
    if (!line || line.startsWith("#")) continue;
    const separator = line.indexOf("=");
    if (separator < 0 || line.slice(0, separator).trim() !== key) continue;
    let value = line.slice(separator + 1).trim();
    if (
      value.length >= 2 &&
      ((value.startsWith('"') && value.endsWith('"')) ||
        (value.startsWith("'") && value.endsWith("'")))
    ) {
      value = value.slice(1, -1);
    } else {
      value = value.replace(/\s+#.*$/, "").trim();
    }
    return value;
  }
  return "";
}

export async function benchmarkDatabaseConfig(repoDirectory, databaseName) {
  if (!/^[a-z][a-z0-9_]{0,62}$/.test(databaseName)) {
    throw new Error("invalid benchmark database name");
  }
  let databaseURL = process.env.DATABASE_URL ?? "";
  if (!databaseURL) {
    const dotenv = await readFile(path.join(repoDirectory, ".env"), "utf8");
    databaseURL = parseDotEnvValue(dotenv, "DATABASE_URL");
  }
  if (!databaseURL) throw new Error("DATABASE_URL is required");

  const parsed = new URL(databaseURL);
  if (parsed.protocol !== "postgres:" && parsed.protocol !== "postgresql:") {
    throw new Error("DATABASE_URL must use postgres or postgresql");
  }
  const owner = decodeURIComponent(parsed.username);
  if (!/^[A-Za-z_][A-Za-z0-9_]{0,62}$/.test(owner)) {
    throw new Error("DATABASE_URL contains an invalid database user");
  }
  parsed.pathname = `/${databaseName}`;
  return { databaseURL: parsed.toString(), owner };
}
