const metadataKeys = [
  "abis",
  "apk_certificate_sha256",
  "apk_sha256",
  "download_path",
  "file_name",
  "metadata_version",
  "minimum_android_api",
  "published_at",
  "size_bytes",
  "version",
  "version_code",
];

const versionPattern = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const sha256Pattern = /^[0-9a-f]{64}$/;
const canonicalUtcPattern =
  /^\d{4}-(0[1-9]|1[0-2])-([012]\d|3[01])T([01]\d|2[0-3]):[0-5]\d:[0-5]\dZ$/;

function invalid() {
  throw new Error("Android release metadata is invalid");
}

function positiveSafeInteger(value) {
  return Number.isSafeInteger(value) && value >= 1;
}

function nonzeroSha(value) {
  return (
    typeof value === "string" &&
    sha256Pattern.test(value) &&
    !/^0+$/.test(value)
  );
}

function canonicalPublishedAt(value) {
  if (typeof value !== "string" || !canonicalUtcPattern.test(value)) return false;
  const parsed = new Date(value);
  return (
    !Number.isNaN(parsed.valueOf()) &&
    parsed.toISOString().replace(".000Z", "Z") === value
  );
}

export function parseAndroidReleaseMetadata(value) {
  if (
    value === null ||
    typeof value !== "object" ||
    Array.isArray(value) ||
    JSON.stringify(Object.keys(value).sort()) !==
      JSON.stringify(metadataKeys)
  ) {
    invalid();
  }

  const metadata = value;
  if (
    metadata.metadata_version !== 1 ||
    typeof metadata.version !== "string" ||
    !versionPattern.test(metadata.version) ||
    !positiveSafeInteger(metadata.version_code) ||
    !canonicalPublishedAt(metadata.published_at) ||
    metadata.file_name !==
      `speakup-v${metadata.version}-production-arm64.apk` ||
    metadata.download_path !==
      `/downloads/android/v${metadata.version}/${metadata.file_name}` ||
    !positiveSafeInteger(metadata.size_bytes) ||
    metadata.minimum_android_api !== 24 ||
    JSON.stringify(metadata.abis) !== JSON.stringify(["arm64-v8a"]) ||
    !nonzeroSha(metadata.apk_sha256) ||
    !nonzeroSha(metadata.apk_certificate_sha256)
  ) {
    invalid();
  }
  return metadata;
}

export async function loadAndroidRelease(fetcher = fetch) {
  try {
    const response = await fetcher("/downloads/android/release.json", {
      cache: "no-store",
      headers: { accept: "application/json" },
    });
    if (response.status === 404) return { status: "preparing" };
    if (!response.ok) return { status: "unavailable" };
    return {
      status: "ready",
      release: parseAndroidReleaseMetadata(await response.json()),
    };
  } catch {
    return { status: "unavailable" };
  }
}
