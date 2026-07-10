import type { ReactElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { ExternalStoreChat } from '../ExternalStoreChat';
import type { ComposerSkillRow } from '../composer/api';

// The pinned skill is set by the picker into the usePinnedSkill seam; here we mock that seam so
// the run drives with a known pinned skill (or none) and assert the aura.skill wire path +
// clear-after-send — mirroring the attachments send-path harness.
const h = vi.hoisted(() => ({
  pinnedSkill: null as ComposerSkillRow | null,
  setPinnedSkill: vi.fn(),
}));

vi.mock('../composer/usePinnedSkill', () => ({
  usePinnedSkill: () => ({ pinnedSkill: h.pinnedSkill, setPinnedSkill: h.setPinnedSkill }),
}));

vi.mock('../composer/useComposerSkills', () => ({
  useComposerSkills: () => [],
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

function stubFetch() {
  const fetchMock = vi.fn((url: unknown) => {
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

describe('ExternalStoreChat pinned skill', () => {
  beforeEach(() => {
    h.pinnedSkill = null;
    h.setPinnedSkill.mockClear();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('carries the pinned skill on the run envelope and clears the pill after send', async () => {
    h.pinnedSkill = {
      name: 'skill-creator',
      description: 'Create a new skill',
      type: 'instruction',
    };
    const fetchMock = stubFetch();
    renderChat(<ExternalStoreChat threadId="conv-1" />);

    send('build me a skill');

    await waitFor(() => {
      expect(runPostBody(fetchMock)).toMatchObject({ aura: { skill: 'skill-creator' } });
      expect(h.setPinnedSkill).toHaveBeenCalledWith(null);
    });
  });

  it('sends no aura.skill and does not clear when nothing is pinned', async () => {
    const fetchMock = stubFetch();
    renderChat(<ExternalStoreChat threadId="conv-1" />);

    send('just a plain message');

    await waitFor(() => {
      expect(fetchMock.mock.calls.some((call) => call[0] === '/agent/run')).toBe(true);
    });
    expect(runPostBody(fetchMock)).not.toHaveProperty('aura');
    expect(h.setPinnedSkill).not.toHaveBeenCalled();
  });
});
