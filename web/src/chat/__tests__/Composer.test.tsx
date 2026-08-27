import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import '../../i18n/i18n';
import { Composer } from '../Composer';
import type { ComposerSkillRow } from '../composer/api';
import { stubGetUserMedia, stubMediaRecorder, type GetUserMediaStub } from '../voice/voiceMocks';

// Shared mutable doubles for the mocked runtime + voice-mode context. vi.hoisted runs
// before the (hoisted) vi.mock factories, so both can close over `h`; tests mutate
// h.caps / h.auiState to drive the caps.stt branch and the dictation lifecycle.
type MockDictation = { status: { type: string } } | undefined;

const h = vi.hoisted(() => {
  const addAttachment = vi.fn();
  const composer: {
    dictation: MockDictation;
    isEditing: boolean;
    text: string;
    attachments: never[];
  } = {
    dictation: undefined,
    isEditing: true,
    text: '',
    attachments: [],
  };
  const caps: { tts: boolean; stt: boolean } = { tts: false, stt: false };
  return {
    setText: vi.fn(),
    startDictation: vi.fn(),
    stopDictation: vi.fn(),
    markTurnDictated: vi.fn(),
    auiState: { thread: { isRunning: false, capabilities: { dictation: true } }, composer },
    addAttachment,
    caps,
  };
});

vi.mock('@assistant-ui/react', async () => ({
  ...(await import('./composerPrimitiveMock')).spread(h),
  useAui: () => ({
    // assistant-ui 0.15 exposes the scopes as PROPERTIES (aui.composer), not calls.
    composer: {
      setText: h.setText,
      addAttachment: h.addAttachment,
      startDictation: h.startDictation,
      stopDictation: h.stopDictation,
      getState: () => h.auiState.composer,
    },
  }),
  useAuiState: <T,>(selector: (state: typeof h.auiState) => T): T => selector(h.auiState),
}));

vi.mock('../voice/voiceModeContext', () => ({
  useVoiceMode: () => ({
    caps: h.caps,
    voiceMode: false,
    turnWasDictated: false,
    toggleVoiceMode: vi.fn(),
    markTurnDictated: h.markTurnDictated,
    clearTurnDictated: vi.fn(),
  }),
}));

let mediaStub: GetUserMediaStub | undefined;

beforeEach(() => {
  vi.resetAllMocks();
  h.caps = { tts: false, stt: false };
  h.auiState.thread = { isRunning: false, capabilities: { dictation: true } };
  h.auiState.composer = { dictation: undefined, isEditing: true, text: '', attachments: [] };
  // jsdom has no scrollIntoView; the SkillPicker's active-option JS-scroll (Pitfall 6) needs it.
  Element.prototype.scrollIntoView = vi.fn();
});

afterEach(() => {
  mediaStub?.restore();
  mediaStub = undefined;
  vi.unstubAllGlobals();
});

describe('Composer attachments', () => {
  it('prefills a document draft prompt and focuses the editable input', () => {
    render(<Composer draftPrompt={{ text: 'Answer from Manual.pdf', nonce: 1 }} />);

    expect(h.setText).toHaveBeenCalledWith('Answer from Manual.pdf');
    expect(document.activeElement).toBe(screen.getByLabelText('Ask Aura'));
  });

  // Paste and drop are no longer Aura's. ComposerPrimitive.Input pastes files itself
  // (addAttachmentOnPaste, default true) and ComposerPrimitive.AttachmentDropzone owns the
  // drag sequence, so the two tests that used to fire a paste and a drop at this component
  // were exercising handlers that had to exist ALONGSIDE the library's — and for paste that
  // pairing was a defect, not a duplicate: both fired, and one pasted file became two
  // attachments. What is left to assert is the wiring, since the behaviour is the library's.
  it('wraps the composer in the dropzone and hands it the lock', () => {
    const { rerender } = render(<Composer />);

    const shell = screen.getByTestId('chat-composer');
    expect(shell.getAttribute('data-disabled')).toBeNull();

    rerender(<Composer approvalLocked />);

    expect(screen.getByTestId('chat-composer').getAttribute('data-disabled')).toBe('true');
  });

  it('disables send while an upload is still running', () => {
    h.auiState.composer.attachments = [
      {
        id: 'a1',
        type: 'document',
        name: 'big.pdf',
        status: { type: 'running', reason: 'uploading', progress: 0.3 },
      },
    ] as never;
    render(<Composer />);

    const send = screen.getByLabelText('Send message');
    expect(send).toHaveProperty('disabled', true);
    expect(send.getAttribute('data-slot')).toBe('button');
  });

  it('keeps the text entry target at the 44px mobile floor', () => {
    render(<Composer />);

    expect(screen.getByLabelText('Ask Aura').className).toContain('min-h-[44px]');
  });
});

