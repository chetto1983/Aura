import { Suspense, lazy, useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate, useParams } from 'react-router-dom';
import { ThreadApprovalCards } from './approvals/ThreadApprovalCards';
import { RuntimeFooter } from './chat/RuntimeFooter';
import { ConversationSidebar } from './conversations/ConversationSidebar';
import { SearchPanel } from './conversations/SearchPanel';
import { RuntimeHealthPanel } from './health/RuntimeHealthPanel';
import { BottomDock } from './shell/BottomDock';
import { Drawer } from './shell/Drawer';
import { ShellHeader } from './shell/ShellHeader';
import { useEdgeSwipe } from './shell/useEdgeSwipe';
import { useSurfaceIntent } from './shell/useSurfaceIntent';
import { useSurfaceRestore } from './shell/useSurfaceRestore';
import type { TurnUsage } from './chat/sseAdapter';
import { useCreateConversation } from './conversations/useConversations';
import { readCookie, readJSON, stringField, valueOrFallback } from './auth/authConfig';

const ExternalStoreChat = lazy(() =>
  import('./chat/ExternalStoreChat').then((mod) => ({ default: mod.ExternalStoreChat })),
);

interface LogoutTarget {
  path: string;
  headers?: Record<string, string>;
  body?: string;
}

const passphraseLogoutTarget: LogoutTarget = { path: '/logout' };
const defaultAuthulaBasePath = '/auth';
const defaultCSRFCookieName = '__Host-authula_csrf_token';
const defaultCSRFHeaderName = 'X-AUTHULA-CSRF-TOKEN';

async function loadLogoutTarget(): Promise<LogoutTarget> {
  try {
    const res = await fetch('/api/auth/config', {
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
    });
    if (!res.ok) return passphraseLogoutTarget;
    const raw = await readJSON(res);
    if (stringField(raw, 'provider') !== 'authula') return passphraseLogoutTarget;

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
    if (csrfToken === '') return passphraseLogoutTarget;

    return {
      path: `${authBasePath}/sign-out`,
      headers: {
        'Content-Type': 'application/json',
        [csrfHeaderName]: csrfToken,
      },
      body: '{}',
    };
  } catch {
    return passphraseLogoutTarget;
  }
}

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
  // §3.1c: the mobile/tablet overlay surfaces (nav drawer + runtime sheet) are driven by
  // the one-heavy-surface intent reducer, NOT two independent booleans. At `lg` the regions
  // are permanent columns, so these only gate the portaled drawers.
  const surfaces = useSurfaceRestore();
  const [resumeNonce, setResumeNonce] = useState(0);
  const [logoutPending, setLogoutPending] = useState(false);

  if ((routeId ?? '') !== lastRouteId) {
    setLastRouteId(routeId ?? '');
    setSelectedId(routeId ?? '');
    setUsage(undefined);
  }

  const activeThreadId = selectedId;

  function selectThread(id: string) {
    setSelectedId(id);
    setUsage(undefined);
    surfaces.closeNav();
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

  const logout = useCallback(async () => {
    if (logoutPending) return;
    setLogoutPending(true);
    try {
      const target = await loadLogoutTarget();
      const init: RequestInit = {
        method: 'POST',
        credentials: 'same-origin',
      };
      if (target.headers !== undefined) init.headers = target.headers;
      if (target.body !== undefined) init.body = target.body;
      const res = await fetch(target.path, init);
      if (res.ok) {
        void navigate('/login', { replace: true });
        return;
      }
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
        onNavigationOpen={surfaces.openNav}
        onRuntimeOpen={surfaces.openOverlay}
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

      {/* §1.1b content-derived 3-column flip. The rails are 15rem + 19rem = 34rem (544px);
          the elastic chat track has a HARD --chat-lane-min (380px) lower bound instead of the
          old `minmax(0,1fr)` 0-floor, so the lane can never render < 380px once 3-col is active.
          The flip stays at `lg` (1024px): 1024 − 544 rails = 480px chat ≥ 380px, with margin;
          a narrower flip (e.g. md=768 → 768 − 544 = 224px) would crush the lane below the floor,
          so it is correctly deferred to `lg`. Window-floor = rails (34rem) + --chat-lane-min
          (380px) ≈ 924px < 1024px, so the `lg` flip honours the floor by construction. */}
      <main className="grid min-h-0 grid-cols-1 lg:grid-cols-[15rem_minmax(var(--chat-lane-min),1fr)_19rem]">
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
            <Suspense
              fallback={
                <div
                  role="status"
                  className="grid h-full place-items-center text-sm text-text-muted"
                >
                  {t('chat.loading')}
                </div>
              }
            >
              <ExternalStoreChat
                threadId={activeThreadId}
                onEnsureThread={ensureThread}
                onUsage={setUsage}
                resumeNonce={resumeNonce}
              />
            </Suspense>
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
        open={surfaces.navOpen}
        side="left"
        title={t('shell.navigation')}
        onClose={surfaces.closeNav}
      >
        {navigation}
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
    </div>
  );
}
