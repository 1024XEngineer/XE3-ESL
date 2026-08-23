import Link from "next/link";
import BrandMark from "./BrandMark";

const steps = [
  { number: "01", title: "说清楚你要面对什么", copy: "英文面试、工作汇报、考试或生活沟通——从下一件真实发生的事开始。" },
  { number: "02", title: "围绕目标反复开口", copy: "SpeakUp 组织针对性练习、模拟对话并给出反馈，不让准备停在背答案。" },
  { number: "03", title: "把进展带到下一次", copy: "你的目标、经历和卡点会成为后续练习的上下文，让每一轮真正接得上。" },
];

const releasePromises = [
  ["唯一官方下载", "APK 就绪前不放空链接、临时二维码或未经核验的文件。"],
  ["制品信息完整", "版本、发布时间、兼容范围、文件大小与 ABI 会随 APK 一起公开。"],
  ["可以独立验证", "APK 文件 SHA-256 与签名证书 SHA-256 分开提供。"],
];

const faqs = [
  ["现在可以下载 Android 版吗？", "还不可以。首个正式 APK 正在准备，完成正式签名和生产验证后才会开放入口。"],
  ["为什么现在不先放一个测试包？", "公开下载页只承载可追溯的正式制品，避免测试包、临时链接和正式版本混在一起。"],
  ["开放下载时会提供什么？", "会同时提供版本、系统要求、文件大小、ABI、文件 SHA-256、签名证书 SHA-256、安装说明与更新记录。"],
];

export default function Home() {
  return (
    <main className="release-site" id="top">
      <nav className="site-nav release-nav" aria-label="主导航">
        <a className="brand" href="#top" aria-label="SpeakUp 首页"><BrandMark /><span>SpeakUp</span></a>
        <div className="nav-links"><a href="#method">怎么练</a><a href="#download">Android 发布</a></div>
        <Link className="button button-small" href="/download/android">查看下载状态</Link>
      </nav>

      <header className="release-hero">
        <div className="release-hero-copy">
          <p className="eyebrow">有记忆的 AI 口语老师</p>
          <h1>下一场重要的英文沟通，先和 SpeakUp 练一遍。</h1>
          <p className="release-hero-subtitle">围绕你真正要面对的任务，准备、开口、复盘。不是背一套标准答案，而是把自己的经历练成说得出口的表达。</p>
          <div className="release-actions">
            <Link className="button" href="/download/android">查看 Android 发布状态 <span aria-hidden="true">↗</span></Link>
            <span className="release-status"><i aria-hidden="true" />首个 APK 正在准备</span>
          </div>
        </div>

        <figure className="release-product">
          <span className="release-product-label">真实产品界面 · 英文面试</span>
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src="/assets/portal-shots/portal-interview-practice.png" alt="SpeakUp 根据后端开发面试目标准备项目经历深挖练习" width="884" height="1832" />
          <figcaption><strong>从一句真实需求开始</strong><span>按目标准备练习，准备好就直接开口。</span></figcaption>
        </figure>
      </header>

      <section className="release-method" id="method" aria-labelledby="method-title">
        <div className="release-section-heading"><p className="eyebrow">怎么练</p><h2 id="method-title">把“我要学英语”，变成下一次具体的准备。</h2></div>
        <ol className="release-steps">
          {steps.map((step) => <li key={step.number}><span>{step.number}</span><h3>{step.title}</h3><p>{step.copy}</p></li>)}
        </ol>
      </section>

      <section className="release-memory" aria-labelledby="memory-title">
        <div><p className="eyebrow eyebrow-light">长期记忆</p><h2 id="memory-title">这次练习里发生的事，下一次还记得。</h2></div>
        <dl>
          <div><dt>你的目标</dt><dd>下一次重要沟通是什么，想进入怎样的团队。</dd></div>
          <div><dt>你的真实经历</dt><dd>做过哪些项目，哪些故事能成为表达素材。</dd></div>
          <div><dt>你的能力变化</dt><dd>哪些问题反复卡住，哪些表达已经更自然。</dd></div>
        </dl>
      </section>

      <section className="release-download" id="download" aria-labelledby="download-title">
        <div className="release-download-copy">
          <p className="eyebrow">Android 官方发布</p><h2 id="download-title">下载要直接，制品也要说得清楚。</h2>
          <p>当前没有可公开下载和验证的正式 APK。页面先把发布规则讲明白，制品完成后再把唯一入口放出来。</p>
          <Link className="button" href="/download/android">查看完整发布状态 <span aria-hidden="true">↗</span></Link>
        </div>
        <dl className="release-promises">{releasePromises.map(([title, copy]) => <div key={title}><dt>{title}</dt><dd>{copy}</dd></div>)}</dl>
      </section>

      <section className="release-faq" aria-labelledby="faq-title">
        <div className="release-section-heading release-section-heading-small"><p className="eyebrow">常见问题</p><h2 id="faq-title">下载前，先把关键问题讲明白。</h2></div>
        <div className="release-faq-list">
          {faqs.map(([question, answer]) => <details key={question}><summary>{question}<span aria-hidden="true">＋</span></summary><p>{answer}</p></details>)}
        </div>
      </section>

      <section className="release-final" aria-labelledby="final-title">
        <div><p className="eyebrow">Android 版本准备中</p><h2 id="final-title">准备完成后，只从这里下载。</h2></div>
        <Link className="button" href="/download/android">前往 Android 下载页 <span aria-hidden="true">↗</span></Link>
      </section>

      <footer><a className="brand" href="#top"><BrandMark /><span>SpeakUp</span></a><p>为真实世界准备英文表达。</p><span>© 2026 SpeakUp</span></footer>
    </main>
  );
}
