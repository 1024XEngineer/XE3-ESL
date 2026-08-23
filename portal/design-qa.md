# Portal design QA

- Reference: ByteDance Seed's restrained editorial layout, adapted rather than
  copied: quiet full-width navigation, black-and-white hierarchy, one primary
  action, generous spacing, and product media on the right.
- Product continuity: retained the existing SpeakUp mascot, real interview
  practice screenshot, product positioning, practice method, and long-term
  memory explanation.
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
- Automated verification: lint, unit tests, rendered HTML checks, production
  build, and `git diff --check` must pass on the final clean worktree.

Final result: passed.
