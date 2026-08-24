export default function BrandMark() {
  return (
    <span className="brand-mark" aria-hidden="true">
      {/* Static assets are served directly because Vinext does not provide Next.js image optimization in development. */}
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img src="/assets/brand/speakup-mascot-blue.png" alt="" width="1254" height="1254" />
    </span>
  );
}
