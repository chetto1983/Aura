import { act, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useCallback, type ReactElement, type RefObject } from 'react';
import '../../i18n/i18n';
import type { Approval } from '../../approvals/useApprovals';
import { ExternalStoreChat } from '../ExternalStoreChat';

const h = vi.hoisted(() => ({
  approvals: [] as Approval[],
  isPending: true,
  inputAvailable: true,
  focusRequested: undefined as ((next: Approval | undefined) => void) | undefined,
  onResolved: vi.fn(),
  onResolutionStarted: vi.fn(),
  onResolutionFailed: vi.fn(),
}));

vi.mock('../../approvals/useThreadApprovals', () => ({
  useThreadApprovals: (
    _threadId: string,
    _onResume: () => Promise<void>,
    onFocusRequested: (next: Approval | undefined) => void,
  ) => {
    h.focusRequested = onFocusRequested;
    return {
      approvals: h.approvals,
      isPending: h.isPending,
      onResolved: h.onResolved,
      onResolutionStarted: h.onResolutionStarted,
      onResolutionFailed: h.onResolutionFailed,
    };
  },
}));

vi.mock('../../approvals/ThreadApprovalCards', () => ({
  ThreadApprovalCards: ({ approvals }: { readonly approvals: readonly Approval[] }) => (
    <div>
      {approvals.map((approval) => (
        <button key={approval.token} type="button" data-approval-token={approval.token}>
          {approval.question}
        </button>
      ))}
    </div>
  ),
}));

vi.mock('../Composer', () => ({
  Composer: ({
    inputRef,
    approvalLocked,
    onInputAvailable,
  }: {
    readonly inputRef: RefObject<HTMLTextAreaElement | null>;
    readonly approvalLocked: boolean;
    readonly onInputAvailable: (input: HTMLTextAreaElement | null) => void;
  }) => {
    const setInput = useCallback(
      (input: HTMLTextAreaElement | null) => {
        inputRef.current = input;
        onInputAvailable(input);
      },
      [inputRef, onInputAvailable],
    );
    return h.inputAvailable ? (
      <textarea ref={setInput} data-testid="focus-composer" disabled={approvalLocked} />
    ) : null;
  },
}));

