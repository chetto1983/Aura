import { useAui, useAuiState, ComposerPrimitive } from '@assistant-ui/react';
import { useComposerDictate } from '@assistant-ui/core/react';
import { ArrowUp, ChevronDown, Mic, Paperclip, Square } from 'lucide-react';
import { useEffect, useId, useLayoutEffect, useRef, useState, type RefObject } from 'react';
import { useTranslation } from 'react-i18next';
import { AttachmentChip } from './attachments/AttachmentChip';
import { SkillPicker } from './composer/SkillPicker';
import type { ComposerSkillRow } from './composer/api';
import { useVoiceMode } from './voice/voiceModeContext';
import { Button } from '@/components/ui/button';

// Composer: the query input + the Send↔Stop swap. Enter sends / Shift+Enter
// newlines / Esc cancels are handled by ComposerPrimitive.Input. Stop is
// ComposerPrimitive.Cancel → api.thread().cancelRun() → the external-store
// onCancel aborts the in-flight fetch (the server streamSSE unwinds on ctx.Done).
//
// The Mic is dictation-primary when caps.stt (WEBVOICE-02): it toggles the runtime
// dictation session (the dictationAdapter's onSpeech inserts an editable transcript
// natively) and announces listening/transcribing/error via an aria-live region. When
// STT is unconfigured OR dictation is unavailable it reverts to the KEPT attachment-
// record path (MediaRecorder → composer.addAttachment), so there is no regression (D-10).
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
  const recorderRef = useRef<MediaRecorder | null>(null);
  const recordingStreamRef = useRef<MediaStream | null>(null);
  const chunksRef = useRef<Blob[]>([]);
  const recordingRequestRef = useRef(false);
  const captureEpochRef = useRef(0);
  const approvalLockedRef = useRef(approvalLocked);
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
  const [recordingEpoch, setRecordingEpoch] = useState<number | null>(null);
  const [dictationCaptureEpoch, setDictationCaptureEpoch] = useState(0);
  const [dictationPhase, setDictationPhase] = useState<DictationPhase>('idle');
  const dictationStartLen = useRef(0);
  const wasDictating = useRef(false);
  const suppressedDictationRef = useRef<{ readonly draft: string; sawActive: boolean } | undefined>(
    undefined,
  );
  // The runtime's own availability answer. useComposerDictate reports `disabled` when no
  // DictationAdapter is configured, and it is the same value ComposerPrimitive.Dictate puts on
  // its button — so reading it here is not a second opinion, it is the primitive's own, asked
  // one step earlier. That is what keeps D-10 intact now that the mic IS that primitive: a
  // disabled button is precisely the dead end D-10 forbids, so the recorder fallback is chosen
  // on this rather than on caps.stt alone.
  const { disabled: dictationUnavailable } = useComposerDictate();
  const canDictate = caps.stt && !dictationUnavailable;
  const isDictating = dictation != null;
  const isRecording = recordingEpoch === captureEpoch;
  const activeDictationPhase = dictationCaptureEpoch === captureEpoch ? dictationPhase : 'idle';
  // An attachment still being uploaded blocks the send, exactly as hasBlockingUploads did —
  // but the state is the runtime's, published by the adapter's generator.
  const uploading = useAuiState((s) =>
    s.composer.attachments.some((a) => a.status.type === 'running'),
  );
  const sendDisabled = approvalLocked || sendBlocked || uploading;

  useLayoutEffect(() => {
    onInputAvailable?.(composerInputRef.current);
    return () => {
      onInputAvailable?.(null);
    };
  }, [composerInputRef, onInputAvailable]);

  useLayoutEffect(() => {
    captureEpochRef.current = captureEpoch;
    approvalLockedRef.current = approvalLocked;
    const becameLocked = approvalLocked && !previousApprovalLockedRef.current;
    previousApprovalLockedRef.current = approvalLocked;
    if (!becameLocked) return;

    const recorder = recorderRef.current;
    if (recorder !== null) {
      recorder.ondataavailable = null;
      recorder.onstop = null;
      if (recorder.state !== 'inactive') recorder.stop();
    }
    for (const track of recordingStreamRef.current?.getTracks() ?? []) track.stop();
    chunksRef.current = [];
    recordingStreamRef.current = null;
    recorderRef.current = null;

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

  const stopRecording = () => {
    recorderRef.current?.stop();
  };

  const startRecording = async () => {
    if (approvalLocked || isRecording || recordingRequestRef.current) {
      return;
    }
    const mediaDevices = (navigator as Partial<Pick<Navigator, 'mediaDevices'>>).mediaDevices;
    if (mediaDevices === undefined) return;
    if (typeof MediaRecorder === 'undefined') return;
    const captureEpoch = captureEpochRef.current;
    recordingRequestRef.current = true;
    let stream: MediaStream | null = null;
    try {
      stream = await mediaDevices.getUserMedia({ audio: true });
      recordingRequestRef.current = false;
      if (approvalLockedRef.current || captureEpochRef.current !== captureEpoch) {
        for (const track of stream.getTracks()) track.stop();
        return;
      }
      recordingStreamRef.current = stream;
      chunksRef.current = [];
      const recorder = new MediaRecorder(stream);
      recorderRef.current = recorder;
      recorder.ondataavailable = (event) => {
        if (
          event.data.size > 0 &&
          !approvalLockedRef.current &&
          captureEpochRef.current === captureEpoch
        ) {
          chunksRef.current.push(event.data);
        }
      };
      recorder.onstop = () => {
        const shouldAttach = !approvalLockedRef.current && captureEpochRef.current === captureEpoch;
        const type = recorder.mimeType || 'audio/webm';
        const file = new File(chunksRef.current, 'voice-note.webm', { type });
        for (const track of stream?.getTracks() ?? []) track.stop();
        chunksRef.current = [];
        recordingStreamRef.current = null;
        recorderRef.current = null;
        setRecordingEpoch(null);
        if (shouldAttach) void aui.composer.addAttachment(file);
      };
      recorder.start();
      setRecordingEpoch(captureEpoch);
    } catch {
      recordingRequestRef.current = false;
      for (const track of stream?.getTracks() ?? []) track.stop();
      chunksRef.current = [];
      recordingStreamRef.current = null;
      recorderRef.current = null;
      setRecordingEpoch(null);
    }
  };

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

  const handleRecordToggle = () => {
    if (approvalLocked) return;
    if (isRecording) stopRecording();
    else void startRecording();
  };

  const visibleDictationPhase = approvalLocked ? 'idle' : activeDictationPhase;
  const dictationBusy =
    visibleDictationPhase === 'listening' || visibleDictationPhase === 'transcribing';
  const micActive = canDictate ? dictationBusy : !approvalLocked && isRecording;
  const micLabel = canDictate
    ? t(dictationBusy ? 'chat.dictation.stop' : 'chat.dictation.start')
    : t(micActive ? 'chat.attachments.micStop' : 'chat.attachments.mic');
  const dictationAnnouncement =
    visibleDictationPhase === 'listening'
      ? t('chat.dictation.listening')
      : visibleDictationPhase === 'transcribing'
        ? t('chat.dictation.transcribing')
        : visibleDictationPhase === 'error'
          ? t('chat.dictation.error')
          : '';

  return (
    // The trigger root groups the '/'-popover with the input it reads from; the popover
    // registers itself by char, so a second trigger (e.g. '@') would be another child here
    // rather than another state machine.
    <ComposerPrimitive.Unstable_TriggerPopoverRoot>
      {/* Root is the FORM — it is what turns Enter and the Send button into a submit — so the
          dropzone is NESTED inside it, exactly as assistant-ui's own composer nests it. Merging
          the two by handing Root a `render` of the dropzone replaces the form element with a
          div, and the composer then looks perfect and sends nothing. */}
      <ComposerPrimitive.Root className="relative mx-3 mb-3 flex flex-col sm:mx-4">
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
                directly and disable themselves when no DictationAdapter is configured, so the
                onClick here carries only what the primitives do NOT know about: the phase our
                aria-live region announces, and the text length that tells a transcript-inserted
                session from an empty one.
                The MediaRecorder branch stays for !canDictate and is NOT a duplicate of them:
                it records an audio ATTACHMENT rather than dictating text, which is the whole
                point of the fallback (D-10) — the mic must not dead-end where STT is
                unconfigured, and a disabled button is a dead end. */}
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
              ) : (
                <Button
                  type="button"
                  size="icon"
                  variant="ghost"
                  aria-label={micLabel}
                  aria-pressed={micActive}
                  disabled={approvalLocked}
                  onClick={handleRecordToggle}
                  className="rounded-full text-text-muted hover:text-text"
                >
                  {micActive ? (
                    <Square data-icon aria-hidden="true" className="size-3.5 fill-current" />
                  ) : (
                    <Mic data-icon aria-hidden="true" className="size-4" />
                  )}
                </Button>
              )}
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
