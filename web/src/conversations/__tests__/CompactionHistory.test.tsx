import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CompactionHistory } from '../CompactionHistory';

function wrapper() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe('CompactionHistory', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('distinguishes semantic compaction, L1 offload, and lossy L2.5 for screen readers', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              operationId: 'op-1',
              checkpointId: 'cp-1',
              priorCheckpointId: 'cp-0',
              status: 'disabled',
              activated: false,
              entries: [
                {
                  checkpoint_id: 'cp-1',
                  kind: 'compaction',
                  trigger: 'manual',
                  reason: 'target_reached',
                  token_delta: -1200,
                  quality: 'passed',
                },
                {
                  checkpoint_id: 'cp-l1',
                  kind: 'l1_offload',
                  trigger: 'boundary',
                  reason: 'tool_payload',
                  token_delta: -300,
                  quality: 'passed',
                },
                {
                  checkpoint_id: 'cp-loss',
                  kind: 'l2_5_loss',
                  trigger: 'overflow',
                  reason: 'provider_waiver',
                  token_delta: -900,
                  quality: 'degraded',
                },
              ],
            }),
            { status: 200 },
          ),
        ),
      ),
    );
    render(<CompactionHistory conversationId="c-1" />, { wrapper: wrapper() });
    expect(await screen.findByRole('heading', { name: 'Compaction history' })).toBeTruthy();
    expect(await screen.findByText('Semantic compaction')).toBeTruthy();
    expect(screen.getByText('L1 payload offload')).toBeTruthy();
    expect(screen.getByText('Lossy emergency reduction')).toBeTruthy();
    expect(screen.getByRole('alert').textContent).toContain('Information was removed');
  });

  it('opens preview by keyboard, restores from a safe point, and returns focus', async () => {
    const fetchMock = vi.fn((_input: RequestInfo | URL, init?: RequestInit) =>
      Promise.resolve(
        new Response(
          JSON.stringify({
            operationId: 'op-1',
            checkpointId: 'cp-1',
            priorCheckpointId: 'cp-0',
            status:
              typeof init?.body === 'string' && init.body.includes('safePoint')
                ? 'recovered'
                : 'disabled',
            activated: false,
            summary: 'Safe historical summary',
            entries: [
              {
                checkpoint_id: 'cp-1',
                kind: 'compaction',
                trigger: 'manual',
                reason: 'target_reached',
                token_delta: -1200,
                quality: 'passed',
              },
            ],
          }),
          { status: 200 },
        ),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);
    render(<CompactionHistory conversationId="c-1" />, { wrapper: wrapper() });
    const preview = await screen.findByRole('button', { name: 'Preview checkpoint cp-1' });
    preview.focus();
    fireEvent.keyDown(preview, { key: 'Enter' });
    expect(await screen.findByRole('region', { name: 'Checkpoint preview' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Restore checkpoint cp-1' }));
    await waitFor(() => {
      expect(fetchMock.mock.calls.length).toBeGreaterThanOrEqual(3);
    });
    expect((await screen.findByRole('status')).textContent).toContain('Checkpoint restored');
  });
});
