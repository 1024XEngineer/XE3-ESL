const metadataKeys = [
  "abis",
  "apk_certificate_sha256",
  "apk_sha256",
  "candidate_metadata_version",
  "candidate_run_id",
  "download_path",
  "environment",
  "file_name",
  "git_sha",
  "manifest_sha256",
  "minimum_android_api",
  "size_bytes",
  "version",
  "version_code",
];

const versionPattern = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const gitShaPattern = /^[0-9a-f]{40}$/;
const sha256Pattern = /^[0-9a-f]{64}$/;

function invalid() {
  throw new Error("Android staging candidate metadata is invalid");
}

function positiveSafeInteger(value) {
  return Number.isSafeInteger(value) && value >= 1;
}

function nonzeroHash(value, pattern) {
  return (
    typeof value === "string" &&
    pattern.test(value) &&
    !/^0+$/.test(value)
  );
}

export function parseAndroidStagingCandidateMetadata(value) {
  if (
    value === null ||
    typeof value !== "object" ||
    Array.isArray(value) ||
    JSON.stringify(Object.keys(value).sort()) !== JSON.stringify(metadataKeys)
  ) {
    invalid();
  }

  const metadata = value;
  if (
    metadata.candidate_metadata_version !== 1 ||
    metadata.environment !== "staging" ||
    typeof metadata.version !== "string" ||
    !versionPattern.test(metadata.version) ||
    !positiveSafeInteger(metadata.version_code) ||
    !nonzeroHash(metadata.git_sha, gitShaPattern) ||
    !positiveSafeInteger(metadata.candidate_run_id) ||
    !nonzeroHash(metadata.manifest_sha256, sha256Pattern) ||
    metadata.file_name !== `speakup-v${metadata.version}-staging-arm64.apk` ||
    metadata.download_path !==
      `/downloads/android/candidates/${metadata.candidate_run_id}/${metadata.file_name}` ||
    !positiveSafeInteger(metadata.size_bytes) ||
    metadata.minimum_android_api !== 24 ||
    JSON.stringify(metadata.abis) !== JSON.stringify(["arm64-v8a"]) ||
    !nonzeroHash(metadata.apk_sha256, sha256Pattern) ||
    !nonzeroHash(metadata.apk_certificate_sha256, sha256Pattern)
  ) {
    invalid();
  }
  return metadata;
}

export async function loadAndroidStagingCandidate(fetcher = fetch) {
  try {
    const response = await fetcher(
      "/downloads/android/staging-candidate.json",
      {
        cache: "no-store",
        headers: { accept: "application/json" },
      },
    );
    if (response.status === 404) return { status: "preparing" };
    if (!response.ok) return { status: "unavailable" };
    return {
      status: "ready",
      release: parseAndroidStagingCandidateMetadata(await response.json()),
    };
  } catch {
    return { status: "unavailable" };
  }
}
