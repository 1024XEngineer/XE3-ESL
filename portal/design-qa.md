# Portal design QA

- Reference: Kimi official download page, used for the compact navigation, clear release hierarchy, direct primary action, and restrained information density.
- Brand continuity: retained SpeakUp's existing logo, cream/lavender palette, serif display type, black outlines, and real product screenshot.
- Desktop and tablet: checked at 1440 × 900 and 768 × 1024; hero, product image, CTA, Android status, and download page render without overlap or clipping.
- Mobile: checked at 390 × 844; navigation collapses, content becomes single-column, CTA remains full-width, and download metadata stays readable.
- Android publication: rechecked the honest preparing state at 1440 × 900 and
  390 × 844 after adding the metadata-driven download action; no overlap,
  clipping, gradient, extra nested card, fake QR code, or `latest.apk` link was
  introduced.
- Interaction: Android CTA route and native FAQ expansion verified.
- Release states: automated contract tests cover preparing, unavailable, and
  ready results; only ready metadata carries the validated versioned APK path.
- Runtime: no application console errors observed.
- Automated verification: lint, tests, and production build passed.

final result: passed
