import { afterEach, describe, expect, it, vi } from 'vitest';
import { type Context, type ReactNode, useState } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import '../i18n/i18n';
import { AssistantSpeakerControl } from './ExternalStoreChat_messages';

// The speaker control reads caps from useVoiceMode and the per-message speech state
// from s.message.speech. This test drives BOTH through doubles: a mocked
// voiceModeContext (controllable caps) and a mocked @assistant-ui/react whose
// AuiIf/useAuiState/ActionBarPrimitive read the speech for THIS message off a React
// context — so two messages can hold independent speech and a click on one Speak can
// cancel the other (single active utterance, RESEARCH Landmine #6).

interface SpeechStub {
  readonly messageId: string;
  readonly status?: { readonly type: string; readonly truncated?: boolean };
  readonly truncated?: boolean;
}
interface MsgValue {
  readonly messageId: string;
  readonly speech: SpeechStub | null;
  readonly onSpeak: (id: string) => void;
}

const H = vi.hoisted(
  (): {
    caps: { tts: boolean; stt: boolean };
    ctxRef: { current: unknown };
  } => ({
    caps: { tts: true, stt: false },
    ctxRef: { current: null },
  }),
);

vi.mock('./voice/voiceModeContext', () => ({
  useVoiceMode: () => ({
    caps: H.caps,
    voiceMode: false,
    turnWasDictated: false,
    toggleVoiceMode: () => undefined,
    markTurnDictated: () => undefined,
    clearTurnDictated: () => undefined,
  }),
}));

interface ButtonProps {
  children?: ReactNode;
  'aria-label'?: string;
  className?: string;
  'data-required-touch-target'?: true;
}

vi.mock('@assistant-ui/react', async () => {
  const React = await import('react');
  const MsgCtx = React.createContext<MsgValue>({
    messageId: '',
    speech: null,
    onSpeak: () => undefined,
  });
  H.ctxRef.current = MsgCtx;
  return {
    useAuiState: <T,>(selector: (s: { message: { speech: SpeechStub | null } }) => T): T =>
      selector({ message: { speech: React.useContext(MsgCtx).speech } }),
    AuiIf: ({
      condition,
      children,
    }: {
      condition: (s: { message: { speech: SpeechStub | null } }) => boolean;
      children: ReactNode;
    }) =>
      condition({ message: { speech: React.useContext(MsgCtx).speech } }) ? <>{children}</> : null,
    ActionBarPrimitive: {
      Root: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
      Copy: () => null,
      Reload: () => null,
      Edit: () => null,
      Speak: ({ children, ...props }: ButtonProps) => {
        const m = React.useContext(MsgCtx);
        return (
          <button
            type="button"
            {...props}
            onClick={() => {
              m.onSpeak(m.messageId);
            }}
          >
            {children}
          </button>
        );
      },
      StopSpeaking: ({ children, ...props }: ButtonProps) => (
        <button type="button" {...props}>
          {children}
        </button>
      ),
    },
    ComposerPrimitive: {
      Root: () => null,
      Input: () => null,
      Cancel: () => null,
      Send: () => null,
    },
    MessagePrimitive: { Root: () => null, Parts: () => null, Error: () => null },
  };
});

function Msg({
  id,
  speech,
  onSpeak = () => undefined,
}: {
  id: string;
  speech: SpeechStub | null;
  onSpeak?: (id: string) => void;
}) {
  const MsgCtx = H.ctxRef.current as Context<MsgValue>;
  return (
    <MsgCtx.Provider value={{ messageId: id, speech, onSpeak }}>
      <AssistantSpeakerControl />
    </MsgCtx.Provider>
  );
}

const SPEAK = 'Read aloud';
const STOP = 'Stop reading';
const HINT = 'Message too long to read fully';

