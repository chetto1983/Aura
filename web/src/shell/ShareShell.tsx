import { Share2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';

// 37F plan 14 — the share affordance 37B reserved (`ArtifactsShell.tsx:20-22` used to read "the
// adjacent share-arrow is 37F, not built" — retired in this same commit). `ShareToggle` mirrors
// `ArtifactsToggle` (ArtifactsShell.tsx:23-45) with three deliberate deviations:
//
//   1. `aria-haspopup="dialog"`, NOT `aria-pressed`. ArtifactsToggle toggles a PANEL, so
//      aria-pressed is correct there. ShareToggle opens a MODAL — aria-pressed on a dialog-opener
//      would be an a11y bug (a screen-reader user told this is a pressed toggle when it is not).
//   2. Icon: lucide `Share2` (the arrow-node glyph — the "share arrow" D-05 names), not the
//      analog's `FileText`. `Link` (open-webui's choice) reads as "copy URL", not "share".
//   3. `data-shared` (tri-state: 'false' | 'internal' | 'public'), styled with the same
//      `data-[…=true-ish]:` idiom the analog already uses for `data-active`. A live public link
//      gets the DISTINCT `text-warning` treatment rather than sharing the ordinary accent — it is
//      the state a user most needs to notice (T-37F-65). Tri-state (rather than a boolean
//      `data-shared` plus a second boolean `data-public`) keeps the two visual states mutually
//      exclusive by construction: two independent data-attribute selectors targeting the same
//      `text-*` property would leave their relative precedence to Tailwind's generated
//      stylesheet order, which is not something this component should depend on.
//
// PLACEMENT NOTE (see 37F-14-SUMMARY.md "Deviations" for the full account): the locked target —
// "the floating overlay cluster at AppShell.tsx:514-517" (a `pointer-events-none absolute` div
// wrapping VoiceModeToggle + ArtifactsToggle) — was refactored away by commit 3f687e456 ("fix(web):
// contain chat controls and message actions", 2026-07-15), which landed AFTER 37F's research/
// patterns were mapped. That cluster is now `ChatWorkspaceControls.tsx`, a normal-flow row (no
// longer `pointer-events-none`). `ShareToggle` mounts there instead, in the same locked order
// `[VoiceModeToggle] [ShareToggle] [ArtifactsToggle]`. `pointer-events-auto` is kept regardless —
// harmless on a non-pointer-events-none ancestor, and it keeps this control correct if the
// cluster is ever floated again.
export interface ShareToggleProps {
  readonly hasInternalShare: boolean;
  readonly hasPublicShare: boolean;
  readonly onOpen: () => void;
}

export function ShareToggle({ hasInternalShare, hasPublicShare, onOpen }: ShareToggleProps) {
  const { t } = useTranslation();
  const shared = hasPublicShare ? 'public' : hasInternalShare ? 'internal' : 'false';
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      aria-label={t('share.toggle.label')}
      aria-haspopup="dialog"
      data-shared={shared}
      onClick={onOpen}
      className="pointer-events-auto rounded-full bg-surface/70 text-text-muted backdrop-blur hover:bg-surface-2 hover:text-text data-[shared=internal]:bg-surface-2 data-[shared=internal]:text-accent-text data-[shared=public]:text-warning"
    >
      <Share2 aria-hidden="true" focusable="false" />
    </Button>
  );
}
