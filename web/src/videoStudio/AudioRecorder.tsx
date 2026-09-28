import { Mic, Square } from 'lucide-react';
import { useEffect, useEffectEvent, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import WaveSurfer from 'wavesurfer.js';
import RecordPlugin from 'wavesurfer.js/plugins/record';
import { Button } from '@/components/ui/button';

// AudioRecorder.tsx — a voice recorded from the microphone, with wavesurfer's Record plugin drawing
// the live level while it runs. The plugin picks the container MediaRecorder offers (webm/Opus in
// Chromium); the file is typed by the container alone, because the audio picker's list and the
// server's name containers, not codecs.

type Phase = 'idle' | 'starting' | 'recording' | 'refused';

type Recorder = ReturnType<typeof RecordPlugin.create>;

export function AudioRecorder({ onRecorded }: { readonly onRecorded: (file: File) => void }) {
  const { t } = useTranslation();
  const host = useRef<HTMLDivElement>(null);
  const recorder = useRef<Recorder>(undefined);
  const [phase, setPhase] = useState<Phase>('idle');
  const handOver = useEffectEvent((blob: Blob) => {
    // A recorder that names no type recorded what Chromium records: webm.
    const type = blob.type === '' ? 'audio/webm' : (blob.type.split(';')[0] ?? blob.type);
    onRecorded(new File([blob], `recording.${type.split('/')[1] ?? 'webm'}`, { type }));
  });

  useEffect(() => {
    const container = host.current;
    if (container === null) return undefined;
    let live = true;
    const record = RecordPlugin.create({ scrollingWaveform: true, renderRecordedAudio: false });
    const surfer = WaveSurfer.create({
      container,
      height: 40,
      waveColor: getComputedStyle(container).color,
      plugins: [record],
    });
    record.on('record-end', (blob) => {
      // Closing the panel mid-recording destroys the plugin, and its teardown still hands over what
      // it had: a recording nobody finished is not a sound anybody asked for.
      if (live) handOver(blob);
    });
    recorder.current = record;
    return () => {
      live = false;
      recorder.current = undefined;
      surfer.destroy();
    };
  }, []);

  async function start() {
    const record = recorder.current;
    if (record === undefined) return;
    setPhase('starting');
    try {
      await record.startRecording();
      setPhase('recording');
    } catch {
      setPhase('refused');
    }
  }

  function stop() {
    setPhase('idle');
    recorder.current?.stopRecording();
  }

  return (
    <div className="video-studio-recorder">
      <div
        ref={host}
        className="video-studio-recorder-level"
        data-live={phase === 'recording'}
        aria-hidden="true"
      />
      {phase === 'recording' ? (
        <>
          <p role="status" className="text-xs text-text-muted">
            {t('videoStudio.audio.panel.recording')}
          </p>
          <Button type="button" size="sm" onClick={stop}>
            <Square aria-hidden="true" />
            {t('videoStudio.audio.panel.stop')}
          </Button>
        </>
      ) : (
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={phase === 'starting'}
          onClick={() => void start()}
        >
          <Mic aria-hidden="true" />
          {t('videoStudio.audio.panel.record')}
        </Button>
      )}
      {phase === 'refused' ? (
        <p role="alert" className="text-xs text-danger">
          {t('videoStudio.audio.panel.noMicrophone')}
        </p>
      ) : null}
    </div>
  );
}