vi.mock('../../conversations/useConversations', () => ({
  CONVERSATION_KEY: 'conversation',
  CONVERSATION_ROT_EVENTS_KEY: 'conversation-rot-events',
  useConversation: () => ({ data: undefined }),
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

vi.mock('../composer/useComposerSkills', () => ({ useComposerSkills: () => [] }));
vi.mock('../composer/usePinnedSkill', () => ({
  usePinnedSkill: () => ({ pinnedSkill: null, setPinnedSkill: vi.fn() }),
}));
vi.mock('../composer/useReasoningCapabilities', () => ({
  useReasoningCapabilities: () => ({ levels: ['auto'] }),
}));
vi.mock('../composer/useReasoningEffort', () => ({
  useReasoningEffort: () => ({ effort: 'auto', setEffort: vi.fn() }),
}));
vi.mock('../voice/useVoiceRuntime', () => ({ useVoiceRuntime: () => undefined }));
vi.mock('../voice/AutoSpeak', () => ({ AutoSpeak: () => null }));

function row(token: string, conversationId = 'a'): Approval {
  return {
    token,
    conversation_id: conversationId,
    kind: 'clarification',
    question: token,
    priority: 0,
  };
}

interface FrameQueue {
  readonly callbacks: Map<number, FrameRequestCallback>;
  readonly request: ReturnType<typeof vi.fn>;
  readonly cancel: ReturnType<typeof vi.fn>;
  readonly runNext: () => void;
}

function installFrameQueue(): FrameQueue {
  const callbacks = new Map<number, FrameRequestCallback>();
  let nextId = 1;
  const request = vi.fn((callback: FrameRequestCallback) => {
    const id = nextId;
    nextId += 1;
    callbacks.set(id, callback);
    return id;
  });
  const cancel = vi.fn((id: number) => {
    callbacks.delete(id);
  });
  vi.stubGlobal('requestAnimationFrame', request);
  vi.stubGlobal('cancelAnimationFrame', cancel);
  return {
    callbacks,
    request,
    cancel,
    runNext() {
      const next = callbacks.entries().next().value;
      if (next === undefined) throw new Error('expected a queued animation frame');
      callbacks.delete(next[0]);
      act(() => {
        next[1](0);
      });
    },
  };
}

function renderFocusChat(threadId: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const view = (id: string): ReactElement => (
    <QueryClientProvider client={client}>
      <ExternalStoreChat threadId={id} />
    </QueryClientProvider>
  );
  const rendered = render(view(threadId));
  return {
    ...rendered,
    rerenderThread(id: string) {
      rendered.rerender(view(id));
    },
  };
}

function requestFocus(next: Approval | undefined) {
  const callback = h.focusRequested;
  if (callback === undefined) throw new Error('focus callback was not registered');
  act(() => {
    callback(next);
  });
}

beforeEach(() => {
  h.approvals = [];
  h.isPending = true;
  h.inputAvailable = true;
  h.focusRequested = undefined;
  h.onResolved.mockReset();
  h.onResolutionStarted.mockReset();
  h.onResolutionFailed.mockReset();
  vi.stubGlobal(
    'fetch',
    vi.fn(() => new Promise<Response>(() => undefined)),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('ExternalStoreChat approval focus ownership', () => {
  it('holds composer focus while locked and schedules it exactly once after same-thread unlock', () => {
    const frames = installFrameQueue();
    const view = renderFocusChat('a');

    requestFocus(undefined);
    expect(frames.request).not.toHaveBeenCalled();
    expect(frames.callbacks.size).toBe(0);

    h.isPending = false;
    view.rerenderThread('a');
    expect(frames.request).toHaveBeenCalledTimes(1);
    expect(frames.callbacks.size).toBe(1);
    frames.runNext();

    expect(document.activeElement).toBe(screen.getByTestId('focus-composer'));
    expect(frames.callbacks.size).toBe(0);
  });

  it('never recursively schedules frames while the composer remains disabled', () => {
    const frames = installFrameQueue();
    renderFocusChat('a');

    requestFocus(undefined);
    for (let count = 0; count < 4 && frames.callbacks.size > 0; count += 1) {
      frames.runNext();
    }

    expect(frames.request.mock.calls.length).toBeLessThanOrEqual(1);
    expect(frames.callbacks.size).toBe(0);
  });

  it('waits for an input-availability commit without polling animation frames', () => {
    h.isPending = false;
    h.inputAvailable = false;
    const frames = installFrameQueue();
    const view = renderFocusChat('a');

    requestFocus(undefined);
    expect(frames.request).not.toHaveBeenCalled();

    h.inputAvailable = true;
    view.rerenderThread('a');
    expect(frames.request).toHaveBeenCalledTimes(1);
    frames.runNext();
    expect(document.activeElement).toBe(screen.getByTestId('focus-composer'));
  });

  it('invalidates queued composer focus when a new approval locks the thread', () => {
    h.isPending = false;
    const frames = installFrameQueue();
    const view = renderFocusChat('a');
    requestFocus(undefined);
    expect(frames.callbacks.size).toBe(1);

    h.isPending = true;
    view.rerenderThread('a');

    expect(frames.cancel).toHaveBeenCalledTimes(1);
    expect(frames.callbacks.size).toBe(0);
    expect(document.activeElement).not.toBe(screen.getByTestId('focus-composer'));
  });

  it('keeps only the newest card-focus request and runs one frame', () => {
    const first = row('first');
    const second = row('second');
    h.approvals = [first, second];
    const frames = installFrameQueue();
    renderFocusChat('a');

    requestFocus(first);
    requestFocus(second);

    expect(frames.request).toHaveBeenCalledTimes(2);
    expect(frames.cancel).toHaveBeenCalledTimes(1);
    expect(frames.callbacks.size).toBe(1);
    frames.runNext();
    expect(
      document.activeElement?.closest('[data-approval-token]')?.getAttribute('data-approval-token'),
    ).toBe('second');
    expect(frames.callbacks.size).toBe(0);
  });

  it('cancels a queued A frame during the B commit and cancels B work on unmount', () => {
    h.isPending = false;
    const frames = installFrameQueue();
    const view = renderFocusChat('a');
    requestFocus(undefined);
    expect(frames.callbacks.size).toBe(1);

    view.rerenderThread('b');
    expect(frames.callbacks.size).toBe(0);
    expect(frames.cancel).toHaveBeenCalledTimes(1);

    requestFocus(undefined);
    expect(frames.callbacks.size).toBe(1);
    view.unmount();
    expect(frames.callbacks.size).toBe(0);
    expect(frames.cancel).toHaveBeenCalledTimes(2);
  });
});
