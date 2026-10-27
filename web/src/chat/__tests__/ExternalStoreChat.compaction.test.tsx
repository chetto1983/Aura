import type { ReactElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { ExternalStoreChat } from '../ExternalStoreChat';
import type { ComposerSkillRow } from '../composer/api';
import { messagesSnapshotResponse, sendPrompt, sseResponse, withTurnSeqs } from './chatTestHarness';

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

const LINEAR = [
  { id: 'msg-1', role: 'user', content: 'quali skill hai' },
  { id: 'msg-2', role: 'assistant', content: 'skill-creator' },
];

// Two roots: the question as first asked (1, 2) and rewritten (3, 4), written last.
const TWO_BRANCHES = [
  { id: 'msg-1', parentId: null, role: 'user', content: 'first question' },
  { id: 'msg-2', parentId: 'msg-1', role: 'assistant', content: 'first answer' },
  { id: 'msg-3', parentId: null, role: 'user', content: 'question rewritten' },
  { id: 'msg-4', parentId: 'msg-3', role: 'assistant', content: 'answer to the rewrite' },
];

const COMPACTED = {
  covers_through_seq: 2,
  source_turns: 2,
  summary: 'The operator asked about skills; Aura listed them.',
  tokens_before: 41000,
  tokens_after: 2600,
};

const NONE = { covers_through_seq: 0, source_turns: 0, summary: '' };

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function answer(text: string, headSeq: number, leafSeq: number) {
  return withTurnSeqs(
    [
      { type: 'RUN_STARTED', threadId: 'conv-1', runId: `run-${text}` },
      { type: 'TEXT_MESSAGE_START', messageId: `m-${text}` },
      { type: 'TEXT_MESSAGE_CONTENT', messageId: `m-${text}`, delta: text },
      { type: 'TEXT_MESSAGE_END', messageId: `m-${text}` },
      { type: 'RUN_FINISHED', threadId: 'conv-1', runId: `run-${text}` },
    ],
    headSeq,
    leafSeq,
  );
}

interface StubOptions {
  readonly history?: readonly Record<string, unknown>[];
  readonly compactStatus?: number;
  /** The marker each read answers, by its leaf_seq ('' when it names none). */
  readonly marks?: Readonly<Record<string, unknown>>;
  /** A read of these leaves waits for its gate. */
  readonly gates?: ReadonlyMap<string, Promise<void>>;
}

/** The chat lane's reads, the compaction routes by leaf, and one run. */
function stubFetch({
  history = LINEAR,
  compactStatus = 200,
  marks = {},
  gates = new Map(),
}: StubOptions = {}) {
  const fetchMock = vi.fn((url: unknown, init?: RequestInit) => {
    const raw = typeof url === 'string' ? url : '';
    const { pathname, searchParams } = new URL(raw, 'http://cockpit.test');
    const leaf = searchParams.get('leaf_seq') ?? '';
    if (pathname.startsWith('/threads/')) return Promise.resolve(messagesSnapshotResponse(history));
    if (pathname.endsWith('/compaction')) {
      const gate = gates.get(leaf) ?? Promise.resolve();
      return gate.then(() => json(marks[leaf] ?? NONE));
    }
    if (pathname.endsWith('/compact') && init?.method === 'POST') {
      return Promise.resolve(compactStatus === 200 ? json(COMPACTED) : json({}, compactStatus));
    }
    if (pathname === '/agent/run') return Promise.resolve(sseResponse(answer('more', 3, 4)));
    if (pathname.startsWith('/api/assets')) return Promise.resolve(json([]));
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

function compactions(fetchMock: ReturnType<typeof stubFetch>): string[] {
  return calledPaths(fetchMock).filter((path) => path.includes('/compact'));
}

function held(): { readonly gate: Promise<void>; readonly release: () => void } {
  let release: () => void = () => undefined;
  const gate = new Promise<void>((done) => {
    release = done;
  });
  return { gate, release };
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
      expect(calledPaths(fetchMock)).toContain('/api/conversations/conv-1/compact?leaf_seq=2');
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
    stubFetch({ compactStatus: 409 });
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
    expect(compactions(fetchMock).filter((path) => path.includes('/compact?'))).toEqual([]);
  });

  // Spec 2026-10-27, D13: the branch on screen is compacted and marked, not the one written
  // last.
  it('compacts and marks the branch on screen after Previous', async () => {
    const fetchMock = stubFetch({ history: TWO_BRANCHES });
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByText('answer to the rewrite');
    fireEvent.click(screen.getByRole('button', { name: 'Previous branch' }));
    await screen.findByText('first answer');

    send('/compact');

    await screen.findByTestId('compaction-marker');
    expect(compactions(fetchMock)).toEqual([
      '/api/conversations/conv-1/compaction?leaf_seq=4',
      '/api/conversations/conv-1/compaction?leaf_seq=2',
      '/api/conversations/conv-1/compact?leaf_seq=2',
    ]);
  });

  // D14: a run moves the leaf, and the marker is read again for it. Until that read answers,
  // the marker of the branch it grew from still describes turns on screen.
  it('keeps the marker on screen while a run grows the branch', async () => {
    const latest = held();
    const grown = held();
    const fetchMock = stubFetch({
      marks: { '2': COMPACTED, '': COMPACTED, '4': COMPACTED },
      gates: new Map([
        ['', latest.gate],
        ['4', grown.gate],
      ]),
    });
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByTestId('compaction-marker');

    sendPrompt('and the others?');
    await screen.findByText('more');
    await waitFor(() => {
      expect(compactions(fetchMock)).toContain('/api/conversations/conv-1/compaction?leaf_seq=4');
    });

    expect(screen.getByTestId('compaction-marker')).toBeTruthy();
    await act(async () => {
      latest.release();
      grown.release();
      await Promise.resolve();
    });
    expect(screen.getByTestId('compaction-marker')).toBeTruthy();
  });

  // D14: the marker of the branch left describes turns that are no longer on screen.
  it('drops the marker of the branch left on a switch', async () => {
    const other = held();
    stubFetch({
      history: TWO_BRANCHES,
      marks: { '4': { ...COMPACTED, covers_through_seq: 4 } },
      gates: new Map([['2', other.gate]]),
    });
    renderChat(<ExternalStoreChat threadId="conv-1" />);
    await screen.findByTestId('compaction-marker');

    fireEvent.click(screen.getByRole('button', { name: 'Previous branch' }));
    await screen.findByText('first answer');

    expect(screen.queryByTestId('compaction-marker')).toBeNull();
    other.release();
  });
});
