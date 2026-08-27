"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import BrandMark from "../../BrandMark";
import { loadAndroidRelease } from "../../../lib/android-release-metadata.mjs";
import { loadAndroidStagingCandidate } from "../../../lib/android-staging-candidate-metadata.mjs";

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

type DownloadState =
  | { channel: "production"; status: "preparing" | "unavailable" }
  | {
      channel: "production";
      status: "ready";
      release: AndroidReleaseMetadata;
    }
  | { channel: "staging"; status: "preparing" | "unavailable" }
  | {
      channel: "staging";
      status: "ready";
      release: AndroidStagingCandidateMetadata;
    };

const supportFields: Array<{ label: string; value: string; href?: string }> = [
  { label: "更新状态", value: "当前下载状态与版本信息以本页为准" },
  {
    label: "更新日志",
    value: "查看正式版本更新记录 →",
    href: "/changelog",
  },
  { label: "隐私与权限", value: "正式隐私说明与权限清单尚未提供" },
  { label: "问题反馈", value: "正式反馈入口尚未提供" },
];

function formatBytes(bytes: number) {
  const megabytes = bytes / (1024 * 1024);
  return `${megabytes.toFixed(megabytes >= 10 ? 1 : 2)} MB（${bytes.toLocaleString("zh-CN")} 字节）`;
}

function formatPublishedAt(value: string) {
  return `${new Intl.DateTimeFormat("zh-CN", {
    timeZone: "Asia/Shanghai",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).format(new Date(value))}（UTC+8）`;
}

function unavailableValue(state: DownloadState) {
  if (state.channel === "staging") {
    return state.status === "unavailable"
      ? "候选信息暂不可用"
      : "候选制品就绪后公开";
  }
  return state.status === "unavailable" ? "发布信息暂不可用" : "制品就绪后公开";
}

