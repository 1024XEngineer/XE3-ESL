#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";
import {
  calculateMetrics,
  evaluateCases,
  parseJsonLog,
  writeReports,
} from "./lib/report.mjs";

const toolDirectory = path.dirname(fileURLToPath(import.meta.url));

function parseArguments(argv) {
  const result = {
    baseUrl: process.env.BENCHMARK_BASE_URL || "http://127.0.0.1:18080",
    casesFile: path.join(toolDirectory, "cases.json"),
    logFile: process.env.BENCHMARK_LOG_FILE || "",
    reportDirectory: path.join(toolDirectory, "reports"),
  };
  for (let index = 0; index < argv.length; index += 1) {
    const value = argv[index + 1];
    switch (argv[index]) {
      case "--base-url":
        result.baseUrl = value;
        index += 1;
        break;
      case "--cases":
        result.casesFile = path.resolve(value);
        index += 1;
        break;
      case "--log-file":
        result.logFile = path.resolve(value);
        index += 1;
        break;
      case "--report-dir":
        result.reportDirectory = path.resolve(value);
        index += 1;
        break;
      default:
        throw new Error(`unknown argument: ${argv[index]}`);
    }
  }
  if (!result.logFile) {
    throw new Error("--log-file or BENCHMARK_LOG_FILE is required");
  }
  return result;
}

async function request(baseUrl, route, { token, body, expected }) {
  const response = await fetch(`${baseUrl}${route}`, {
    method: body === undefined ? "GET" : "POST",
    headers: {
      Accept: "application/json",
      ...(body === undefined ? {} : { "Content-Type": "application/json" }),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    signal: AbortSignal.timeout(120_000),
  });
  const text = await response.text();
  let payload = {};
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      payload = { raw: text };
    }
  }
  if (!expected.includes(response.status)) {
    throw new Error(
      `${route} returned HTTP ${response.status}: ${text.slice(0, 500)}`,
    );
  }
  return { status: response.status, payload };
}

