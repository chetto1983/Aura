import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import './i18n/i18n';
import { AppShell } from './AppShell';

// AppShell 37C-04 — the ephemeral voice-mode header toggle. The heavy shell children
// are mocked to trivial stubs so the test isolates AppShell's own VoiceModeProvider +
// VoiceModeToggle wiring. The toggle is caps.tts-gated (WEBVOICE-03): it appears only
// once GET /api/voice/capabilities resolves {tts:true}, flips voiceMode on click, and
// is absent when tts is unconfigured.

vi.mock('./chat/ExternalStoreChat', () => ({
  ExternalStoreChat: (props: { threadId: string }) => (
    <div data-testid="chat-lane" data-thread={props.threadId} />
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

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function stubCapabilities(tts: boolean, stt = false): void {
  vi.stubGlobal(
    'fetch',
    vi.fn<typeof fetch>((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      if (url.includes('/api/voice/capabilities')) {
        return Promise.resolve(jsonResponse({ tts, stt }));
      }
      return Promise.reject(new Error(`unexpected fetch: ${url}`));
    }),
  );
}

function renderShell() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/c/conv-1']}>
        <Routes>
          <Route path="/c/:id" element={<AppShell />} />
          <Route path="*" element={<AppShell />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function expectRequiredTouchTarget(element: HTMLElement): void {
  const classes = element.getAttribute('class') ?? '';
  expect(element.getAttribute('data-required-touch-target')).not.toBeNull();
  expect(classes).toMatch(/(?:^|\s)(?:min-h-\[44px\]|h-\[44px\])(?:\s|$)/);
  expect(classes).toMatch(/(?:^|\s)(?:min-w-\[44px\]|w-\[44px\])(?:\s|$)/);
}

beforeEach(() => {
  localStorage.clear();
});
afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

describe('AppShell — voice-mode header toggle (D-06 / WEBVOICE-03)', () => {
  it('shows the toggle when tts is configured and flips voiceMode on click', async () => {
    stubCapabilities(true);
    renderShell();
    await screen.findByTestId('chat-lane');

    const toggle = await screen.findByLabelText('Voice mode off');
    expect(toggle.getAttribute('aria-pressed')).toBe('false');
    expectRequiredTouchTarget(toggle);

    fireEvent.click(toggle);

    const on = await screen.findByLabelText('Voice mode on');
    expect(on.getAttribute('aria-pressed')).toBe('true');
    // A single toggle, not two — the off/on labels are the same control's states.
    expect(screen.queryByLabelText('Voice mode off')).toBeNull();
  });

  it('hides the toggle when tts is not configured (graceful degrade)', async () => {
    stubCapabilities(false);
    renderShell();
    await screen.findByTestId('chat-lane');

    // Let the capabilities probe resolve, then assert the control never appears.
    await waitFor(() => {
      expect(
        (globalThis.fetch as unknown as { mock: { calls: unknown[] } }).mock.calls.length,
      ).toBeGreaterThan(0);
    });
    expect(screen.queryByLabelText('Voice mode off')).toBeNull();
    expect(screen.queryByLabelText('Voice mode on')).toBeNull();
  });
});
