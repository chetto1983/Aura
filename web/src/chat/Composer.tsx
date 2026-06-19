import { useAuiState, ComposerPrimitive } from '@assistant-ui/react';
import { useRef, useState, type ChangeEvent, type ClipboardEvent, type DragEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { AttachmentChip } from './attachments/AttachmentChip';
import type { AttachmentUploads } from './attachments/useAttachmentUploads';

// Composer: the query input + the Send↔Stop swap. Enter sends / Shift+Enter
// newlines / Esc cancels are handled by ComposerPrimitive.Input. Stop is
// ComposerPrimitive.Cancel → api.thread().cancelRun() → the external-store
// onCancel aborts the in-flight fetch (the server streamSSE unwinds on ctx.Done).
//
// Accent is reserved for the primary Send CTA only (UI-SPEC §Color list item 1);
// Stop is a neutral danger-tinted control so the accent stays scarce.

interface ComposerProps {
  readonly uploads?: AttachmentUploads;
}

export function Composer({ uploads }: ComposerProps) {
  const { t } = useTranslation();
  const isRunning = useAuiState((s) => s.thread.isRunning);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const recorderRef = useRef<MediaRecorder | null>(null);
  const recordingStreamRef = useRef<MediaStream | null>(null);
  const chunksRef = useRef<Blob[]>([]);
  const [isRecording, setIsRecording] = useState(false);
  const sendDisabled = uploads?.hasBlockingUploads === true;

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

  const handleMic = () => {
    if (isRecording) {
      stopRecording();
      return;
    }
    void startRecording();
  };

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
            <button
              type="button"
              aria-label={t('chat.attachments.add')}
              onClick={() => fileInputRef.current?.click()}
              className="flex min-h-10 min-w-10 items-center justify-center rounded-full text-text-muted outline-none hover:bg-surface-2 hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
            >
              <PaperclipIcon />
            </button>
            <button
              type="button"
              aria-label={t(isRecording ? 'chat.attachments.micStop' : 'chat.attachments.mic')}
              onClick={handleMic}
              className="flex min-h-10 min-w-10 items-center justify-center rounded-full text-text-muted outline-none hover:bg-surface-2 hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
            >
              {isRecording ? <StopIcon /> : <MicIcon />}
            </button>
          </>
        ) : null}
        <ComposerPrimitive.Input
          rows={1}
          placeholder={t('chat.composer.placeholder')}
          aria-label={t('chat.composer.placeholder')}
          className="max-h-40 min-h-10 flex-1 resize-none bg-transparent px-3 py-2 text-[1.0625rem] leading-relaxed text-text outline-none placeholder:text-text-faint focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        />
        {isRunning ? (
          <ComposerPrimitive.Cancel
            aria-label={t('chat.composer.stopAria')}
            className="flex min-h-10 min-w-10 items-center justify-center rounded-full bg-accent text-on-accent outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          >
            <span aria-hidden="true" className="h-3 w-3 rounded-[3px] bg-current" />
          </ComposerPrimitive.Cancel>
        ) : (
          <ComposerPrimitive.Send
            aria-label={t('chat.composer.sendAria')}
            disabled={sendDisabled}
            className="flex min-h-10 min-w-10 items-center justify-center rounded-full bg-accent text-on-accent outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:bg-surface-3 disabled:text-text-disabled"
          >
            <ArrowUpIcon />
          </ComposerPrimitive.Send>
        )}
      </div>
    </ComposerPrimitive.Root>
  );
}

function PaperclipIcon() {
  return (
    <svg
      width="17"
      height="17"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="m21.4 11.6-8.8 8.8a6 6 0 0 1-8.5-8.5l9.2-9.2a4 4 0 0 1 5.7 5.7l-9.2 9.2a2 2 0 1 1-2.8-2.8l8.8-8.8" />
    </svg>
  );
}

function MicIcon() {
  return (
    <svg
      width="17"
      height="17"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M12 3a3 3 0 0 0-3 3v6a3 3 0 0 0 6 0V6a3 3 0 0 0-3-3Z" />
      <path d="M19 10v2a7 7 0 0 1-14 0v-2" />
      <path d="M12 19v3" />
    </svg>
  );
}

function StopIcon() {
  return <span aria-hidden="true" className="h-3 w-3 rounded-[3px] bg-current" />;
}

function ArrowUpIcon() {
  return (
    <svg
      width="17"
      height="17"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.4"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M12 19V5" />
      <path d="m5 12 7-7 7 7" />
    </svg>
  );
}
