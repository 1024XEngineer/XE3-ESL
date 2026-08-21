import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { request } from "node:http";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { setTimeout as delay } from "node:timers/promises";

const prototypeDirectory = dirname(fileURLToPath(new URL("../package.json", import.meta.url)));

async function reservePort() {
  const server = createServer();
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  const port = typeof address === "object" && address ? address.port : 0;
  await new Promise((resolve, reject) => {
    server.close((error) => error ? reject(error) : resolve());
  });
  return port;
}

function sendRequest(port, {
  body,
  headers = {},
  method = "GET",
  path = "/",
} = {}) {
  const payload = body === undefined ? undefined : JSON.stringify(body);
  return new Promise((resolve, reject) => {
    const outgoing = request({
      host: "127.0.0.1",
      port,
      path,
      method,
      headers: {
        Host: "speak-up.top",
        ...(payload
          ? {
              "Content-Length": Buffer.byteLength(payload),
              "Content-Type": "application/json",
            }
          : {}),
        ...headers,
      },
    }, (response) => {
      let responseBody = "";
      response.setEncoding("utf8");
      response.on("data", (chunk) => {
        responseBody += chunk;
      });
      response.on("end", () => {
        resolve({ body: responseBody, status: response.statusCode });
      });
    });
    outgoing.once("error", reject);
    if (payload) outgoing.write(payload);
    outgoing.end();
  });
}

async function waitForServer(port, logs) {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    try {
      const response = await sendRequest(port);
      if (response.status === 200) return;
    } catch {
      // The production server is still starting.
    }
    await delay(50);
  }
  throw new Error(`vinext production server did not start:\n${logs.join("")}`);
}

test("production server restores the HTTPS origin from the trusted proxy", {
  timeout: 20_000,
}, async (t) => {
  const directory = await mkdtemp(join(tmpdir(), "speakup-proxy-origin-"));
  const port = await reservePort();
  const logs = [];
  const server = spawn(
    process.execPath,
    [
      "node_modules/vinext/dist/cli.js",
      "start",
      "--hostname",
      "127.0.0.1",
      "--port",
      String(port),
    ],
    {
      cwd: prototypeDirectory,
      env: {
        ...process.env,
        PORTAL_ADMIN_PASSWORD: "proxy-origin-test-password",
        PORTAL_SQLITE_PATH: join(directory, "portal.sqlite"),
        VINEXT_TRUSTED_HOSTS: "speak-up.top,www.speak-up.top",
      },
      stdio: ["ignore", "pipe", "pipe"],
    },
  );
  server.stdout.on("data", (chunk) => logs.push(String(chunk)));
  server.stderr.on("data", (chunk) => logs.push(String(chunk)));

  t.after(async () => {
    if (server.exitCode === null) {
      server.kill("SIGTERM");
      for (let attempt = 0; attempt < 40 && server.exitCode === null; attempt += 1) {
        await delay(50);
      }
    }
    await rm(directory, { force: true, recursive: true });
  });

  await waitForServer(port, logs);
  const event = {
    eventType: "page_view",
    sessionId: "proxy-origin-session",
  };
  const accepted = await sendRequest(port, {
    body: event,
    headers: {
      Origin: "https://speak-up.top",
      "X-Forwarded-Proto": "https",
    },
    method: "POST",
    path: "/api/events",
  });
  assert.equal(accepted.status, 201, logs.join(""));

  const rejected = await sendRequest(port, {
    body: event,
    headers: {
      Origin: "https://attacker.invalid",
      "X-Forwarded-Proto": "https",
    },
    method: "POST",
    path: "/api/events",
  });
  assert.equal(rejected.status, 403, logs.join(""));
});
