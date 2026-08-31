import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { InlineApprovalCard } from '../InlineApprovalCard';
import type { ApprovalResolution } from '../useThreadApprovals';
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
// be asserted). Resolve answers 200 + the ResolveDirective (Phase A); `outcome` scripts the
// runner's verdict, `fail` makes resolve 403/500 instead.
function stubResolve(calls: ResolveCall[], fail = false, outcome = 'continue') {
  return vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    if (url.includes('/resolve') && init?.method === 'POST') {
      const body = JSON.parse(init.body as string) as { action: string; content?: string };
      calls.push({ url, body });
      if (fail) return Promise.resolve(new Response('forbidden', { status: 403 }));
      return Promise.resolve(
        new Response(JSON.stringify({ outcome, remaining: 0 }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
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
  onResolved?: (resolution: unknown) => void;
  onResolutionStarted?: (resolution: unknown) => void;
  onResolutionFailed?: (resolution: unknown) => void;
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
      {...(props.onResolutionStarted !== undefined
        ? { onResolutionStarted: props.onResolutionStarted }
        : {})}
      {...(props.onResolutionFailed !== undefined
        ? { onResolutionFailed: props.onResolutionFailed }
        : {})}
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
    expect(screen.getByPlaceholderText('Type your answer').getAttribute('data-slot')).toBe(
      'textarea',
    );
    expect(
      screen.getByText('Which city should I check?').closest('[data-slot="card"]'),
    ).not.toBeNull();
    expect(screen.getByRole('button', { name: 'Answer' })).toBeTruthy();
  });

  it('omits aria-invalid while valid and emits the explicit true token on a failed empty answer', async () => {
    vi.stubGlobal('fetch', stubResolve(calls, true));
    renderCard({ approval: approval({ token: 't-1', conversation_id: 'c-1' }) });
    const field = screen.getByPlaceholderText('Type your answer');
    expect(field.getAttribute('aria-invalid')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Answer' }));

    await waitFor(() => {
      expect(field.getAttribute('aria-invalid')).toBe('true');
    });
  });

  it('Answer (option) resolves {action:"accept", content} → answered terminal chip', async () => {
    const onResolved = vi.fn();
    renderCard({
      approval: approval({ token: 't-1', conversation_id: 'c-1', options: ['Rome', 'Milan'] }),
      onResolved,
    });
    fireEvent.click(screen.getByRole('button', { name: 'Milan' }));
    await waitFor(() => {
      expect(screen.getByText('Answered.')).toBeTruthy();
    });
    expect(screen.getByText('Answered.').closest('[data-tone]')?.getAttribute('data-tone')).toBe(
      'success',
    );
    expect(calls).toHaveLength(1);
    expect(calls[0]?.url).toContain('/api/approvals/t-1/resolve');
    expect(calls[0]?.body).toEqual({ action: 'accept', content: 'Milan' });
    const resolution = onResolved.mock.calls[0]?.[0] as ApprovalResolution | undefined;
    expect(resolution?.approval.token).toBe('t-1');
    expect(resolution?.approval.conversation_id).toBe('c-1');
    expect(resolution?.action).toBe('accept');
  });

  it('a scheduled approval renders the server verdict and carries it to the gate', async () => {
    // The server is authoritative: a DECLINE on a scheduled gate comes back "rejected", and the
    // verdict must reach the thread gate so it settles WITHOUT re-driving a turn that the
    // ResumeHook already completed.
    vi.stubGlobal('fetch', stubResolve(calls, false, 'rejected'));
    const onResolved = vi.fn();
    renderCard({ approval: approval({ token: 't-sched', conversation_id: 'c-1' }), onResolved });

    fireEvent.click(screen.getByRole('button', { name: 'Decline' }));
    await waitFor(() => {
      expect(screen.getByText('Declined.')).toBeTruthy();
    });
    const resolution = onResolved.mock.calls[0]?.[0] as ApprovalResolution | undefined;
    expect(resolution?.outcome).toBe('rejected');
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
      expect(screen.getByText('Declined.')).toBeTruthy();
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
    expect(
      screen.getByText('Run cancelled.').closest('[data-tone]')?.getAttribute('data-tone'),
    ).toBe('danger');
    expect(calls[0]?.body).toEqual({ action: 'cancel' });
  });

  it('carries the exact started attempt identity through successful resolution', async () => {
    const onResolutionStarted = vi.fn();
    const onResolved = vi.fn();
    renderCard({
      approval: approval({ token: 't-owned', conversation_id: 'c-1' }),
      onResolutionStarted,
      onResolved,
    });

    fireEvent.click(screen.getByRole('button', { name: 'Decline' }));
    await waitFor(() => {
      expect(onResolved).toHaveBeenCalledTimes(1);
    });

    expect(onResolutionStarted).toHaveBeenCalledTimes(1);
    // Phase A: the resolved attempt carries the server's verdict IN ADDITION to the started
    // identity (the verdict does not exist yet when the attempt starts). The identity itself —
    // approval, action, attemptId — must still match exactly, which is what this test guards.
    const started = onResolutionStarted.mock.calls[0]?.[0] as Record<string, unknown>;
    expect(onResolved.mock.calls[0]?.[0]).toEqual({ ...started, outcome: 'continue' });
  });

  it('signals Cancel intent synchronously before starting the resolve request', async () => {
    const order: string[] = [];
    const fetchStub = stubResolve(calls);
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        order.push('post');
        return fetchStub(input, init);
      }),
    );
    renderCard({
      approval: approval({ token: 't-1', conversation_id: 'c-1' }),
      isStreaming: false,
      onResolutionStarted: () => {
        order.push('start');
      },
    });

    fireEvent.click(screen.getByRole('button', { name: 'Cancel run' }));

    await waitFor(() => {
      expect(order).toContain('post');
    });
    expect(order.slice(0, 2)).toEqual(['start', 'post']);
  });

  it('signals the matching Cancel attempt when its resolve request fails', async () => {
    vi.stubGlobal('fetch', stubResolve(calls, true));
    const onResolutionFailed = vi.fn();
    const onResolved = vi.fn();
    renderCard({
      approval: approval({ token: 't-1', conversation_id: 'c-1' }),
      isStreaming: false,
      onResolved,
      onResolutionFailed,
    });

    fireEvent.click(screen.getByRole('button', { name: 'Cancel run' }));

    await waitFor(() => {
      expect(onResolutionFailed).toHaveBeenCalledTimes(1);
    });
    const failed = onResolutionFailed.mock.calls[0]?.[0] as
      { action: string; attemptId: string } | undefined;
    expect(failed?.action).toBe('cancel');
    expect(failed?.attemptId.length).toBeGreaterThan(0);
    expect(onResolved).not.toHaveBeenCalled();
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

  it('moves focus to Keep running, restores Cancel run on Escape, and gives confirmation controls a 44px floor', async () => {
    renderCard({ approval: approval({ token: 't-1', conversation_id: 'c-1' }), isStreaming: true });
    const cancel = screen.getByRole('button', { name: 'Cancel run' });
    fireEvent.click(cancel);

    const keepRunning = screen.getByRole('button', { name: 'Keep running' });
    const stopRun = screen.getByRole('button', { name: 'Stop run' });
    await waitFor(() => {
      expect(document.activeElement).toBe(keepRunning);
    });
    expect(keepRunning.className).toContain('min-h-11');
    expect(stopRun.className).toContain('min-h-11');

    fireEvent.keyDown(keepRunning, { key: 'Escape' });
    await waitFor(() => {
      expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Cancel run' }));
    });
    expect(screen.queryByText('Stop this run?')).toBeNull();
    expect(calls).toHaveLength(0);
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

  it('a failed resolve renders the error in a persistent status region', async () => {
    vi.stubGlobal('fetch', stubResolve(calls, true));
    renderCard({ approval: approval({ token: 't-1', conversation_id: 'c-1' }) });
    fireEvent.click(screen.getByRole('button', { name: 'Answer' }));
    await waitFor(() => {
      const status = screen.getByRole('status');
      expect(status.textContent).toContain("Couldn't resume this run.");
      expect(status.getAttribute('data-tone')).toBe('danger');
    });
  });

  it('uses truthful generic approval framing, preserves whitespace, and keeps the token out of visible text', async () => {
    const risky = approval({
      token: 'resume-secret-123',
      conversation_id: 'c-1',
      kind: 'approval',
      question: 'Run command:\n  rm -rf /tmp/example\nReview scope first.',
    });
    renderCard({ approval: risky });

    expect(screen.getByText('Approval required')).toBeTruthy();
    expect(screen.getByText('Review the scope and consequence before continuing.')).toBeTruthy();
    expect(screen.queryByText(/container|install|activate/i)).toBeNull();
    expect(screen.queryByText('resume-secret-123')).toBeNull();
    expect(
      screen.getByText((_, element) => element?.textContent === risky.question).className,
    ).toMatch(/whitespace-pre-wrap/);

    fireEvent.click(screen.getByRole('button', { name: 'Decline' }));
    await waitFor(() => {
      expect(calls).toHaveLength(1);
    });
    expect(calls[0]?.url).toContain('/resume-secret-123/resolve');
    expect(calls[0]?.body).toEqual({ action: 'decline' });
    expect(
      screen
        .getByText(/declined/i)
        .closest('[data-tone]')
        ?.getAttribute('data-tone'),
    ).toBe('neutral');
    expect(screen.queryByText('resume-secret-123')).toBeNull();
  });

  it('keeps token sentinels hidden in pending, terminal, and failure states', async () => {
    const pending = approval({ token: 'pending-secret-1', conversation_id: 'c-1' });
    const pendingView = renderCard({ approval: pending });
    expect(screen.queryByText('pending-secret-1')).toBeNull();
    pendingView.unmount();

    const terminal = approval({
      token: 'terminal-secret-2',
      conversation_id: 'c-1',
      terminal: true,
    });
    const terminalView = renderCard({ approval: terminal });
    expect(screen.queryByText('terminal-secret-2')).toBeNull();
    expect(screen.getByRole('status').textContent).toContain('Expired');
    expect(
      screen
        .getByText(/expired/i)
        .closest('[data-tone]')
        ?.getAttribute('data-tone'),
    ).toBe('warning');
    terminalView.unmount();

    vi.stubGlobal('fetch', stubResolve(calls, true));
    const failed = approval({ token: 'failure secret/3', conversation_id: 'c-1' });
    renderCard({ approval: failed });
    fireEvent.click(screen.getByRole('button', { name: 'Answer' }));
    await screen.findByRole('status');
    expect(screen.queryByText('failure secret/3')).toBeNull();
    expect(calls.at(-1)?.url).toContain('/failure%20secret%2F3/resolve');
    expect(calls.at(-1)?.body).toEqual({ action: 'accept', content: '' });
  });
});
