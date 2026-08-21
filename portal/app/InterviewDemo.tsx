"use client";

import { useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";

type DemoKind = "understand" | "practice" | "interview" | "review" | "real-world";

type DemoFeature = {
  index: string;
  kind: DemoKind;
  title: string;
  copy: string;
  status: string;
  href: string;
  action: string;
};

function CoachTurn({ children }: { children: ReactNode }) {
  return (
    <div className="demo-coach-turn">
      <span className="demo-coach-mark" aria-hidden="true">S</span>
      <div>
        <strong>SpeakUp</strong>
        {children}
      </div>
    </div>
  );
}

function VoiceBubble({ label, duration }: { label: string; duration: string }) {
  return (
    <div className="demo-voice-bubble">
      <span className="demo-mic" aria-hidden="true" />
      <span className="demo-wave" aria-hidden="true">
        {[7, 15, 22, 12, 26, 18, 10, 24, 16, 8, 20, 12].map((height, index) => (
          <i key={index} style={{ height }} />
        ))}
      </span>
      <strong>{label}</strong>
      <span>{duration}</span>
    </div>
  );
}

function InterviewerQuestion({ question }: { question: string }) {
  const [showTranslation, setShowTranslation] = useState(false);
  const [showHint, setShowHint] = useState(false);

  function replayQuestion() {
    if (!("speechSynthesis" in window)) return;
    window.speechSynthesis.cancel();
    window.speechSynthesis.speak(new SpeechSynthesisUtterance(question));
  }

  return (
    <div className="demo-interviewer-turn">
      <span className="demo-interviewer-avatar" aria-hidden="true">E</span>
      <div>
        <div className="demo-interviewer-name">
          <strong>Ethan</strong>
          <small>Engineering Manager</small>
        </div>
        <div className="demo-interviewer-bubble">
          <p>{question}</p>
          {showTranslation && <p className="demo-question-support">请介绍一个你设计过的后端系统，以及当时最困难的技术取舍。</p>}
          {showHint && <p className="demo-question-support">提示：先交代系统规模，再说明约束、选择与结果。</p>}
          <div className="demo-question-actions">
            <button type="button" aria-pressed={showTranslation} onClick={() => setShowTranslation(!showTranslation)}>翻译</button>
            <button className="demo-audio-action" type="button" aria-label="重播问题" onClick={replayQuestion}>
              <span className="demo-speaker-icon" aria-hidden="true" />
            </button>
            <button type="button" aria-pressed={showHint} onClick={() => setShowHint(!showHint)}>提示</button>
          </div>
        </div>
      </div>
    </div>
  );
}

function CoachScreen({ children }: { children: ReactNode }) {
  return (
    <div className="demo-product-screen demo-coach-screen">
      <div className="demo-screen-bar">
        <strong>SpeakUp</strong>
        <span>AI 口语老师</span>
      </div>
      <div className="demo-screen-body">{children}</div>
    </div>
  );
}

function DemoPanel({ kind, href }: { kind: DemoKind; href: string }) {
  if (kind === "understand") {
    return (
      <CoachScreen>
        <p className="demo-user-message">我下周有一场外企后端面试，好紧张。</p>
        <CoachTurn>
          <p>先别急着练。是哪家公司？方便把岗位 JD 和简历发给我吗？我先帮你判断这轮最可能考什么。</p>
        </CoachTurn>
        <p className="demo-user-message demo-user-message-short">已经上传了。</p>
        <div className="demo-attachments" aria-label="已上传资料">
          <span>Backend Engineer JD</span>
          <span>中文简历.pdf</span>
        </div>
        <CoachTurn>
          <p>看完了。这个岗位很看重高并发系统、消息队列和技术取舍。我们先练自我介绍，再练项目深挖和系统设计。</p>
        </CoachTurn>
      </CoachScreen>
    );
  }

  if (kind === "practice") {
    return (
      <CoachScreen>
        <CoachTurn>
          <p>我们先练自我介绍。不要只说工作年限，可以先说你解决过什么问题。</p>
        </CoachTurn>
        <div className="demo-phrase">
          <span aria-hidden="true">▶</span>
          <p>I&apos;m a backend engineer focused on building reliable systems at scale.</p>
        </div>
        <div className="demo-user-row"><VoiceBubble label="语音回答" duration="0:18" /></div>
        <CoachTurn>
          <p>很好，经历已经说清楚了。接下来补一句你的代表项目，以及你具体解决了什么问题。</p>
        </CoachTurn>
        <div className="demo-user-row"><VoiceBubble label="语音回答" duration="0:24" /></div>
        <CoachTurn>
          <p>准备得差不多了。现在我邀请面试官和你进行一次真实追问。</p>
        </CoachTurn>
        <div className="demo-invite">
          <div>
            <strong>后端开发工程师 · 模拟面试</strong>
            <span>项目深挖与系统设计</span>
          </div>
          <a href={href}>进入模拟面试</a>
        </div>
      </CoachScreen>
    );
  }

  if (kind === "interview") {
    return (
      <div className="demo-product-screen demo-interview-screen">
        <div className="demo-screen-bar">
          <span>系统设计模拟</span>
          <strong>08:42</strong>
          <span>结束面试</span>
        </div>
        <div className="demo-interviewer-strip" aria-label="本轮面试官">
          <span>Mia</span>
          <span className="is-active">Ethan</span>
          <span>Noah</span>
        </div>
        <div className="demo-interview-body">
          <InterviewerQuestion question="Tell me about a backend system you designed and the most difficult trade-off you made." />
          <div className="demo-user-row"><VoiceBubble label="你的回答" duration="0:42" /></div>
          <div className="demo-follow-up">
            <strong>Ethan 继续追问</strong>
            <p>Why did you choose Kafka instead of a simpler queue?</p>
            <p>What would happen if a consumer processed the same message twice?</p>
          </div>
        </div>
      </div>
    );
  }

  if (kind === "review") {
    return (
      <CoachScreen>
        <div className="demo-session-summary">
          <strong>模拟面试已完成</strong>
          <span>18 分钟 · 6 个问题</span>
          <a href={href}>查看记录</a>
        </div>
        <CoachTurn>
          <p>刚才项目背景说得很清楚。但回答“为什么选择 Kafka”时，你只讲了技术特点，还没有说明当时的业务压力。</p>
        </CoachTurn>
        <blockquote>“Kafka can handle high throughput, so we chose it.”</blockquote>
        <CoachTurn>
          <p>可以补上当时的业务约束和最后的结果。我们按刚才那道题再说一次。</p>
        </CoachTurn>
        <div className="demo-user-row"><VoiceBubble label="重新回答" duration="0:29" /></div>
      </CoachScreen>
    );
  }

  return (
    <CoachScreen>
      <p className="demo-real-date">7 月 28 日 · 真实面试结束后</p>
      <CoachTurn>
        <p>今天的面试怎么样？之前练过的 Kafka 和系统设计有出现吗？</p>
      </CoachTurn>
      <p className="demo-user-message">都问到了，回答得还不错。但面试官问了数据库迁移失败怎么回滚，这部分我们没练到。</p>
      <CoachTurn>
        <p>太好了，之前的准备确实有成效。Kafka 这部分你已经越来越稳定了。</p>
      </CoachTurn>
      <CoachTurn>
        <p>我已经根据这次没练到的问题，创建了一轮数据库迁移与回滚专项模拟。我们现在把它补上。</p>
      </CoachTurn>
      <div className="demo-invite">
        <div>
          <strong>数据库迁移与回滚 · 专项模拟</strong>
          <span>失败处理、回滚策略与风险沟通</span>
        </div>
        <a href={href}>进入专项练习</a>
      </div>
    </CoachScreen>
  );
}

export default function InterviewDemo({ features }: { features: DemoFeature[] }) {
  const [activeIndex, setActiveIndex] = useState(0);
  const stepRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const active = features[activeIndex];

  useEffect(() => {
    const desktopQuery = window.matchMedia("(min-width: 641px)");
    let observer: IntersectionObserver | null = null;

    function observeSteps() {
      observer?.disconnect();
      if (!desktopQuery.matches) return;

      observer = new IntersectionObserver(
        (entries) => {
          const current = entries.find((entry) => entry.isIntersecting);
          if (!current) return;
          const index = Number((current.target as HTMLElement).dataset.demoIndex);
          if (Number.isInteger(index)) setActiveIndex(index);
        },
        { rootMargin: "-40% 0px -40% 0px", threshold: 0 },
      );

      stepRefs.current.forEach((step) => {
        if (step) observer?.observe(step);
      });
    }

    observeSteps();
    desktopQuery.addEventListener("change", observeSteps);
    return () => {
      observer?.disconnect();
      desktopQuery.removeEventListener("change", observeSteps);
    };
  }, [features.length]);

  function goToStep(index: number) {
    setActiveIndex(index);
    const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    stepRefs.current[index]?.scrollIntoView({
      behavior: reduceMotion ? "auto" : "smooth",
      block: "center",
    });
  }

  function selectByKey(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    if (!["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(event.key)) return;
    event.preventDefault();
    const direction = event.key === "ArrowRight" || event.key === "ArrowDown" ? 1 : -1;
    const nextIndex = (index + direction + features.length) % features.length;
    goToStep(nextIndex);
    stepRefs.current[nextIndex]?.focus();
  }

  return (
    <>
      <div className={`demo-sequence demo-tone-${activeIndex + 1}`}>
        <nav className="demo-step-list" aria-label="SpeakUp 陪伴一次真实任务的五个阶段">
          {features.map((feature, index) => (
            <button
              className="demo-step"
              data-demo-index={index}
              id={`demo-step-${feature.index}`}
              key={feature.index}
              type="button"
              ref={(element) => { stepRefs.current[index] = element; }}
              aria-current={activeIndex === index ? "step" : undefined}
              onClick={() => goToStep(index)}
              onKeyDown={(event) => selectByKey(event, index)}
            >
              <span><b>{feature.index}</b><em>{feature.status}</em></span>
              <strong>{feature.title}</strong>
              <small>{feature.copy}</small>
            </button>
          ))}
        </nav>

        <article
          className="demo-stage"
          id="demo-stage"
          aria-labelledby={`demo-step-${active.index}`}
        >
          <header>
            <span>{active.index} / {String(features.length).padStart(2, "0")}</span>
            <em>{active.status}</em>
            <a href={active.href}>
              {active.action} <span aria-hidden="true">↗</span>
            </a>
          </header>
          <div className="demo-stage-frame">
            <DemoPanel key={active.kind} kind={active.kind} href={active.href} />
          </div>
        </article>
      </div>

      <div className="demo-mobile-story" aria-label="SpeakUp 陪伴一次真实任务的五个阶段">
        {features.map((feature, index) => (
          <article className={`demo-mobile-chapter demo-tone-${index + 1}`} key={feature.index}>
            <header>
              <span><b>{feature.index}</b><em>{feature.status}</em></span>
              <h3>{feature.title}</h3>
              <p>{feature.copy}</p>
            </header>
            <div className="demo-mobile-stage-frame">
              <DemoPanel kind={feature.kind} href={feature.href} />
            </div>
            <a className="demo-mobile-action" href={feature.href}>
              {feature.action} <span aria-hidden="true">↗</span>
            </a>
          </article>
        ))}
      </div>
    </>
  );
}
