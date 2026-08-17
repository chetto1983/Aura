import type { ButtonHTMLAttributes, HTMLAttributes, TextareaHTMLAttributes } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createEvent, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import '../../i18n/i18n';
import { Composer } from '../Composer';
import type { ComposerSkillRow } from '../composer/api';
import type { AttachmentUploads } from '../attachments/useAttachmentUploads';
import { stubGetUserMedia, stubMediaRecorder, type GetUserMediaStub } from '../voice/voiceMocks';

// Shared mutable doubles for the mocked runtime + voice-mode context. vi.hoisted runs
// before the (hoisted) vi.mock factories, so both can close over `h`; tests mutate
// h.caps / h.auiState to drive the caps.stt branch and the dictation lifecycle.
type MockDictation = { status: { type: string } } | undefined;

const h = vi.hoisted(() => {
  const composer: { dictation: MockDictation; text: string } = { dictation: undefined, text: '' };
  const caps: { tts: boolean; stt: boolean } = { tts: false, stt: false };
  return {
    setText: vi.fn(),
    startDictation: vi.fn(),
    stopDictation: vi.fn(),
    markTurnDictated: vi.fn(),
    auiState: { thread: { isRunning: false }, composer },
    caps,
  };
});

vi.mock('@assistant-ui/react', () => ({
  useAui: () => ({
    composer: () => ({
      setText: h.setText,
      startDictation: h.startDictation,
      stopDictation: h.stopDictation,
      getState: () => h.auiState.composer,
    }),
  }),
  useAuiState: <T,>(selector: (state: typeof h.auiState) => T): T => selector(h.auiState),
  ComposerPrimitive: {
    Root: (props: HTMLAttributes<HTMLDivElement>) => <div {...props} />,
    Input: (props: TextareaHTMLAttributes<HTMLTextAreaElement>) => <textarea {...props} />,
    Cancel: (props: ButtonHTMLAttributes<HTMLButtonElement>) => <button type="button" {...props} />,
    Send: (props: ButtonHTMLAttributes<HTMLButtonElement>) => <button type="submit" {...props} />,
  },
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

function uploads(overrides: Partial<AttachmentUploads> = {}): AttachmentUploads {
  return {
    items: [],
    readyAssetIds: [],
    hasBlockingUploads: false,
    addFiles: vi.fn(),
    remove: vi.fn(),
    clearReady: vi.fn(),
    ...overrides,
  };
}

let mediaStub: GetUserMediaStub | undefined;

beforeEach(() => {
  vi.resetAllMocks();
  h.caps = { tts: false, stt: false };
  h.auiState.thread = { isRunning: false };
  h.auiState.composer = { dictation: undefined, text: '' };
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

  it('file input passes selected files to addFiles', () => {
    const addFiles = vi.fn();
    const { container } = render(<Composer uploads={uploads({ addFiles })} />);
    const input = container.querySelector('input[type="file"]');
    if (!(input instanceof HTMLInputElement)) throw new Error('expected hidden file input');
    const file = new File(['x'], 'manual.pdf', { type: 'application/pdf' });

    fireEvent.change(input, { target: { files: [file] } });

    expect(Array.from(addFiles.mock.calls[0]?.[0] as FileList | File[])).toEqual([file]);
  });

  it('paste passes clipboard files to addFiles', () => {
    const addFiles = vi.fn();
    const { container } = render(<Composer uploads={uploads({ addFiles })} />);
    const root = container.firstElementChild;
    if (root === null) throw new Error('expected composer root');
    const file = new File(['x'], 'manual.pdf', { type: 'application/pdf' });

    fireEvent.paste(root, { clipboardData: { files: [file] } });

    expect(Array.from(addFiles.mock.calls[0]?.[0] as FileList | File[])).toEqual([file]);
  });

  it('disables send while uploads are blocking', () => {
    render(<Composer uploads={uploads({ hasBlockingUploads: true })} />);

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
    const addFiles = vi.fn();
    const onEffortChange = vi.fn();
    const { container, rerender } = render(
      <Composer
        approvalLocked
        uploads={uploads({ addFiles })}
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

    const fileInput = container.querySelector('input[type="file"]');
    if (!(fileInput instanceof HTMLInputElement)) throw new Error('expected hidden file input');
    fireEvent.change(fileInput, {
      target: { files: [new File(['x'], 'blocked.txt', { type: 'text/plain' })] },
    });
    fireEvent.paste(root, {
      clipboardData: { files: [new File(['x'], 'blocked-paste.txt', { type: 'text/plain' })] },
    });
    expect(addFiles).not.toHaveBeenCalled();

    rerender(
      <Composer
        approvalLocked
        uploads={uploads({ addFiles })}
        skills={SKILLS}
        effort="auto"
        effortLevels={['auto', 'off']}
        onEffortChange={onEffortChange}
      />,
    );
    expect(screen.getByTestId('chat-composer').getAttribute('aria-describedby')).toBe(hintId);
  });

  it('disables the running Cancel action while approval-locked', () => {
    h.auiState.thread = { isRunning: true };
    render(<Composer approvalLocked uploads={uploads()} />);

    expect(screen.getByRole('button', { name: 'Stop the current response' })).toHaveProperty(
      'disabled',
      true,
    );
  });

  it('literally disables attachment removal actions', () => {
    const remove = vi.fn();
    const lockedUploads = uploads({
      items: [
        {
          localId: 'locked-file',
          file: new File(['x'], 'locked.txt', { type: 'text/plain' }),
          progress: 1,
          status: 'ready',
        },
      ],
      remove,
    });
    render(<Composer approvalLocked uploads={lockedUploads} />);

    const removeAttachment = screen.getByRole('button', { name: 'Remove locked.txt' });
    expect(removeAttachment).toHaveProperty('disabled', true);

    fireEvent.click(removeAttachment);

    expect(remove).not.toHaveBeenCalled();
  });
});

describe('Composer dictation', () => {
  it('caps.stt=false → the Mic records an audio attachment (no regression, WEBVOICE-04)', async () => {
    stubMediaRecorder();
    mediaStub = stubGetUserMedia();
    const addFiles = vi.fn();
    render(<Composer uploads={uploads({ addFiles })} />);

    fireEvent.click(screen.getByLabelText('Record audio'));
    await waitFor(() => screen.getByLabelText('Stop recording'));
    fireEvent.click(screen.getByLabelText('Stop recording'));

    expect(mediaStub.getUserMedia).toHaveBeenCalledTimes(1);
    expect(addFiles).toHaveBeenCalledTimes(1);
    expect(h.startDictation).not.toHaveBeenCalled(); // never dictation when STT is off
  });

  it('caps.stt=true → the Mic starts a runtime dictation session (not an attachment)', () => {
    h.caps = { tts: false, stt: true };
    render(<Composer uploads={uploads()} />);

    fireEvent.click(screen.getByLabelText('Dictate'));

    expect(h.startDictation).toHaveBeenCalledTimes(1);
  });

  it('marks the turn dictated when a transcript is inserted (auto-speak parity, D-07)', () => {
    h.caps = { tts: false, stt: true };
    h.auiState.composer.text = '';
    const { rerender } = render(<Composer uploads={uploads()} />);

    fireEvent.click(screen.getByLabelText('Dictate'));
    // Runtime opens the session (dictation state becomes non-null).
    h.auiState.composer.dictation = { status: { type: 'running' } };
    rerender(<Composer uploads={uploads()} />);

    fireEvent.click(screen.getByLabelText('Stop dictation'));
    expect(h.stopDictation).toHaveBeenCalledTimes(1);

    // The adapter's onSpeech inserts the transcript, then the session tears down.
    h.auiState.composer.text = 'hello world';
    h.auiState.composer.dictation = undefined;
    rerender(<Composer uploads={uploads()} />);

    expect(h.markTurnDictated).toHaveBeenCalledTimes(1);
  });

  it('announces listening → transcribing via an aria-live region', () => {
    h.caps = { tts: false, stt: true };
    render(<Composer uploads={uploads()} />);
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
    const { rerender } = render(<Composer uploads={uploads()} />);
    const status = screen.getByRole('status');

    fireEvent.click(screen.getByLabelText('Dictate'));
    h.auiState.composer.dictation = { status: { type: 'running' } };
    rerender(<Composer uploads={uploads()} />);
    fireEvent.click(screen.getByLabelText('Stop dictation'));

    // Session tears down with the composer text unchanged (nothing was transcribed).
    h.auiState.composer.dictation = undefined;
    rerender(<Composer uploads={uploads()} />);

    expect(h.markTurnDictated).not.toHaveBeenCalled();
    expect(status.textContent).toContain('No transcription');
  });

  it('caps.stt=true but dictation unavailable → degrades to the attachment record path (D-10)', async () => {
    h.caps = { tts: false, stt: true };
    h.startDictation.mockImplementation(() => {
      throw new Error('Dictation adapter not configured');
    });
    stubMediaRecorder();
    mediaStub = stubGetUserMedia();
    render(<Composer uploads={uploads()} />);

    fireEvent.click(screen.getByLabelText('Dictate'));

    await waitFor(() => {
      expect(mediaStub?.getUserMedia).toHaveBeenCalledTimes(1);
    });
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

describe('Composer skill picker', () => {
  it("typing '/' opens the ARIA combobox and keeps focus on the input", () => {
    h.auiState.composer.text = '/';
    render(<Composer uploads={uploads()} skills={SKILLS} />);
    const input = screen.getByLabelText('Ask Aura');
    input.focus();

    const listbox = screen.getByRole('listbox');
    const options = within(listbox).getAllByRole('option');
    expect(input.getAttribute('role')).toBe('combobox');
    expect(input.getAttribute('aria-expanded')).toBe('true');
    expect(input.getAttribute('aria-controls')).toBe(listbox.id);
    expect(input.getAttribute('aria-activedescendant')).toBe(options[0]?.id);
    // APG combobox: DOM focus never leaves the input for the listbox.
    expect(document.activeElement).toBe(input);
  });

  it('filters incrementally to the matching skill', () => {
    h.auiState.composer.text = '/creat';
    render(<Composer uploads={uploads()} skills={SKILLS} />);

    const listbox = screen.getByRole('listbox');
    expect(within(listbox).getByText('skill-creator')).toBeTruthy();
    expect(within(listbox).queryByText('codeql')).toBeNull();
  });

  it('ArrowDown moves the active option (aria-activedescendant follows)', () => {
    h.auiState.composer.text = '/';
    render(<Composer uploads={uploads()} skills={SKILLS} />);
    const input = screen.getByLabelText('Ask Aura');
    const ids = within(screen.getByRole('listbox'))
      .getAllByRole('option')
      .map((option) => option.id);
    expect(input.getAttribute('aria-activedescendant')).toBe(ids[0]);

    fireEvent.keyDown(input, { key: 'ArrowDown' });

    expect(input.getAttribute('aria-activedescendant')).toBe(ids[1]);
  });

  // Rewritten 2026-08-17 on the operator's instruction ("la skill deve stare nel messaggio
  // non sopra"): selecting a skill no longer pins it to a chip above the composer, it
  // completes the command INTO the message so '/skill-creator <instruction>' is the turn.
  it('Enter completes the active skill into the message and does NOT send', () => {
    h.auiState.composer.text = '/creat';
    render(<Composer uploads={uploads()} skills={SKILLS} />);
    const input = screen.getByLabelText('Ask Aura');

    const event = createEvent.keyDown(input, { key: 'Enter' });
    fireEvent(input, event);

    expect(event.defaultPrevented).toBe(true); // never fell through to Enter-send
    // The trailing space is load-bearing: it is what closes the menu, so the next Enter
    // sends the message instead of re-selecting an option.
    expect(h.setText).toHaveBeenCalledWith('/skill-creator ');
  });

  it('Escape closes the menu (preventDefault) and a keystroke re-arms it', () => {
    h.auiState.composer.text = '/';
    const { rerender } = render(<Composer uploads={uploads()} skills={SKILLS} />);
    const input = screen.getByLabelText('Ask Aura');
    expect(screen.getByRole('listbox')).toBeTruthy();

    const event = createEvent.keyDown(input, { key: 'Escape' });
    fireEvent(input, event);

    expect(event.defaultPrevented).toBe(true);
    expect(screen.queryByRole('listbox')).toBeNull();

    h.auiState.composer.text = '/c';
    rerender(<Composer uploads={uploads()} skills={SKILLS} />);
    expect(screen.getByRole('listbox')).toBeTruthy();
  });

  it('does NOT intercept Enter when the menu is closed (Enter-send preserved, D-09)', () => {
    h.auiState.composer.text = 'hello world';
    render(<Composer uploads={uploads()} skills={SKILLS} />);
    const input = screen.getByLabelText('Ask Aura');
    expect(screen.queryByRole('listbox')).toBeNull();

    const event = createEvent.keyDown(input, { key: 'Enter' });
    fireEvent(input, event);

    expect(event.defaultPrevented).toBe(false); // the library's Enter-send runs untouched
  });

  it('a literal slash mid-text never opens the menu (D-05)', () => {
    h.auiState.composer.text = 'a/b';
    render(<Composer uploads={uploads()} skills={SKILLS} />);
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('degrades to a no-op when the skills list is empty ( / never opens, D-09)', () => {
    h.auiState.composer.text = '/';
    render(<Composer uploads={uploads()} skills={[]} />);
    const input = screen.getByLabelText('Ask Aura');
    expect(screen.queryByRole('listbox')).toBeNull();
    expect(input.getAttribute('aria-expanded')).toBe('false');
    // Regression guard (shell.spec.ts): the idle composer is a plain textbox, not a combobox.
    expect(input.getAttribute('role')).toBeNull();
    expect(screen.getByRole('textbox', { name: 'Ask Aura' })).toBe(input);

    const event = createEvent.keyDown(input, { key: 'Enter' });
    fireEvent(input, event);
    expect(event.defaultPrevented).toBe(false);
  });

  it('the add-files quick action clicks the hidden file input (no agent run)', () => {
    h.auiState.composer.text = '/add';
    const clickSpy = vi
      .spyOn(HTMLInputElement.prototype, 'click')
      .mockImplementation(() => undefined);
    render(<Composer uploads={uploads()} skills={SKILLS} />);

    fireEvent.keyDown(screen.getByLabelText('Ask Aura'), { key: 'Enter' });

    expect(clickSpy).toHaveBeenCalledTimes(1);
    clickSpy.mockRestore();
  });

  it('the new-chat quick action calls onNewChat (no agent run)', () => {
    h.auiState.composer.text = '/new';
    const onNewChat = vi.fn();
    render(<Composer uploads={uploads()} skills={SKILLS} onNewChat={onNewChat} />);

    fireEvent.keyDown(screen.getByLabelText('Ask Aura'), { key: 'Enter' });

    expect(onNewChat).toHaveBeenCalledTimes(1);
  });

  it('the clear quick action resets the text, pinned pill, and pending attachments', () => {
    h.auiState.composer.text = '/clear';
    const remove = vi.fn();
    const withItem = uploads({
      items: [
        {
          localId: 'x',
          file: new File(['a'], 'a.txt', { type: 'text/plain' }),
          progress: 1,
          status: 'ready',
        },
      ],
      remove,
    });
    render(<Composer uploads={withItem} skills={SKILLS} />);

    fireEvent.keyDown(screen.getByLabelText('Ask Aura'), { key: 'Enter' });

    expect(remove).toHaveBeenCalledWith('x');
    expect(h.setText).toHaveBeenCalledWith('');
  });

  // The pinned-skill pill is deliberately gone (operator, 2026-08-17). Removing a skill is
  // now editing the message that carries it, so there is no chip to render or dismiss.
  it('renders no pinned-skill chip above the composer', () => {
    render(<Composer uploads={uploads()} skills={SKILLS} />);

    expect(screen.queryByLabelText('Remove pinned skill skill-creator')).toBeNull();
  });
});

describe('Composer reasoning-effort selector', () => {
  it('renders EXACTLY the advertised levels (dynamic, D-13 — never the full 7)', () => {
    render(<Composer uploads={uploads()} effort="auto" effortLevels={['auto', 'off', 'high']} />);
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
    render(<Composer uploads={uploads()} effort="high" effortLevels={['auto', 'off', 'high']} />);
    expect(screen.getByRole('combobox', { name: 'Reasoning effort' })).toHaveProperty(
      'value',
      'high',
    );
  });

  it('calls onEffortChange when a level is picked', () => {
    const onEffortChange = vi.fn();
    render(
      <Composer
        uploads={uploads()}
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
    render(<Composer uploads={uploads()} />);
    expect(screen.queryByRole('combobox', { name: 'Reasoning effort' })).toBeNull();
  });

  it('does not reclassify the message input — the idle textbox is preserved (no regression)', () => {
    render(<Composer uploads={uploads()} effort="auto" effortLevels={['auto', 'off', 'high']} />);
    // The selector is a separate combobox; the message input stays a plain textbox (shell.spec).
    expect(screen.getByRole('textbox', { name: 'Ask Aura' })).toBeTruthy();
    expect(screen.getByLabelText('Ask Aura').getAttribute('role')).toBeNull();
  });
});
