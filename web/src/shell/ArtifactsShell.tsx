import { Suspense, lazy } from 'react';
import { FileText } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Drawer } from './Drawer';
import type { CloseIntent } from './useSurfaceRestore';
import { CHAT_ARTIFACTS_PANEL_ID } from './useArtifactsPanel';
import { Button } from '@/components/ui/button';
import { ResizableHandle, ResizablePanel } from '@/components/ui/resizable';

// 37B plan 07 — the Artefatti panel presentational pieces AppShell mounts: the header toggle, the
// desktop ResizablePanel, and the mobile right Drawer. The state/wiring seam is in
// ./useArtifactsPanel; ArtifactsPanel itself is built in plan 06.

// D-08: the panel (and its docx-preview/read-excel-file renderer chunks) is a lazy chunk — it never lands
// in the main bundle, only when the panel first mounts (toggle open or auto-open).
const ArtifactsPanel = lazy(() =>
  import('../chat/artifacts/ArtifactsPanel').then((mod) => ({ default: mod.ArtifactsPanel })),
);

// D-01: the header-style doc toggle (the reference's top-right icon). The adjacent share-arrow
// (37F plan 14, `ShareToggle` in `./ShareShell`) now sits beside it: ChatWorkspaceControls mounts
// [VoiceModeToggle, ShareToggle, ArtifactsToggle] in that order in the workspace's normal-flow
// controls row, so all three read as header controls without editing ShellHeader (the app-level
// header) or shifting the chat layout.
export function ArtifactsToggle({
  active,
  onToggle,
}: {
  readonly active: boolean;
  readonly onToggle: () => void;
}) {
  const { t } = useTranslation();
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      aria-label={t('artifacts.toggleAria')}
      aria-pressed={active}
      data-active={active}
      data-required-touch-target
      onClick={onToggle}
      className="min-h-[44px] min-w-[44px] rounded-full bg-surface/70 text-text-muted backdrop-blur hover:bg-surface-2 hover:text-text data-[active=true]:bg-surface-2 data-[active=true]:text-accent-text"
    >
      <FileText aria-hidden="true" focusable="false" />
    </Button>
  );
}

// The desktop surface: a third ResizablePanel + its handle (mirroring the nav rail), mounted AFTER
// chat-workspace so react-resizable-panels infers the [nav | workspace | artefatti] order from
// registration order (D-02 — no `order` prop, which v4 does not support).
export function ArtifactsResizablePanel({
  threadId,
  onClose,
}: {
  readonly threadId: string;
  readonly onClose: () => void;
}) {
  const { t } = useTranslation();
  return (
    <>
      <ResizableHandle
        id="chat-artifacts-resizer"
        aria-label={t('shell.resizeArtifacts')}
        className="shell-nav-resize-handle"
        withHandle
      />
      <ResizablePanel
        id={CHAT_ARTIFACTS_PANEL_ID}
        defaultSize="19rem"
        minSize="16rem"
        maxSize="32rem"
        groupResizeBehavior="preserve-pixel-size"
        className="h-full min-h-0"
      >
        <aside
          aria-label={t('artifacts.title')}
          className="flex h-full min-h-0 flex-col border-l border-border bg-surface"
        >
          <Suspense fallback={null}>
            <ArtifactsPanel threadId={threadId} onClose={onClose} />
          </Suspense>
        </aside>
      </ResizablePanel>
    </>
  );
}

// D-04: below `lg` the panel collapses into the shared right Drawer, routed through
// useSurfaceRestore's overlay slot so it obeys "one heavy overlay at a time" against the nav
// drawer. Identical portal/focus-trap/Esc UX to the left nav drawer.
export function ArtifactsDrawer({
  open,
  threadId,
  onClose,
}: {
  readonly open: boolean;
  readonly threadId: string;
  readonly onClose: (intent: CloseIntent) => void;
}) {
  const { t } = useTranslation();
  return (
    <Drawer
      open={open}
      side="right"
      title={t('artifacts.title')}
      onClose={(intent) => {
        onClose(intent ?? 'explicit');
      }}
    >
      <Suspense fallback={null}>
        <ArtifactsPanel
          threadId={threadId}
          onClose={() => {
            onClose('explicit');
          }}
        />
      </Suspense>
    </Drawer>
  );
}
