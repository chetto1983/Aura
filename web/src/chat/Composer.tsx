import { useAui, useAuiState, ComposerPrimitive } from '@assistant-ui/react';
import { ArrowUp, Mic, Paperclip, Square } from 'lucide-react';
import {
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
  type ClipboardEvent,
  type DragEvent,
} from 'react';
import { useTranslation } from 'react-i18next';
import { AttachmentChip } from './attachments/AttachmentChip';
import type { AttachmentUploads } from './attachments/useAttachmentUploads';
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

interface ComposerProps {
  readonly uploads?: AttachmentUploads;
  readonly draftPrompt?: ComposerDraftPrompt | undefined;
}

export interface ComposerDraftPrompt {
  readonly text: string;
  readonly nonce: number;
}

type DictationPhase = 'idle' | 'listening' | 'transcribing' | 'error';

export function Composer({ uploads, draftPrompt }: ComposerProps) {
  const { t } = useTranslation();
  const aui = useAui();
  const isRunning = useAuiState((s) => s.thread.isRunning);
  const dictation = useAuiState((s) => s.composer.dictation);
  const { caps, markTurnDictated } = useVoiceMode();
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const composerInputRef = useRef<HTMLTextAreaElement | null>(null);
  const recorderRef = useRef<MediaRecorder | null>(null);
  const recordingStreamRef = useRef<MediaStream | null>(null);
  const chunksRef = useRef<Blob[]>([]);
  const appliedDraftNonce = useRef<number | undefined>(undefined);
  const [isRecording, setIsRecording] = useState(false);
  const [dictationPhase, setDictationPhase] = useState<DictationPhase>('idle');
  const dictationStartLen = useRef(0);
  const wasDictating = useRef(false);
  const isDictating = dictation != null;
  const sendDisabled = uploads?.hasBlockingUploads === true;

  useEffect(() => {
    if (draftPrompt === undefined || draftPrompt.nonce === appliedDraftNonce.current) return;
    appliedDraftNonce.current = draftPrompt.nonce;
    aui.composer().setText(draftPrompt.text);
    requestAnimationFrame(() => composerInputRef.current?.focus());
  }, [aui, draftPrompt]);

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
    if (files.length > 0) uploads?.addFiles(files);
  };

  const handleFileChange = (event: ChangeEvent<HTMLInputElement>) => {
    if (event.currentTarget.files !== null) addFiles(event.currentTarget.files);
    event.currentTarget.value = '';
  };

  const handlePaste = (event: ClipboardEvent) => {
    if (event.clipboardData.files.length === 0) return;
    addFiles(event.clipboardData.files);
  };

  const handleDrop = (event: DragEvent) => {
    if (event.dataTransfer.files.length === 0) return;
    event.preventDefault();
    addFiles(event.dataTransfer.files);
  };

  const handleDragOver = (event: DragEvent) => {
    if (event.dataTransfer.types.includes('Files')) event.preventDefault();
  };

  const stopRecording = () => {
    recorderRef.current?.stop();
  };

  const startRecording = async () => {
    if (uploads === undefined || isRecording) return;
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
      onPaste={handlePaste}
      onDrop={handleDrop}
      onDragOver={handleDragOver}
      className="mx-3 mb-3 flex flex-col gap-2 rounded-[var(--radius-xl)] border border-border bg-surface p-2 shadow-[var(--shadow-popover)] sm:mx-4"
    >
      {uploads !== undefined && uploads.items.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          {uploads.items.map((item) => (
            <AttachmentChip key={item.localId} item={item} onRemove={uploads.remove} />
          ))}
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
              onChange={handleFileChange}
            />
            <Button
              type="button"
              size="icon"
              variant="ghost"
              aria-label={t('chat.attachments.add')}
              onClick={() => fileInputRef.current?.click()}
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
              onClick={handleMic}
              className="rounded-full text-text-muted hover:text-text data-[dictating=true]:text-accent"
              data-dictating={caps.stt && dictationBusy ? 'true' : undefined}
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
          className="max-h-40 min-h-[44px] flex-1 resize-none bg-transparent px-3 py-2 text-[1.0625rem] leading-relaxed text-text outline-none placeholder:text-text-faint focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        />
        {isRunning ? (
          <Button asChild size="icon" className="rounded-full">
            <ComposerPrimitive.Cancel aria-label={t('chat.composer.stopAria')}>
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
    </ComposerPrimitive.Root>
  );
}