describe('Composer approval lock', () => {
  it('exposes a stable localized lock relationship and natively disables every primary action', () => {
    h.auiState.composer.text = '/';
    const onEffortChange = vi.fn();
    const { rerender } = render(
      <Composer
        approvalLocked
        skills={SKILLS}
        effort="auto"
        effortLevels={['auto', 'off']}
        onEffortChange={onEffortChange}
      />,
    );

    const root = screen.getByTestId('chat-composer');
    const hintId = root.getAttribute('aria-describedby');
    expect(root.getAttribute('aria-disabled')).toBe('true');
    expect(hintId).not.toBeNull();
    expect(document.getElementById(hintId ?? '')?.textContent).toBe(
      'Answer the request above to continue.',
    );
    expect(screen.getByPlaceholderText('Ask Aura')).toHaveProperty('disabled', true);
    expect(screen.getByRole('button', { name: 'Add files' })).toHaveProperty('disabled', true);
    expect(screen.getByRole('button', { name: 'Record audio' })).toHaveProperty('disabled', true);
    expect(screen.getByRole('button', { name: 'Send message' })).toHaveProperty('disabled', true);
    expect(screen.getByRole('combobox', { name: 'Reasoning effort' })).toHaveProperty(
      'disabled',
      true,
    );
    expect(screen.queryByRole('listbox')).toBeNull();

    // The file input itself belongs to ComposerPrimitive.AddAttachment; the disabled button
    // above is the control Aura renders and therefore the one it can be held to.
    fireEvent.paste(root, {
      clipboardData: { files: [new File(['x'], 'blocked-paste.txt', { type: 'text/plain' })] },
    });
    expect(h.addAttachment).not.toHaveBeenCalled();

    rerender(
      <Composer
        approvalLocked
        skills={SKILLS}
        effort="auto"
        effortLevels={['auto', 'off']}
        onEffortChange={onEffortChange}
      />,
    );
    expect(screen.getByTestId('chat-composer').getAttribute('aria-describedby')).toBe(hintId);
  });

  it('disables the running Cancel action while approval-locked', () => {
    h.auiState.thread = { ...h.auiState.thread, isRunning: true };
    render(<Composer approvalLocked />);

    expect(screen.getByRole('button', { name: 'Stop the current response' })).toHaveProperty(
      'disabled',
      true,
    );
  });

  // The chip and its remove control are rendered by ComposerPrimitive.Attachments from
  // runtime state, so the lock is asserted where Aura still owns it — the attach button —
  // and the chip's own disabled wiring is covered in AttachmentChip.test.
  it('literally disables the attach action while approval-locked', () => {
    render(<Composer approvalLocked />);

    expect(screen.getByLabelText('Add files')).toHaveProperty('disabled', true);
  });
});

