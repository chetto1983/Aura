import { useAui, useAuiState, ComposerPrimitive } from '@assistant-ui/react';
import { ArrowUp, ChevronDown, Mic, Paperclip, Square } from 'lucide-react';
import { useEffect, useId, useLayoutEffect, useRef, useState, type RefObject } from 'react';
import { useTranslation } from 'react-i18next';
import { AttachmentChip } from './attachments/AttachmentChip';
import { GarageMentionPicker } from './composer/GarageMentionPicker';
import { SkillPicker } from './composer/SkillPicker';
import type { ComposerSkillRow } from './composer/api';
import { useVoiceMode } from './voice/voiceModeContext';
import { Button } from '@/components/ui/button';

// Composer: the query input + the Send↔Stop swap. Enter sends / Shift+Enter
// newlines / Esc cancels are handled by ComposerPrimitive.Input. Stop is
// ComposerPrimitive.Cancel → api.thread().cancelRun() → the external-store
// onCancel aborts the in-flight fetch (the server streamSSE unwinds on ctx.Done).
//
// The Mic is DICTATION, and only dictation: it toggles the runtime dictation session
// (the dictationAdapter's onSpeech inserts an editable transcript natively) and
// announces listening/transcribing/error via an aria-live region. Where STT is not
// configured, or dictation is otherwise unavailable, the mic is NOT rendered at all.
//
// It used to fall back to recording an audio ATTACHMENT (MediaRecorder →
// composer.addAttachment) so the control was never a dead end. That fallback is gone:
// the attachment reached the model as audio bytes, not as words, which is precisely
// the thing speech in this product must never be. A mic that is absent says "not here";
// a mic that quietly sends something else says nothing and is believed.
//
// Accent is reserved for the primary Send CTA only (UI-SPEC §Color list item 1);
// Stop is a neutral danger-tinted control so the accent stays scarce.

const EMPTY_SKILLS: readonly ComposerSkillRow[] = [];

interface ComposerProps {
  readonly inputRef?: RefObject<HTMLTextAreaElement | null>;
  readonly onInputAvailable?: (input: HTMLTextAreaElement | null) => void;
  readonly approvalLocked?: boolean;
  /** RS-07 §4.2: blocks ONLY Send (typing stays free) while the thread's detached run is live. */
  readonly sendBlocked?: boolean;
  readonly steerAvailable?: boolean; // D-10: true while the live run for this thread is steerable.
  readonly onSteerSubmit?: (text: string) => void; // D-10: routes composer text as a steer.
  readonly draftPrompt?: ComposerDraftPrompt | undefined;
  readonly onDraftPromptConsumed?: ((nonce: number) => void) | undefined;
  readonly skills?: readonly ComposerSkillRow[];
  /** Runs a '/' COMMAND (a verb the composer performs) by name. Absent ⇒ the menu lists
   * skills only — a command row that cannot act is worse than no row. */
  readonly onCommand?: ((name: string) => void) | undefined;
  /** 37E reasoning-effort selector: the currently selected symbol (default 'auto'). */
  readonly effort?: string;
  /** The advertised effort levels to render — auto-first, dynamic (D-13). Absent/empty ⇒ the
   * selector is not rendered at all (e.g. the Composer mounted without the effort wiring). */
  readonly effortLevels?: readonly string[];
  readonly onEffortChange?: (effort: string) => void;
}

export interface ComposerDraftPrompt {
  readonly text: string;
  readonly nonce: number;
}

type DictationPhase = 'idle' | 'listening' | 'transcribing' | 'error';

