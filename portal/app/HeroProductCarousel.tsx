"use client";

import {
  type CSSProperties,
  type FocusEvent,
  useEffect,
  useRef,
  useState,
} from "react";

const AUTOPLAY_DELAY_MS = 5000;

const productSlides = [
  {
    src: "/assets/portal-shots/interview-entry.webp",
    width: 898,
    height: 1824,
    crop: { x: 52, y: 36, width: 794, height: 1750 },
    alt: "SpeakUp 英文面试专项练习入口",
    title: "场景化专项练习",
    description: "模拟面试与真实任务，直接开口练习。",
  },
  {
    src: "/assets/portal-shots/interview-chat.webp",
    width: 912,
    height: 1816,
    crop: { x: 54, y: 35, width: 804, height: 1746 },
    alt: "SpeakUp 根据后端开发面试目标生成项目经历深挖练习",
    title: "AI 深度追问",
    description: "根据你的目标，生成有上下文的针对性训练。",
  },
  {
    src: "/assets/portal-shots/ielts-review.webp",
    width: 900,
    height: 1790,
    crop: { x: 54, y: 32, width: 792, height: 1726 },
    alt: "SpeakUp 雅思口语 Part 1 四维评分与练习建议",
    title: "多维能力评分",
    description: "从流利度、词汇、语法和发音分析表现。",
  },
  {
    src: "/assets/portal-shots/practice-progress.webp",
    width: 916,
    height: 1806,
    crop: { x: 73, y: 31, width: 787, height: 1718 },
    alt: "SpeakUp 根据历史练习总结最近的口语进步",
    title: "看见长期进步",
    description: "对比历史练习，持续发现提升和薄弱点。",
  },
] as const;

function getSlidePosition(index: number, activeIndex: number) {
  const length = productSlides.length;
  let position = index - activeIndex;

  if (position > length / 2) position -= length;
  if (position < -length / 2) position += length;

  return position;
}

function getCropStyle(slide: (typeof productSlides)[number]): CSSProperties {
  return {
    width: `${(slide.width / slide.crop.width) * 100}%`,
    height: `${(slide.height / slide.crop.height) * 100}%`,
    left: `${(-slide.crop.x / slide.crop.width) * 100}%`,
    top: `${(-slide.crop.y / slide.crop.height) * 100}%`,
  };
}

