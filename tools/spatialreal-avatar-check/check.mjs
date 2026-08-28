#!/usr/bin/env node

import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const avatarIDPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
const defaultEndpoint = "https://api.intl.spatialwalk.cloud/v2/character/";
const toolDirectory = path.dirname(fileURLToPath(import.meta.url));
const defaultCatalogPath = path.join(toolDirectory, "avatars.json");
const maximumMetadataBytes = 2 * 1024 * 1024;

function isObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function isSpatialRealAssetHost(hostname) {
  return (
    hostname === "spatialwalk.cloud" ||
    hostname.endsWith(".spatialwalk.cloud") ||
    hostname === "spatialwalk.top" ||
    hostname.endsWith(".spatialwalk.top")
  );
}

function remoteResources(value, resources = new Set()) {
  if (Array.isArray(value)) {
    for (const item of value) remoteResources(item, resources);
    return resources;
  }
  if (!isObject(value)) return resources;
  if (typeof value.remote === "string") resources.add(value.remote);
  for (const child of Object.values(value)) remoteResources(child, resources);
  return resources;
}

function requireResourceGroup(metadata, field, reasons) {
  const group = metadata[field];
  if (!isObject(group)) {
    reasons.push(`${field} must be an object (received ${group === null ? "null" : typeof group})`);
    return;
  }
  if (remoteResources(group).size === 0) {
    reasons.push(`${field} does not contain a downloadable remote resource`);
  }
}

export function assessMetadata(metadata, expectedID) {
  const reasons = [];
  if (!isObject(metadata)) {
    return { compatible: false, reasons: ["metadata must be a JSON object"], resourceURLs: [] };
  }
  if (metadata.characterId !== expectedID) {
    reasons.push("characterId does not match the requested avatar ID");
  }
  if (typeof metadata.version !== "string" || metadata.version.trim() === "") {
    reasons.push("version must be a non-empty string");
  }
  requireResourceGroup(metadata, "camera", reasons);
  requireResourceGroup(metadata, "models", reasons);
  requireResourceGroup(metadata, "animations", reasons);

  const resourceURLs = [...remoteResources(metadata)];
  for (const resourceURL of resourceURLs) {
    let parsed;
    try {
      parsed = new URL(resourceURL);
    } catch {
      reasons.push(`resource URL is invalid: ${resourceURL}`);
      continue;
    }
    if (parsed.protocol !== "https:") {
      reasons.push(`resource URL must use HTTPS: ${resourceURL}`);
    } else if (!isSpatialRealAssetHost(parsed.hostname)) {
      reasons.push(`resource URL uses an unexpected host: ${parsed.hostname}`);
    }
  }
  return { compatible: reasons.length === 0, reasons, resourceURLs };
}

async function fetchWithTimeout(fetchImpl, url, options, timeoutMilliseconds) {
  return fetchImpl(url, {
    ...options,
    signal: AbortSignal.timeout(timeoutMilliseconds),
  });
}

async function probeResource(fetchImpl, url, timeoutMilliseconds) {
  let response = await fetchWithTimeout(
    fetchImpl,
    url,
    { method: "HEAD", redirect: "follow" },
    timeoutMilliseconds,
  );
  if (response.status === 405 || response.status === 501) {
    response = await fetchWithTimeout(
      fetchImpl,
      url,
      {
        method: "GET",
        headers: { Range: "bytes=0-0" },
        redirect: "follow",
      },
      timeoutMilliseconds,
    );
    await response.body?.cancel();
  }
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
}

export async function checkAvatar(
  avatar,
  {
    endpoint = defaultEndpoint,
    fetchImpl = fetch,
    probeAssets = false,
    timeoutMilliseconds = 10_000,
  } = {},
) {
  if (!avatarIDPattern.test(avatar.id)) {
    return {
      ...avatar,
      status: "error",
      reasons: ["avatar ID must be a UUID"],
      resourceCount: 0,
    };
  }

  let response;
  try {
    response = await fetchWithTimeout(
      fetchImpl,
      new URL(encodeURIComponent(avatar.id), endpoint),
      { headers: { Accept: "application/json" }, redirect: "follow" },
      timeoutMilliseconds,
    );
  } catch (error) {
    return {
      ...avatar,
      status: "error",
      reasons: [`metadata request failed: ${error.message}`],
      resourceCount: 0,
    };
  }
  if (!response.ok) {
    await response.body?.cancel();
    return {
      ...avatar,
      status: "error",
      reasons: [`metadata request returned HTTP ${response.status}`],
      resourceCount: 0,
    };
  }

  let body;
  try {
    body = await response.text();
  } catch (error) {
    return {
      ...avatar,
      status: "error",
      reasons: [`metadata response could not be read: ${error.message}`],
      resourceCount: 0,
    };
  }
  if (Buffer.byteLength(body) > maximumMetadataBytes) {
    return {
      ...avatar,
      status: "error",
      reasons: ["metadata response exceeds 2 MiB"],
      resourceCount: 0,
    };
  }
  let metadata;
  try {
    metadata = JSON.parse(body);
  } catch {
    return {
      ...avatar,
      status: "error",
      reasons: ["metadata response is not valid JSON"],
      resourceCount: 0,
    };
  }

  const assessment = assessMetadata(metadata, avatar.id);
  const reasons = [...assessment.reasons];
  if (assessment.compatible && probeAssets) {
    const probes = await Promise.allSettled(
      assessment.resourceURLs.map((url) =>
        probeResource(fetchImpl, url, timeoutMilliseconds),
      ),
    );
    probes.forEach((probe, index) => {
      if (probe.status === "rejected") {
        const resource = new URL(assessment.resourceURLs[index]);
        reasons.push(
          `asset probe failed for ${resource.hostname}${resource.pathname}: ${probe.reason.message}`,
        );
      }
    });
  }

  return {
    ...avatar,
    status: reasons.length === 0 ? "compatible" : "incompatible",
    version: typeof metadata.version === "string" ? metadata.version : null,
    reasons,
    resourceCount: assessment.resourceURLs.length,
  };
}

