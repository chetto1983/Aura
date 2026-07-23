import type { ReactElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { ExternalStoreChat } from '../ExternalStoreChat';

// Compact tool cards through the REAL runtime + snapshot rehydration path
// (compact-chat spec §3): AC-8 typed displays sit BEHIND the collapsed row on
// replay too; AC-9 system_event/local_artifact render inline with NO disclosure
// row. Drives GET /threads/{id}/messages exactly like the cockpit does.

function renderChat(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

function snapshotFetchMock(messages: unknown[]): (url: unknown) => Promise<Response> {
  return (url: unknown) => {
    if (url === '/threads/conv-1/messages') {
      return Promise.resolve(
        new Response(JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    }
    return Promise.resolve(new Response('[]', { status: 200 }));
  };
}

function toolTurn(display: unknown, name = 'web_search'): unknown[] {
  return [
    { id: 'msg-0', role: 'user', content: 'run it' },
    {
      id: 'msg-1',
      role: 'assistant',
      content: '',
      toolCalls: [
        {
          id: 'call-1',
          type: 'function',
          function: { name, arguments: '{"query":"compact"}' },
          ...(display !== undefined ? { display } : {}),
        },
      ],
    },
    { id: 'msg-2', role: 'tool', toolCallId: 'call-1', content: 'raw result body' },
    { id: 'msg-3', role: 'assistant', content: 'Answer.' },
  ];
}

describe('compact tool cards on snapshot rehydration', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('AC-8: a web_result display renders a collapsed row with meta; expanding reveals the card', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(
        snapshotFetchMock(
          toolTurn({
            type: 'web_result',
            tool_call_id: 'call-1',
            web_results: [{ title: 'Snapshot Result', url: 'https://example.com/1' }],
          }),
        ),
      ),
    );
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('Answer.');

    expect(screen.getByTestId('tool-row')).toBeTruthy();
    expect(screen.getByTestId('tool-meta').textContent).toBe('1 result');
    expect(screen.getByTestId('tool-body').hidden).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: 'web_search activity' }));
    expect(screen.getByTestId('tool-body').hidden).toBe(false);
    expect(screen.getByText('Snapshot Result')).toBeTruthy();
  });

  it('AC-9: a system_event payload renders INLINE with no disclosure row', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(
        snapshotFetchMock(
          toolTurn({
            type: 'system_event',
            tool_call_id: 'call-1',
            system: { class: 'web_fetch', reason: 'blocked_url', severity: 'warning' },
          }),
        ),
      ),
    );
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('Answer.');
    expect(screen.queryByTestId('tool-row')).toBeNull();
    // The enum-mapped safe reason renders (never free text, T-26-12).
    expect(screen.getByText('This URL was blocked by the safety policy.')).toBeTruthy();
  });

  it('AC-9: a local_artifact payload renders the inline chip with no disclosure row', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(
        snapshotFetchMock(
          toolTurn(
            {
              type: 'local_artifact',
              tool_call_id: 'call-1',
              artifact: { filename: 'report.xlsx', asset_id: 'as-1' },
            },
            'send_file',
          ),
        ),
      ),
    );
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('Answer.');
    expect(screen.queryByTestId('tool-row')).toBeNull();
    expect(screen.getByText('report.xlsx')).toBeTruthy();
  });

  it('AC-7 on replay: a raw tool turn (no display) expands to the Request/Result panel', async () => {
    vi.stubGlobal('fetch', vi.fn(snapshotFetchMock(toolTurn(undefined))));
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('Answer.');

    expect(screen.getByTestId('tool-row')).toBeTruthy();
    expect(screen.getByTestId('tool-body').hidden).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: 'web_search activity' }));
    expect(screen.getByText('Request')).toBeTruthy();
    expect(screen.getByText('Result')).toBeTruthy();
    expect(screen.getByText('raw result body')).toBeTruthy();
  });
});
