import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
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

vi.mock('./chat/ExternalStoreChat', () => ({
  ExternalStoreChat: () => <div data-testid="chat-lane" />,
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

function renderShell() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const utils = render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/c/conv-1']}>
        <Routes>
          <Route path="/c/:id" element={<AppShell />} />
          <Route path="*" element={<AppShell />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { client, ...utils };
}

beforeEach(() => {
  localStorage.clear();
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
    expect(screen.getByLabelText(TOGGLE_LABEL)).toBeTruthy();
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
