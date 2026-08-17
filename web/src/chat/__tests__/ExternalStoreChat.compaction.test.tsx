import type { ReactElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { ExternalStoreChat } from '../ExternalStoreChat';
import type { ComposerSkillRow } from '../composer/api';

// The `/compact` command end-to-end through the chat lane: typed into the composer, it runs
// the compaction instead of becoming a turn, and the marker lands in the transcript at the
// watermark the server answered with.
//
// A command reaching /agent/run is the failure this file exists to prevent — '/compact' would
// arrive at the model as a question, and the operator would get an answer about compaction
// rather than a compacted conversation.

const SKILLS: readonly ComposerSkillRow[] = [
  { name: 'skill-creator', description: 'Create a new skill', type: 'instruction' },
];

vi.mock('../composer/useComposerSkills', () => ({
  useComposerSkills: () => SKILLS,
}));

const SNAPSHOT = {
  type: 'MESSAGES_SNAPSHOT',
  messages: [
    { id: 'msg-1', role: 'user', content: 'quali skill hai' },
    { id: 'msg-2', role: 'assistant', content: 'skill-creator' },
  ],
};

const COMPACTED = {
  covers_through_seq: 2,
  source_turns: 2,
  summary: 'The operator asked about skills; Aura listed them.',
  tokens_before: 41000,
  tokens_after: 2600,
};

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

/** The chat lane's reads, plus a compaction POST whose status the test picks. */
function stubFetch(compactStatus = 200) {
  const fetchMock = vi.fn((url: unknown, init?: RequestInit) => {
    const path = typeof url === 'string' ? url : '';
    if (path.startsWith('/threads/')) return Promise.resolve(json(SNAPSHOT));
    if (path.endsWith('/compaction')) {
      return Promise.resolve(json({ covers_through_seq: 0, source_turns: 0, summary: '' }));
    }
    if (path.endsWith('/compact') && init?.method === 'POST') {
      return Promise.resolve(compactStatus === 200 ? json(COMPACTED) : json({}, compactStatus));
    }
    if (path.startsWith('/api/assets')) return Promise.resolve(json([]));
    return Promise.resolve(json({}));
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

function renderChat(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

function send(text: string) {
  const input = screen.getByPlaceholderText('Ask Aura');
  fireEvent.change(input, { target: { value: text } });
  fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' });
}

function calledPaths(fetchMock: ReturnType<typeof stubFetch>): string[] {
  return fetchMock.mock.calls.map((call) => String(call[0]));
}

describe('ExternalStoreChat /compact', () => {
  beforeEach(() => {
    // jsdom has no scrollIntoView; the '/' menu's active-option effect calls it on open.
    Element.prototype.scrollIntoView = vi.fn();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('runs the compaction and marks the transcript, sending no turn', async () => {
    const fetchMock = stubFetch();
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('skill-creator');
    expect(screen.queryByTestId('compaction-marker')).toBeNull();

    send('/compact');

    await waitFor(() => {
      expect(calledPaths(fetchMock)).toContain('/api/conversations/conv-1/compact');
    });
    const marker = await screen.findByTestId('compaction-marker');
    expect(marker.textContent).toContain('Context compacted');
    expect(marker.textContent).toContain('2 earlier turns');
    // A command is a verb the composer performs, never a message the agent answers.
    expect(calledPaths(fetchMock)).not.toContain('/agent/run');
  });

  // The composer clears the command as it runs, so a failure that said nothing would look
  // exactly like a compaction that worked.
  it('says why a compaction did not happen', async () => {
    stubFetch(409);
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('skill-creator');

    send('/compact');

    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('Nothing to compact yet');
    expect(screen.queryByTestId('compaction-marker')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }));
    await waitFor(() => {
      expect(screen.queryByRole('alert')).toBeNull();
    });
  });

  it('leaves an ordinary message a message', async () => {
    const fetchMock = stubFetch();
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('skill-creator');

    send('compact the report please');

    await waitFor(() => {
      expect(calledPaths(fetchMock)).toContain('/agent/run');
    });
    expect(calledPaths(fetchMock)).not.toContain('/api/conversations/conv-1/compact');
  });
});
