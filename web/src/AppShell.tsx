import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate, useParams } from 'react-router-dom';
import { ThreadApprovalCards } from './approvals/ThreadApprovalCards';
import { ExternalStoreChat } from './chat/ExternalStoreChat';
import { RuntimeFooter } from './chat/RuntimeFooter';
import { ConversationSidebar } from './conversations/ConversationSidebar';
import { SearchPanel } from './conversations/SearchPanel';
import { RuntimeHealthPanel } from './health/RuntimeHealthPanel';
import { BottomDock } from './shell/BottomDock';
import { Drawer } from './shell/Drawer';
import { ShellHeader } from './shell/ShellHeader';
import { useEdgeSwipe } from './shell/useEdgeSwipe';
import { useSurfaceIntent } from './shell/useSurfaceIntent';
import type { TurnUsage } from './chat/sseAdapter';
import { useCreateConversation } from './conversations/useConversations';

export function AppShell() {
  const { t } = useTranslation();
  const { id: routeId } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { surface, setSurface } = useSurfaceIntent();
  const createConversation = useCreateConversation();
  const [selectedId, setSelectedId] = useState(routeId ?? '');
  const [lastRouteId, setLastRouteId] = useState(routeId ?? '');
  const [usage, setUsage] = useState<TurnUsage | undefined>(undefined);
  const [approvalsOpen, setApprovalsOpen] = useState(false);
  const [navigationOpen, setNavigationOpen] = useState(false);
  const [runtimeOpen, setRuntimeOpen] = useState(false);
  const [resumeNonce, setResumeNonce] = useState(0);

  if ((routeId ?? '') !== lastRouteId) {
    setLastRouteId(routeId ?? '');
    setSelectedId(routeId ?? '');
    setUsage(undefined);
  }

  const activeThreadId = selectedId;

  function selectThread(id: string) {
    setSelectedId(id);
    setUsage(undefined);
    setNavigationOpen(false);
  }

  function redriveRun(conversationId: string) {
    if (conversationId.length === 0) return;
    setResumeNonce((n) => n + 1);
  }

  const ensureThread = useCallback(
    async (initialPrompt: string) => {
      if (activeThreadId.length > 0) return activeThreadId;
      const conv = await createConversation.mutateAsync(initialPrompt);
      setSelectedId(conv.ID);
      setUsage(undefined);
      void navigate(`/c/${encodeURIComponent(conv.ID)}`);
      return conv.ID;
    },
    [activeThreadId, createConversation, navigate],
  );

  const edgeSwipe = useEdgeSwipe({
    onLeftEdge: () => {
      setNavigationOpen(true);
    },
    onRightEdge: () => {
      setRuntimeOpen(true);
    },
  });

  const navigation = (
    <div className="flex h-full min-h-0 flex-col gap-2 bg-surface px-3 py-3">
      <SearchPanel
        onOpen={(id) => {
          selectThread(id);
        }}
      />
      <div className="min-h-0 flex-1 overflow-hidden">
        <ConversationSidebar activeId={activeThreadId} onSelect={selectThread} />
      </div>
    </div>
  );

  const runtime = <RuntimeHealthPanel />;

  return (
    <div
      className="grid h-[100svh] min-h-0 overflow-hidden bg-bg text-text [grid-template-rows:auto_minmax(0,1fr)_auto]"
      {...edgeSwipe}
    >
      <ShellHeader
        activeMode={surface}
        approvalsOpen={approvalsOpen}
        onModeSelect={setSurface}
        onNavigationOpen={() => {
          setNavigationOpen(true);
        }}
        onRuntimeOpen={() => {
          setRuntimeOpen(true);
        }}
        onApprovalsToggle={() => {
          setApprovalsOpen((v) => !v);
        }}
        onApprovalOpen={(id) => {
          selectThread(id);
          setApprovalsOpen(false);
        }}
      />

      <main className="grid min-h-0 grid-cols-1 lg:grid-cols-[15rem_minmax(0,1fr)_19rem]">
        <aside
          aria-label={t('shell.navigation')}
          className="hidden min-h-0 border-r border-border bg-surface lg:flex lg:flex-col"
        >
          {navigation}
        </aside>

        <section
          aria-label={t('shell.chatRegion')}
          className="flex min-h-[min(45svh,100%)] flex-col bg-bg"
        >
          <div className="min-h-0 flex-1">
            <ExternalStoreChat
              threadId={activeThreadId}
              onEnsureThread={ensureThread}
              onUsage={setUsage}
              resumeNonce={resumeNonce}
            />
          </div>
          <ThreadApprovalCards conversationId={activeThreadId} onResolved={redriveRun} />
        </section>

        <aside
          aria-label={t('shell.displayWorkspace')}
          className="hidden min-h-0 overflow-y-auto border-l border-border bg-surface lg:block"
        >
          {runtime}
        </aside>
      </main>

      <BottomDock activeMode={surface} onModeSelect={setSurface}>
        <RuntimeFooter usage={usage} conversationId={activeThreadId} />
      </BottomDock>

      <Drawer
        open={navigationOpen}
        side="left"
        title={t('shell.navigation')}
        onClose={() => {
          setNavigationOpen(false);
        }}
      >
        {navigation}
      </Drawer>
      <Drawer
        open={runtimeOpen}
        side="right"
        title={t('shell.displayWorkspace')}
        onClose={() => {
          setRuntimeOpen(false);
        }}
      >
        {runtime}
      </Drawer>
    </div>
  );
}
