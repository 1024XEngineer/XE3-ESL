#!/usr/bin/env node

import { createReadStream } from "node:fs";
import { stat, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";
import { loadHistory, recordReport } from "./lib/history.mjs";

const toolDirectory = path.dirname(fileURLToPath(import.meta.url));

function parseOptions(argv) {
  const result = {
    host: "127.0.0.1",
    port: Number(process.env.BENCHMARK_REPORT_PORT || 0),
    reportDirectory: path.join(toolDirectory, "reports"),
    urlFile: "",
  };
  for (let index = 0; index < argv.length; index += 1) {
    const value = argv[index + 1];
    if (argv[index] === "--port") result.port = Number(value);
    else if (argv[index] === "--report-dir") {
      result.reportDirectory = path.resolve(value);
    } else if (argv[index] === "--url-file") {
      result.urlFile = path.resolve(value);
    } else {
      throw new Error(`unknown argument: ${argv[index]}`);
    }
    index += 1;
  }
  if (!Number.isInteger(result.port) || result.port < 0 || result.port > 65535) {
    throw new Error("invalid report server port");
  }
  return result;
}

function sendJson(response, status, payload) {
  const body = JSON.stringify(payload);
  response.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Content-Length": Buffer.byteLength(body),
    "Cache-Control": "no-store",
  });
  response.end(body);
}

async function readBody(request) {
  const chunks = [];
  let size = 0;
  for await (const chunk of request) {
    size += chunk.length;
    if (size > 16_384) throw new Error("request body too large");
    chunks.push(chunk);
  }
  return JSON.parse(Buffer.concat(chunks).toString("utf8") || "{}");
}

function contentType(file) {
  if (file.endsWith(".html")) return "text/html; charset=utf-8";
  if (file.endsWith(".json")) return "application/json; charset=utf-8";
  if (file.endsWith(".md")) return "text/markdown; charset=utf-8";
  if (file.endsWith(".log")) return "text/plain; charset=utf-8";
  return "application/octet-stream";
}

async function serveFile(reportDirectory, pathname, response) {
  const name = pathname === "/" ? "latest.html" : pathname.slice(1);
  if (
    !/^[A-Za-z0-9._:-]+$/.test(name) ||
    name.startsWith(".") ||
    ![".html", ".json", ".md", ".log"].includes(path.extname(name))
  ) {
    sendJson(response, 404, { error: "not_found" });
    return;
  }
  const file = path.join(reportDirectory, name);
  try {
    const info = await stat(file);
    if (!info.isFile()) throw new Error("not a file");
    response.writeHead(200, {
      "Content-Type": contentType(file),
      "Content-Length": info.size,
      "Cache-Control": "no-store",
    });
    createReadStream(file).pipe(response);
  } catch {
    sendJson(response, 404, { error: "not_found" });
  }
}

async function main() {
  const config = parseOptions(process.argv.slice(2));
  const server = createServer(async (request, response) => {
    try {
      const url = new URL(request.url, "http://127.0.0.1");
      if (request.method === "GET" && url.pathname === "/api/health") {
        sendJson(response, 200, { status: "ok" });
        return;
      }
      if (request.method === "GET" && url.pathname === "/api/history") {
        sendJson(response, 200, await loadHistory(config.reportDirectory));
        return;
      }
      if (request.method === "POST" && url.pathname === "/api/history") {
        const body = await readBody(request);
        const result = await recordReport(
          config.reportDirectory,
          body.report_id,
          body.label,
        );
        sendJson(response, result.created ? 201 : 200, result);
        return;
      }
      if (request.method === "GET") {
        await serveFile(config.reportDirectory, url.pathname, response);
        return;
      }
      sendJson(response, 405, { error: "method_not_allowed" });
    } catch (error) {
      const status = /not found/.test(error.message) ? 404 : 400;
      sendJson(response, status, { error: error.message });
    }
  });

  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(config.port, config.host, async () => {
      try {
        const address = server.address();
        const url = `http://${config.host}:${address.port}`;
        if (config.urlFile) await writeFile(config.urlFile, url);
        process.stdout.write(`report server started ${url}\n`);
        resolve();
      } catch (error) {
        reject(error);
      }
    });
  });
}

main().catch((error) => {
  process.stderr.write(`report server failed: ${error.stack || error.message}\n`);
  process.exitCode = 1;
});
