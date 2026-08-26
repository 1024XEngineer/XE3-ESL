"use client";

import {
  type CSSProperties,
  type FocusEvent,
  type TransitionEvent,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";

const AUTOPLAY_DELAY_MS = 5000;
const SLIDE_TRANSITION_MS = 620;
const LOOP_START_INDEX = 1;

const productSlides = [
  {
    src: "/assets/portal-shots/interview-entry.webp",
    width: 840,
    height: 1826,
    crop: { x: 0, y: 0, width: 840, height: 1826 },
    alt: "SpeakUp 英文面试专项练习入口",
    title: "场景化专项练习",
    description: "模拟面试与真实任务，直接开口练习。",
  },
  {
    src: "/assets/portal-shots/interview-chat.webp",
    width: 840,
    height: 1826,
    crop: { x: 0, y: 0, width: 840, height: 1826 },
    alt: "SpeakUp 根据用户指令创建 Hiring Manager 英文面试练习",
    title: "AI 深度追问",
    description: "根据你的目标，生成有上下文的针对性训练。",
  },
  {
    src: "/assets/portal-shots/ielts-review.webp",
    width: 840,
    height: 1826,
    crop: { x: 0, y: 0, width: 840, height: 1826 },
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

const loopSlides = [
  {
    slide: productSlides[productSlides.length - 1],
    sourceIndex: productSlides.length - 1,
    instanceKey: "leading-clone",
  },
  ...productSlides.map((slide, sourceIndex) => ({
    slide,
    sourceIndex,
    instanceKey: `original-${sourceIndex}`,
  })),
  ...productSlides.slice(0, 3).map((slide, sourceIndex) => ({
    slide,
    sourceIndex,
    instanceKey: `trailing-clone-${sourceIndex}`,
  })),
];

const LOOP_RESET_INDEX = productSlides.length + LOOP_START_INDEX;

function getSlidePosition(index: number, trackIndex: number) {
  return Math.max(-2, Math.min(2, index - trackIndex));
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
  const [trackIndex, setTrackIndex] = useState(LOOP_START_INDEX);
  const [transitionsEnabled, setTransitionsEnabled] = useState(true);
  const [autoplayEnabled, setAutoplayEnabled] = useState(true);
  const [isHovered, setIsHovered] = useState(false);
  const [hasFocus, setHasFocus] = useState(false);
  const [isInView, setIsInView] = useState(true);
  const [isDocumentVisible, setIsDocumentVisible] = useState(true);
  const rootRef = useRef<HTMLElement>(null);

  const activeIndex =
    (trackIndex - LOOP_START_INDEX + productSlides.length) %
    productSlides.length;

  const resetLoop = useCallback(() => {
    setTransitionsEnabled(false);
    setTrackIndex(LOOP_START_INDEX);

    window.requestAnimationFrame(() => {
      window.requestAnimationFrame(() => setTransitionsEnabled(true));
    });
  }, []);

  useEffect(() => {
    const mediaQuery = window.matchMedia("(prefers-reduced-motion: reduce)");
    const updateMotionPreference = () => {
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
      !isHovered &&
      !hasFocus &&
      isInView &&
      isDocumentVisible;

    if (!shouldAutoplay) return;

    const timer = window.setTimeout(() => {
      setTrackIndex((current) => current + 1);
    }, AUTOPLAY_DELAY_MS);

    return () => window.clearTimeout(timer);
  }, [
    activeIndex,
    autoplayEnabled,
    hasFocus,
    isDocumentVisible,
    isHovered,
    isInView,
  ]);

  useEffect(() => {
    if (trackIndex !== LOOP_RESET_INDEX) return;

    const fallback = window.setTimeout(resetLoop, SLIDE_TRANSITION_MS + 80);
    return () => window.clearTimeout(fallback);
  }, [resetLoop, trackIndex]);

  const handleFocusOut = (event: FocusEvent<HTMLElement>) => {
    if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
      setHasFocus(false);
    }
  };

  const toggleAutoplay = () => {
    const willEnableAutoplay = !autoplayEnabled;
    setAutoplayEnabled(willEnableAutoplay);
    if (willEnableAutoplay) setHasFocus(false);
  };

  const handleSlideTransitionEnd = (
    event: TransitionEvent<HTMLElement>,
  ) => {
    if (
      event.target === event.currentTarget &&
      event.propertyName === "transform" &&
      trackIndex === LOOP_RESET_INDEX
    ) {
      resetLoop();
    }
  };

  const activeSlide = productSlides[activeIndex];

  return (
    <section
      ref={rootRef}
      className="hero-product-carousel"
      data-snap={!transitionsEnabled}
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
            {loopSlides.map(
              ({ slide, sourceIndex, instanceKey }, renderIndex) => {
                const position = getSlidePosition(renderIndex, trackIndex);
                const isActive = position === 0;
                const isAdjacent = Math.abs(position) === 1;

                return (
                  <figure
                    className="hero-product-carousel__slide"
                    data-active={isActive}
                    data-adjacent={isAdjacent}
                    data-position={position}
                    data-source-index={sourceIndex}
                    aria-hidden={!isActive}
                    key={instanceKey}
                    onTransitionEnd={
                      isActive ? handleSlideTransitionEnd : undefined
                    }
                  >
                    <div className="hero-product-carousel__device">
                      <div className="hero-product-carousel__screen">
                        {/* eslint-disable-next-line @next/next/no-img-element */}
                        <img
                          src={slide.src}
                          alt={isActive ? slide.alt : ""}
                          width={slide.width}
                          height={slide.height}
                          loading={
                            renderIndex === LOOP_START_INDEX ? "eager" : "lazy"
                          }
                          fetchPriority={
                            renderIndex === LOOP_START_INDEX ? "high" : "auto"
                          }
                          decoding="async"
                          draggable="false"
                          style={getCropStyle(slide)}
                        />
                      </div>
                    </div>
                  </figure>
                );
              },
            )}
          </div>
        </div>
      </div>

      <div
        className="hero-product-carousel__caption"
        aria-live={autoplayEnabled ? "off" : "polite"}
        aria-atomic="true"
      >
        <strong>{activeSlide.title}</strong>
        <span>{activeSlide.description}</span>
      </div>

      <div className="hero-product-carousel__controls">
        <div className="hero-product-carousel__progress" aria-hidden="true">
          {productSlides.map((slide, index) => (
            <span key={slide.src} data-active={index === activeIndex} />
          ))}
        </div>

        <button
          type="button"
          className="hero-product-carousel__autoplay"
          aria-label={autoplayEnabled ? "暂停自动播放" : "继续自动播放"}
          onClick={toggleAutoplay}
        >
          <span aria-hidden="true">{autoplayEnabled ? "Ⅱ" : "▶"}</span>
        </button>
      </div>
    </section>
  );
}