describe('Composer dictation', () => {
  it('caps.stt=false → the Mic records an audio attachment (no regression, WEBVOICE-04)', async () => {
    stubMediaRecorder();
    mediaStub = stubGetUserMedia();
    render(<Composer />);

    fireEvent.click(screen.getByLabelText('Record audio'));
    await waitFor(() => screen.getByLabelText('Stop recording'));
    fireEvent.click(screen.getByLabelText('Stop recording'));

    expect(mediaStub.getUserMedia).toHaveBeenCalledTimes(1);
    // The voice note goes through the same adapter as any other attachment (D-10).
    expect(h.addAttachment).toHaveBeenCalledTimes(1);
    expect(h.startDictation).not.toHaveBeenCalled(); // never dictation when STT is off
  });

  it('caps.stt=true → the Mic starts a runtime dictation session (not an attachment)', () => {
    h.caps = { tts: false, stt: true };
    render(<Composer />);

    fireEvent.click(screen.getByLabelText('Dictate'));

    expect(h.startDictation).toHaveBeenCalledTimes(1);
  });

  it('marks the turn dictated when a transcript is inserted (auto-speak parity, D-07)', () => {
    h.caps = { tts: false, stt: true };
    h.auiState.composer.text = '';
    const { rerender } = render(<Composer />);

    fireEvent.click(screen.getByLabelText('Dictate'));
    // Runtime opens the session (dictation state becomes non-null).
    h.auiState.composer.dictation = { status: { type: 'running' } };
    rerender(<Composer />);

    fireEvent.click(screen.getByLabelText('Stop dictation'));
    expect(h.stopDictation).toHaveBeenCalledTimes(1);

    // The adapter's onSpeech inserts the transcript, then the session tears down.
    h.auiState.composer.text = 'hello world';
    h.auiState.composer.dictation = undefined;
    rerender(<Composer />);

    expect(h.markTurnDictated).toHaveBeenCalledTimes(1);
  });

  it('announces listening → transcribing via an aria-live region', () => {
    h.caps = { tts: false, stt: true };
    render(<Composer />);
    const status = screen.getByRole('status');

    fireEvent.click(screen.getByLabelText('Dictate'));
    expect(status.textContent).toContain('Listening');
    expect(status.getAttribute('aria-live')).toBe('polite');

    fireEvent.click(screen.getByLabelText('Stop dictation'));
    expect(status.textContent).toContain('Transcribing');
  });

  it('announces an error when a session ends without inserting a transcript (empty/4xx)', () => {
    h.caps = { tts: false, stt: true };
    h.auiState.composer.text = '';
    const { rerender } = render(<Composer />);
    const status = screen.getByRole('status');

    fireEvent.click(screen.getByLabelText('Dictate'));
    h.auiState.composer.dictation = { status: { type: 'running' } };
    rerender(<Composer />);
    fireEvent.click(screen.getByLabelText('Stop dictation'));

    // Session tears down with the composer text unchanged (nothing was transcribed).
    h.auiState.composer.dictation = undefined;
    rerender(<Composer />);

    expect(h.markTurnDictated).not.toHaveBeenCalled();
    expect(status.textContent).toContain('No transcription');
  });

  // D-10, driven by the signal production reads. The mic used to try startDictation and fall
  // back in the catch; the primitive does not throw when there is no adapter, it renders a
  // DISABLED button — which is the dead end D-10 forbids — so availability is asked up front
  // (the thread's dictation CAPABILITY) and the recorder branch is rendered instead.
  it('caps.stt=true but dictation unavailable → degrades to the attachment record path (D-10)', async () => {
    h.caps = { tts: false, stt: true };
    h.auiState.thread = { ...h.auiState.thread, capabilities: { dictation: false } };
    stubMediaRecorder();
    mediaStub = stubGetUserMedia();
    render(<Composer />);

    fireEvent.click(screen.getByLabelText('Record audio'));

    await waitFor(() => {
      expect(mediaStub?.getUserMedia).toHaveBeenCalledTimes(1);
    });
    expect(h.startDictation).not.toHaveBeenCalled();
  });
});

