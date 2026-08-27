const showcaseImages = [
  {
    src: "/assets/portal-shots/readme-practice-flow.png",
    width: 1051,
    height: 1232,
    alt: "SpeakUp 开练流程：Agent 理解目标、创建练习并陪用户进行语音实战",
    className: "hero-product-showcase__panel--practice",
    loading: "eager",
    fetchPriority: "high",
  },
  {
    src: "/assets/portal-shots/readme-review-flow.png",
    width: 1065,
    height: 1231,
    alt: "SpeakUp 复盘流程：多维评分、练习报告与下一步训练建议",
    className: "hero-product-showcase__panel--review",
    loading: "lazy",
    fetchPriority: "auto",
  },
] as const;

export default function HeroProductShowcase() {
  return (
    <section
      className="hero-product-showcase"
      aria-label="SpeakUp 从开练到复盘的产品流程"
    >
      {showcaseImages.map((image) => (
        <figure
          className={`hero-product-showcase__panel ${image.className}`}
          key={image.src}
        >
          {/* Static assets are served directly because Vinext does not provide Next.js image optimization in development. */}
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img
            src={image.src}
            width={image.width}
            height={image.height}
            alt={image.alt}
            loading={image.loading}
            fetchPriority={image.fetchPriority}
            decoding="async"
            draggable="false"
          />
        </figure>
      ))}
    </section>
  );
}
