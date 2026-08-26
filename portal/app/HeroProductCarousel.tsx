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
const WHEEL_DELTA_THRESHOLD_PX = 40;
const WHEEL_GESTURE_IDLE_MS = 180;
const LOOP_START_INDEX = 1;
const LOOP_LEADING_CLONE_INDEX = 0;

const productSlides = [
  {
    src: "/assets/portal-shots/interview-entry.webp",
    width: 840,
    height: 1826,
    crop: { x: 12, y: 0, width: 816, height: 1826 },
    alt: "SpeakUp 英文面试专项练习入口",
    title: "场景化专项练习",
    description: "模拟面试与真实任务，直接开口练习。",
  },
  {
    src: "/assets/portal-shots/interview-chat.webp",
    width: 840,
    height: 1826,
    crop: { x: 12, y: 0, width: 816, height: 1826 },
    alt: "SpeakUp 根据用户指令创建 Hiring Manager 英文面试练习",
    title: "AI 深度追问",
    description: "根据你的目标，生成有上下文的针对性训练。",
  },
  {
    src: "/assets/portal-shots/ielts-review.webp",
    width: 840,
    height: 1826,
    crop: { x: 12, y: 0, width: 816, height: 1826 },
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

const LOOP_TRAILING_CLONE_INDEX = productSlides.length + LOOP_START_INDEX;

function getLoopResetIndex(trackIndex: number) {
  if (trackIndex === LOOP_LEADING_CLONE_INDEX) return productSlides.length;
  if (trackIndex === LOOP_TRAILING_CLONE_INDEX) return LOOP_START_INDEX;
  return null;
}

function getWheelDeltaInPixels(event: WheelEvent, pageHeight: number) {
  if (event.deltaMode === 1) return event.deltaY * 16;
  if (event.deltaMode === 2) return event.deltaY * pageHeight;
  return event.deltaY;
}

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
  const isSlidingRef = useRef(false);
  const wheelDeltaRef = useRef(0);
  const wheelGestureLockedRef = useRef(false);
  const wheelGestureTimerRef = useRef<number | null>(null);

  const activeIndex =
    (trackIndex - LOOP_START_INDEX + productSlides.length) %
    productSlides.length;

  const resetLoop = useCallback((resetIndex: number) => {
    setTransitionsEnabled(false);
    setTrackIndex(resetIndex);

    window.requestAnimationFrame(() => {
      window.requestAnimationFrame(() => {
        setTransitionsEnabled(true);
        isSlidingRef.current = false;
      });
    });
  }, []);

  const moveSlides = useCallback((offset: -1 | 1) => {
    if (isSlidingRef.current) return;

    isSlidingRef.current = true;
    setTrackIndex((current) => current + offset);
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
    const root = rootRef.current;
    if (!root) return;

    const finishWheelGesture = () => {
      wheelDeltaRef.current = 0;
      wheelGestureLockedRef.current = false;
      wheelGestureTimerRef.current = null;
    };

    const scheduleWheelGestureEnd = () => {
      if (wheelGestureTimerRef.current !== null) {
        window.clearTimeout(wheelGestureTimerRef.current);
      }

      wheelGestureTimerRef.current = window.setTimeout(
        finishWheelGesture,
        WHEEL_GESTURE_IDLE_MS,
      );
    };

    const handleWheel = (event: WheelEvent) => {
      if (event.ctrlKey || event.deltaY === 0) return;

      event.preventDefault();
      scheduleWheelGestureEnd();

      if (wheelGestureLockedRef.current) return;

      const delta = getWheelDeltaInPixels(event, root.clientHeight);
      const accumulatedDelta = wheelDeltaRef.current;

      if (
        accumulatedDelta !== 0 &&
        Math.sign(accumulatedDelta) !== Math.sign(delta)
      ) {
        wheelDeltaRef.current = 0;
      }

      wheelDeltaRef.current += delta;

      if (Math.abs(wheelDeltaRef.current) < WHEEL_DELTA_THRESHOLD_PX) return;

      wheelGestureLockedRef.current = true;
      moveSlides(wheelDeltaRef.current > 0 ? 1 : -1);
    };

    root.addEventListener("wheel", handleWheel, { passive: false });

    return () => {
      root.removeEventListener("wheel", handleWheel);
      if (wheelGestureTimerRef.current !== null) {
        window.clearTimeout(wheelGestureTimerRef.current);
      }
    };
  }, [moveSlides]);

  useEffect(() => {
    const shouldAutoplay =
      autoplayEnabled &&
      !isHovered &&
      !hasFocus &&
      isInView &&
      isDocumentVisible;

    if (!shouldAutoplay) return;

    const timer = window.setTimeout(() => {
      moveSlides(1);
    }, AUTOPLAY_DELAY_MS);

    return () => window.clearTimeout(timer);
  }, [
    activeIndex,
    autoplayEnabled,
    hasFocus,
    isDocumentVisible,
    isHovered,
    isInView,
    moveSlides,
  ]);

  useEffect(() => {
    const resetIndex = getLoopResetIndex(trackIndex);

    const fallback = window.setTimeout(() => {
      if (resetIndex === null) {
        isSlidingRef.current = false;
        return;
      }

      resetLoop(resetIndex);
    }, SLIDE_TRANSITION_MS + 80);
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
      event.target !== event.currentTarget ||
      event.propertyName !== "transform"
    )
      return;

    const resetIndex = getLoopResetIndex(trackIndex);
    if (resetIndex === null) {
      isSlidingRef.current = false;
      return;
    }

    resetLoop(resetIndex);
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