export function Composer({
  inputRef,
  onInputAvailable,
  approvalLocked = false,
  sendBlocked = false,
  steerAvailable = false,
  onSteerSubmit,
  draftPrompt,
  onDraftPromptConsumed,
  skills,
  onCommand,
  effort,
  effortLevels,
  onEffortChange,
}: ComposerProps) {
  const { t } = useTranslation();
  const aui = useAui();
  const isRunning = useAuiState((s) => s.thread.isRunning);
  const dictation = useAuiState((s) => s.composer.dictation);
  const { caps, markTurnDictated } = useVoiceMode();
  const fallbackComposerInputRef = useRef<HTMLTextAreaElement | null>(null);
  const composerInputRef = inputRef ?? fallbackComposerInputRef;
  const previousApprovalLockedRef = useRef(approvalLocked);
  const appliedDraftNonce = useRef<number | undefined>(undefined);
  const [captureGeneration, setCaptureGeneration] = useState({
    approvalLocked,
    epoch: 0,
  });
  if (captureGeneration.approvalLocked !== approvalLocked) {
    setCaptureGeneration({
      approvalLocked,
      epoch: captureGeneration.epoch + (approvalLocked ? 1 : 0),
    });
  }
  const captureEpoch = captureGeneration.epoch;
  const [dictationCaptureEpoch, setDictationCaptureEpoch] = useState(0);
  const [dictationPhase, setDictationPhase] = useState<DictationPhase>('idle');
  const dictationStartLen = useRef(0);
  const wasDictating = useRef(false);
  const suppressedDictationRef = useRef<{ readonly draft: string; sawActive: boolean } | undefined>(
    undefined,
  );
  // Whether the mic can be a DICTATION button at all — if it cannot, there is no mic,
  // because ComposerPrimitive.Dictate renders itself DISABLED rather than absent, and a
  // disabled control that never becomes enabled is a dead end.
  //
  // Read from the runtime state directly rather than through useComposerDictate's `disabled`,
  // which answers a different question: it is `dictation != null || !capabilities.dictation ||
  // !isEditing`, so it is ALSO true while a session is running. Gating availability on it
  // swapped the mic back to the recorder the instant dictation started — the operator could
  // not stop what they had just begun. Measured 2026-08-22 in Linux Chrome, where the fake
  // media device starts the session immediately; on a host whose real device is slower the
  // session never opened and the bug stayed invisible.
  const dictationUnavailable = useAuiState(
    (s) => !s.thread.capabilities.dictation || !s.composer.isEditing,
  );
  const canDictate = caps.stt && !dictationUnavailable;
  const isDictating = dictation != null;
  const activeDictationPhase = dictationCaptureEpoch === captureEpoch ? dictationPhase : 'idle';
  // An attachment still being uploaded blocks the send, exactly as hasBlockingUploads did —
  // but the state is the runtime's, published by the adapter's generator.
  const uploading = useAuiState((s) =>
    s.composer.attachments.some((a) => a.status.type === 'running'),
  );
  const sendDisabled = approvalLocked || sendBlocked || uploading;
  // D-10: assistant-ui disables Send while running (no thread.capabilities.queue) — steer needs its own control.
  const steering = isRunning && steerAvailable;
  const submitSteer = () => {
    const text = aui.composer.getState().text;
    if (text.trim().length === 0) return;
    aui.composer.setText('');
    onSteerSubmit?.(text);
  };

  useLayoutEffect(() => {
    onInputAvailable?.(composerInputRef.current);
    return () => {
      onInputAvailable?.(null);
    };
  }, [composerInputRef, onInputAvailable]);

  useLayoutEffect(() => {
    const becameLocked = approvalLocked && !previousApprovalLockedRef.current;
    previousApprovalLockedRef.current = approvalLocked;
    if (!becameLocked) return;

    const dictationBusy =
      dictationPhase === 'listening' || dictationPhase === 'transcribing' || isDictating;
    if (dictationBusy) {
      suppressedDictationRef.current = {
        draft: aui.composer.getState().text,
        sawActive: isDictating || wasDictating.current,
      };
      aui.composer.stopDictation();
    }
  }, [approvalLocked, aui, captureEpoch, dictationPhase, isDictating]);

  // Skill / command picker (WEBSKILL-01/03): the '/'-triggered ARIA combobox. The composer
  // text is read reactively and every decision (trigger, filter, wrap-around active index,
  // key→action) lives in the pure skillPickerModel, so this component only maps state → ARIA.
  const skillList = skills ?? EMPTY_SKILLS;
  const approvalHintId = useId();

  // Focus + caret at the end of whatever the composer now holds. The '/' picker needs it
  // after a mouse pick (see SkillPicker.execute); it is also simply where the caret belongs
  // once text has been written into the composer on the operator's behalf.
  const focusComposerEnd = () => {
    const input = composerInputRef.current;
    if (!input) return;
    input.focus();
    input.setSelectionRange(input.value.length, input.value.length);
  };

  useEffect(() => {
    if (
      approvalLocked ||
      draftPrompt === undefined ||
      draftPrompt.nonce === appliedDraftNonce.current
    )
      return;
    appliedDraftNonce.current = draftPrompt.nonce;
    aui.composer.setText(draftPrompt.text);
    composerInputRef.current?.focus();
    onDraftPromptConsumed?.(draftPrompt.nonce);
  }, [approvalLocked, aui, composerInputRef, draftPrompt, onDraftPromptConsumed]);

  // Detect a dictation session ending. If the transcript was inserted (the composer text
  // grew via onSpeech), mark the turn dictated for auto-speak parity (D-07). If nothing was
  // inserted (an empty transcript or an /api/stt error), surface chat.dictation.error and
  // leave the mic usable (D-10). The insert lands before the session tears down (Landmine #1).
  useEffect(() => {
    const suppressed = suppressedDictationRef.current;
    if (suppressed !== undefined) {
      if (isDictating) {
        suppressed.sawActive = true;
        return;
      }
      const composer = aui.composer;
      if (composer.getState().text !== suppressed.draft) composer.setText(suppressed.draft);
      if (suppressed.sawActive || !approvalLocked) {
        suppressedDictationRef.current = undefined;
        wasDictating.current = false;
      }
      return;
    }
    if (isDictating) {
      wasDictating.current = true;
      return;
    }
    if (!wasDictating.current) return;
    wasDictating.current = false;
    const inserted = aui.composer.getState().text.length > dictationStartLen.current;
    if (inserted) {
      markTurnDictated();
      setDictationPhase('idle');
    } else {
      setDictationPhase((phase) => (phase === 'idle' ? 'idle' : 'error'));
    }
  }, [approvalLocked, isDictating, aui, markTurnDictated]);

  // Paste and drop are the primitives': ComposerPrimitive.Input pastes files itself
  // (addAttachmentOnPaste, default true) and ComposerPrimitive.AttachmentDropzone owns the
  // whole drag sequence. Both used to be re-implemented here on top of them — which for paste
  // was not merely redundant: the primitive added the pasted file and the handler beside it
  // added the same file again, so one paste produced two attachments.

  // armDictation records what the session is about to be measured against. ComposerPrimitive
  // .Dictate opens the session itself; this only notes the epoch the phase belongs to and the
  // text length, because "did a transcript arrive" is length-after > length-before and there
  // is no other signal for an empty transcript or a failed /api/stt.
  const armDictation = () => {
    if (approvalLocked) return;
    suppressedDictationRef.current = undefined;
    setDictationCaptureEpoch(captureEpoch);
    dictationStartLen.current = aui.composer.getState().text.length;
    setDictationPhase('listening');
  };

  const visibleDictationPhase = approvalLocked ? 'idle' : activeDictationPhase;
  const dictationBusy =
    visibleDictationPhase === 'listening' || visibleDictationPhase === 'transcribing';
  const micLabel = t(dictationBusy ? 'chat.dictation.stop' : 'chat.dictation.start');
  const dictationAnnouncement =
    visibleDictationPhase === 'listening'
      ? t('chat.dictation.listening')
      : visibleDictationPhase === 'transcribing'
        ? t('chat.dictation.transcribing')
        : visibleDictationPhase === 'error'
          ? t('chat.dictation.error')
          : '';

  return (
    // The trigger root groups the '/' and '@' popovers with the input they read from. Each
    // registers by character, so both share assistant-ui's trigger state machine.
    <ComposerPrimitive.Unstable_TriggerPopoverRoot>
      {/* Root is the FORM — it is what turns Enter and the Send button into a submit — so the
          dropzone is NESTED inside it, exactly as assistant-ui's own composer nests it. Merging
          the two by handing Root a `render` of the dropzone replaces the form element with a
          div, and the composer then looks perfect and sends nothing. */}
      <ComposerPrimitive.Root className="relative mx-3 mb-3 flex shrink-0 flex-col sm:mx-4">
        <ComposerPrimitive.AttachmentDropzone
          data-testid="chat-composer"
          aria-disabled={approvalLocked}
          aria-describedby={approvalLocked ? approvalHintId : undefined}
          // The dropzone owns dragenter/over/leave/drop: it refuses a drop the thread has no
          // attachment capability for, tracks leaving through a child correctly, and publishes
          // data-dragging while a file is over the composer — which is what the dashed border
          // reads. The two handlers it replaced called preventDefault and nothing else, so a
          // drag gave the operator no feedback at all.
          disabled={approvalLocked}
          className="flex flex-col gap-2 rounded-[var(--radius-xl)] border border-border bg-surface p-2 shadow-[var(--shadow-popover)] transition-colors data-[dragging=true]:border-dashed data-[dragging=true]:border-accent"
        >
          {approvalLocked ? (
            <p id={approvalHintId} className="px-1 text-xs text-warning">
              {t('approval.lock')}
            </p>
          ) : null}
          <fieldset disabled={approvalLocked} className="contents">
            <SkillPicker
              skills={skillList}
              disabled={approvalLocked}
              onCommand={onCommand}
              onSelect={focusComposerEnd}
            />
            <GarageMentionPicker
              disabled={approvalLocked || isRunning}
              onSelect={focusComposerEnd}
            />
            <div className="flex flex-wrap gap-2 empty:hidden">
              <ComposerPrimitive.Attachments>
                {({ attachment }) => (
                  <AttachmentChip attachment={attachment} disabled={approvalLocked} />
                )}
              </ComposerPrimitive.Attachments>
            </div>
            {/* Live region: announces the dictation state to screen readers; kept mounted so the
          transition is picked up (empty + sr-only while idle). */}
            <p
              role="status"
              aria-live="polite"
              className={
                dictationAnnouncement === ''
                  ? 'sr-only'
                  : 'px-1 text-[0.75rem] text-text-muted [font-variant-numeric:tabular-nums]'
              }
            >
              {dictationAnnouncement}
            </p>
            {/* Input ABOVE its own toolbar, at every width.
              The controls used to share one row with the textarea, and on a phone that row has
              no width to share: measured on the operator's screen, the reasoning pill and the
              four buttons left the input so narrow that a three-word placeholder wrapped onto
              three lines. Stacking gives the textarea the full width and puts the toolbar where
              assistant-ui's own composer puts it.
              It stacks on the DESKTOP too, and that is the point rather than a side effect: a
              breakpoint-only stack needs CSS `order` to keep one of the two layouts looking
              right, and `order` moves the drawing without moving the focus — so the layout that
              did not get its own DOM order would read in one sequence and tab in another. One
              shape means DOM order IS reading order at every width. */}
            <ComposerPrimitive.Input
              ref={composerInputRef}
              rows={1}
              placeholder={t('chat.composer.placeholder')}
              aria-label={t('chat.composer.placeholder')}
              // The combobox ARIA is published by the primitive while the '/'-menu is open and
              // withdrawn when it closes, so the input stays a plain role=textbox at rest —
              // the property shell.spec.ts asserts, and the reason screen readers no longer
              // announce a listbox popup on every ordinary message.
              disabled={approvalLocked}
              onKeyDown={(e) => {
                if (steering && e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                  e.preventDefault();
                  submitSteer();
                }
              }}
              className="max-h-40 min-h-[44px] w-full resize-none bg-transparent px-3 py-2 text-[1.0625rem] leading-relaxed text-text outline-none placeholder:text-text-faint focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
            />
            <div className="flex items-center gap-1">
              {/* The hidden input is the primitive's now: AddAttachment opens the dialog,
                    filters by the adapter's `accept`, and feeds add() directly. */}
              <ComposerPrimitive.AddAttachment
                multiple
                render={
                  <Button
                    type="button"
                    size="icon"
                    variant="ghost"
                    aria-label={t('chat.attachments.add')}
                    disabled={approvalLocked}
                    className="rounded-full text-text-muted hover:text-text"
                  >
                    <Paperclip data-icon aria-hidden="true" className="size-4" />
                  </Button>
                }
              />
              {/* Dictation is ComposerPrimitive.Dictate / StopDictation — the same two buttons
                assistant-ui's own composer renders. They call the runtime's dictation session
                directly, so the onClick here carries only what the primitives do NOT know
                about: the phase our aria-live region announces, and the text length that tells
                a transcript-inserted session from an empty one.
                No mic at all where dictation is unavailable: the primitives render themselves
                DISABLED rather than absent, and the audio-attachment fallback that used to
                stand in its place sent the model bytes instead of words. */}
              {canDictate ? (
                dictationBusy ? (
                  <ComposerPrimitive.StopDictation
                    render={
                      <Button
                        type="button"
                        size="icon"
                        variant="ghost"
                        aria-label={micLabel}
                        aria-pressed
                        disabled={approvalLocked}
                        onClick={() => {
                          setDictationPhase('transcribing');
                        }}
                        className="rounded-full text-text-muted hover:text-text"
                      >
                        <Square data-icon aria-hidden="true" className="size-3.5 fill-current" />
                      </Button>
                    }
                  />
                ) : (
                  <ComposerPrimitive.Dictate
                    render={
                      <Button
                        type="button"
                        size="icon"
                        variant="ghost"
                        aria-label={micLabel}
                        aria-pressed={false}
                        disabled={approvalLocked}
                        onClick={armDictation}
                        className="rounded-full text-text-muted hover:text-text"
                      >
                        <Mic data-icon aria-hidden="true" className="size-4" />
                      </Button>
                    }
                  />
                )
              ) : null}
              {/* Reasoning-effort selector (WEBMODEL-01/03, D-13): a compact native select — keyboard-
            and screen-reader-correct out of the box, and separate from the textbox so it never
            reclassifies the input or intercepts Enter-send / paste / drop. It renders ONLY the
            model's advertised levels (effortLevels, auto-first); absent ⇒ not rendered. */}
              {effortLevels !== undefined && effortLevels.length > 0 ? (
                <div className="relative ml-1 flex items-center">
                  <select
                    aria-label={t('chat.composer.effort.ariaLabel')}
                    value={effort ?? 'auto'}
                    disabled={approvalLocked}
                    onChange={(event) => {
                      if (approvalLocked) return;
                      onEffortChange?.(event.currentTarget.value);
                    }}
                    className="h-8 cursor-pointer appearance-none rounded-full border border-border bg-surface-2 py-1 pr-7 pl-3 text-[0.75rem] font-medium tracking-tight text-text-muted transition-colors hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent [font-variant-caps:all-small-caps]"
                  >
                    {effortLevels.map((level) => (
                      <option key={level} value={level}>
                        {t(`chat.composer.effort.${level}`)}
                      </option>
                    ))}
                  </select>
                  <ChevronDown
                    data-icon
                    aria-hidden="true"
                    className="pointer-events-none absolute right-2 size-3.5 text-text-faint"
                  />
                </div>
              ) : null}
              {steering ? (
                <Button
                  type="button"
                  size="icon"
                  variant="ghost"
                  aria-label={t('chat.steer.sendAria')}
                  onClick={submitSteer}
                  className="ml-auto rounded-full text-accent-text hover:text-accent-text"
                >
                  <ArrowUp data-icon aria-hidden="true" className="size-4" />
                </Button>
              ) : null}
              {/* ml-auto, not a spacer: the send is the only control anchored to the far end, so
                the gap between the toolbar and it is the send's own margin rather than an empty
                element a screen reader would have to skip. */}
              {isRunning ? (
                <Button asChild size="icon" className="ml-auto rounded-full">
                  <ComposerPrimitive.Cancel
                    aria-label={t('chat.composer.stopAria')}
                    disabled={approvalLocked}
                  >
                    <Square data-icon aria-hidden="true" className="size-3.5 fill-current" />
                  </ComposerPrimitive.Cancel>
                </Button>
              ) : (
                <Button asChild size="icon" className="ml-auto rounded-full">
                  <ComposerPrimitive.Send
                    aria-label={t('chat.composer.sendAria')}
                    disabled={sendDisabled}
                  >
                    <ArrowUp data-icon aria-hidden="true" className="size-4" />
                  </ComposerPrimitive.Send>
                </Button>
              )}
            </div>
          </fieldset>
        </ComposerPrimitive.AttachmentDropzone>
      </ComposerPrimitive.Root>
    </ComposerPrimitive.Unstable_TriggerPopoverRoot>
  );
}
