import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { InlineApprovalCard } from '../InlineApprovalCard';
import type { Approval } from '../useApprovals';

function approval(over: Partial<Approval> & Pick<Approval, 'token' | 'conversation_id'>): Approval {
  return {
    kind: 'clarification',
    question: 'Which city should I check?',
    priority: 0,
    ...over,
  };
}

interface ResolveCall {
  readonly url: string;
  readonly body: { action: string; content?: string };
}

// A fetch double that records the resolve POSTs (so the verb + content payload can
// be asserted). Returns 204 by default; pass `fail` to make resolve 403/500.
function stubResolve(calls: ResolveCall[], fail = false) {
  return vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    if (url.includes('/resolve') && init?.method === 'POST') {
      const body = JSON.parse(init.body as string) as { action: string; content?: string };
      calls.push({ url, body });
      if (fail) return Promise.resolve(new Response('forbidden', { status: 403 }));
      return Promise.resolve(new Response(null, { status: 204 }));
    }
    // /agent/run re-drive (continue-after-resume) + approvals poll → benign 200.
    return Promise.resolve(new Response('[]', { status: 200 }));
  });
}

function client() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

function renderCard(props: {
  approval: Approval;
  isStreaming?: boolean;
  onResolved?: (id: string) => void;
}) {
  const qc = client();
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return render(
    <InlineApprovalCard
      approval={props.approval}
      {...(props.isStreaming !== undefined ? { isStreaming: props.isStreaming } : {})}
      {...(props.onResolved !== undefined ? { onResolved: props.onResolved } : {})}
    />,
    { wrapper: Wrapper },
  );
}

describe('InlineApprovalCard (APRV-02/03 / D-03/D-05/D-06)', () => {
  let calls: ResolveCall[];
  beforeEach(() => {
    calls = [];
    vi.stubGlobal('fetch', stubResolve(calls));
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('renders the backend question VERBATIM + option buttons', () => {
    renderCard({
      approval: approval({ token: 't-1', conversation_id: 'c-1', options: ['Rome', 'Milan'] }),
    });
    // The question string is rendered as-is, no client-side rewrite.
    expect(screen.getByText('Which city should I check?')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Rome' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Milan' })).toBeTruthy();
  });

  it('renders a free-text input when the pause offers no options', () => {
    renderCard({ approval: approval({ token: 't-1', conversation_id: 'c-1' }) });
    expect(screen.getByPlaceholderText('Type your answer')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Answer' })).toBeTruthy();
  });

  it('Answer (option) resolves {action:"accept", content} → answered terminal chip', async () => {
    const onResolved = vi.fn();
    renderCard({
      approval: approval({ token: 't-1', conversation_id: 'c-1', options: ['Rome', 'Milan'] }),
      onResolved,
    });
    fireEvent.click(screen.getByRole('button', { name: 'Milan' }));
    await waitFor(() => {
      expect(screen.getByText('Answered — run resumed.')).toBeTruthy();
    });
    expect(calls).toHaveLength(1);
    expect(calls[0]?.url).toContain('/api/approvals/t-1/resolve');
    expect(calls[0]?.body).toEqual({ action: 'accept', content: 'Milan' });
    // accept keeps the run alive → re-driven.
    expect(onResolved).toHaveBeenCalledWith('c-1');
  });

  it('Answer (free-text) resolves accept with the typed content', async () => {
    renderCard({ approval: approval({ token: 't-1', conversation_id: 'c-1' }) });
    fireEvent.change(screen.getByPlaceholderText('Type your answer'), {
      target: { value: 'check Turin' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Answer' }));
    await waitFor(() => {
      expect(calls).toHaveLength(1);
    });
    expect(calls[0]?.body).toEqual({ action: 'accept', content: 'check Turin' });
  });

  it('Decline resolves {action:"decline"} and does NOT carry the operator text (deny != accept, T-25-17)', async () => {
    renderCard({ approval: approval({ token: 't-1', conversation_id: 'c-1' }) });
    // The operator typed something, then chose Decline — the typed text must NOT be
    // sent as the accepted answer (the load-bearing footgun guard).
    fireEvent.change(screen.getByPlaceholderText('Type your answer'), {
      target: { value: 'this should never be the answer' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Decline' }));
    await waitFor(() => {
      expect(screen.getByText('The agent will continue, informed you declined.')).toBeTruthy();
    });
    expect(calls).toHaveLength(1);
    expect(calls[0]?.body.action).toBe('decline');
    // No operator content on the wire — the server injects declinedContent.
    expect(calls[0]?.body.content).toBeUndefined();
  });

  it('Cancel run while idle resolves {action:"cancel"} immediately', async () => {
    renderCard({
      approval: approval({ token: 't-1', conversation_id: 'c-1' }),
      isStreaming: false,
    });
    fireEvent.click(screen.getByRole('button', { name: 'Cancel run' }));
    await waitFor(() => {
      expect(screen.getByText('Run cancelled.')).toBeTruthy();
    });
    expect(calls[0]?.body).toEqual({ action: 'cancel' });
  });

  it('Cancel run while STREAMING shows an inline "Stop this run?" confirm (not a modal)', async () => {
    renderCard({ approval: approval({ token: 't-1', conversation_id: 'c-1' }), isStreaming: true });
    fireEvent.click(screen.getByRole('button', { name: 'Cancel run' }));
    // Inline confirm appears — NOT a dialog.
    expect(screen.getByText('Stop this run?')).toBeTruthy();
    expect(screen.queryByRole('dialog')).toBeNull();
    // No resolve fired until the confirm.
    expect(calls).toHaveLength(0);
    fireEvent.click(screen.getByRole('button', { name: 'Stop run' }));
    await waitFor(() => {
      expect(calls).toHaveLength(1);
    });
    expect(calls[0]?.body).toEqual({ action: 'cancel' });
  });

  it('D-06: an expired/auto-terminated interrupt renders its terminal state inline, verbs gone (never silent)', () => {
    renderCard({
      approval: approval({ token: 't-1', conversation_id: 'c-1', terminal: true }),
    });
    expect(screen.getByText('Expired — auto-resolved.')).toBeTruthy();
    // The verbs are not offered for a terminal interrupt.
    expect(screen.queryByRole('button', { name: 'Answer' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Decline' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Cancel run' })).toBeNull();
  });

  it('a failed resolve renders the error in a role="alert"', async () => {
    vi.stubGlobal('fetch', stubResolve(calls, true));
    renderCard({ approval: approval({ token: 't-1', conversation_id: 'c-1' }) });
    fireEvent.click(screen.getByRole('button', { name: 'Answer' }));
    await waitFor(() => {
      const alert = screen.getByRole('alert');
      expect(alert.textContent).toContain("Couldn't resume this run.");
    });
  });
});