function expectRequiredTouchTarget(element: HTMLElement): void {
  const classes = element.getAttribute('class') ?? '';
  expect(element.getAttribute('data-required-touch-target')).not.toBeNull();
  expect(classes).toMatch(/(?:^|\s)(?:min-h-\[44px\]|h-\[44px\])(?:\s|$)/);
  expect(classes).toMatch(/(?:^|\s)(?:min-w-\[44px\]|w-\[44px\])(?:\s|$)/);
}

afterEach(() => {
  H.caps = { tts: true, stt: false };
});

describe('AssistantSpeakerControl (caps.tts-gated, D-04/D-05)', () => {
  it('shows Speak when idle and StopSpeaking when this message is being spoken', () => {
    const { rerender } = render(<Msg id="m1" speech={null} />);
    expectRequiredTouchTarget(screen.getByLabelText(SPEAK));
    expect(screen.queryByLabelText(STOP)).toBeNull();

    rerender(<Msg id="m1" speech={{ messageId: 'm1', status: { type: 'running' } }} />);
    expectRequiredTouchTarget(screen.getByLabelText(STOP));
    expect(screen.queryByLabelText(SPEAK)).toBeNull();
  });

  it('renders nothing when tts is not configured (WEBVOICE-03 degrade)', () => {
    H.caps = { tts: false, stt: false };
    render(<Msg id="m1" speech={null} />);
    expect(screen.queryByLabelText(SPEAK)).toBeNull();
    expect(screen.queryByLabelText(STOP)).toBeNull();
  });

  it('renders the tooLong hint only while the active utterance is truncated (D-05)', () => {
    const { rerender } = render(
      <Msg id="m1" speech={{ messageId: 'm1', status: { type: 'running', truncated: true } }} />,
    );
    expect(screen.getByText(HINT)).toBeTruthy();
    expect(screen.getByRole('note')).toBeTruthy();

    // A non-truncated utterance shows NO hint.
    rerender(
      <Msg id="m1" speech={{ messageId: 'm1', status: { type: 'running', truncated: false } }} />,
    );
    expect(screen.queryByText(HINT)).toBeNull();

    // Not speaking → no hint either.
    rerender(<Msg id="m1" speech={null} />);
    expect(screen.queryByText(HINT)).toBeNull();
  });

  it('honors a truncated flag surfaced at the top level of the speech state', () => {
    render(<Msg id="m1" speech={{ messageId: 'm1', truncated: true }} />);
    expect(screen.getByText(HINT)).toBeTruthy();
  });

  it('enforces a single active utterance — a second Speak cancels the first', () => {
    function TwoMessages() {
      const [speakingId, setSpeakingId] = useState<string | undefined>(undefined);
      const speechFor = (id: string): SpeechStub | null =>
        speakingId === id ? { messageId: id, status: { type: 'running' } } : null;
      return (
        <>
          <Msg id="A" speech={speechFor('A')} onSpeak={setSpeakingId} />
          <Msg id="B" speech={speechFor('B')} onSpeak={setSpeakingId} />
        </>
      );
    }
    render(<TwoMessages />);
    // Both idle → two Speak buttons, no StopSpeaking.
    expect(screen.getAllByLabelText(SPEAK)).toHaveLength(2);
    expect(screen.queryAllByLabelText(STOP)).toHaveLength(0);

    // Speak A → exactly one active utterance.
    const [firstSpeak] = screen.getAllByLabelText(SPEAK);
    if (firstSpeak === undefined) throw new Error('expected an idle Speak button');
    fireEvent.click(firstSpeak);
    expect(screen.getAllByLabelText(STOP)).toHaveLength(1);
    expect(screen.getAllByLabelText(SPEAK)).toHaveLength(1);

    // Speak the OTHER message → the first is cancelled; still exactly ONE StopSpeaking.
    fireEvent.click(screen.getByLabelText(SPEAK));
    expect(screen.getAllByLabelText(STOP)).toHaveLength(1);
    expect(screen.getAllByLabelText(SPEAK)).toHaveLength(1);
  });
});
