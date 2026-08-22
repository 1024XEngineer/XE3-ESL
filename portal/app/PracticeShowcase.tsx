"use client";

import { useEffect, useRef, useState } from "react";

const practiceScreens = [
  {
    image: "/assets/practice-screen/ielts.webp",
    alt: "SpeakUp 雅思口语练习首页，提供 Part 1、Part 2、Part 3 与完整模考",
  },
  {
    image: "/assets/practice-screen/interview.webp",
    alt: "SpeakUp 英文面试练习首页，提供模拟面试与轮次专项练习",
  },
  {
    image: "/assets/practice-screen/travel.webp",
    alt: "SpeakUp 生活与旅行练习首页，覆盖日常交流与出行场景",
  },
  {
    image: "/assets/practice-screen/workplace.webp",
    alt: "SpeakUp 职场英语练习首页，覆盖会议、协作与客户沟通",
  },
];

export default function PracticeShowcase() {
  const sectionRef = useRef<HTMLElement | null>(null);
  const viewportRef = useRef<HTMLDivElement | null>(null);
  const trackRef = useRef<HTMLDivElement | null>(null);
  const [activeIndex, setActiveIndex] = useState(0);

  useEffect(() => {
    const section = sectionRef.current;
    const viewport = viewportRef.current;
    const track = trackRef.current;
    if (!section || !viewport || !track) return;

    const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
    let frame = 0;

    const update = () => {
      frame = 0;
      const usesNativeScroll = window.innerWidth <= 980 || reduceMotion.matches;

      if (usesNativeScroll) {
        section.style.height = "auto";
        track.style.transform = "none";
        const maxScroll = Math.max(1, viewport.scrollWidth - viewport.clientWidth);
        setActiveIndex(Math.round((viewport.scrollLeft / maxScroll) * (practiceScreens.length - 1)));
        return;
      }

      const maxShift = Math.max(0, track.scrollWidth - viewport.clientWidth);
      section.style.height = `${window.innerHeight * 3.4}px`;
      const sectionTop = section.getBoundingClientRect().top + window.scrollY;
      const scrollRange = Math.max(1, section.offsetHeight - window.innerHeight);
      const progress = Math.min(1, Math.max(0, (window.scrollY - sectionTop) / scrollRange));
      track.style.transform = `translate3d(${-progress * maxShift}px, 0, 0)`;
      setActiveIndex(Math.round(progress * (practiceScreens.length - 1)));
    };

    const scheduleUpdate = () => {
      if (!frame) frame = window.requestAnimationFrame(update);
    };

    const resizeObserver = new ResizeObserver(scheduleUpdate);
    resizeObserver.observe(viewport);
    resizeObserver.observe(track);
    window.addEventListener("scroll", scheduleUpdate, { passive: true });
    window.addEventListener("resize", scheduleUpdate);
    viewport.addEventListener("scroll", scheduleUpdate, { passive: true });
    reduceMotion.addEventListener("change", scheduleUpdate);
    update();

    return () => {
      resizeObserver.disconnect();
      window.removeEventListener("scroll", scheduleUpdate);
      window.removeEventListener("resize", scheduleUpdate);
      viewport.removeEventListener("scroll", scheduleUpdate);
      reduceMotion.removeEventListener("change", scheduleUpdate);
      if (frame) window.cancelAnimationFrame(frame);
    };
  }, []);

  return (
    <section className="practice-section" id="use-cases" ref={sectionRef} aria-labelledby="practice-title">
      <div className="practice-horizontal-frame">
        <div className="practice-horizontal-header">
          <h2 id="practice-title">你要面对什么，SpeakUp 就陪你练什么。</h2>
          <div className="practice-progress" aria-label={`第 ${activeIndex + 1} 张，共 4 张`}>
            {practiceScreens.map((screen, index) => (
              <i data-active={activeIndex === index} key={screen.image} />
            ))}
          </div>
        </div>

        <div className="practice-carousel-card">
          <div className="practice-track-viewport" ref={viewportRef} aria-label="SpeakUp 四类真实练习页面">
            <div className="practice-track" ref={trackRef}>
              {practiceScreens.map((screen, index) => (
                <figure className="practice-slide" key={screen.image}>
                  {/* eslint-disable-next-line @next/next/no-img-element */}
                  <img src={screen.image} alt={screen.alt} loading={index === 0 ? "eager" : "lazy"} />
                </figure>
              ))}
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
