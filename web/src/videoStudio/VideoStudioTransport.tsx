import { Pause, Play, SkipBack, SkipForward } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { formatTimecode } from '../mediaEdit/timecode';
import { Button } from '@/components/ui/button';

interface VideoStudioTransportProps {
  readonly playing: boolean;
  readonly time: number;
  readonly duration: number;
  readonly onPlayToggle: () => void;
  readonly onSeek: (time: number) => void;
}

export function VideoStudioTransport({
  playing,
  time,
  duration,
  onPlayToggle,
  onSeek,
}: VideoStudioTransportProps) {
  const { t } = useTranslation();
  return (
    <div
      className="video-studio-transport"
      role="group"
      aria-label={t('videoStudio.transport.label')}
    >
      <span className="video-studio-timecode">
        {formatTimecode(time)} <span aria-hidden="true">/</span> {formatTimecode(duration)}
      </span>
      <div className="video-studio-transport-buttons">
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="video-studio-icon-button"
          aria-label={t('videoStudio.transport.start')}
          onClick={() => {
            onSeek(0);
          }}
        >
          <SkipBack aria-hidden="true" />
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="video-studio-play-button"
          aria-label={playing ? t('videoStudio.transport.pause') : t('videoStudio.transport.play')}
          onClick={onPlayToggle}
        >
          {playing ? <Pause aria-hidden="true" /> : <Play aria-hidden="true" />}
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="video-studio-icon-button"
          aria-label={t('videoStudio.transport.end')}
          onClick={() => {
            onSeek(duration);
          }}
        >
          <SkipForward aria-hidden="true" />
        </Button>
      </div>
      <span className="video-studio-transport-spacer" aria-hidden="true" />
    </div>
  );
}
