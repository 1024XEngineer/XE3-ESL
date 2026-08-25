"use client";

import { useEffect, useState } from "react";
import { loadAndroidRelease } from "../../lib/android-release-metadata.mjs";
import { loadAndroidReleaseNotes } from "../../lib/android-release-notes.mjs";

type AndroidReleaseMetadata = {
  version: string;
  published_at: string;
};

type ChangeType =
  | "feature"
  | "improvement"
  | "fix"
  | "compatibility"
  | "known_limitation";

type ReleaseChange = {
  type: ChangeType;
  text: string;
};

type ReleaseNotes = {
  version: string;
  published_at: string;
  changes: ReleaseChange[];
};

type ChangelogState =
  | { status: "loading" }
  | { status: "preparing" }
  | { status: "unavailable" }
  | { status: "ready"; notes: ReleaseNotes };

const changeSections: Array<{ type: ChangeType; title: string }> = [
  { type: "feature", title: "新功能" },
  { type: "improvement", title: "体验优化" },
  { type: "fix", title: "问题修复" },
  { type: "compatibility", title: "兼容性" },
  { type: "known_limitation", title: "已知限制" },
];

function formatReleaseDate(value: string) {
  return new Intl.DateTimeFormat("zh-CN", {
    timeZone: "Asia/Shanghai",
    year: "numeric",
    month: "long",
    day: "numeric",
  }).format(new Date(value));
}

export default function ChangelogContent() {
  const [state, setState] = useState<ChangelogState>({ status: "loading" });

  useEffect(() => {
    let active = true;

    async function load() {
      const releaseState = await loadAndroidRelease();
      if (!active) return;
      if (releaseState.status !== "ready") {
        setState({ status: releaseState.status });
        return;
      }

      const release = releaseState.release as AndroidReleaseMetadata;
      const notesState = await loadAndroidReleaseNotes(release);
      if (!active) return;
      if (notesState.status !== "ready") {
        setState({ status: "unavailable" });
        return;
      }
      setState({
        status: "ready",
        notes: notesState.notes as ReleaseNotes,
      });
    }

    void load();
    return () => {
      active = false;
    };
  }, []);

  if (state.status !== "ready") {
    const copy =
      state.status === "loading"
        ? {
            title: "正在确认当前正式版本…",
            detail: "确认完成后，只展示已经正式发布的更新内容。",
          }
        : state.status === "preparing"
          ? {
              title: "暂无正式版本更新",
              detail: "首个正式版本发布后，这里会同步记录用户可以感知的变化。",
            }
          : {
              title: "更新日志暂不可用",
              detail:
                "暂时无法验证当前正式版本，因此不会展示可能过期的更新内容。",
            };

    return (
      <section className="changelog-list changelog-status" aria-live="polite">
        <h2>{copy.title}</h2>
        <p>{copy.detail}</p>
      </section>
    );
  }

  return (
    <section className="changelog-list" aria-label="Android 正式版本更新">
      <article
        className="changelog-release"
        aria-labelledby={`release-v${state.notes.version.replaceAll(".", "-")}`}
      >
        <div className="changelog-release-meta">
          <p>当前正式版本</p>
          <time dateTime={state.notes.published_at}>
            {formatReleaseDate(state.notes.published_at)}
          </time>
        </div>
        <div className="changelog-release-body">
          <h2 id={`release-v${state.notes.version.replaceAll(".", "-")}`}>
            v{state.notes.version}
          </h2>
          {changeSections.map((section) => {
            const changes = state.notes.changes.filter(
              (change) => change.type === section.type,
            );
            if (changes.length === 0) return null;
            return (
              <div className="changelog-change-group" key={section.type}>
                <h3>{section.title}</h3>
                <ul>
                  {changes.map((change) => (
                    <li key={change.text}>{change.text}</li>
                  ))}
                </ul>
              </div>
            );
          })}
        </div>
      </article>
    </section>
  );
}
