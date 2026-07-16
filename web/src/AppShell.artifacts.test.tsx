import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import './i18n/i18n';
import { AppShell } from './AppShell';

// AppShell 37B plan 07 — the Artefatti panel INTEGRATION into the chat shell:
//  - a header doc toggle shows/hides the panel; open/closed state persists (D-03);
//  - at/above `lg` the panel is a third ResizablePanel (id chat-artifacts) whose
//    membership is driven dynamically so the persisted 2-panel layout key is untouched
//    (D-02, RESEARCH Pattern 1 — no CHAT_SHELL_LAYOUT_ID bump);
//  - below `lg` (matchMedia mock) the panel content routes through the shared right
//    Drawer instead of the ResizablePanel (D-04);
//  - onArtifact invalidates ['assets', threadId] and auto-opens the panel exactly once
//    per thread, re-arming on thread change (D-11).
// The heavy shell children are mocked to trivial stubs so the test isolates AppShell's
// own mount/toggle/wiring logic (ArtifactsPanel itself is covered by plan 06).

// Capture the props ExternalStoreChat receives so the onArtifact wiring is observable and can be
// fired directly (as the SSE pump does in production).
let lastChat: { threadId?: string; onArtifact?: (assetId?: string) => void } = {};
vi.mock('./chat/ExternalStoreChat', () => ({
  ExternalStoreChat: (props: { threadId: string; onArtifact?: (assetId?: string) => void }) => {
    lastChat = props;
    return <div data-testid="chat-lane" data-thread={props.threadId} />;
  },
}));
vi.mock('./chat/artifacts/ArtifactsPanel', () => ({
  ArtifactsPanel: ({ threadId, onClose }: { threadId: string; onClose: () => void }) => (
    <div data-testid="artifacts-panel" data-thread={threadId}>
      <button type="button" onClick={onClose}>
        panel-close
      </button>
    </div>
  ),
}));
vi.mock('./admin/useAdmin', () => ({
  useCapabilities: () => ({ isAdmin: true, isLoading: false }),
}));
vi.mock('./onboarding/onboardingApi', () => ({
  fetchOnboardingStatus: () => Promise.resolve({ required: false }),
}));
vi.mock('./conversations/useConversations', () => ({
  useCreateConversation: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));
vi.mock('./approvals/ThreadApprovalCards', () => ({ ThreadApprovalCards: () => null }));
vi.mock('./conversations/ConversationSidebar', () => ({ ConversationSidebar: () => null }));
vi.mock('./conversations/SearchPanel', () => ({ SearchPanel: () => null }));
vi.mock('./chat/RuntimeFooter', () => ({ RuntimeFooter: () => null }));
vi.mock('./shell/BottomDock', () => ({
  BottomDock: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));
vi.mock('./shell/MobileAppSidebar', () => ({
  MobileAppSidebar: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));
vi.mock('./shell/ShellHeader', () => ({ ShellHeader: () => <header /> }));

const NAV_KEY = 'react-resizable-panels:aura-chat-shell-v3:chat-navigation:chat-workspace';
const ART_KEY =
  'react-resizable-panels:aura-chat-shell-v3:chat-navigation:chat-workspace:chat-artifacts';
const SEED_2PANEL = JSON.stringify({ 'chat-navigation': 220, 'chat-workspace': 800 });
const TOGGLE_LABEL = 'Toggle the artifacts panel';

function setViewport(isDesktop: boolean): void {
  window.matchMedia = (query: string) =>
    ({
      matches: isDesktop,
      media: query,
      onchange: null,
      addListener: () => undefined,
      removeListener: () => undefined,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      dispatchEvent: () => false,
    }) as MediaQueryList;
}

// A minimal in-router control so a test can drive a real thread change (routeId → activeThreadId)
// and prove the one-time auto-open re-arms per thread.
function NavHelper() {
  const navigate = useNavigate();
  return (
    <button
      type="button"
      onClick={() => {
        void navigate('/c/conv-2');
      }}
    >
      go-conv-2
    </button>
  );
}

function renderShell(initial = '/c/conv-1') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const utils = render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[initial]}>
        <NavHelper />
        <Routes>
          <Route path="/c/:id" element={<AppShell />} />
          <Route path="*" element={<AppShell />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { client, ...utils };
}

function expectRequiredTouchTarget(element: HTMLElement): void {
  const classes = element.getAttribute('class') ?? '';
  expect(element.getAttribute('data-required-touch-target')).not.toBeNull();
  expect(classes).toMatch(/(?:^|\s)(?:min-h-\[44px\]|h-\[44px\])(?:\s|$)/);
  expect(classes).toMatch(/(?:^|\s)(?:min-w-\[44px\]|w-\[44px\])(?:\s|$)/);
}

beforeEach(() => {
  localStorage.clear();
  lastChat = {};
});

afterEach(() => {
  localStorage.clear();
});

describe('AppShell — Artefatti panel mount (desktop ResizablePanel / mobile Drawer)', () => {
  it('is closed by default: no artifacts panel, and the persisted 2-panel key is untouched', async () => {
    setViewport(true);
    localStorage.setItem(NAV_KEY, SEED_2PANEL);
    renderShell();
    await screen.findByTestId('chat-lane');

    expect(screen.queryByTestId('artifacts-panel')).toBeNull();
    expectRequiredTouchTarget(screen.getByLabelText(TOGGLE_LABEL));
    // Dynamic panelIds means the 3-panel key is never read/written while closed.
    expect(localStorage.getItem(NAV_KEY)).toBe(SEED_2PANEL);
    expect(localStorage.getItem(ART_KEY)).toBeNull();
  });

  it('desktop toggle mounts the third ResizablePanel (aside) without corrupting the 2-panel key', async () => {
    setViewport(true);
    localStorage.setItem(NAV_KEY, SEED_2PANEL);
    renderShell();
    await screen.findByTestId('chat-lane');

    fireEvent.click(screen.getByLabelText(TOGGLE_LABEL));

    const panel = await screen.findByTestId('artifacts-panel');
    expect(panel.getAttribute('data-thread')).toBe('conv-1');
    // Desktop surface = a ResizablePanel <aside> (complementary), NOT a dialog Drawer.
    expect(screen.getByRole('complementary', { name: 'Artifacts' })).toBeTruthy();
    expect(screen.queryByRole('dialog', { name: 'Artifacts' })).toBeNull();
    // Opening the panel must not overwrite the existing 2-panel layout.
    expect(localStorage.getItem(NAV_KEY)).toBe(SEED_2PANEL);
  });

  it('persists open state across remount (D-03)', async () => {
    setViewport(true);
    const first = renderShell();
    await screen.findByTestId('chat-lane');
    fireEvent.click(screen.getByLabelText(TOGGLE_LABEL));
    await screen.findByTestId('artifacts-panel');
    expect(localStorage.getItem('aura.shell.artifacts-open')).toBe('1');
    first.unmount();

    renderShell();
    // Restored from localStorage: the panel is open again without a click.
    expect(await screen.findByTestId('artifacts-panel')).toBeTruthy();
  });

  it('below lg the toggle routes to the right Drawer, not the ResizablePanel (D-04)', async () => {
    setViewport(false);
    renderShell();
    await screen.findByTestId('chat-lane');

    // Closed: neither surface renders the panel.
    expect(screen.queryByTestId('artifacts-panel')).toBeNull();

    fireEvent.click(screen.getByLabelText(TOGGLE_LABEL));

    const dialog = await screen.findByRole('dialog', { name: 'Artifacts' });
    expect(dialog.querySelector('[data-testid="artifacts-panel"]')).toBeTruthy();
    // Mobile must NOT mount the desktop ResizablePanel aside.
    expect(screen.queryByRole('complementary', { name: 'Artifacts' })).toBeNull();
  });
});

describe('AppShell — onArtifact handler (invalidate + one-time auto-open, D-11)', () => {
  it('is wired to ExternalStoreChat and invalidates ["assets", threadId] when fired', async () => {
    setViewport(true);
    const { client } = renderShell();
    await screen.findByTestId('chat-lane');
    expect(typeof lastChat.onArtifact).toBe('function');

    const spy = vi.spyOn(client, 'invalidateQueries');
    act(() => {
      lastChat.onArtifact?.('asset-1');
    });

    expect(spy).toHaveBeenCalledWith({ queryKey: ['assets', 'conv-1'] });
  });

  it('auto-opens once per thread: no reopen after manual close, re-arms on thread change', async () => {
    setViewport(true);
    renderShell();
    await screen.findByTestId('chat-lane');
    expect(screen.queryByTestId('artifacts-panel')).toBeNull();

    // First artifact in conv-1 auto-opens the panel exactly once.
    act(() => {
      lastChat.onArtifact?.();
    });
    expect(await screen.findByTestId('artifacts-panel')).toBeTruthy();

    // The user closes it; a subsequent artifact in the SAME thread must NOT reopen it.
    fireEvent.click(screen.getByText('panel-close'));
    await waitFor(() => {
      expect(screen.queryByTestId('artifacts-panel')).toBeNull();
    });
    act(() => {
      lastChat.onArtifact?.();
    });
    expect(screen.queryByTestId('artifacts-panel')).toBeNull();

    // Switching threads re-arms the guard: conv-2's first artifact auto-opens again.
    fireEvent.click(screen.getByText('go-conv-2'));
    await waitFor(() => {
      expect(lastChat.threadId).toBe('conv-2');
    });
    act(() => {
      lastChat.onArtifact?.();
    });
    expect(await screen.findByTestId('artifacts-panel')).toBeTruthy();
  });
});
