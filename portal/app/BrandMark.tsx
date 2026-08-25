export default function BrandMark() {
  return (
    <span className="brand-mark" aria-hidden="true">
      {/* Static assets are served directly because Vinext does not provide Next.js image optimization in development. */}
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img src="/assets/brand/speakup-mark.svg" alt="" width="48" height="48" />
    </span>
  );
}