async function waitForRun(baseUrl, token, run) {
  let current = run;
  for (let attempt = 0; attempt < 120; attempt += 1) {
    if (current.status === "completed" || current.status === "failed") {
      return current;
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
    const response = await request(
      baseUrl,
      `/v1/agent-runs/${current.run_id}`,
      { token, expected: [200] },
    );
    current = response.payload;
  }
  throw new Error(`run ${run.run_id} did not reach a terminal state`);
}

async function assistantMessageForRun(baseUrl, token, threadId, runId) {
  const response = await request(
    baseUrl,
    `/v1/agent-threads/${encodeURIComponent(threadId)}/messages?page_size=20`,
    { token, expected: [200] },
  );
  const messages = Array.isArray(response.payload.messages)
    ? response.payload.messages
    : [];
  const message = messages.find(
    (item) =>
      item?.role === "assistant" && item?.produced_by_run_id === runId,
  );
  if (!message || typeof message.message_id !== "string" ||
      typeof message.content !== "string") {
    throw new Error(`run ${runId} has no persisted assistant message`);
  }
  return message;
}

async function createIdentity(baseUrl) {
  const suffix = `${Date.now()}-${process.pid}`;
  const email = `agent-routing-benchmark-${suffix}@example.com`;
  const password = `Benchmark-${suffix}-Only`;
  await request(baseUrl, "/v1/auth/register", {
    body: { email, password, display_name: "Routing Benchmark" },
    expected: [201],
  });
  const login = await request(baseUrl, "/v1/auth/login", {
    body: { email, password },
    expected: [200],
  });
  return { email, token: login.payload.session_token };
}

async function executeCase(baseUrl, token, testCase, index) {
  const threadResponse = await request(baseUrl, "/v1/agent-threads", {
    token,
    body: {},
    expected: [201],
  });
  const threadId = threadResponse.payload.thread_id;
  const runs = [];

  for (let turn = 0; turn < testCase.messages.length; turn += 1) {
    const response = await request(
      baseUrl,
      `/v1/agent-threads/${threadId}/runs`,
      {
        token,
        body: {
          client_message_id: `benchmark-${Date.now()}-${index}-${turn}`,
          content: testCase.messages[turn],
        },
        expected: [201, 202],
      },
    );
    runs.push(await waitForRun(baseUrl, token, response.payload));
  }

  const targetRun = runs.at(-1);
  const targetMessage = targetRun.status === "completed"
    ? await assistantMessageForRun(baseUrl, token, threadId, targetRun.run_id)
    : null;
  return {
    name: testCase.name,
    thread_id: threadId,
    run_ids: runs.map((run) => run.run_id),
    target_run_id: targetRun.run_id,
    status: targetRun.status,
    http_ok: true,
    assistant_message_id: targetMessage?.message_id ?? "",
    assistant_response: targetMessage?.content ?? "",
    provider: targetRun.requested_provider ?? "",
    model: targetRun.requested_model ?? "",
    failure: targetRun.failure ?? null,
  };
}

function gitRevision() {
  try {
    const revision = execFileSync("git", ["rev-parse", "--short", "HEAD"], {
      encoding: "utf8",
    }).trim();
    const status = execFileSync("git", ["status", "--porcelain"], {
      encoding: "utf8",
    }).trim();
    return status ? `${revision}-dirty` : revision;
  } catch {
    return "";
  }
}

function reportName(date) {
  return date.toISOString().replaceAll(":", "").replaceAll(".", "-");
}

async function main() {
  const options = parseArguments(process.argv.slice(2));
  const casesRaw = await readFile(options.casesFile, "utf8");
  const cases = JSON.parse(casesRaw);
  if (!Array.isArray(cases) || cases.length === 0) {
    throw new Error("cases file must contain a non-empty JSON array");
  }
  await request(options.baseUrl, "/health", { expected: [200] });
  const identity = await createIdentity(options.baseUrl);
  const executions = [];

  for (let index = 0; index < cases.length; index += 1) {
    const testCase = cases[index];
    process.stdout.write(
      `[${index + 1}/${cases.length}] ${testCase.name} ... `,
    );
    try {
      const execution = await executeCase(
        options.baseUrl,
        identity.token,
        testCase,
        index,
      );
      executions.push(execution);
      process.stdout.write(`${execution.status}\n`);
    } catch (error) {
      executions.push({
        name: testCase.name,
        http_ok: false,
        status: "error",
        error: error.message,
      });
      process.stdout.write(`error: ${error.message}\n`);
    }
  }

  await mkdir(options.reportDirectory, { recursive: true });
  const executionFile = path.join(
    options.reportDirectory,
    ".latest-executions.json",
  );
  await writeFile(executionFile, JSON.stringify(executions, null, 2));
  const logContent = await readFile(options.logFile, "utf8");
  const results = evaluateCases(cases, executions, parseJsonLog(logContent));
  const metrics = calculateMetrics(results);
  const now = new Date();
  const currentReportName = reportName(now);
  const firstExecution = executions.find((execution) => execution.model);
  const metadata = {
    report_id: currentReportName,
    generated_at: now.toISOString(),
    git_revision: gitRevision(),
    suite_fingerprint: `sha256:${createHash("sha256").update(JSON.stringify(cases)).digest("hex")}`,
    base_url: options.baseUrl,
    cases_file: path.relative(process.cwd(), options.casesFile),
    provider: firstExecution?.provider ?? "",
    model: firstExecution?.model ?? "",
    temporary_user: identity.email,
  };
  const outputs = await writeReports({
    reportDirectory: options.reportDirectory,
    reportName: currentReportName,
    metadata,
    results,
    metrics,
    executions,
    logContent,
  });

  process.stdout.write(`\n总体准确率: ${metrics.overall.percentage.toFixed(1)}%\n`);
  process.stdout.write(`Markdown: ${outputs.markdown}\n`);
  process.stdout.write(`HTML: ${outputs.html}\n`);
  process.exitCode = metrics.overall.passed === metrics.overall.total ? 0 : 2;
}

main().catch((error) => {
  process.stderr.write(`benchmark failed: ${error.stack || error.message}\n`);
  process.exitCode = 1;
});
