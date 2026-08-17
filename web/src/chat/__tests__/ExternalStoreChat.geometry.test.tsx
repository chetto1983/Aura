import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n';
import { ExternalStoreChat } from '../ExternalStoreChat';
import { AttachmentChip } from '../attachments/AttachmentChip';

function sseResponse(frames: readonly Record<string, unknown>[]): Response {
  const encoder = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      const wire = frames
        .map((frame) => `event: ${String(frame.type)}\ndata: ${JSON.stringify(frame)}\n\n`)
        .join('');
      controller.enqueue(encoder.encode(wire));
      controller.close();
    },
  });
  return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
}

function messagesSnapshotResponse(messages: readonly Record<string, unknown>[]): Response {
  return new Response(JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function completedTurn(messageId: string, text: string): readonly Record<string, unknown>[] {
  return [
    { type: 'RUN_STARTED', threadId: 'conv-1', runId: `run-${messageId}` },
    { type: 'TEXT_MESSAGE_START', messageId },
    { type: 'TEXT_MESSAGE_CONTENT', messageId, delta: text },
    { type: 'TEXT_MESSAGE_END', messageId },
    {
      type: 'RUN_FINISHED',
      threadId: 'conv-1',
      runId: `run-${messageId}`,
      outcome: { type: 'success' },
    },
  ];
}

function expectRequiredTouchTarget(element: Element): void {
  const classes = element.getAttribute('class') ?? '';
  expect(element.getAttribute('data-required-touch-target')).not.toBeNull();
  expect(classes).toMatch(/(?:^|\s)(?:min-h-\[44px\]|h-\[44px\])(?:\s|$)/);
  expect(classes).toMatch(/(?:^|\s)(?:min-w-\[44px\]|w-\[44px\])(?:\s|$)/);
}

function sendPrompt(text: string): void {
  const input = screen.getByPlaceholderText('Ask Aura');
  fireEvent.change(input, { target: { value: text } });
  fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' });
}

function renderChat(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

function renderMarkdownMessage(markdown: string) {
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => {
      if (url.startsWith('/api/assets')) {
        return Promise.resolve(new Response('[]', { status: 200 }));
      }
      if (url.startsWith('/threads/')) {
        return Promise.resolve(messagesSnapshotResponse([]));
      }
      return Promise.resolve(sseResponse(completedTurn('msg-markdown', markdown)));
    }),
  );
  const rendered = renderChat(<ExternalStoreChat threadId="conv-1" />);
  sendPrompt('show metrics');
  return rendered;
}

function expectFluidTable(table: HTMLElement): void {
  expect(table.closest('[data-message-content]')).not.toBeNull();
  expect(table.closest('[data-message-prose]')).toBeNull();
  expect(table.closest('[class~="max-w-[48rem]"]')).toBeNull();
}

beforeEach(() => {
  localStorage.removeItem('aura.chat.reasoning.shown');
});

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.removeItem('aura.chat.reasoning.shown');
});

