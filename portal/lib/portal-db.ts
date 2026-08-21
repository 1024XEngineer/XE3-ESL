import {
  createPortalEventsSessionIndex,
  createPortalEventsTable,
  createPortalEventsTypeDateIndex,
  createPortalWaitlistDateIndex,
  createWaitlistTable,
} from "../db/schema";

interface D1Result<T = Record<string, unknown>> {
  results?: T[];
  success: boolean;
}

interface D1PreparedStatement {
  bind(...values: unknown[]): D1PreparedStatement;
  first<T = Record<string, unknown>>(): Promise<T | null>;
  all<T = Record<string, unknown>>(): Promise<D1Result<T>>;
  run(): Promise<D1Result>;
}

export interface PortalDatabase {
  prepare(query: string): D1PreparedStatement;
  batch(statements: D1PreparedStatement[]): Promise<D1Result[]>;
}

interface PortalEnv {
  DB?: PortalDatabase;
  PORTAL_ADMIN_PASSWORD?: string;
}

type NodeSqliteValue =
  | null
  | number
  | bigint
  | string
  | NodeJS.ArrayBufferView;

interface NodeSqliteStatement {
  all(...values: NodeSqliteValue[]): Record<string, unknown>[];
  get(...values: NodeSqliteValue[]): Record<string, unknown> | undefined;
  run(...values: NodeSqliteValue[]): unknown;
}

interface NodeSqliteDatabase {
  exec(query: string): void;
  prepare(query: string): NodeSqliteStatement;
}

interface NodeSqliteModule {
  DatabaseSync: new (path: string) => NodeSqliteDatabase;
}

interface NodeFsModule {
  mkdir(path: string, options: { recursive: true }): Promise<unknown>;
}

interface NodePathModule {
  dirname(path: string): string;
}

let cloudflareEnvPromise: Promise<PortalEnv> | undefined;
let nodeDatabasePromise: Promise<PortalDatabase> | undefined;

function getNodeDatabasePath(): string | null {
  if (
    typeof process === "undefined" ||
    process.release?.name !== "node"
  ) {
    return null;
  }
  return process.env.PORTAL_SQLITE_PATH?.trim() || null;
}

async function importRuntimeModule<T>(specifier: string): Promise<T> {
  // A variable specifier keeps Vite from resolving Node-only modules into the
  // Cloudflare bundle. Node-only modules are imported only when
  // PORTAL_SQLITE_PATH explicitly enables the server fallback.
  return import(/* @vite-ignore */ specifier) as Promise<T>;
}

async function loadCloudflareEnv(): Promise<PortalEnv> {
  const { env } = await import("cloudflare:workers");
  return env as unknown as PortalEnv;
}

function getCloudflareEnv(): Promise<PortalEnv> {
  cloudflareEnvPromise ??= loadCloudflareEnv();
  return cloudflareEnvPromise;
}

function asNodeSqliteValues(values: unknown[]): NodeSqliteValue[] {
  return values as NodeSqliteValue[];
}

class NodePortalPreparedStatement implements D1PreparedStatement {
  private statement: NodeSqliteStatement | undefined;

  constructor(
    private readonly database: NodeSqliteDatabase,
    private readonly query: string,
    private readonly values: unknown[] = [],
  ) {}

  bind(...values: unknown[]): D1PreparedStatement {
    return new NodePortalPreparedStatement(this.database, this.query, values);
  }

  private getStatement(): NodeSqliteStatement {
    this.statement ??= this.database.prepare(this.query);
    return this.statement;
  }

  async first<T = Record<string, unknown>>(): Promise<T | null> {
    const row = this.getStatement().get(...asNodeSqliteValues(this.values));
    return (row as T | undefined) ?? null;
  }

  async all<T = Record<string, unknown>>(): Promise<D1Result<T>> {
    const results = this.getStatement().all(
      ...asNodeSqliteValues(this.values),
    ) as T[];
    return { results, success: true };
  }

  runInBatch(): D1Result {
    this.getStatement().run(...asNodeSqliteValues(this.values));
    return { success: true };
  }

  async run(): Promise<D1Result> {
    return this.runInBatch();
  }
}

class NodePortalDatabase implements PortalDatabase {
  constructor(private readonly database: NodeSqliteDatabase) {}

  prepare(query: string): D1PreparedStatement {
    return new NodePortalPreparedStatement(this.database, query);
  }

  async batch(statements: D1PreparedStatement[]): Promise<D1Result[]> {
    const nodeStatements = statements.map((statement) => {
      if (!(statement instanceof NodePortalPreparedStatement)) {
        throw new TypeError(
          "A Node portal database batch can only contain its own statements.",
        );
      }
      return statement;
    });

    this.database.exec("BEGIN IMMEDIATE");
    try {
      const results = nodeStatements.map((statement) =>
        statement.runInBatch()
      );
      this.database.exec("COMMIT");
      return results;
    } catch (error) {
      try {
        this.database.exec("ROLLBACK");
      } catch {
        // Preserve the original database error if rollback also fails.
      }
      throw error;
    }
  }
}

async function createNodeDatabase(databasePath: string): Promise<PortalDatabase> {
  const sqliteSpecifier = ["node", "sqlite"].join(":");
  const fsSpecifier = ["node:fs", "promises"].join("/");
  const pathSpecifier = ["node", "path"].join(":");
  const [sqlite, fs, path] = await Promise.all([
    importRuntimeModule<NodeSqliteModule>(sqliteSpecifier),
    importRuntimeModule<NodeFsModule>(fsSpecifier),
    importRuntimeModule<NodePathModule>(pathSpecifier),
  ]);

  if (databasePath !== ":memory:") {
    await fs.mkdir(path.dirname(databasePath), { recursive: true });
  }

  return new NodePortalDatabase(new sqlite.DatabaseSync(databasePath));
}

function getNodeDatabase(databasePath: string): Promise<PortalDatabase> {
  nodeDatabasePromise ??= createNodeDatabase(databasePath);
  return nodeDatabasePromise;
}

export async function getPortalEnv(): Promise<PortalEnv> {
  return getNodeDatabasePath()
    ? { PORTAL_ADMIN_PASSWORD: process.env.PORTAL_ADMIN_PASSWORD }
    : getCloudflareEnv();
}

export async function getPortalDatabase(): Promise<PortalDatabase> {
  const databasePath = getNodeDatabasePath();
  const database = databasePath
    ? await getNodeDatabase(databasePath)
    : (await getCloudflareEnv()).DB;
  if (!database) throw new Error("Portal database binding is unavailable.");

  await database.batch([
    database.prepare(createPortalEventsTable),
    database.prepare(createWaitlistTable),
    database.prepare(createPortalEventsTypeDateIndex),
    database.prepare(createPortalEventsSessionIndex),
    database.prepare(createPortalWaitlistDateIndex),
  ]);

  return database;
}
