import EarlyAccessDialog from "./EarlyAccessDialog";
import BrandMark from "./BrandMark";

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

const journeyStages = [
  {
    index: "01",
    stage: "考出去",
    type: "雅思口语",
    copy: "“陪我练一次 IELTS Part 2，重点看我能不能讲满两分钟。”",
    action: "准备下一次口语考试",
    href: earlyAccessHref,
  },
  {
    index: "02",
    stage: "面进去",
    type: "英文面试",
    copy: "“我下周面试后端开发工程师，想练系统设计和技术取舍。”",
    action: "准备下一场面试",
    href: earlyAccessHref,
  },
  {
    index: "03",
    stage: "适应好",
    type: "海外日常",
    copy: "“明天要去医院，我怕听不懂医生的追问。”",
    action: "说一件马上要办的事",
    href: earlyAccessHref,
  },
  {
    index: "04",
    stage: "发展好",
    type: "国际职场",
    copy: "“我要向海外客户解释项目延期，但不能承诺新的日期。”",
    action: "准备下一次工作沟通",
    href: earlyAccessHref,
  },
];

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

        <div className="hero-product" aria-label="SpeakUp 后端开发工程师模拟面试示例">
          <div className="hero-product-copy">
            <span className="demo-label">老师主动了解 · 后端开发面试</span>
            <p className="demo-question">我下周要面试后端开发工程师，想提前练一下。</p>
            <div className="voice-answer">
              <span className="voice-icon" aria-hidden="true">●</span>
              <div className="voice-bars" aria-hidden="true">
                <i /><i /><i /><i /><i /><i /><i /><i />
              </div>
              <span>0:16</span>
            </div>
            <p className="demo-answer">可以。把岗位 JD 和简历发给我，我先看看这轮更可能考什么，再和你一起准备。</p>
            <div className="instant-feedback">
              <span>接下来</span>
              <p><strong>了解岗位与经历</strong> → 给建议 → 教表达 → 邀请模拟</p>
            </div>
          </div>
          <div className="hero-phone">
            <img
              src="/assets/portal-shots/portal-interview-start.jpg"
              alt="SpeakUp 为后端开发工程师生成四轮一对一模拟面试计划"
            />
          </div>
          <span className="floating-chip chip-top">会主动追问</span>
          <span className="floating-chip chip-bottom">长期 Memory 已开启</span>
        </div>
      </header>

      <section className="journey-section" id="use-cases">
        <div className="section-intro dark-copy">
          <p className="eyebrow">不只是一场面试</p>
          <h2>
            <span className="journey-title-line">考出去、面进去，</span>
            <span className="journey-title-line">适应好，再到真正发展好。</span>
          </h2>
          <p>场景会变，陪你的老师不变。直接说出眼前要面对的事，SpeakUp 会用已经了解的你继续准备。</p>
        </div>
        <div className="journey-grid" aria-label="SpeakUp 覆盖的四类真实英语任务">
          {journeyStages.map((item) => (
            <article className="journey-card" key={item.type}>
              <div className="journey-meta"><span>{item.index}</span><em>{item.stage}</em></div>
              <h3>{item.type}</h3>
              <p>{item.copy}</p>
              <a href={item.href} data-scenario={item.type}>
                {item.action} <span aria-hidden="true">↗</span>
              </a>
            </article>
          ))}
        </div>
      </section>

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

      <section className="outcome-section" id="real-world">
        <div className="outcome-copy">
          <p className="eyebrow">现实回来，学习继续</p>
          <h2>真正发生过的事，<br />会改变下一次怎么练。</h2>
          <p>练习不是关掉页面就结束。把真实沟通的结果告诉老师，它会庆祝已经发生的进步，也把遗漏的问题补进新的计划。</p>
        </div>
        <div className="outcome-dialogue" aria-label="用户把真实面试结果带回给 SpeakUp">
          <div className="dialogue-message user-message">
            <small>你 · 面试结束后</small>
            <p>老师，你压中 Kafka 了！但数据库迁移这题没练到。</p>
          </div>
          <div className="dialogue-message agent-message">
            <small>SpeakUp</small>
            <p>太好了，之前的练习有成效。趁记忆还清楚，我们把没练到的题还原一下，再补进下一轮。</p>
          </div>
          <div className="dialogue-memory">
            <span>已带回 Memory</span>
            <strong>数据库迁移 · 真实面试新问题</strong>
          </div>
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
