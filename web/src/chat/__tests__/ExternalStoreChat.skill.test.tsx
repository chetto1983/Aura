import type { ReactElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { ExternalStoreChat } from '../ExternalStoreChat';
import type { ComposerSkillRow } from '../composer/api';

// The skill is named INSIDE the message ('/skill-creator build me a skill') and read back
// out of it on send (operator, 2026-08-17 — "la skill deve stare nel messaggio non sopra"),
// so the harness mocks the installed-skills list rather than a pinned-chip seam, and
// asserts the aura.skill wire path — mirroring the attachments send-path harness.
const SKILLS: readonly ComposerSkillRow[] = [
  { name: 'skill-creator', description: 'Create a new skill', type: 'instruction' },
];

vi.mock('../composer/useComposerSkills', () => ({
  useComposerSkills: () => SKILLS,
}));

vi.mock('../attachments/useAttachmentUploads', () => ({
  useAttachmentUploads: () => ({
    items: [],
    readyAssetIds: [],
    hasBlockingUploads: false,
    addFiles: vi.fn(),
    remove: vi.fn(),
    clearReady: vi.fn(),
  }),
}));

function sseResponse(): Response {
  const enc = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(enc.encode('event: RUN_STARTED\ndata: {"type":"RUN_STARTED"}\n\n'));
      controller.enqueue(
        enc.encode(
          'event: RUN_FINISHED\ndata: {"type":"RUN_FINISHED","outcome":{"type":"success"}}\n\n',
        ),
      );
      controller.close();
    },
  });
  return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
}

function stubFetch(garageFiles: readonly Record<string, unknown>[] = []) {
  const fetchMock = vi.fn((url: unknown) => {
    if (typeof url === 'string' && url.startsWith('/api/filemanager/files')) {
      return Promise.resolve(
        new Response(JSON.stringify(garageFiles), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    }
    if (typeof url === 'string' && url.startsWith('/threads/')) {
      return Promise.resolve(
        new Response(JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    }
    return Promise.resolve(sseResponse());
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

function renderChat(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

function runPostBody(fetchMock: ReturnType<typeof stubFetch>): Record<string, unknown> {
  const post = fetchMock.mock.calls.find((call) => call[0] === '/agent/run') as
    | [string, RequestInit]
    | undefined;
  if (post === undefined) throw new Error('expected /agent/run POST');
  return JSON.parse(post[1].body as string) as Record<string, unknown>;
}

function send(text: string) {
  const input = screen.getByPlaceholderText('Ask Aura');
  fireEvent.change(input, { target: { value: text } });
  fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' });
}

describe('ExternalStoreChat slash-skill', () => {
  beforeEach(() => {
    // jsdom has no scrollIntoView; the picker's active-option effect calls it as soon as
    // the menu opens, which a '/'-text now does in this harness.
    Element.prototype.scrollIntoView = vi.fn();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('carries the skill named in the message on the run envelope', async () => {
    const fetchMock = stubFetch();
    renderChat(<ExternalStoreChat threadId="conv-1" />);

    send('/skill-creator build me a skill');

    await waitFor(() => {
      expect(runPostBody(fetchMock)).toMatchObject({ aura: { skill: 'skill-creator' } });
    });
  });

  it('starts a skill invoked with no instruction at all', async () => {
    const fetchMock = stubFetch();
    renderChat(<ExternalStoreChat threadId="conv-1" />);

    send('/skill-creator ');

    await waitFor(() => {
      expect(runPostBody(fetchMock)).toMatchObject({ aura: { skill: 'skill-creator' } });
    });
  });

  it('sends no aura.skill for a plain message', async () => {
    const fetchMock = stubFetch();
    renderChat(<ExternalStoreChat threadId="conv-1" />);

    send('just a plain message');

    await waitFor(() => {
      expect(fetchMock.mock.calls.some((call) => call[0] === '/agent/run')).toBe(true);
    });
    expect(runPostBody(fetchMock)).not.toHaveProperty('aura');
  });

  it('keeps Garage mentions visible and sends their structural document scope', async () => {
    const fetchMock = stubFetch();
    renderChat(<ExternalStoreChat threadId="conv-1" />);

    send('Review @file:"/finance/q1.pdf" with @folder:"/policies/current" now');

    await waitFor(() => {
      expect(fetchMock.mock.calls.some((call) => call[0] === '/agent/run')).toBe(true);
    });
    expect(runPostBody(fetchMock)).toMatchObject({
      messages: [
        {
          role: 'user',
          content: 'Review @file:"/finance/q1.pdf" with @folder:"/policies/current" now',
        },
      ],
      aura: {
        document_scope: [
          { kind: 'file', path: 'finance/q1.pdf' },
          { kind: 'folder', path: 'policies/current' },
        ],
      },
    });
  });

  it('delegates the @ listbox and keyboard selection to assistant-ui', async () => {
    const fetchMock = stubFetch([{ id: '/finance', type: 'folder', lazy: true }]);
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    const input = screen.getByPlaceholderText<HTMLTextAreaElement>('Ask Aura');

    fireEvent.change(input, { target: { value: '@' } });

    const listbox = await screen.findByRole('listbox', { name: 'Garage files and folders' });
    expect((await within(listbox).findByRole('option')).textContent).toContain('finance');
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' });

    expect(input.value).toBe('@folder:"/finance" ');
    expect(fetchMock.mock.calls.some((call) => call[0] === '/agent/run')).toBe(false);
  });

  // A slash word that names nothing installed must not be turned into a skill: the operator
  // sees their typo in the transcript instead of an invented attachment to the turn.
  it('sends no aura.skill for an unknown slash word', async () => {
    const fetchMock = stubFetch();
    renderChat(<ExternalStoreChat threadId="conv-1" />);

    send('/nope do something');

    await waitFor(() => {
      expect(fetchMock.mock.calls.some((call) => call[0] === '/agent/run')).toBe(true);
    });
    expect(runPostBody(fetchMock)).not.toHaveProperty('aura');
  });
});