describe('ExternalStoreChat message geometry', () => {
  it('caps Markdown prose while keeping a GFM table fluid in the same assistant part', async () => {
    const markdown = [
      'Measured paragraph.',
      '',
      '| Metric | Value |',
      '| --- | ---: |',
      '| Latency | 42 ms |',
    ].join('\n');
    const { container } = renderMarkdownMessage(markdown);

    const paragraph = await screen.findByText('Measured paragraph.');
    expect(paragraph.getAttribute('data-message-prose')).not.toBeNull();
    expect(paragraph.classList.contains('max-w-[48rem]')).toBe(true);

    expectFluidTable(screen.getByRole('table'));
    expect(
      container.querySelector('[data-message-content]')?.classList.contains('max-w-[48rem]'),
    ).toBe(false);
  });

  it('keeps a quoted GFM table outside prose caps', async () => {
    renderMarkdownMessage(
      [
        '> Quoted context.',
        '>',
        '> | Metric | Value |',
        '> | --- | ---: |',
        '> | Latency | 42 ms |',
      ].join('\n'),
    );

    const paragraph = await screen.findByText('Quoted context.');
    expect(paragraph.getAttribute('data-message-prose')).not.toBeNull();
    expect(paragraph.classList.contains('max-w-[48rem]')).toBe(true);
    expectFluidTable(screen.getByRole('table'));
  });

  it('keeps a list-nested GFM table outside prose caps', async () => {
    renderMarkdownMessage(
      [
        '- Listed context.',
        '',
        '  | Metric | Value |',
        '  | --- | ---: |',
        '  | Latency | 42 ms |',
      ].join('\n'),
    );

    const paragraph = await screen.findByText('Listed context.');
    expect(paragraph.getAttribute('data-message-prose')).not.toBeNull();
    expect(paragraph.classList.contains('max-w-[48rem]')).toBe(true);
    expectFluidTable(screen.getByRole('table'));
  });

  it('keeps assistant content fluid and exposes responsive action rows', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((url: unknown) =>
        Promise.resolve(
          typeof url === 'string' && url.startsWith('/api/assets')
            ? new Response('[]', { status: 200 })
            : messagesSnapshotResponse([
                { id: 'msg-2', role: 'user', content: 'persisted prompt' },
                { id: 'msg-3', role: 'assistant', content: 'persisted answer' },
              ]),
        ),
      ),
    );

    const { container } = renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('persisted answer');

    const assistant = container.querySelector('[data-message-role="assistant"]');
    if (!(assistant instanceof HTMLElement)) throw new Error('expected assistant message root');
    expect(assistant.classList.contains('w-full')).toBe(true);
    expect(assistant.classList.contains('min-w-0')).toBe(true);

    const content = assistant.querySelector('[data-message-content]');
    if (!(content instanceof HTMLElement)) throw new Error('expected assistant content region');
    expect(content.classList.contains('w-full')).toBe(true);
    expect(content.classList.contains('min-w-0')).toBe(true);
    expect(content.classList.contains('overflow-x-auto')).toBe(true);
    expect(content.classList.contains('max-w-[48rem]')).toBe(false);

    const actionRows = Array.from(container.querySelectorAll('[data-message-actions]'));
    expect(actionRows).toHaveLength(2);
    for (const row of actionRows) {
      expect(row.classList.contains('flex-wrap')).toBe(true);
      expect(row.classList.contains('opacity-0')).toBe(true);
      expect(row.classList.contains('hover:opacity-100')).toBe(true);
      expect(row.classList.contains('focus-within:opacity-100')).toBe(true);
      expect(row.classList.contains('[@media(pointer:coarse)]:opacity-100')).toBe(true);
    }

    expectRequiredTouchTarget(screen.getByRole('button', { name: 'Edit' }));
    expectRequiredTouchTarget(screen.getByRole('button', { name: 'Regenerate' }));
    screen.getAllByRole('button', { name: 'Copy' }).forEach(expectRequiredTouchTarget);
  });

  it('keeps both branch arrows at the required touch target after a real edit fork', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url.startsWith('/api/assets'))
        return Promise.resolve(new Response('[]', { status: 200 }));
      if (url.startsWith('/threads/') && !url.includes('/edit')) {
        return Promise.resolve(messagesSnapshotResponse([]));
      }
      const edited = url.includes('/edit');
      return Promise.resolve(
        sseResponse(
          completedTurn(
            edited ? 'msg-edited' : 'msg-first',
            edited ? 'edited answer' : 'first answer',
          ),
        ),
      );
    });
    vi.stubGlobal('fetch', fetchMock);
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    sendPrompt('first question');
    await screen.findByText('first answer');

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    fireEvent.input(await screen.findByRole('textbox', { name: 'Edit message' }), {
      target: { value: 'edited question' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save and re-run' }));

    expectRequiredTouchTarget(await screen.findByRole('button', { name: 'Previous branch' }));
    expectRequiredTouchTarget(screen.getByRole('button', { name: 'Next branch' }));
  });

  it('contains long attachment filenames without allowing remove or download actions to shrink', async () => {
    const longName = `${'quarterly-evidence-'.repeat(12)}.pdf`;
    const chipRender = render(
      <AttachmentChip
        attachment={{
          id: 'upload-1',
          type: 'document',
          name: longName,
          contentType: 'application/pdf',
          file: new File(['document'], longName, { type: 'application/pdf' }),
          status: { type: 'requires-action', reason: 'composer-send' },
        }}
      />,
    );
    const remove = screen.getByRole('button', { name: `Remove ${longName}` });
    const chip = remove.parentElement;
    if (!(chip instanceof HTMLElement)) throw new Error('expected attachment chip');
    expect(chip.classList.contains('min-w-0')).toBe(true);
    expect(chip.classList.contains('max-w-full')).toBe(true);
    expect(remove.classList.contains('shrink-0')).toBe(true);
    expectRequiredTouchTarget(remove);
    chipRender.unmount();

    vi.stubGlobal(
      'fetch',
      vi.fn((url: unknown) => {
        if (url === '/threads/conv-1/messages') {
          return Promise.resolve(
            messagesSnapshotResponse([
              { id: 'msg-1', role: 'user', content: 'make a document' },
              { id: 'msg-2', role: 'assistant', content: 'document ready' },
            ]),
          );
        }
        if (url === '/api/assets?thread_id=conv-1') {
          return Promise.resolve(
            new Response(
              JSON.stringify([
                {
                  id: 'asset-long',
                  source_kind: 'agent',
                  status: 'complete',
                  modality: 'document',
                  file_name: longName,
                  mime_type: 'application/pdf',
                  declared_size_bytes: 8,
                  size_bytes: 8,
                },
              ]),
              { status: 200, headers: { 'Content-Type': 'application/json' } },
            ),
          );
        }
        return Promise.reject(new Error(`unexpected fetch: ${String(url)}`));
      }),
    );
    renderChat(<ExternalStoreChat threadId="conv-1" />);

    const download = await screen.findByRole('link', { name: `Download ${longName}` });
    const downloadOwner = download.parentElement;
    if (!(downloadOwner instanceof HTMLElement)) throw new Error('expected download owner');
    expect(downloadOwner.classList.contains('min-w-0')).toBe(true);
    expect(download.classList.contains('max-w-full')).toBe(true);
    expectRequiredTouchTarget(download);
    const filename = screen.getByText(longName);
    expect(filename.classList.contains('min-w-0')).toBe(true);
    expect(filename.classList.contains('[overflow-wrap:anywhere]')).toBe(true);
  });

  it('sizes persisted attachment Retry, Promote, and Remove actions for touch', async () => {
    const baseAsset = {
      modality: 'document',
      mime_type: 'application/pdf',
      declared_size_bytes: 8,
      size_bytes: 8,
    };
    vi.stubGlobal(
      'fetch',
      vi.fn((url: unknown) => {
        if (url === '/threads/conv-1/messages') {
          return Promise.resolve(
            messagesSnapshotResponse([
              { id: 'msg-1', role: 'user', content: 'failed document' },
              { id: 'msg-2', role: 'assistant', content: 'retry available' },
              { id: 'msg-3', role: 'user', content: 'ready document' },
              { id: 'msg-4', role: 'assistant', content: 'promotion available' },
            ]),
          );
        }
        if (url === '/api/assets?thread_id=conv-1') {
          return Promise.resolve(
            new Response(
              JSON.stringify([
                { ...baseAsset, id: 'asset-failed', status: 'failed', file_name: 'broken.pdf' },
                { ...baseAsset, id: 'asset-ready', status: 'complete', file_name: 'ready.pdf' },
              ]),
              { status: 200, headers: { 'Content-Type': 'application/json' } },
            ),
          );
        }
        return Promise.reject(new Error(`unexpected fetch: ${String(url)}`));
      }),
    );
    renderChat(<ExternalStoreChat threadId="conv-1" />);

    await screen.findByText('broken.pdf');
    const actions = [
      screen.getByRole('button', { name: 'Retry' }),
      screen.getByRole('button', { name: 'Promote' }),
      ...screen.getAllByRole('button', { name: /^Remove (?:broken|ready)\.pdf$/ }),
    ];
    expect(actions).toHaveLength(4);
    actions.forEach(expectRequiredTouchTarget);
  });
});
