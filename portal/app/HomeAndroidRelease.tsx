"use client";

import { useEffect, useState } from "react";
import { loadAndroidRelease } from "../lib/android-release-metadata.mjs";
import { loadAndroidStagingCandidate } from "../lib/android-staging-candidate-metadata.mjs";
import { homeAndroidReleaseView } from "../lib/home-android-release.mjs";
import { homeAndroidStagingCandidateView } from "../lib/home-android-staging-candidate.mjs";

type AndroidReleaseMetadata = {
  metadata_version: 1;
  version: string;
  version_code: number;
  published_at: string;
  file_name: string;
  download_path: string;
  size_bytes: number;
  minimum_android_api: 24;
  abis: ["arm64-v8a"];
  apk_sha256: string;
  apk_certificate_sha256: string;
};

type ReleaseState =
  | { status: "preparing" }
  | { status: "unavailable" }
  | { status: "ready"; release: AndroidReleaseMetadata };

type AndroidStagingCandidateMetadata = {
  candidate_metadata_version: 1;
  environment: "staging";
  version: string;
  version_code: number;
  git_sha: string;
  candidate_run_id: number;
  manifest_sha256: string;
  file_name: string;
  download_path: string;
  size_bytes: number;
  minimum_android_api: 24;
  abis: ["arm64-v8a"];
  apk_sha256: string;
  apk_certificate_sha256: string;
};

type StagingCandidateState =
  | { status: "preparing" }
  | { status: "unavailable" }
  | { status: "ready"; release: AndroidStagingCandidateMetadata };

type ReleaseAction =
  | { kind: "disabled"; label: string }
  | { kind: "download"; href: string; download: string; label: string };

type HomeReleaseView = {
  status: ReleaseState["status"];
  action: ReleaseAction;
  supportLine: string;
};

export function HomeAndroidDownloadAction({
  channel,
}: {
  channel: "production" | "staging";
}) {
  const [releaseState, setReleaseState] = useState<
    ReleaseState | StagingCandidateState
  >({
    status: "preparing",
  });

  useEffect(() => {
    let active = true;
    const load =
      channel === "staging"
        ? loadAndroidStagingCandidate
        : loadAndroidRelease;
    void load().then((nextState) => {
      if (active) {
        setReleaseState(nextState as ReleaseState | StagingCandidateState);
      }
    });
    return () => {
      active = false;
    };
  }, [channel]);

  const view = (
    channel === "staging"
      ? homeAndroidStagingCandidateView(releaseState as StagingCandidateState)
      : homeAndroidReleaseView(releaseState as ReleaseState)
  ) as HomeReleaseView;

  return (
    <div className="release-actions">
      {view.action.kind === "download" ? (
        <a
          className="button release-download-button"
          href={view.action.href}
          download={view.action.download}
          aria-describedby="android-release-status"
        >
          {view.action.label} <span aria-hidden="true">↓</span>
        </a>
      ) : (
        <button
          className="button release-download-button"
          type="button"
          disabled
        >
          {view.action.label}
        </button>
      )}
      <span
        className={`release-status release-status-${view.status}`}
        id="android-release-status"
        role="status"
        aria-live="polite"
      >
        <i aria-hidden="true" />
        {view.supportLine}
      </span>
    </div>
  );
}
