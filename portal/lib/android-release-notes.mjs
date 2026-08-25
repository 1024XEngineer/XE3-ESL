const documentKeys = [
  "changes",
  "locale",
  "published_at",
  "release_notes_version",
  "version",
];
const changeKeys = ["text", "type"];
const changeTypes = new Set([
  "feature",
  "improvement",
  "fix",
  "compatibility",
  "known_limitation",
]);
const versionPattern = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const canonicalUtcPattern =
  /^\d{4}-(0[1-9]|1[0-2])-([012]\d|3[01])T([01]\d|2[0-3]):[0-5]\d:[0-5]\dZ$/;

function invalid() {
  throw new Error("Android release notes are invalid");
}

function exactObject(value, keys) {
  return (
    value !== null &&
    typeof value === "object" &&
    !Array.isArray(value) &&
    JSON.stringify(Object.keys(value).sort()) === JSON.stringify(keys)
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

export function parseAndroidReleaseNotes(value) {
  if (
    !exactObject(value, documentKeys) ||
    value.release_notes_version !== 1 ||
    value.locale !== "zh-CN" ||
    typeof value.version !== "string" ||
    !versionPattern.test(value.version) ||
    !canonicalPublishedAt(value.published_at) ||
    !Array.isArray(value.changes) ||
    value.changes.length < 1 ||
    value.changes.length > 20
  ) {
    invalid();
  }

  const changeTexts = new Set();
  for (const change of value.changes) {
    if (
      !exactObject(change, changeKeys) ||
      !changeTypes.has(change.type) ||
      typeof change.text !== "string" ||
      change.text.length < 1 ||
      change.text.length > 160 ||
      change.text.trim() !== change.text ||
      /[\r\n]/.test(change.text) ||
      changeTexts.has(change.text)
    ) {
      invalid();
    }
    changeTexts.add(change.text);
  }

  return value;
}

export function androidReleaseNotesPath(version) {
  if (typeof version !== "string" || !versionPattern.test(version)) invalid();
  return `/release-notes/android/v${version}.zh-CN.json`;
}

export function matchAndroidReleaseNotes(release, notes) {
  const parsed = parseAndroidReleaseNotes(notes);
  if (
    release === null ||
    typeof release !== "object" ||
    parsed.version !== release.version ||
    parsed.published_at !== release.published_at
  ) {
    invalid();
  }
  return parsed;
}

export async function loadAndroidReleaseNotes(release, fetcher = fetch) {
  try {
    const response = await fetcher(androidReleaseNotesPath(release.version), {
      cache: "force-cache",
      headers: { accept: "application/json" },
    });
    if (!response.ok) return { status: "unavailable" };
    return {
      status: "ready",
      notes: matchAndroidReleaseNotes(release, await response.json()),
    };
  } catch {
    return { status: "unavailable" };
  }
}