const SKILL_CREATOR: ComposerSkillRow = {
  name: 'skill-creator',
  description: 'Create a new skill',
  type: 'instruction',
};
const CODEQL: ComposerSkillRow = {
  name: 'codeql',
  description: 'Static analysis',
  type: 'executable',
};
const SKILLS: readonly ComposerSkillRow[] = [SKILL_CREATOR, CODEQL];

// The '/'-menu is ComposerPrimitive.Unstable_TriggerPopover now, so its open/close
// lifecycle, incremental filter, keyboard navigation and combobox ARIA are the library's
// contract and are not re-asserted here — that would be testing the dependency. What Aura
// still owns is WHICH items exist and how a chosen one is written into the message, covered
// in composer/skillTrigger.test.ts, plus the mount decision below.
//
// Deleted with the hand-rolled picker, deliberately: the three quick commands. A trigger
// popover carries ONE behavior, and the operator asked for skills to be inserted into the
// message (Directive), which cannot coexist with commands that execute (Action) on the same
// char. add-files remains the paperclip button and new-chat the sidebar button; clear had no
// counterpart and is gone.
describe('Composer skill trigger', () => {
  it('mounts the trigger when skills are installed', () => {
    render(<Composer skills={SKILLS} />);

    expect(
      screen.getAllByTestId('trigger-popover').map((node) => node.getAttribute('data-char')),
    ).toEqual(['/', '@']);
  });

  it('degrades to a no-op when the skills list is empty (D-09)', () => {
    render(<Composer skills={[]} />);

    expect(
      screen.getAllByTestId('trigger-popover').map((node) => node.getAttribute('data-char')),
    ).toEqual(['@']);
  });

  it('renders no pinned-skill chip above the composer', () => {
    render(<Composer skills={SKILLS} />);

    expect(screen.queryByLabelText('Remove pinned skill skill-creator')).toBeNull();
  });
});

describe('Composer reasoning-effort selector', () => {
  it('renders EXACTLY the advertised levels (dynamic, D-13 — never the full 7)', () => {
    render(<Composer effort="auto" effortLevels={['auto', 'off', 'high']} />);
    const select = screen.getByRole('combobox', { name: 'Reasoning effort' });
    const options = within(select)
      .getAllByRole('option')
      .map((o) => o.textContent);
    expect(options).toEqual(['Auto', 'Off', 'High']);
    // A level the model did NOT advertise is absent (not a placebo).
    expect(within(select).queryByText('Medium')).toBeNull();
    expect(within(select).queryByText('Max')).toBeNull();
  });

  it('shows the hydrated value as the current selection', () => {
    render(<Composer effort="high" effortLevels={['auto', 'off', 'high']} />);
    expect(screen.getByRole('combobox', { name: 'Reasoning effort' })).toHaveProperty(
      'value',
      'high',
    );
  });

  it('calls onEffortChange when a level is picked', () => {
    const onEffortChange = vi.fn();
    render(
      <Composer
        effort="auto"
        effortLevels={['auto', 'off', 'high']}
        onEffortChange={onEffortChange}
      />,
    );
    fireEvent.change(screen.getByRole('combobox', { name: 'Reasoning effort' }), {
      target: { value: 'high' },
    });
    expect(onEffortChange).toHaveBeenCalledWith('high');
  });

  it('is NOT rendered when no levels are provided (Composer mounted without the effort wiring)', () => {
    render(<Composer />);
    expect(screen.queryByRole('combobox', { name: 'Reasoning effort' })).toBeNull();
  });

  it('does not reclassify the message input — the idle textbox is preserved (no regression)', () => {
    render(<Composer effort="auto" effortLevels={['auto', 'off', 'high']} />);
    // The selector is a separate combobox; the message input stays a plain textbox (shell.spec).
    expect(screen.getByRole('textbox', { name: 'Ask Aura' })).toBeTruthy();
    expect(screen.getByLabelText('Ask Aura').getAttribute('role')).toBeNull();
  });
});
