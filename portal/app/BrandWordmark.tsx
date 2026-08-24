export default function BrandWordmark() {
  return (
    // Static assets are served directly because Vinext does not provide Next.js image optimization in development.
    // eslint-disable-next-line @next/next/no-img-element
    <img
      className="brand-wordmark"
      src="/assets/brand/speak-up-wordmark-black.png"
      alt=""
      aria-hidden="true"
      width="1889"
      height="632"
    />
  );
}
