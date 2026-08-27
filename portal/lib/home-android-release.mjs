export function homeAndroidReleaseView(state) {
  if (state.status === "ready") {
    const { release } = state;
    return {
      status: "ready",
      action: {
        kind: "download",
        href: release.download_path,
        download: release.file_name,
        label: "下载客户端",
      },
      supportLine: "当前支持 Android 7.0 及以上",
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
      action: { kind: "disabled", label: "客户端准备中" },
      supportLine: "正式版本就绪后开放下载",
    };
  }

  throw new Error("Unknown Android release state");
}
