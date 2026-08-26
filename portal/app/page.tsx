import type { Metadata } from "next";
import Link from "next/link";
import BrandMark from "./BrandMark";
import BrandWordmark from "./BrandWordmark";
import HeroProductCarousel from "./HeroProductCarousel";
import { HomeAndroidDownloadAction } from "./HomeAndroidRelease";

export const metadata: Metadata = {
  title: "SpeakUp · 越用越懂你的 AI 口语老师",
  description:
    "围绕真实沟通场景练习、反馈和复盘，下载 SpeakUp Android APK。",
};

const steps = [
  {
    number: "01",
    title: "说清楚你要面对什么",
    copy: "英文面试、工作汇报、考试或生活沟通——从下一件真实发生的事开始。",
  },
  {
    number: "02",
    title: "围绕目标反复开口",
    copy: "SpeakUp 组织针对性练习、模拟对话并给出反馈，不让准备停在背答案。",
  },
  {
    number: "03",
    title: "把进展带到下一次",
    copy: "你的目标、经历和卡点会成为后续练习的上下文，让每一轮真正接得上。",
  },
];

export default function Home() {
  return (
    <main className="release-site release-home" id="top">
      <nav className="site-nav release-nav release-nav-simple" aria-label="主导航">
        <a className="brand" href="#top" aria-label="SpeakUp 首页">
          <BrandMark />
          <BrandWordmark />
        </a>
        <div className="nav-links">
          <a href="#top">产品介绍</a>
          <Link href="/changelog">更新日志</Link>
          <a
            href="https://github.com/1024XEngineer/XE3-ESL"
            target="_blank"
            rel="noopener noreferrer"
            aria-label="在 GitHub 查看 SpeakUp 主仓库（新窗口打开）"
          >
            GitHub <span aria-hidden="true">↗</span>
          </a>
        </div>
      </nav>

      <header className="release-hero" id="download">
        <div className="release-hero-copy">
          <h1>下一场重要的英文沟通，先练一遍。</h1>
          <p className="release-hero-subtitle">
            越用越懂你的 AI 口语老师，围绕真实任务陪你准备、开口和复盘。
          </p>
          <HomeAndroidDownloadAction />
        </div>

        <HeroProductCarousel />
      </header>

      <section className="release-method" id="method" aria-labelledby="method-title">
        <div className="release-section-heading">
          <h2 id="method-title">把“我要学英语”，变成下一次具体的准备。</h2>
        </div>
        <ol className="release-steps">
          {steps.map((step) => (
            <li key={step.number}>
              <span>{step.number}</span>
              <h3>{step.title}</h3>
              <p>{step.copy}</p>
            </li>
          ))}
        </ol>
      </section>

      <section
        className="release-memory"
        id="memory"
        aria-labelledby="memory-title"
      >
        <div>
          <h2 id="memory-title">这次练习里发生的事，下一次还记得。</h2>
        </div>
        <dl>
          <div>
            <dt>你的目标</dt>
            <dd>下一次重要沟通是什么，想进入怎样的团队。</dd>
          </div>
          <div>
            <dt>你的真实经历</dt>
            <dd>做过哪些项目，哪些故事能成为表达素材。</dd>
          </div>
          <div>
            <dt>你的能力变化</dt>
            <dd>哪些问题反复卡住，哪些表达已经更自然。</dd>
          </div>
        </dl>
      </section>

      <footer>
        <a className="brand" href="#top" aria-label="SpeakUp 首页">
          <BrandMark />
          <BrandWordmark />
        </a>
        <p>为真实世界准备英文表达。</p>
        <span className="release-footer-meta">
          <Link href="/changelog">更新日志</Link>
          <i aria-hidden="true">·</i>© 2026 SpeakUp
        </span>
      </footer>
    </main>
  );
}
