export function homeAndroidReleaseView(state) {
  if (state.status === "ready") {
    const { release } = state;
    return {
      status: "ready",
      action: {
        kind: "download",
        href: release.download_path,
        download: release.file_name,
        label: "下载 Android APK",
      },
      supportLine: `Android 7.0 及以上 · 当前版本 v${release.version}`,
    };
  }

  if (state.status === "unavailable") {
    return {
      status: "unavailable",
      action: { kind: "disabled", label: "下载暂不可用" },
      supportLine: "发布信息暂时无法验证，请稍后重试",
    };
  }

  if (state.status === "preparing") {
    return {
      status: "preparing",
      action: { kind: "disabled", label: "Android 版本准备中" },
      supportLine: "正式 APK 就绪后开放下载",
    };
  }

  throw new Error("Unknown Android release state");
}
