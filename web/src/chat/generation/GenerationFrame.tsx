import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { MediaKind } from './generationState';
import { ImageGeneration } from '@/components/image-generation';

// GenerationFrame (spec §5 "While running", R22/R23): the ImageGeneration element for a
// running image_generate/video_generate call, or for a detached video job replayed from the
// thread. Running shows elapsed time from mount, not a percentage: the video client reads
// only the job status, so there is no progress value. A detached job is static, because a
// replayed part must not look like work still in flight.

export interface GenerationFrameProps {
  readonly kind: MediaKind;
  readonly prompt: string;
  readonly aspectRatio: string;
  readonly generating: boolean;
}

export function GenerationFrame({ kind, prompt, aspectRatio, generating }: GenerationFrameProps) {
  const { t } = useTranslation();
  const runningLabel =
    kind === 'video'
      ? t('media.generation.generatingVideo')
      : t('media.generation.generatingImage');
  return (
    <ImageGeneration
      data-testid="generation-frame"
      data-generating={generating}
      prompt={prompt}
      aspectRatio={aspectRatio}
      generating={generating}
      label={generating ? runningLabel : t('media.generation.arriving')}
      meta={generating ? <ElapsedClock /> : undefined}
    />
  );
}

// The ticking number is a timer, whose implicit aria-live is off: its name carries the
// value when a reader reaches it, and nothing is announced once a second.
function ElapsedClock() {
  const { t } = useTranslation();
  const clock = useElapsedClock();
  return (
    <span role="timer" aria-label={t('media.generation.elapsed', { time: clock })}>
      {clock}
    </span>
  );
}

function useElapsedClock(): string {
  const [mountedAt] = useState(() => Date.now());
  const [now, setNow] = useState(mountedAt);
  useEffect(() => {
    const id = setInterval(() => {
      setNow(Date.now());
    }, 1000);
    return () => {
      clearInterval(id);
    };
  }, []);
  const seconds = Math.max(0, Math.floor((now - mountedAt) / 1000));
  return `${String(Math.floor(seconds / 60))}:${String(seconds % 60).padStart(2, '0')}`;
}
