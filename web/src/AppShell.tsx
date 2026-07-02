import { Suspense, lazy, useCallback, useEffect, useRef, useState } from 'react';
import { Menu, Plus } from 'lucide-react';
import { useDefaultLayout } from 'react-resizable-panels';
import { useTranslation } from 'react-i18next';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { ThreadApprovalCards } from './approvals/ThreadApprovalCards';
import { RuntimeFooter } from './chat/RuntimeFooter';
import { ConversationSidebar } from './conversations/ConversationSidebar';
import { SearchPanel } from './conversations/SearchPanel';
import { RuntimeHealthPanel } from './health/RuntimeHealthPanel';
import { BottomDock } from './shell/BottomDock';
import { Drawer } from './shell/Drawer';
import { MobileAppSidebar } from './shell/MobileAppSidebar';
import { ShellHeader } from './shell/ShellHeader';
import type { SurfaceIntent } from './shell/modes';
import { useEdgeSwipe } from './shell/useEdgeSwipe';
import { useSurfaceIntent } from './shell/useSurfaceIntent';
import { useSurfaceRestore } from './shell/useSurfaceRestore';
import { fetchOnboardingStatus } from './onboarding/onboardingApi';
import type { TurnUsage } from './chat/sseAdapter';
import type { ComposerDraftPrompt } from './chat/Composer';
import { useCreateConversation } from './conversations/useConversations';
import type { DocumentItem } from './documents/documentApi';
import { readCookie, readJSON, stringField, valueOrFallback } from './auth/authConfig';
import { Button } from '@/components/ui/button';
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from '@/components/ui/resizable';

const ExternalStoreChat = lazy(() =>
  import('./chat/ExternalStoreChat').then((mod) => ({ default: mod.ExternalStoreChat })),
);

// The Frame-06 Graph Explorer is a SEPARATE lazy chunk (Pitfall 7): the Sigma WebGL stack it
// pulls in must never land in the main bundle — it loads only when surface==='graph'.
const GraphExplorer = lazy(() => import('./graph/GraphExplorer'));

// The Phase-28 Governance workspace (MCP/Skills/Scheduler boards) is its own lazy chunk too
// (D-01) — it loads only when surface==='governance'.
const GovernanceWorkspace = lazy(() => import('./governance/GovernanceWorkspace'));
const DocumentsWorkspace = lazy(() => import('./documents/DocumentsWorkspace'));
const SettingsWorkspace = lazy(() => import('./settings/SettingsWorkspace'));

// The Phase-28 onboarding+provisioning wizard is a SEPARATE full-screen overlay (D-04 — NOT a
// MODES entry / surface tab). Its own lazy chunk: it loads only when the operator opens the
// "Create identity" overlay, never landing in the main bundle.
const OnboardingWizard = lazy(() => import('./onboarding/OnboardingWizard'));
const ProfileOnboardingWizard = lazy(() => import('./onboarding/ProfileOnboardingWizard'));

const CHAT_SHELL_LAYOUT_ID = 'aura-chat-shell-v3';
const CHAT_SHELL_PANEL_IDS = ['chat-navigation', 'chat-workspace'];
const CHAT_WORKSPACE_MIN_WIDTH = '380px';

interface LogoutTarget {
  path: string;
  headers?: Record<string, string>;
  body?: string;
}

const defaultAuthulaBasePath = '/auth';
const defaultCSRFCookieName = '__Host-authula_csrf_token';
const defaultCSRFHeaderName = 'X-AUTHULA-CSRF-TOKEN';

async function loadLogoutTarget(): Promise<LogoutTarget | null> {
  try {
    const res = await fetch('/api/auth/config', {
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
    });
    if (!res.ok) return null;
    const raw = await readJSON(res);
    if (stringField(raw, 'provider') !== 'authula') return null;

    const authBasePath = valueOrFallback(
      stringField(raw, 'auth_base_path'),
      defaultAuthulaBasePath,
    );
    const csrfCookieName = valueOrFallback(
      stringField(raw, 'csrf_cookie_name'),
      defaultCSRFCookieName,
    );
    const csrfHeaderName = valueOrFallback(
      stringField(raw, 'csrf_header_name'),
      defaultCSRFHeaderName,
    );
    const csrfToken = valueOrFallback(
      stringField(raw, 'csrf_token'),
      res.headers.get(csrfHeaderName) ?? readCookie(csrfCookieName),
    );
    if (csrfToken === '') return null;

    return {
      path: `${authBasePath}/sign-out`,
      headers: {
        'Content-Type': 'application/json',
        [csrfHeaderName]: csrfToken,
      },
      body: '{}',
    };
  } catch {
    return null;
  }
}

