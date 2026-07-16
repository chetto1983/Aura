import { useAui, useAuiState, ComposerPrimitive } from '@assistant-ui/react';
import { ArrowUp, ChevronDown, Mic, Paperclip, Square } from 'lucide-react';
import {
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type ClipboardEvent,
  type DragEvent,
  type KeyboardEvent,
  type RefObject,
} from 'react';
import { useTranslation } from 'react-i18next';
import { AttachmentChip } from './attachments/AttachmentChip';
import type { AttachmentUploads } from './attachments/useAttachmentUploads';
import { SkillPicker } from './composer/SkillPicker';
import { SkillPill } from './composer/SkillPill';
import {
  filterPickerItems,
  flattenItems,
  nextActiveIndex,
  optionId,
  pickerKeyAction,
  shouldOpen,
  type PickerItem,
} from './composer/skillPickerModel';
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
// record path (MediaRecorder → uploads.addFiles), so there is no regression (D-10).
//
// Accent is reserved for the primary Send CTA only (UI-SPEC §Color list item 1);
// Stop is a neutral danger-tinted control so the accent stays scarce.

const EMPTY_SKILLS: readonly ComposerSkillRow[] = [];

interface ComposerProps {
  readonly inputRef?: RefObject<HTMLTextAreaElement | null>;
  readonly approvalLocked?: boolean;
  readonly uploads?: AttachmentUploads;
  readonly draftPrompt?: ComposerDraftPrompt | undefined;
  readonly skills?: readonly ComposerSkillRow[];
  readonly pinnedSkill?: ComposerSkillRow | null;
  readonly onPinSkill?: (row: ComposerSkillRow | null) => void;
  readonly onNewChat?: (() => void | Promise<void>) | undefined;
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
  approvalLocked = false,
  uploads,
  draftPrompt,
  skills,
  pinnedSkill,
  onPinSkill,
  onNewChat,
  effort,
  effortLevels,
  onEffortChange,
}: ComposerProps) {
  const { t } = useTranslation();
  const aui = useAui();
  const isRunning = useAuiState((s) => s.thread.isRunning);
  const dictation = useAuiState((s) => s.composer.dictation);
  const { caps, markTurnDictated } = useVoiceMode();
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const fallbackComposerInputRef = useRef<HTMLTextAreaElement | null>(null);
  const composerInputRef = inputRef ?? fallbackComposerInputRef;
  const recorderRef = useRef<MediaRecorder | null>(null);
  const recordingStreamRef = useRef<MediaStream | null>(null);
  const chunksRef = useRef<Blob[]>([]);
  const appliedDraftNonce = useRef<number | undefined>(undefined);
  const [isRecording, setIsRecording] = useState(false);
  const [dictationPhase, setDictationPhase] = useState<DictationPhase>('idle');
  const dictationStartLen = useRef(0);
  const wasDictating = useRef(false);
  const isDictating = dictation != null;
  const sendDisabled = approvalLocked || uploads?.hasBlockingUploads === true;

  // Skill / command picker (WEBSKILL-01/03): the '/'-triggered ARIA combobox. The composer
  // text is read reactively and every decision (trigger, filter, wrap-around active index,
  // key→action) lives in the pure skillPickerModel, so this component only maps state → ARIA.
  const composerText = useAuiState((s) => s.composer.text);
  const skillList = skills ?? EMPTY_SKILLS;
  const listboxBaseId = useId();
  const approvalHintId = useId();
  const listboxId = `${listboxBaseId}-skills`;
  const pickerFilter = composerText.startsWith('/') ? composerText.slice(1) : '';
  const pickerGroups = useMemo(
    () => filterPickerItems(skillList, pickerFilter),
    [skillList, pickerFilter],
  );
  const pickerOptions = useMemo(() => flattenItems(pickerGroups), [pickerGroups]);
  // Escape records the text it dismissed at; the menu stays closed only while the text is
  // unchanged, so the next keystroke re-arms it — a purely derived close, no effect needed.
  const [dismissedAt, setDismissedAt] = useState<string | null>(null);
  const [activeIndex, setActiveIndex] = useState(0);
  const [activeIndexFilter, setActiveIndexFilter] = useState(pickerFilter);
  if (activeIndexFilter !== pickerFilter) {
    // A changed filter restarts the active option at the top — adjust-during-render (React
    // re-renders before commit, no cascading paint), not a setState-in-effect.
    setActiveIndexFilter(pickerFilter);
    setActiveIndex(0);
  }
  const menuOpen =
    !approvalLocked &&
    shouldOpen(composerText, skillList.length) &&
    dismissedAt !== composerText &&
    pickerOptions.length > 0;
  const activeOptionIndex =
    pickerOptions.length > 0 ? Math.min(activeIndex, pickerOptions.length - 1) : 0;
  const activeOptionId = menuOpen ? optionId(listboxId, activeOptionIndex) : undefined;

  const clearPendingAttachments = () => {
    if (uploads === undefined) return;
    for (const item of uploads.items) uploads.remove(item.localId);
  };

  const handlePickItem = (item: PickerItem) => {
    if (approvalLocked) return;
    if (item.kind === 'skill') {
      onPinSkill?.({ name: item.name, description: item.description, type: item.type });
    } else if (item.command === 'add-files') {
      fileInputRef.current?.click();
    } else if (item.command === 'new-chat') {
      void onNewChat?.();
    } else {
      onPinSkill?.(null);
      clearPendingAttachments();
    }
    // Every pick clears the '/'-filter text (the pinned skill rides the pill, not the text);
    // the now-empty text makes shouldOpen() false, closing the menu on the next render.
    aui.composer().setText('');
  };

  const handleComposerKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (approvalLocked) return;
    // D-09: never intercept a key while the menu is closed — Enter-send / paste / drop stay
    // intact. Only the four navigation keys are consumed, and only while the menu is open.
    if (!menuOpen) return;
    const action = pickerKeyAction(event.key);
    if (action === 'none') return;
    event.preventDefault();
    if (action === 'up' || action === 'down') {
      setActiveIndex(
        nextActiveIndex(activeOptionIndex, action === 'down' ? 1 : -1, pickerOptions.length),
      );
    } else if (action === 'select') {
      const item = pickerOptions[activeOptionIndex];
      if (item !== undefined) handlePickItem(item);
    } else {
      setDismissedAt(composerText);
    }
  };

  useEffect(() => {
    if (draftPrompt === undefined || draftPrompt.nonce === appliedDraftNonce.current) return;
    appliedDraftNonce.current = draftPrompt.nonce;
    aui.composer().setText(draftPrompt.text);
    requestAnimationFrame(() => composerInputRef.current?.focus());
  }, [aui, composerInputRef, draftPrompt]);

  // Detect a dictation session ending. If the transcript was inserted (the composer text
  // grew via onSpeech), mark the turn dictated for auto-speak parity (D-07). If nothing was
  // inserted (an empty transcript or an /api/stt error), surface chat.dictation.error and
  // leave the mic usable (D-10). The insert lands before the session tears down (Landmine #1).
  useEffect(() => {
    if (isDictating) {
      wasDictating.current = true;
      return;
    }
    if (!wasDictating.current) return;
    wasDictating.current = false;
    const inserted = aui.composer().getState().text.length > dictationStartLen.current;
    if (inserted) {
      markTurnDictated();
      setDictationPhase('idle');
    } else {
      setDictationPhase((phase) => (phase === 'idle' ? 'idle' : 'error'));
    }
  }, [isDictating, aui, markTurnDictated]);

  const addFiles = (files: FileList | File[]) => {
    if (approvalLocked) return;
    if (files.length > 0) uploads?.addFiles(files);
  };

  const handleFileChange = (event: ChangeEvent<HTMLInputElement>) => {
    if (approvalLocked) return;
    if (event.currentTarget.files !== null) addFiles(event.currentTarget.files);
    event.currentTarget.value = '';
  };

  const handlePaste = (event: ClipboardEvent) => {
    if (approvalLocked) return;
    if (event.clipboardData.files.length === 0) return;
    addFiles(event.clipboardData.files);
  };

  const handleDrop = (event: DragEvent) => {
    if (approvalLocked) return;
    if (event.dataTransfer.files.length === 0) return;
    event.preventDefault();
    addFiles(event.dataTransfer.files);
  };

  const handleDragOver = (event: DragEvent) => {
    if (approvalLocked) return;
    if (event.dataTransfer.types.includes('Files')) event.preventDefault();
  };

  const stopRecording = () => {
    recorderRef.current?.stop();
  };

  const startRecording = async () => {
    if (approvalLocked || uploads === undefined || isRecording) return;
    const mediaDevices = (navigator as Partial<Pick<Navigator, 'mediaDevices'>>).mediaDevices;
    if (mediaDevices === undefined) return;
    if (typeof MediaRecorder === 'undefined') return;
    let stream: MediaStream | null = null;
    try {
      stream = await mediaDevices.getUserMedia({ audio: true });
      recordingStreamRef.current = stream;
      chunksRef.current = [];
      const recorder = new MediaRecorder(stream);
      recorderRef.current = recorder;
      recorder.ondataavailable = (event) => {
        if (event.data.size > 0) chunksRef.current.push(event.data);
      };
      recorder.onstop = () => {
        const type = recorder.mimeType || 'audio/webm';
        const file = new File(chunksRef.current, 'voice-note.webm', { type });
        uploads.addFiles([file]);
        for (const track of stream?.getTracks() ?? []) track.stop();
        recordingStreamRef.current = null;
        recorderRef.current = null;
        setIsRecording(false);
      };
      recorder.start();
      setIsRecording(true);
    } catch {
      for (const track of stream?.getTracks() ?? []) track.stop();
      recordingStreamRef.current = null;
      recorderRef.current = null;
      setIsRecording(false);
    }
  };

  // beginDictation opens a runtime dictation session; on any failure (e.g. no adapter
  // configured) it degrades to the attachment record path so the Mic never dead-ends (D-10).
  const beginDictation = () => {
    try {
      dictationStartLen.current = aui.composer().getState().text.length;
      aui.composer().startDictation();
      setDictationPhase('listening');
    } catch {
      setDictationPhase('error');
      void startRecording();
    }
  };

  const handleMic = () => {
    if (approvalLocked) return;
    if (!caps.stt) {
      if (isRecording) stopRecording();
      else void startRecording();
      return;
    }
    if (dictationPhase === 'listening') {
      setDictationPhase('transcribing');
      aui.composer().stopDictation();
      return;
    }
    beginDictation();
  };

  const dictationBusy = dictationPhase === 'listening' || dictationPhase === 'transcribing';
  const micActive = caps.stt ? dictationBusy : isRecording;
  const micLabel = caps.stt
    ? t(dictationBusy ? 'chat.dictation.stop' : 'chat.dictation.start')
    : t(isRecording ? 'chat.attachments.micStop' : 'chat.attachments.mic');
  const dictationAnnouncement =
    dictationPhase === 'listening'
      ? t('chat.dictation.listening')
      : dictationPhase === 'transcribing'
        ? t('chat.dictation.transcribing')
        : dictationPhase === 'error'
          ? t('chat.dictation.error')
          : '';

  return (
    <ComposerPrimitive.Root
      data-testid="chat-composer"
      aria-disabled={approvalLocked}
      aria-describedby={approvalLocked ? approvalHintId : undefined}
      onPaste={handlePaste}
      onDrop={handleDrop}
      onDragOver={handleDragOver}
      className="relative mx-3 mb-3 flex flex-col gap-2 rounded-[var(--radius-xl)] border border-border bg-surface p-2 shadow-[var(--shadow-popover)] sm:mx-4"
    >
      {approvalLocked ? (
        <p id={approvalHintId} className="px-1 text-xs text-warning">
          {t('approval.lock')}
        </p>
      ) : null}
      <fieldset disabled={approvalLocked} className="contents">
        {menuOpen ? (
          <SkillPicker
            groups={pickerGroups}
            activeOptionId={activeOptionId}
            listboxId={listboxId}
            onSelect={handlePickItem}
          />
        ) : null}
        {pinnedSkill != null || (uploads !== undefined && uploads.items.length > 0) ? (
          <div className="flex flex-wrap gap-2">
            {pinnedSkill != null ? (
              <SkillPill name={pinnedSkill.name} onRemove={() => onPinSkill?.(null)} />
            ) : null}
            {uploads !== undefined
              ? uploads.items.map((item) => (
                  <AttachmentChip key={item.localId} item={item} onRemove={uploads.remove} />
                ))
              : null}
          </div>
        ) : null}
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
        <div className="flex items-end gap-2">
          {uploads !== undefined ? (
            <>
              <input
                ref={fileInputRef}
                type="file"
                multiple
                className="sr-only"
                aria-label={t('chat.attachments.add')}
                disabled={approvalLocked}
                onChange={handleFileChange}
              />
              <Button
                type="button"
                size="icon"
                variant="ghost"
                aria-label={t('chat.attachments.add')}
                disabled={approvalLocked}
                onClick={() => {
                  if (approvalLocked) return;
                  fileInputRef.current?.click();
                }}
                className="rounded-full text-text-muted hover:text-text"
              >
                <Paperclip data-icon aria-hidden="true" className="size-4" />
              </Button>
              <Button
                type="button"
                size="icon"
                variant="ghost"
                aria-label={micLabel}
                aria-pressed={micActive}
                disabled={approvalLocked}
                onClick={handleMic}
                className="rounded-full text-text-muted hover:text-text"
              >
                {micActive ? (
                  <Square data-icon aria-hidden="true" className="size-3.5 fill-current" />
                ) : (
                  <Mic data-icon aria-hidden="true" className="size-4" />
                )}
              </Button>
            </>
          ) : null}
          <ComposerPrimitive.Input
            ref={composerInputRef}
            rows={1}
            placeholder={t('chat.composer.placeholder')}
            aria-label={t('chat.composer.placeholder')}
            // The composer is a plain message textbox by default; it only takes on APG
            // combobox semantics while the '/'-menu is open. Applying role="combobox"
            // unconditionally reclassifies the input away from role=textbox even when
            // idle — that regressed shell.spec.ts (getByRole('textbox', 'Ask Aura')) and
            // made screen readers announce a listbox popup on every plain message.
            role={menuOpen ? 'combobox' : undefined}
            aria-expanded={menuOpen}
            aria-controls={menuOpen ? listboxId : undefined}
            aria-activedescendant={menuOpen ? activeOptionId : undefined}
            aria-haspopup={menuOpen ? 'listbox' : undefined}
            aria-autocomplete={menuOpen ? 'list' : undefined}
            disabled={approvalLocked}
            onKeyDown={handleComposerKeyDown}
            className="max-h-40 min-h-[44px] flex-1 resize-none bg-transparent px-3 py-2 text-[1.0625rem] leading-relaxed text-text outline-none placeholder:text-text-faint focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          />
          {/* Reasoning-effort selector (WEBMODEL-01/03, D-13): a compact native select — keyboard-
            and screen-reader-correct out of the box, and separate from the textbox so it never
            reclassifies the input or intercepts Enter-send / paste / drop. It renders ONLY the
            model's advertised levels (effortLevels, auto-first); absent ⇒ not rendered. */}
          {effortLevels !== undefined && effortLevels.length > 0 ? (
            <div className="relative flex items-center self-center">
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
          {isRunning ? (
            <Button asChild size="icon" className="rounded-full">
              <ComposerPrimitive.Cancel
                aria-label={t('chat.composer.stopAria')}
                disabled={approvalLocked}
              >
                <Square data-icon aria-hidden="true" className="size-3.5 fill-current" />
              </ComposerPrimitive.Cancel>
            </Button>
          ) : (
            <Button asChild size="icon" className="rounded-full">
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
    </ComposerPrimitive.Root>
  );
}
