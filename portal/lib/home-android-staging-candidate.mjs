export function homeAndroidStagingCandidateView(state) {
  if (state.status === "ready") {
    const { release } = state;
    return {
      status: "ready",
      action: {
        kind: "download",
        href: release.download_path,
        download: release.file_name,
        label: "下载 Staging APK",
      },
      supportLine:
        `Staging 候选环境 · v${release.version} · ` +
        `versionCode ${release.version_code} · APK SHA-256 ${release.apk_sha256.slice(0, 8)}…`,
    };
  }

  if (state.status === "unavailable") {
    return {
      status: "unavailable",
      action: { kind: "disabled", label: "Staging 下载暂不可用" },
      supportLine: "Staging 候选信息无法验证，请稍后重试",
    };
  }

  if (state.status === "preparing") {
    return {
      status: "preparing",
      action: { kind: "disabled", label: "Staging 候选 APK 准备中" },
      supportLine: "Staging 候选环境 · 制品就绪后开放下载",
    };
  }

  throw new Error("Unknown Android staging candidate state");
}
