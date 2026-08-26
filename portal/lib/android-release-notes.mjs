const documentKeys = [
  "changes",
  "locale",
  "published_at",
  "release_notes_version",
  "version",
];
const indexKeys = ["locale", "release_notes_index_version", "versions"];
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
const maximumIndexedVersions = 50;

export const androidReleaseNotesIndexPath =
  "/release-notes/android/index.zh-CN.json";

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

function compareVersions(left, right) {
  const leftParts = left.split(".").map(Number);
  const rightParts = right.split(".").map(Number);
  for (let index = 0; index < leftParts.length; index += 1) {
    if (leftParts[index] !== rightParts[index]) {
      return leftParts[index] - rightParts[index];
    }
  }
  return 0;
}

export function parseAndroidReleaseNotesIndex(value) {
  if (
    !exactObject(value, indexKeys) ||
    value.release_notes_index_version !== 1 ||
    value.locale !== "zh-CN" ||
    !Array.isArray(value.versions) ||
    value.versions.length < 1 ||
    value.versions.length > maximumIndexedVersions
  ) {
    invalid();
  }

  const versions = new Set();
  for (const [index, version] of value.versions.entries()) {
    if (
      typeof version !== "string" ||
      !versionPattern.test(version) ||
      versions.has(version) ||
      (index > 0 && compareVersions(value.versions[index - 1], version) <= 0)
    ) {
      invalid();
    }
    versions.add(version);
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

export function matchAndroidReleaseNotesHistory(release, index, notes) {
  const parsedIndex = parseAndroidReleaseNotesIndex(index);
  if (
    release === null ||
    typeof release !== "object" ||
    typeof release.version !== "string" ||
    !versionPattern.test(release.version) ||
    !canonicalPublishedAt(release.published_at)
  ) {
    invalid();
  }

  const currentIndex = parsedIndex.versions.indexOf(release.version);
  if (currentIndex < 0 || !Array.isArray(notes)) invalid();

  const visibleVersions = parsedIndex.versions.slice(currentIndex);
  if (notes.length !== visibleVersions.length) invalid();

  const parsedNotes = notes.map(parseAndroidReleaseNotes);
  for (const [noteIndex, note] of parsedNotes.entries()) {
    if (note.version !== visibleVersions[noteIndex]) invalid();
    if (noteIndex === 0) {
      matchAndroidReleaseNotes(release, note);
      continue;
    }
    if (
      new Date(parsedNotes[noteIndex - 1].published_at).valueOf() <=
      new Date(note.published_at).valueOf()
    ) {
      invalid();
    }
  }

  return parsedNotes;
}

export async function loadAndroidReleaseHistory(release, fetcher = fetch) {
  try {
    const indexResponse = await fetcher(androidReleaseNotesIndexPath, {
      cache: "no-store",
      headers: { accept: "application/json" },
    });
    if (!indexResponse.ok) return { status: "unavailable" };

    const index = parseAndroidReleaseNotesIndex(await indexResponse.json());
    const currentIndex = index.versions.indexOf(release.version);
    if (currentIndex < 0) invalid();
    const visibleVersions = index.versions.slice(currentIndex);
    const responses = await Promise.all(
      visibleVersions.map((version) =>
        fetcher(androidReleaseNotesPath(version), {
          cache: "force-cache",
          headers: { accept: "application/json" },
        }),
      ),
    );
    if (responses.some((response) => !response.ok)) {
      return { status: "unavailable" };
    }
    const notes = await Promise.all(
      responses.map((response) => response.json()),
    );

    return {
      status: "ready",
      notes: matchAndroidReleaseNotesHistory(release, index, notes),
    };
  } catch {
    return { status: "unavailable" };
  }
}
