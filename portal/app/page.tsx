import EarlyAccessDialog from "./EarlyAccessDialog";
import BrandMark from "./BrandMark";
import PracticeShowcase from "./PracticeShowcase";

const earlyAccessHref = "#early-access";

const flowingTasks = [
  "我下周有一场英文面试",
  "明天要第一次独自去医院",
  "我要向海外客户汇报项目",
  "帮我准备 IELTS Part 2",
  "我要参加第一次全英文会议",
  "帮我和房东说明维修问题",
  "下周要做一次英文产品演示",
  "我要准备海外大学课堂发言",
];

const flowingTaskLoop = `${flowingTasks.join(" · ")} · ${flowingTasks.join(" · ")} · `;

export default function Home() {
  return (
    <main>
      <nav className="site-nav" aria-label="主导航">
        <a className="brand" href="#top" aria-label="SpeakUp 首页">
          <BrandMark />
          <span>SpeakUp</span>
        </a>
        <div className="nav-links">
          <a href="#use-cases">适用阶段</a>
          <a href="#memory">长期记忆</a>
        </div>
        <a className="button button-small" href={earlyAccessHref} data-scenario="英文面试">
          申请体验
        </a>
      </nav>

      <header className="hero" id="top">
        <div className="hero-copy">
          <h1>
            <span className="headline-muted">下一场重要的英文沟通，</span>
            <br />
            先和 SpeakUp 练一遍。
          </h1>
          <p className="hero-subtitle">
            一个有记忆、越用越懂你的 AI 口语老师。
          </p>
          <div className="button-group">
            <a className="button" href={earlyAccessHref} data-scenario="英文面试">
              告诉 SpeakUp，我要准备什么 <span aria-hidden="true">↗</span>
            </a>
            <a className="button button-secondary" href="#use-cases">
              看看适用阶段
            </a>
          </div>
        </div>

        <div
          className="scenario-flow"
          role="img"
          aria-label={`可以告诉 SpeakUp 的真实任务：${flowingTasks.join("；")}`}
        >
          <div className="scenario-flow-stage" aria-hidden="true">
            <svg viewBox="0 0 1200 240" preserveAspectRatio="xMidYMid meet">
              <defs>
                <path
                  id="scenario-flow-input-path"
                  d="M -40 196 C 180 208 330 190 470 158 C 610 126 720 102 840 108 C 980 116 1100 82 1240 48"
                />
              </defs>

              <text className="scenario-flow-copy scenario-flow-copy-muted scenario-flow-copy-motion">
                <textPath href="#scenario-flow-input-path" startOffset="-50%">
                  {flowingTaskLoop}
                  <animate attributeName="startOffset" from="-50%" to="0%" dur="2.2s" repeatCount="indefinite" />
                </textPath>
              </text>
              <text className="scenario-flow-copy scenario-flow-copy-muted scenario-flow-copy-static">
                <textPath href="#scenario-flow-input-path" startOffset="4%">
                  {flowingTaskLoop}
                </textPath>
              </text>
            </svg>

            <div className="scenario-flow-agent">
              <span className="scenario-flow-listener">
                <span className="scenario-flow-wave">
                  <i /><i /><i /><i /><i /><i /><i /><i /><i />
                </span>
              </span>
            </div>
          </div>
        </div>

        <div className="hero-product" aria-label="SpeakUp 根据目标准备后端开发英文面试练习示例">
          <div className="hero-product-copy">
            <span className="demo-label">一句话开始 · 后端开发面试</span>
            <p className="demo-question">我下周要面试后端开发工程师，想提前练一下。</p>
            <div className="voice-answer">
              <span className="voice-icon" aria-hidden="true">●</span>
              <div className="voice-bars" aria-hidden="true">
                <i /><i /><i /><i /><i /><i /><i /><i />
              </div>
              <span>0:16</span>
            </div>
            <p className="demo-answer">可以。先选你最想练的方向，我会为你准备一场贴近真实面试的英文练习。</p>
            <div className="instant-feedback">
              <span>已为你准备好</span>
              <p><strong>项目经历深挖</strong> · 你是候选人，AI 是英文技术面试官</p>
            </div>
          </div>
          <div className="hero-phone">
            <img
              src="/assets/portal-shots/portal-interview-practice.png"
              alt="SpeakUp 根据后端开发面试目标准备项目经历深挖练习"
            />
          </div>
          <span className="floating-chip chip-top">按目标生成练习</span>
          <span className="floating-chip chip-bottom">准备好就开练</span>
        </div>
      </header>

      <PracticeShowcase />

      <section className="context-section" id="memory">
        <div className="context-shot">
          <div className="context-shot-frame">
            <img
              src="/assets/portal-shots/portal-memory-chat.jpg"
              alt="SpeakUp Memory 记录用户目标、真实项目、能力变化与下一轮重点"
            />
          </div>
        </div>
        <div className="context-copy">
          <p className="eyebrow eyebrow-light">越用越懂你</p>
          <h2>每一次练习，<br />都留给下一次。</h2>
          <p>SpeakUp 记住的不只是聊天记录，而是那些会改变下一轮教学的真实信息。</p>
          <ul className="context-list">
            <li>
              <span>01</span>
              <div><strong>你的目标</strong><small>想进入怎样的团队，下一次重要沟通是什么。</small></div>
            </li>
            <li>
              <span>02</span>
              <div><strong>你的真实经历</strong><small>做过哪些项目，哪些故事可以成为你的表达素材。</small></div>
            </li>
            <li>
              <span>03</span>
              <div><strong>你的能力变化</strong><small>哪些问题反复卡住，哪些表达已经真正变得自然。</small></div>
            </li>
            <li>
              <span>04</span>
              <div><strong>现实带回来的结果</strong><small>哪些题命中了，哪些新问题需要补进下一轮。</small></div>
            </li>
          </ul>
        </div>
      </section>

      <section className="final-cta">
        <p className="eyebrow eyebrow-light">从下一件必须说清楚的事开始</p>
        <h2>告诉 SpeakUp，<br />接下来要面对什么。</h2>
        <p>可以是一场雅思口语考试、英文面试，也可以是海外生活和工作里马上要发生的关键沟通。</p>
        <div className="button-group">
          <a className="button" href={earlyAccessHref} data-scenario="英文面试">开始一次任务准备 ↗</a>
          <a className="button button-dark-secondary" href="#top">回到顶部</a>
        </div>
      </section>

      <EarlyAccessDialog />

      <footer>
        <a className="brand" href="#top"><BrandMark /><span>SpeakUp</span></a>
        <p>有记忆的 AI Agent 口语老师</p>
        <span>© 2026 SpeakUp</span>
      </footer>
    </main>
  );
}
