import { useTranslation } from 'react-i18next';
import { DisabledTile, ImageTile } from './ImageTile';
import type { StudioImageRef, StudioModel } from './studioApi';
import { maxImages, type StudioDraft } from './studioForm';

// StudioFrames — the composer's top row: the images a generation starts from. A video takes
// one start frame and, once it has one, an end frame if the model declares `last_frame`; an
// image takes references up to the model's declared ceiling. A model that takes none still
// shows a tile, disabled and saying so, because a row that silently disappears reads as a
// missing feature rather than a refused one.

interface StudioFramesProps {
  readonly draft: StudioDraft;
  readonly model: StudioModel;
  readonly onChange: (draft: StudioDraft) => void;
}

export function StudioFrames({ draft, model, onChange }: StudioFramesProps) {
  const { t } = useTranslation();
  const max = maxImages(model, draft.kind);
  const slotLabel =
    draft.kind === 'image' ? t('studio.frames.references') : t('studio.frames.firstFrame');

  if (max === 0) {
    return <DisabledTile label={slotLabel} reason={t('studio.frames.unsupported')} />;
  }

  const setImages = (images: readonly StudioImageRef[]) => {
    onChange({
      ...draft,
      images,
      // An end frame with nothing to start from is not a clip the provider can make.
      endFrame: images.length === 0 ? undefined : draft.endFrame,
    });
  };
  const takesEndFrame =
    draft.kind === 'video' &&
    (model.frame_images ?? []).includes('last_frame') &&
    draft.images.length > 0;

  return (
    <div className="flex flex-wrap items-center gap-2">
      {draft.images.map((image, index) => (
        <ImageTile
          key={image.id}
          label={slotLabel}
          image={image}
          onChange={(next) => {
            setImages(
              next === undefined
                ? draft.images.filter((_, at) => at !== index)
                : draft.images.map((current, at) => (at === index ? next : current)),
            );
          }}
        />
      ))}

      {draft.images.length < max ? (
        <ImageTile
          label={slotLabel}
          image={undefined}
          onChange={(next) => {
            if (next !== undefined) setImages([...draft.images, next]);
          }}
        />
      ) : null}

      {takesEndFrame ? (
        <ImageTile
          label={t('studio.frames.lastFrame')}
          image={draft.endFrame}
          onChange={(endFrame) => {
            onChange({ ...draft, endFrame });
          }}
        />
      ) : null}

      {max > 1 ? (
        <span className="text-[11px] text-text-faint tabular-nums">
          {t('studio.frames.count', { used: draft.images.length, max })}
        </span>
      ) : null}
    </div>
  );
}