export default function HeroProductCarousel() {
  const [activeIndex, setActiveIndex] = useState(0);
  const [autoplayEnabled, setAutoplayEnabled] = useState(true);
  const [isHovered, setIsHovered] = useState(false);
  const [hasFocus, setHasFocus] = useState(false);
  const [isInView, setIsInView] = useState(true);
  const [isDocumentVisible, setIsDocumentVisible] = useState(true);
  const [prefersReducedMotion, setPrefersReducedMotion] = useState(false);
  const rootRef = useRef<HTMLElement>(null);

  const showSlide = (index: number, pauseAutoplay = true) => {
    setActiveIndex((index + productSlides.length) % productSlides.length);
    if (pauseAutoplay) setAutoplayEnabled(false);
  };

  const showPrevious = () => {
    setAutoplayEnabled(false);
    setActiveIndex(
      (current) =>
        (current - 1 + productSlides.length) % productSlides.length,
    );
  };

  const showNext = () => {
    setAutoplayEnabled(false);
    setActiveIndex((current) => (current + 1) % productSlides.length);
  };

  useEffect(() => {
    const mediaQuery = window.matchMedia("(prefers-reduced-motion: reduce)");
    const updateMotionPreference = () => {
      setPrefersReducedMotion(mediaQuery.matches);
      if (mediaQuery.matches) setAutoplayEnabled(false);
    };

    updateMotionPreference();
    mediaQuery.addEventListener("change", updateMotionPreference);
    return () => mediaQuery.removeEventListener("change", updateMotionPreference);
  }, []);

  useEffect(() => {
    const updateVisibility = () => {
      setIsDocumentVisible(document.visibilityState === "visible");
    };

    updateVisibility();
    document.addEventListener("visibilitychange", updateVisibility);
    return () => document.removeEventListener("visibilitychange", updateVisibility);
  }, []);

  useEffect(() => {
    const root = rootRef.current;
    if (!root) return;

    const observer = new IntersectionObserver(
      ([entry]) => setIsInView(entry.isIntersecting),
      { threshold: 0.35 },
    );

    observer.observe(root);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const shouldAutoplay =
      autoplayEnabled &&
      !prefersReducedMotion &&
      !isHovered &&
      !hasFocus &&
      isInView &&
      isDocumentVisible;

    if (!shouldAutoplay) return;

    const timer = window.setTimeout(() => {
      setActiveIndex((current) => (current + 1) % productSlides.length);
    }, AUTOPLAY_DELAY_MS);

    return () => window.clearTimeout(timer);
  }, [
    activeIndex,
    autoplayEnabled,
    hasFocus,
    isDocumentVisible,
    isHovered,
    isInView,
    prefersReducedMotion,
  ]);

  const handleFocusOut = (event: FocusEvent<HTMLElement>) => {
    if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
      setHasFocus(false);
    }
  };

  const activeSlide = productSlides[activeIndex];
  const announcementsArePolite = !autoplayEnabled || prefersReducedMotion;

  return (
    <section
      ref={rootRef}
      className="hero-product-carousel"
      aria-label="SpeakUp 产品界面"
      aria-roledescription="轮播图"
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
      onFocusCapture={() => setHasFocus(true)}
      onBlurCapture={handleFocusOut}
    >
      <div className="hero-product-carousel__stage">
        <div className="hero-product-carousel__viewport">
          <div className="hero-product-carousel__track">
            {productSlides.map((slide, index) => {
              const position = getSlidePosition(index, activeIndex);
              const isActive = position === 0;
              const isAdjacent = Math.abs(position) === 1;

              return (
                <figure
                  className="hero-product-carousel__slide"
                  data-active={isActive}
                  data-adjacent={isAdjacent}
                  aria-hidden={!isActive}
                  key={slide.src}
                  style={{
                    transform: `translate3d(calc(-50% + ${position * 82}%), 0, 0) scale(${isActive ? 1 : 0.92})`,
                  }}
                >
                  <div className="hero-product-carousel__device">
                    <div className="hero-product-carousel__screen">
                      {/* eslint-disable-next-line @next/next/no-img-element */}
                      <img
                        src={slide.src}
                        alt={isActive ? slide.alt : ""}
                        width={slide.width}
                        height={slide.height}
                        loading={index === 0 ? "eager" : "lazy"}
                        fetchPriority={index === 0 ? "high" : "auto"}
                        decoding="async"
                        draggable="false"
                        style={getCropStyle(slide)}
                      />
                    </div>
                  </div>
                </figure>
              );
            })}
          </div>
        </div>

        <button
          type="button"
          className="hero-product-carousel__arrow hero-product-carousel__arrow--previous"
          aria-label="上一张产品截图"
          onClick={showPrevious}
        >
          <span aria-hidden="true">←</span>
        </button>
        <button
          type="button"
          className="hero-product-carousel__arrow hero-product-carousel__arrow--next"
          aria-label="下一张产品截图"
          onClick={showNext}
        >
          <span aria-hidden="true">→</span>
        </button>
      </div>

      <div
        className="hero-product-carousel__caption"
        aria-live={announcementsArePolite ? "polite" : "off"}
        aria-atomic="true"
      >
        <strong>{activeSlide.title}</strong>
        <span>{activeSlide.description}</span>
      </div>

      <div className="hero-product-carousel__controls">
        <div className="hero-product-carousel__progress" aria-label="选择产品截图">
          {productSlides.map((slide, index) => (
            <button
              type="button"
              key={slide.src}
              aria-label={`展示第 ${index + 1} 张：${slide.title}`}
              aria-current={index === activeIndex ? "true" : undefined}
              data-active={index === activeIndex}
              onClick={() => showSlide(index)}
            >
              <span />
            </button>
          ))}
        </div>

        {!prefersReducedMotion ? (
          <button
            type="button"
            className="hero-product-carousel__autoplay"
            aria-label={autoplayEnabled ? "暂停自动播放" : "继续自动播放"}
            onClick={() => setAutoplayEnabled((enabled) => !enabled)}
          >
            <span aria-hidden="true">{autoplayEnabled ? "Ⅱ" : "▶"}</span>
          </button>
        ) : null}
      </div>
    </section>
  );
}
