import { Ban, Blend, Circle, ScanLine, Sun, ZoomIn } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAssetSource } from '../chat/artifacts/renderers/assetSourceContext';
import { setJunctionTransition } from './commands';
import {
  clipTimelineDuration,
  sourceOf,
  type ClipJunction,
  type JunctionTransition,
  type ProjectSource,
  type VideoProject,
} from './project';
import { Button } from '@/components/ui/button';
import { Slider } from '@/components/ui/slider';

interface JunctionTransitionInspectorProps {
  readonly project: VideoProject;
  readonly junction: ClipJunction;
  readonly onCommand: (edit: (current: VideoProject) => VideoProject) => void;
}

const OPTIONS = [
  { id: 'none', icon: Ban },
  { id: 'crossfade', icon: Blend },
  { id: 'fadeBlack', icon: Circle },
  { id: 'fadeWhite', icon: Sun },
  { id: 'zoom', icon: ZoomIn },
  { id: 'blur', icon: ScanLine },
] as const satisfies readonly {
  readonly id: JunctionTransition;
  readonly icon: typeof Ban;
}[];

function MediaThumbnail({ source }: { readonly source: ProjectSource | undefined }) {
  const { assetUrl, streamUrl } = useAssetSource();
  if (source === undefined) return <span />;
  if (source.kind === 'image') {
    return <img src={assetUrl(source.assetId)} alt="" aria-hidden="true" draggable={false} />;
  }
  return (
    // Decorative, muted transition preview.
    // oxlint-disable-next-line jsx-a11y/media-has-caption
    <video
      src={streamUrl(source.assetId)}
      muted
      playsInline
      preload="metadata"
      aria-hidden="true"
    />
  );
}

export function JunctionTransitionInspector({
  project,
  junction,
  onCommand,
}: JunctionTransitionInspectorProps) {
  const { t } = useTranslation();
  const toIndex = project.video.findIndex((clip) => clip.id === junction.toClipId);
  const incoming = project.video[toIndex];
  const outgoing = project.video[toIndex - 1];
  if (incoming === undefined || outgoing?.id !== junction.fromClipId) return null;
  const selected = incoming.junctionTransition ?? 'none';
  const maxDuration = Math.max(
    0.1,
    Math.min(5, clipTimelineDuration(outgoing) / 2, clipTimelineDuration(incoming) / 2),
  );
  const duration = Math.min(incoming.junctionDuration ?? 1, maxDuration);
  const source = sourceOf(project, incoming.sourceId);

  function choose(transition: JunctionTransition) {
    onCommand((current) =>
      setJunctionTransition(current, {
        ...junction,
        transition,
        duration,
      }),
    );
  }

  return (
    <section className="video-studio-transition-inspector">
      <h3>{t('videoStudio.inspector.transition.title')}</h3>
      <div className="video-studio-transition-grid">
        {OPTIONS.map(({ id, icon: Icon }) => (
          <Button
            key={id}
            type="button"
            variant="ghost"
            className="video-studio-transition-card"
            data-transition={id}
            aria-pressed={selected === id}
            onClick={() => {
              choose(id);
            }}
          >
            <span className="video-studio-transition-preview" aria-hidden="true">
              {id === 'none' ? <Icon /> : <MediaThumbnail source={source} />}
              {id === 'none' ? null : <Icon className="video-studio-transition-glyph" />}
            </span>
            <span>{t(`videoStudio.inspector.transition.presets.${id}`)}</span>
          </Button>
        ))}
      </div>
      {selected === 'none' ? null : (
        <div
          role="group"
          aria-label={t('videoStudio.inspector.transition.duration')}
          className="video-studio-junction-duration"
        >
          <span>{t('videoStudio.inspector.transition.duration')}</span>
          <output>{duration.toFixed(1)}s</output>
          <Slider
            key={`${junction.fromClipId}-${junction.toClipId}-${String(duration)}`}
            aria-label={t('videoStudio.inspector.transition.duration')}
            min={0.1}
            max={maxDuration}
            step={0.1}
            defaultValue={[duration]}
            onValueCommit={([next = duration]) => {
              onCommand((current) =>
                setJunctionTransition(current, {
                  ...junction,
                  transition: selected,
                  duration: next,
                }),
              );
            }}
          />
        </div>
      )}
    </section>
  );
}
