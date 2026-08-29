import { lazy, Suspense } from 'react';
import { useTranslation } from 'react-i18next';
import { Drawer } from './Drawer';
import type { CloseIntent } from './useSurfaceRestore';
import { CHAT_WORKER_PANEL_ID } from './useWorkerPane';
import { ResizableHandle, ResizablePanel } from '@/components/ui/resizable';

const WorkerPane = lazy(() =>
  import('../chat/workers/WorkerPane').then((module) => ({ default: module.WorkerPane })),
);

interface WorkerSurfaceProps {
  readonly conversationId: string;
  readonly childId: string;
}

export function WorkerResizablePanel({
  conversationId,
  childId,
  onClose,
}: WorkerSurfaceProps & { readonly onClose: () => void }) {
  const { t } = useTranslation();
  return (
    <>
      <ResizableHandle
        id="chat-worker-resizer"
        aria-label={t('shell.resizeWorker')}
        className="shell-nav-resize-handle"
        withHandle
      />
      <ResizablePanel
        id={CHAT_WORKER_PANEL_ID}
        defaultSize="24rem"
        minSize="18rem"
        maxSize="40rem"
        groupResizeBehavior="preserve-pixel-size"
        className="h-full min-h-0"
      >
        <aside
          aria-label={t('swarm.pane.title')}
          className="flex h-full min-h-0 flex-col border-l border-border bg-surface"
        >
          <Suspense fallback={null}>
            <WorkerPane conversationId={conversationId} childId={childId} onClose={onClose} />
          </Suspense>
        </aside>
      </ResizablePanel>
    </>
  );
}

export function WorkerDrawer({
  open,
  conversationId,
  childId,
  onClose,
}: WorkerSurfaceProps & {
  readonly open: boolean;
  readonly onClose: (intent: CloseIntent) => void;
}) {
  const { t } = useTranslation();
  return (
    <Drawer
      open={open}
      side="right"
      title={t('swarm.pane.title')}
      onClose={(intent) => {
        onClose(intent ?? 'explicit');
      }}
    >
      <Suspense fallback={null}>
        <WorkerPane
          conversationId={conversationId}
          childId={childId}
          onClose={() => {
            onClose('explicit');
          }}
        />
      </Suspense>
    </Drawer>
  );
}