function usage() {
  return `Usage:
  node tools/spatialreal-avatar-check/check.mjs [options]

Options:
  --avatar LABEL=UUID   Check one avatar; repeat to check several
  --catalog PATH        Read avatars from a JSON catalog (default: bundled catalog)
  --probe-assets        Also verify every referenced CDN asset
  --json                Print machine-readable JSON
  --timeout-ms NUMBER   Per-request timeout (default: 10000)
  --help                Show this help

This is a metadata compatibility preflight for the current Android AvatarKit.
It does not guarantee that a supported device can complete a rendering session.`;
}

export function parseArguments(arguments_) {
  const options = {
    avatars: [],
    catalogPath: defaultCatalogPath,
    json: false,
    probeAssets: false,
    timeoutMilliseconds: 10_000,
  };
  for (let index = 0; index < arguments_.length; index += 1) {
    const argument = arguments_[index];
    if (argument === "--avatar") {
      const value = arguments_[index + 1];
      if (!value) throw new Error("--avatar requires LABEL=UUID or UUID");
      index += 1;
      const separator = value.indexOf("=");
      options.avatars.push(
        separator === -1
          ? { label: value, id: value }
          : { label: value.slice(0, separator), id: value.slice(separator + 1) },
      );
    } else if (argument === "--catalog") {
      if (!arguments_[index + 1]) throw new Error("--catalog requires a path");
      options.catalogPath = path.resolve(arguments_[index + 1]);
      index += 1;
    } else if (argument === "--probe-assets") {
      options.probeAssets = true;
    } else if (argument === "--json") {
      options.json = true;
    } else if (argument === "--timeout-ms") {
      const value = Number(arguments_[index + 1]);
      if (!Number.isSafeInteger(value) || value < 100 || value > 120_000) {
        throw new Error("--timeout-ms must be an integer from 100 to 120000");
      }
      options.timeoutMilliseconds = value;
      index += 1;
    } else if (argument === "--help") {
      options.help = true;
    } else {
      throw new Error(`unknown argument: ${argument}`);
    }
  }
  return options;
}

async function catalogAvatars(file) {
  const parsed = JSON.parse(await readFile(file, "utf8"));
  if (
    !Array.isArray(parsed) ||
    parsed.length === 0 ||
    parsed.some(
      (avatar) =>
        !isObject(avatar) ||
        typeof avatar.label !== "string" ||
        avatar.label.trim() === "" ||
        typeof avatar.id !== "string",
    )
  ) {
    throw new Error("avatar catalog must be a non-empty array of {label, id}");
  }
  return parsed;
}

function printResults(results, json) {
  if (json) {
    console.log(JSON.stringify(results, null, 2));
    return;
  }
  for (const result of results) {
    const marker = result.status === "compatible" ? "PASS" : "FAIL";
    const details = [
      result.version ? `version=${result.version}` : null,
      `resources=${result.resourceCount}`,
    ].filter(Boolean);
    console.log(`${marker} ${result.label} (${result.id}) ${details.join(" ")}`);
    for (const reason of result.reasons) console.log(`  - ${reason}`);
  }
  const compatible = results.filter((result) => result.status === "compatible").length;
  console.log(`Summary: ${compatible}/${results.length} avatars passed metadata compatibility preflight.`);
}

async function main() {
  let options;
  try {
    options = parseArguments(process.argv.slice(2));
    if (options.help) {
      console.log(usage());
      return;
    }
    const avatars =
      options.avatars.length > 0
        ? options.avatars
        : await catalogAvatars(options.catalogPath);
    const results = [];
    for (const avatar of avatars) {
      results.push(
        await checkAvatar(avatar, {
          probeAssets: options.probeAssets,
          timeoutMilliseconds: options.timeoutMilliseconds,
        }),
      );
    }
    printResults(results, options.json);
    if (results.some((result) => result.status !== "compatible")) {
      process.exitCode = 1;
    }
  } catch (error) {
    console.error(`Error: ${error.message}`);
    console.error(usage());
    process.exitCode = 2;
  }
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? "").href) await main();
