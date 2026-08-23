import type { Metadata } from "next";
import BrandMark from "./BrandMark";
import { HomeAndroidDownloadAction } from "./HomeAndroidRelease";

export const metadata: Metadata = {
  title: "SpeakUp · 有记忆的 AI 口语老师",
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
          <span>SpeakUp</span>
        </a>
        <div className="nav-links">
          <a href="#top">产品介绍</a>
          <a href="#method">怎么练</a>
          <a href="#memory">长期记忆</a>
        </div>
      </nav>

      <header className="release-hero" id="download">
        <div className="release-hero-copy">
          <h1>为下一场重要的英文沟通，先练一遍。</h1>
          <p className="release-hero-subtitle">
            有记忆的 AI 口语老师，围绕真实任务陪你准备、开口和复盘。
          </p>
          <HomeAndroidDownloadAction />
        </div>

        <figure className="release-product">
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img
            src="/assets/portal-shots/portal-interview-practice.png"
            alt="SpeakUp 根据后端开发面试目标准备项目经历深挖练习"
            width="884"
            height="1832"
          />
          <figcaption>
            <strong>真实产品界面 · 英文面试</strong>
            <span>围绕真实任务练习、反馈和复盘。</span>
          </figcaption>
        </figure>
      </header>

      <section className="release-method" id="method" aria-labelledby="method-title">
        <div className="release-section-heading">
          <p className="eyebrow">怎么练</p>
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
          <p className="eyebrow eyebrow-light">长期记忆</p>
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
        <a className="brand" href="#top">
          <BrandMark />
          <span>SpeakUp</span>
        </a>
        <p>为真实世界准备英文表达。</p>
        <span>© 2026 SpeakUp</span>
      </footer>
    </main>
  );
}
