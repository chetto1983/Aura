import { Upload } from 'lucide-react';
import { useEffect, useId, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useVoiceCapabilities } from '../chat/voice/useVoiceCapabilities';
import { synthesizeSpeechAudio } from '../chat/voice/voiceApi';
import { AudioRecorder } from './AudioRecorder';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Textarea } from '@/components/ui/textarea';

// VideoStudio_audioPanel.tsx — the rail's Add audio: a sound from a file, from the microphone, or
// read aloud from a text by Aura's own voice (spec §UI). Whatever its origin, a sound enters the
// project through the workspace's one door, `addFile`, so it is probed, uploaded and placed at the
// playhead exactly as a picked file is. Text to speech is offered only where /api/voice/capabilities
// says a voice is configured; the probe runs when the panel opens, not with the editor.

/** How much of a text names the sound it became. */
const LABEL_CHARS = 40;

type Reading =
  | { readonly state: 'idle' }
  | { readonly state: 'reading' }
  | { readonly state: 'cut' }
  | { readonly state: 'failed'; readonly reason: string };

interface SpeechDoorProps {
  readonly onSound: (file: File, label: string) => void;
  readonly onAdded: () => void;
}

function SpeechDoor({ onSound, onAdded }: SpeechDoorProps) {
  const { t } = useTranslation();
  const { tts } = useVoiceCapabilities();
  const id = useId();
  const [text, setText] = useState('');
  const [reading, setReading] = useState<Reading>({ state: 'idle' });
  // Aborted when the panel closes: DialogContent unmounts, and a reading nobody waits for adds
  // nothing.
  const running = useRef<AbortController>(undefined);
  useEffect(
    () => () => {
      running.current?.abort();
    },
    [],
  );
  if (!tts) return null;

  async function speak() {
    const words = text.trim();
    const controller = new AbortController();
    running.current = controller;
    setReading({ state: 'reading' });
    try {
      const { blob, truncated } = await synthesizeSpeechAudio(words, controller.signal);
      if (controller.signal.aborted) return;
      const file = new File([blob], 'speech.mp3', { type: blob.type || 'audio/mpeg' });
      onSound(
        file,
        words.length > LABEL_CHARS ? `${words.slice(0, LABEL_CHARS).trimEnd()}…` : words,
      );
      if (truncated) {
        setReading({ state: 'cut' });
        return;
      }
      setReading({ state: 'idle' });
      onAdded();
    } catch (error) {
      if (controller.signal.aborted) return;
      setReading({
        state: 'failed',
        reason: error instanceof Error ? error.message : String(error),
      });
    }
  }

  return (
    <div className="grid gap-2">
      <label htmlFor={id} className="text-xs font-medium">
        {t('videoStudio.audio.panel.speech')}
      </label>
      <Textarea
        id={id}
        rows={3}
        value={text}
        onChange={(event) => {
          setText(event.target.value);
        }}
      />
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={text.trim() === '' || reading.state === 'reading'}
        onClick={() => void speak()}
      >
        {t(
          reading.state === 'reading'
            ? 'videoStudio.audio.panel.speaking'
            : 'videoStudio.audio.panel.speak',
        )}
      </Button>
      {reading.state === 'cut' ? (
        <p role="status" className="text-xs text-text-muted">
          {t('videoStudio.audio.panel.truncated')}
        </p>
      ) : null}
      {reading.state === 'failed' ? (
        <p role="alert" className="text-xs text-danger">
          {t('videoStudio.audio.panel.speechFailed', { reason: reading.reason })}
        </p>
      ) : null}
    </div>
  );
}

interface AudioPanelProps {
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly onUpload: () => void;
  readonly onSound: (file: File, label: string) => Promise<void>;
}

export function AudioPanel({ open, onOpenChange, onUpload, onSound }: AudioPanelProps) {
  const { t, i18n } = useTranslation();
  const close = () => {
    onOpenChange(false);
  };
  const add = (file: File, label: string) => {
    void onSound(file, label);
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="video-studio-audio-panel">
        <DialogHeader>
          <DialogTitle>{t('videoStudio.audio.panel.title')}</DialogTitle>
          <DialogDescription>{t('videoStudio.audio.panel.description')}</DialogDescription>
        </DialogHeader>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => {
            close();
            onUpload();
          }}
        >
          <Upload aria-hidden="true" />
          {t('videoStudio.audio.panel.upload')}
        </Button>
        <AudioRecorder
          onRecorded={(file) => {
            const time = new Date().toLocaleTimeString(i18n.language, {
              hour: '2-digit',
              minute: '2-digit',
            });
            add(file, t('videoStudio.audio.panel.recordingLabel', { time }));
            close();
          }}
        />
        <SpeechDoor onSound={add} onAdded={close} />
      </DialogContent>
    </Dialog>
  );
}
