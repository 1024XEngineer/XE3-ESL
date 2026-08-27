# Portal design QA

- Reference: ByteDance Seed's restrained editorial layout, adapted rather than
  copied: quiet full-width navigation, neutral hierarchy, one primary action,
  generous spacing, and product media on the right.
- Brand: replaced only the 3D mascot with the approved flat SpeakUp mark and
  retained the existing handwritten SpeakUp wordmark. Two rising opening quotes
  use violet `#7157E8` and coral `#FF654F`; there is no gradient, shadow, glow,
  character, or decorative UI restyle. The same SVG remains legible at favicon
  and navigation sizes.
- Product continuity: the hero reuses the exact two product-flow images from
  the repository README: Agent goal understanding and voice practice on the
  left, multidimensional review and next-step guidance on the right. The
  handwritten `SpeakUp` lettering inside those product images remains visible.
- Public homepage: the ready state presents one neutral “下载客户端” action and
  a secondary Android compatibility line. The APK file type and version leave
  the visible hero hierarchy while the validated versioned APK href and
  download filename remain unchanged. Signing, SHA-256, ABI, FAQ, and a second
  download route remain absent.
- Hero behavior: the former autoplay carousel, eight loop instances, wheel
  interception, edge fades, low-opacity adjacent devices, captions, progress
  indicators, and pause control are removed. The two README images are static,
  fully opaque, and contain no additional homepage explanation rail.
- Desktop: checked at 1440 × 900 in both preparing and mocked ready states. The
  heading remains two semantic lines without splitting “英文沟通”, both README
  images fit in the first viewport, and no horizontal overflow occurs. The ready
  action computes to solid `rgb(17, 17, 17)` rather than a gradient or violet.
- Tablet: checked at 768 × 1024. Copy and product media become a vertical hero,
  the two README images remain side by side within their own row, and no
  horizontal overflow occurs.
- Mobile: checked at 390 × 844 and 320 × 800. Navigation collapses, the CTA is
  full-width, the README images enter a single-column document flow, “英文沟通”
  stays intact, and no horizontal overflow occurs.
- Release states: preparing and unavailable states keep the action disabled;
  ready metadata produces exactly one versioned APK link with the matching
  download filename.
- Android detail page: remains a separate technical verification surface at
  `/download/android`; homepage selectors are scoped under `.release-home`
  and no longer alter that page's navigation, buttons, typography, or theme.
- Interaction: the ready-state action was verified to use the exact versioned
  APK path and native download filename. The homepage update-log link navigates
  to `/changelog`, and focus-visible styles remain available for the brand,
  navigation links, and action.
- Runtime: no framework overlay, JavaScript exception, warning, or error was
  observed in the mocked ready-state desktop check. The ordinary local server
  returns the expected `404` for absent release metadata and renders the honest
  preparing state.
- Changelog: checked `/changelog` at 1280 × 720, 390 × 844, and 320 × 800.
  The page uses the homepage's restrained black-and-white editorial language,
  one flat release list, clear version/date/category hierarchy, and no cards,
  gradients, glow, or horizontal overflow.
- Changelog release state: the browser rendered the active v0.1.7 first and
  the previously published v0.1.4 beneath it only after the production pointer,
  ordered history index, and both versioned Chinese notes matched. The compact
  page heading puts the first release at 213 px on desktop and 162 px on mobile.
- Changelog discovery: both the homepage navigation and the Android detail
  page's “查看正式版本更新记录” link navigated to `/changelog`. The mobile
  navigation collapses at narrow widths; the footer becomes one-column at
  320 px, keeps the entry visible, and provides a 44 px-high touch target.
- Changelog runtime: no application console warnings, errors, or Vinext error
  overlays were observed during the desktop and mobile checks.
- Automated verification: lint, unit tests, rendered HTML checks, production
  build, and `git diff --check` must pass on the final clean worktree.

Final result: passed.