export default function AndroidDownloadPageClient({
  channel,
}: {
  channel: "production" | "staging";
}) {
  const [releaseState, setReleaseState] = useState<DownloadState>({
    channel,
    status: "preparing",
  });

  useEffect(() => {
    let active = true;
    if (channel === "staging") {
      void loadAndroidStagingCandidate().then((nextState) => {
        if (active) {
          setReleaseState({
            channel: "staging",
            ...nextState,
          } as DownloadState);
        }
      });
    } else {
      void loadAndroidRelease().then((nextState) => {
        if (active) {
          setReleaseState({
            channel: "production",
            ...nextState,
          } as DownloadState);
        }
      });
    }
    return () => {
      active = false;
    };
  }, [channel]);

  const isStaging = releaseState.channel === "staging";
  const release =
    releaseState.status === "ready" ? releaseState.release : undefined;
  const stagingRelease =
    releaseState.channel === "staging" && releaseState.status === "ready"
      ? releaseState.release
      : undefined;
  const placeholder = unavailableValue(releaseState);
  let releaseFields: Array<[string, string]>;
  if (releaseState.channel === "staging" && releaseState.status === "ready") {
    const candidate = releaseState.release;
    releaseFields = [
      ["环境", "Staging 候选环境"],
      ["版本名称（versionName）", `v${candidate.version}`],
      ["版本号（versionCode）", String(candidate.version_code)],
      ["代码提交 SHA", candidate.git_sha],
      ["候选构建（Candidate run）", String(candidate.candidate_run_id)],
      ["APK 大小", formatBytes(candidate.size_bytes)],
      ["最低 Android 版本", `API ${candidate.minimum_android_api} 及以上`],
      ["支持的 ABI", candidate.abis.join(", ")],
    ];
  } else if (
    releaseState.channel === "production" &&
    releaseState.status === "ready"
  ) {
    const currentRelease = releaseState.release;
    releaseFields = [
      ["版本名称（versionName）", `v${currentRelease.version}`],
      ["版本号（versionCode）", String(currentRelease.version_code)],
      ["发布时间", formatPublishedAt(currentRelease.published_at)],
      ["APK 大小", formatBytes(currentRelease.size_bytes)],
      [
        "最低 Android 版本",
        `API ${currentRelease.minimum_android_api} 及以上`,
      ],
      ["支持的 ABI", currentRelease.abis.join(", ")],
    ];
  } else {
    const labels = isStaging
      ? [
          "环境",
          "版本名称（versionName）",
          "版本号（versionCode）",
          "代码提交 SHA",
          "候选构建（Candidate run）",
          "APK 大小",
          "最低 Android 版本",
          "支持的 ABI",
        ]
      : [
          "版本名称（versionName）",
          "版本号（versionCode）",
          "发布时间",
          "APK 大小",
          "最低 Android 版本",
          "支持的 ABI",
        ];
    releaseFields = labels.map((label) => [label, placeholder]);
  }

  const stateCopy = release
    ? isStaging
      ? {
          label: "候选可下载",
          detail: `v${release.version} · versionCode ${release.version_code} 已绑定 Staging 候选清单。`,
        }
      : {
          label: "可下载",
          detail: `v${release.version} 已通过发布清单校验，下载后请继续核对下方指纹。`,
        }
    : releaseState.status === "unavailable"
      ? isStaging
        ? {
            label: "候选暂不可用",
            detail: "Staging 候选信息当前无法验证，因此不会提供下载链接。",
          }
        : {
            label: "暂不可用",
            detail: "发布信息当前无法验证，因此不会提供下载链接，请稍后重试。",
          }
      : isStaging
        ? {
            label: "候选准备中",
            detail: "Staging 候选制品完成校验后，本页才会开放下载。",
          }
        : {
            label: "准备中",
            detail: "不会提供空链接、假二维码或未经核验的版本。",
          };

  return (
    <main className="release-site download-page" id="top">
      <nav className="site-nav release-nav" aria-label="主导航">
        <Link className="brand" href="/" aria-label="返回 SpeakUp 首页">
          <BrandMark />
          <span>SpeakUp</span>
        </Link>
        <div className="nav-links">
          <Link href="/">产品首页</Link>
          <Link href="/changelog">更新日志</Link>
          <a href="#verify">验证信息</a>
        </div>
        <Link className="button button-small" href="/">
          返回首页
        </Link>
      </nav>

      <header className="download-hero">
        <div className="download-hero-copy">
          <Link className="download-back" href="/">
            ← 返回产品首页
          </Link>
          <p className="eyebrow">
            {isStaging ? "Staging 候选环境" : "Android 官方版"}
          </p>
          <h1>SpeakUp for Android</h1>
          <p>
            {release
              ? isStaging
                ? "这是连接 Staging API 的 arm64 候选 APK，仅供团队验收，不代表正式发布。安装前请核对候选版本与 SHA-256。"
                : "正式 Android arm64 APK 已开放下载。安装前请核对文件与签名证书 SHA-256，确认拿到的是本页发布的版本。"
              : releaseState.status === "unavailable"
                ? isStaging
                  ? "Staging 候选入口暂时无法读取或验证，本页不会回退到正式版或猜测下载地址。"
                  : "发布入口暂时无法读取或验证。为避免把错误文件交给你，本页不会生成猜测链接。"
                : isStaging
                  ? "Staging 候选 APK 正在准备，完成制品校验后才会开放团队验收入口。"
                  : "首个公开版本正在准备。当前还没有可公开下载和验证的 APK，完成签名与生产验证后，本页才会开放唯一入口。"}
          </p>
          <div className="download-release-action">
            <div
              className={`download-state download-state-${releaseState.status}`}
              role="status"
              aria-live="polite"
            >
              <span>
                <i aria-hidden="true" />
                {stateCopy.label}
              </span>
              <small>{stateCopy.detail}</small>
            </div>
            {release ? (
              <a
                className="button download-primary-action"
                href={release.download_path}
                download={release.file_name}
              >
                {isStaging ? "下载 Staging APK" : "下载 Android APK"}
              </a>
            ) : null}
          </div>
        </div>
        <aside className="download-availability" aria-labelledby="available-title">
          <p className="eyebrow">
            {isStaging ? "候选验收依据" : "开放下载时同时提供"}
          </p>
          <h2 id="available-title">文件之外，还有完整的验证信息。</h2>
          <ul>
            <li>{isStaging ? "环境、版本、versionCode 与代码 SHA" : "版本、时间与兼容范围"}</li>
            <li>APK 文件 SHA-256</li>
            <li>签名证书 SHA-256</li>
            <li>安装步骤与校验说明</li>
          </ul>
        </aside>
      </header>

      <section className="download-section" aria-labelledby="release-title">
        <div className="download-section-heading">
          <p className="eyebrow">{isStaging ? "候选信息" : "发布信息"}</p>
          <h2 id="release-title">
            {isStaging
              ? "验收前，确认候选环境与构建身份。"
              : "拿到文件前，先知道它是什么。"}
          </h2>
        </div>
        <dl className="download-facts">
          {releaseFields.map(([label, value]) => (
            <div key={label}>
              <dt>{label}</dt>
              <dd>{value}</dd>
            </div>
          ))}
        </dl>
      </section>

      <section
        className="download-section download-verify"
        id="verify"
        aria-labelledby="verify-title"
      >
        <div className="download-section-heading">
          <p className="eyebrow">验证信息</p>
          <h2 id="verify-title">文件校验与签名校验，是两件不同的事。</h2>
          <p>前者确认下载文件没有变化，后者确认应用由预期证书签名。</p>
        </div>
        <dl className="download-hashes">
          <div>
            <dt>APK 文件 SHA-256</dt>
            <dd>{release?.apk_sha256 ?? placeholder}</dd>
          </div>
          <div>
            <dt>签名证书 SHA-256</dt>
            <dd>{release?.apk_certificate_sha256 ?? placeholder}</dd>
          </div>
          {isStaging ? (
            <>
              <div>
                <dt>代码提交 SHA</dt>
                <dd>{stagingRelease?.git_sha ?? placeholder}</dd>
              </div>
              <div>
                <dt>Candidate manifest SHA-256</dt>
                <dd>{stagingRelease?.manifest_sha256 ?? placeholder}</dd>
              </div>
            </>
          ) : null}
        </dl>
      </section>

      <section className="download-section" aria-labelledby="support-title">
        <div className="download-section-heading">
          <p className="eyebrow">{isStaging ? "验收与支持" : "发布与支持"}</p>
          <h2 id="support-title">
            {isStaging
              ? "候选验收与正式发布记录分别说明。"
              : "已提供与待补充的信息，都明确说明。"}
          </h2>
        </div>
        <dl className="download-facts">
          {supportFields.map((field) => (
            <div key={field.label}>
              <dt>{field.label}</dt>
              <dd>
                {field.href ? (
                  <Link className="download-inline-link" href={field.href}>
                    {field.value}
                  </Link>
                ) : (
                  field.value
                )}
              </dd>
            </div>
          ))}
        </dl>
      </section>

      <section className="download-section" id="install" aria-labelledby="install-title">
        <div className="download-section-heading">
          <p className="eyebrow">安装说明</p>
          <h2 id="install-title">只从这里下载，只为可信来源授权。</h2>
        </div>
        <ol className="download-steps">
          <li>
            <span>01</span>
            <div>
              <strong>下载并核验</strong>
              <p>从本页获取 APK，并对照公开的文件 SHA-256。</p>
            </div>
          </li>
          <li>
            <span>02</span>
            <div>
              <strong>按来源临时授权</strong>
              <p>
                Android 8.0
                及以上版本会要求为当前浏览器或文件管理器允许“安装未知应用”。
              </p>
            </div>
          </li>
          <li>
            <span>03</span>
            <div>
              <strong>安装后收回权限</strong>
              <p>完成安装后，可回到系统设置关闭该来源的安装权限。</p>
            </div>
          </li>
        </ol>
      </section>

      <section className="release-final download-final">
        <div>
          <p className="eyebrow">当前状态</p>
          <h2>
            {release
              ? isStaging
                ? `Staging 候选 v${release.version} · versionCode ${release.version_code} 已开放团队验收。`
                : `v${release.version} 已开放下载，校验信息同步可查。`
              : releaseState.status === "unavailable"
                ? isStaging
                  ? "Staging 候选信息暂不可用，下载入口保持关闭。"
                  : "发布信息暂不可用，下载入口保持关闭。"
                : isStaging
                  ? "Staging 候选制品尚未就绪。"
                  : "现在还不能下载，但发布信息不会含糊。"}
          </h2>
        </div>
        <Link className="button" href="/">
          返回产品首页
        </Link>
      </section>
      <footer>
        <Link className="brand" href="/">
          <BrandMark />
          <span>SpeakUp</span>
        </Link>
        <p>
          {isStaging
            ? "Android Staging 候选下载与验证信息。"
            : "Android 官方下载与验证信息。"}
        </p>
        <span>© 2026 SpeakUp</span>
      </footer>
    </main>
  );
}
