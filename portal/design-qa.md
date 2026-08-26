# Portal design QA

- Reference: ByteDance Seed's restrained editorial layout, adapted rather than
  copied: quiet full-width navigation, neutral hierarchy, one primary action,
  generous spacing, and product media on the right.
- Brand: replaced only the 3D mascot with the approved flat SpeakUp mark and
  retained the existing handwritten SpeakUp wordmark. Two rising opening quotes
  use violet `#7157E8` and coral `#FF654F`; there is no gradient, shadow, glow,
  character, or decorative UI restyle. The same SVG remains legible at favicon
  and navigation sizes.
- Product continuity: retained the real interview practice screenshot, product
  positioning, practice method, and long-term memory explanation.
- Public homepage: presents one Android APK action at the top. It does not
  expose signing, SHA-256, ABI, FAQ, release promises, or a second download
  route.
- Desktop: checked at 1440 × 900. The hero fits one viewport, the complete
  phone remains visible, navigation anchors reach visible headings, and no
  horizontal overflow occurs.
- Tablet: checked at 768 × 1024. The hero remains two-column, the long-term
  memory section becomes one-column, and no horizontal overflow occurs.
- Mobile: checked at 390 × 844 and the 640 px breakpoint. Navigation collapses,
  the CTA becomes full-width, the phone follows the release action, and no
  clipping or horizontal overflow occurs.
- Release states: preparing and unavailable states keep the action disabled;
  ready metadata produces exactly one versioned APK link with the matching
  download filename.
- Android detail page: remains a separate technical verification surface at
  `/download/android`; homepage selectors are scoped under `.release-home`
  and no longer alter that page's navigation, buttons, typography, or theme.
- Interaction: the ready-state action was verified to use the exact versioned
  APK path and trigger a native download. Focus-visible styles remain available
  for the brand, navigation links, and action.
- Runtime: no application console errors were observed during the desktop and
  mobile checks.
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
