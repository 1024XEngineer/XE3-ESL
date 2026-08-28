import assert from "node:assert/strict";
import test from "node:test";

import { assessMetadata, checkAvatar, parseArguments } from "./check.mjs";

const id = "1843ff9f-db3a-45de-be28-9c2b9d6412a3";
const resource = (name) => ({
  resource: {
    type: name,
    local: `${name}.bin`,
    remote: `https://cdn.spatialwalk.cloud/${name}.bin`,
  },
});
const validMetadata = () => ({
  characterId: id,
  version: "2.3.0",
  camera: resource("camera"),
  models: { gsStandard: resource("model") },
  animations: { idle: resource("animation") },
});

test("accepts metadata with camera, model, and animation resources", () => {
  const result = assessMetadata(validMetadata(), id);
  assert.equal(result.compatible, true);
  assert.equal(result.resourceURLs.length, 3);
});

test("rejects Lisa-shaped metadata whose top-level camera is null", () => {
  const metadata = validMetadata();
  metadata.camera = null;
  metadata.characterSettings = { camera: { translationZ: 1.2 } };
  const result = assessMetadata(metadata, id);
  assert.equal(result.compatible, false);
  assert.deepEqual(result.reasons, ["camera must be an object (received null)"]);
});

test("rejects asset resources hosted outside SpatialReal domains", () => {
  const metadata = validMetadata();
  metadata.camera.resource.remote = "https://example.test/camera.bin";
  const result = assessMetadata(metadata, id);
  assert.equal(result.compatible, false);
  assert.deepEqual(result.reasons, [
    "resource URL uses an unexpected host: example.test",
  ]);
});

test("fails closed for invalid IDs without making a request", async () => {
  let requested = false;
  const result = await checkAvatar(
    { label: "invalid", id: "not-an-id" },
    { fetchImpl: async () => { requested = true; } },
  );
  assert.equal(result.status, "error");
  assert.equal(requested, false);
});

test("reports non-success HTTP responses", async () => {
  const result = await checkAvatar(
    { label: "Nathan", id },
    { fetchImpl: async () => new Response("missing", { status: 404 }) },
  );
  assert.equal(result.status, "error");
  assert.deepEqual(result.reasons, ["metadata request returned HTTP 404"]);
});

test("reports malformed JSON responses", async () => {
  const result = await checkAvatar(
    { label: "Nathan", id },
    { fetchImpl: async () => new Response("not-json") },
  );
  assert.equal(result.status, "error");
  assert.deepEqual(result.reasons, ["metadata response is not valid JSON"]);
});

test("reports response body read failures", async () => {
  const result = await checkAvatar(
    { label: "Nathan", id },
    {
      fetchImpl: async () => ({
        ok: true,
        text: async () => {
          throw new Error("socket closed");
        },
      }),
    },
  );
  assert.equal(result.status, "error");
  assert.deepEqual(result.reasons, [
    "metadata response could not be read: socket closed",
  ]);
});

test("reports network failures without exposing request configuration", async () => {
  const result = await checkAvatar(
    { label: "Nathan", id },
    { fetchImpl: async () => { throw new Error("connection refused"); } },
  );
  assert.equal(result.status, "error");
  assert.deepEqual(result.reasons, ["metadata request failed: connection refused"]);
});

test("parses repeated avatar arguments and probe options", () => {
  const result = parseArguments([
    "--avatar",
    `Nathan=${id}`,
    "--avatar",
    id,
    "--probe-assets",
    "--json",
    "--timeout-ms",
    "5000",
  ]);
  assert.deepEqual(result.avatars, [
    { label: "Nathan", id },
    { label: id, id },
  ]);
  assert.equal(result.probeAssets, true);
  assert.equal(result.json, true);
  assert.equal(result.timeoutMilliseconds, 5000);
});