export function AppShell() {
  const { t } = useTranslation();
  const { id: routeId } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const { surface, setSurface } = useSurfaceIntent();
  const showConversationNavigation = surface === 'chat';
  // The Graph Explorer and the Governance boards are focused workspaces with their own panes
  // (canvas/inspector, master/detail), so the shell's right-hand runtime rail is redundant
  // there — drop it on lg so the workspace gets the width back instead of being squeezed into a
  // narrow middle column. The chat approval cards are also chat-only.
  const isFocusedWorkspace =
    surface === 'graph' ||
    surface === 'governance' ||
    surface === 'documents' ||
    surface === 'settings';
  const createConversation = useCreateConversation();
  const [selectedId, setSelectedId] = useState(routeId ?? '');
  const [lastRouteId, setLastRouteId] = useState(routeId ?? '');
  const [usage, setUsage] = useState<TurnUsage | undefined>(undefined);
  const [approvalsOpen, setApprovalsOpen] = useState(false);
  // §3.1c: the mobile/tablet overlay surfaces (nav drawer + runtime sheet) are driven by
  // the one-heavy-surface intent reducer, NOT two independent booleans. At `lg` the regions
  // are permanent columns, so these only gate the portaled drawers.
  const surfaces = useSurfaceRestore();
  const closeNav = surfaces.closeNav;
  const [resumeNonce, setResumeNonce] = useState(0);
  const [documentDraftPrompt, setDocumentDraftPrompt] = useState<ComposerDraftPrompt | undefined>(
    undefined,
  );
  const [logoutPending, setLogoutPending] = useState(false);
  // The onboarding+provisioning wizard is a full-screen overlay (D-04), opened by an explicit
  // trigger and covering the shell while active — NOT a surface/mode.
  const [onboardingOpen, setOnboardingOpen] = useState(false);
  const [profileOnboardingOpen, setProfileOnboardingOpen] = useState(false);
  const autoOpenedOnboarding = useRef(false);

  if ((routeId ?? '') !== lastRouteId) {
    setLastRouteId(routeId ?? '');
    setSelectedId(routeId ?? '');
    setUsage(undefined);
  }

  const activeThreadId = selectedId;

  useEffect(() => {
    if (searchParams.get('onboarding') !== '1' || autoOpenedOnboarding.current) return;
    autoOpenedOnboarding.current = true;
    setProfileOnboardingOpen(true);
    closeNav();
    void navigate('/', { replace: true });
  }, [closeNav, navigate, searchParams]);

  useEffect(() => {
    let cancelled = false;
    void fetchOnboardingStatus()
      .then((status) => {
        if (cancelled || autoOpenedOnboarding.current || !status.required) return;
        autoOpenedOnboarding.current = true;
        setProfileOnboardingOpen(true);
        closeNav();
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [closeNav]);

  function selectThread(id: string) {
    setSelectedId(id);
    setUsage(undefined);
    surfaces.closeNav();
  }

  function selectThreadFromMobileNav(id: string) {
    setSurface('chat');
    setSelectedId(id);
    setUsage(undefined);
    surfaces.closeNav();
    void navigate(`/c/${encodeURIComponent(id)}`);
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

  const askDocument = useCallback(
    (document: DocumentItem) => {
      const searchDocumentID =
        typeof document.metadata.search_document_id === 'string'
          ? document.metadata.search_document_id
          : '';
      const text =
        searchDocumentID === ''
          ? t('documents.ask.prompt', { title: document.title })
          : t('documents.ask.promptWithId', {
              title: document.title,
              documentId: searchDocumentID,
            });
      setDocumentDraftPrompt((current) => ({
        text,
        nonce: (current?.nonce ?? 0) + 1,
      }));
      setSurface('chat');
      closeNav();
    },
    [closeNav, setSurface, t],
  );

  const logout = useCallback(async () => {
    if (logoutPending) return;
    setLogoutPending(true);
    try {
      const target = await loadLogoutTarget();
      if (target === null) {
        setLogoutPending(false);
        return;
      }
      const init: RequestInit = {
        method: 'POST',
        credentials: 'same-origin',
      };
      if (target.headers !== undefined) init.headers = target.headers;
      if (target.body !== undefined) init.body = target.body;
      await fetch(target.path, init);
      void navigate('/login', { replace: true });
      return;
    } catch {
      // Keep the operator in the cockpit if the server could not clear the session.
    }
    setLogoutPending(false);
  }, [logoutPending, navigate]);

  const edgeSwipe = useEdgeSwipe({
    onLeftEdge: surfaces.openNav,
    onRightEdge: surfaces.openOverlay,
    onLeftClose: surfaces.closeNav,
    // Swipe-dismissing the runtime overlay is the "get this out of my way" intent — it must
    // NOT restore the remembered nav (§3.1c), so the swipe path passes 'swipe'.
    onRightClose: () => {
      surfaces.closeOverlay('swipe');
    },
    isLeftOpen: surfaces.navOpen,
    isRightOpen: surfaces.overlayOpen,
  });

  const chatShellLayout = useDefaultLayout({
    id: CHAT_SHELL_LAYOUT_ID,
    panelIds: CHAT_SHELL_PANEL_IDS,
    onlySaveAfterUserInteractions: true,
  });

  const navigation = (
    <div className="flex h-full min-h-0 flex-col gap-2 bg-surface px-3 py-3">
      <SearchPanel
        onOpen={(id) => {
          selectThread(id);
        }}
      />
      <Button
        type="button"
        variant="outline"
        onClick={() => {
          setOnboardingOpen(true);
          surfaces.closeNav();
        }}
        className="w-full bg-surface-2 hover:border-border-strong"
      >
        <Plus data-icon="inline-start" aria-hidden="true" focusable="false" />
        {t('onboarding.open')}
      </Button>
      <div className="min-h-0 flex-1 overflow-hidden">
        <ConversationSidebar activeId={activeThreadId} onSelect={selectThread} />
      </div>
    </div>
  );

  const runtime = <RuntimeHealthPanel />;

  const documentsMobileMenu = (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      aria-label={t('shell.openNavigation')}
      onClick={surfaces.openNav}
      className="rounded-full text-text hover:bg-surface-2"
    >
      <Menu aria-hidden="true" />
    </Button>
  );

  function selectMobileMode(next: SurfaceIntent) {
    setSurface(next);
    surfaces.closeNav();
  }

  const mobileNavigation = (
    <MobileAppSidebar
      activeMode={surface}
      onModeSelect={selectMobileMode}
      onCreateIdentity={() => {
        setOnboardingOpen(true);
        surfaces.closeNav();
      }}
    >
      <div className="flex h-full min-h-0 flex-col gap-2">
        <SearchPanel
          onOpen={(id) => {
            setSurface('chat');
            selectThread(id);
          }}
        />
        <div className="min-h-0 flex-1 overflow-hidden">
          <ConversationSidebar activeId={activeThreadId} onSelect={selectThreadFromMobileNav} />
        </div>
      </div>
    </MobileAppSidebar>
  );

  const workspace = (
    <section aria-label={t('shell.chatRegion')} className="flex h-full min-h-0 flex-col bg-bg">
      <div className="min-h-0 flex-1">
        <Suspense
          fallback={
            <div role="status" className="grid h-full place-items-center text-sm text-text-muted">
              {surface === 'graph'
                ? t('graph.loading')
                : surface === 'governance'
                  ? t('governance.loading')
                  : surface === 'documents'
                    ? t('documents.loading')
                    : surface === 'settings'
                      ? t('settings.loading')
                      : t('chat.loading')}
            </div>
          }
        >
          {surface === 'graph' ? (
            <GraphExplorer threadId={activeThreadId} />
          ) : surface === 'governance' ? (
            <GovernanceWorkspace />
          ) : surface === 'documents' ? (
            <DocumentsWorkspace mobileMenu={documentsMobileMenu} onAskDocument={askDocument} />
          ) : surface === 'settings' ? (
            <SettingsWorkspace />
          ) : (
            <ExternalStoreChat
              threadId={activeThreadId}
              onEnsureThread={ensureThread}
              onUsage={setUsage}
              resumeNonce={resumeNonce}
              draftPrompt={documentDraftPrompt}
            />
          )}
        </Suspense>
      </div>
      {!isFocusedWorkspace ? (
        <ThreadApprovalCards conversationId={activeThreadId} onResolved={redriveRun} />
      ) : null}
    </section>
  );

  return (
    <div
      className="aura-shell grid h-[100svh] min-h-0 overflow-hidden bg-bg text-text [grid-template-rows:auto_minmax(0,1fr)_auto]"
      data-surface={surface}
      {...edgeSwipe}
    >
      <ShellHeader
        activeMode={surface}
        approvalsOpen={approvalsOpen}
        onModeSelect={(next) => {
          setSurface(next);
          if (next !== 'chat') surfaces.closeNav();
        }}
        onNavigationOpen={surfaces.openNav}
        onRuntimeOpen={surfaces.openOverlay}
        navigationAvailable
        onApprovalsToggle={() => {
          setApprovalsOpen((v) => !v);
        }}
        onApprovalOpen={(id) => {
          selectThread(id);
          setApprovalsOpen(false);
        }}
        logoutPending={logoutPending}
        onLogout={() => {
          void logout();
        }}
      />

      {/* Chat keeps the old 15rem/19rem rail defaults, but operators can resize the
          conversation rail while the middle lane still honors --chat-lane-min. */}
      {showConversationNavigation ? (
        <main className="shell-main grid min-h-0 grid-cols-[minmax(0,1fr)_19rem]">
          <ResizablePanelGroup
            {...chatShellLayout}
            id={CHAT_SHELL_LAYOUT_ID}
            orientation="horizontal"
            resizeTargetMinimumSize={{ coarse: 32, fine: 12 }}
            className="shell-main-resizable min-w-0"
          >
            <ResizablePanel
              id="chat-navigation"
              defaultSize="15rem"
              minSize="13rem"
              maxSize="28rem"
              groupResizeBehavior="preserve-pixel-size"
              className="h-full min-h-0"
            >
              <aside
                aria-label={t('shell.navigation')}
                className="shell-side-nav flex h-full min-h-0 flex-col border-r border-border bg-surface"
              >
                {navigation}
              </aside>
            </ResizablePanel>
            <ResizableHandle
              id="chat-navigation-resizer"
              aria-label={t('shell.resizeNavigation')}
              className="shell-nav-resize-handle"
              withHandle
            />
            <ResizablePanel
              id="chat-workspace"
              minSize={CHAT_WORKSPACE_MIN_WIDTH}
              className="h-full min-h-0"
            >
              {workspace}
            </ResizablePanel>
          </ResizablePanelGroup>
          <aside
            id="runtime-rail"
            aria-label={t('shell.displayWorkspace')}
            className="shell-runtime-rail h-full min-h-0 overflow-y-auto border-l border-border bg-surface"
          >
            {runtime}
          </aside>
        </main>
      ) : (
        <main className="shell-main grid min-h-0 grid-cols-1 lg:grid-cols-[minmax(0,1fr)]">
          {workspace}
        </main>
      )}

      <BottomDock activeMode={surface} onModeSelect={setSurface}>
        <RuntimeFooter usage={usage} conversationId={activeThreadId} />
      </BottomDock>

      <Drawer open={surfaces.navOpen} side="left" title="Aura" onClose={surfaces.closeNav}>
        {mobileNavigation}
      </Drawer>
      <Drawer
        open={surfaces.overlayOpen}
        side="right"
        title={t('shell.displayWorkspace')}
        onClose={(intent) => {
          // Drawer-originated closes (button / Esc / backdrop) are always 'explicit' → restore
          // the remembered nav; the 'swipe' path comes from the edge-swipe handler above.
          surfaces.closeOverlay(intent ?? 'explicit');
        }}
      >
        {runtime}
      </Drawer>

      {/* Full-screen onboarding wizard overlay (D-04) — a lazy chunk covering the shell when
          active. It is NOT a surface/mode; an explicit trigger opens it and its own close button
          (or completion) dismisses it. */}
      {onboardingOpen ? (
        <Suspense
          fallback={
            <div
              role="status"
              className="fixed inset-0 z-50 grid place-items-center bg-bg text-sm text-text-muted"
            >
              {t('onboarding.starting')}
            </div>
          }
        >
          <OnboardingWizard
            onClose={() => {
              setOnboardingOpen(false);
            }}
          />
        </Suspense>
      ) : null}
      {profileOnboardingOpen ? (
        <Suspense
          fallback={
            <div
              role="status"
              className="fixed inset-0 z-50 grid place-items-center bg-bg text-sm text-text-muted"
            >
              {t('onboarding.starting')}
            </div>
          }
        >
          <ProfileOnboardingWizard
            onClose={() => {
              setProfileOnboardingOpen(false);
            }}
          />
        </Suspense>
      ) : null}
    </div>
  );
}
