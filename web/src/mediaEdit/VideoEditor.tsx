import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { Asset } from '../chat/attachments/types';
import { CommandRefusal } from '../videoStudio/commands';
import VideoStudio from '../videoStudio/VideoStudio';
import { projectFromClip, uploadSource, type StudioOpen } from '../videoStudio/VideoStudio_sources';
import { MediaEditorLayer } from './MediaEditorLayer';
import { probeVideo } from './videoMedia';

export interface EditorProps {
  readonly asset: Asset;
  readonly source: Blob;
  readonly onClose: () => void;
}

type Opening =
  | { readonly kind: 'loading' }
  | { readonly kind: 'ready'; readonly open: StudioOpen }
  | { readonly kind: 'failed'; readonly key: string };

/**
 * The former single-clip editor ended here in a second, incompatible workspace. A clip now enters
 * the composition directly: trim, crop, rotation, audio, titles and multi-track edits all share
 * one history and one export. Garage files first receive a durable Aura asset id, because a saved
 * project cannot point at the temporary `garage:` identity this host used while downloading it.
 */
export default function VideoEditor({ asset, source, onClose }: EditorProps) {
  const { t } = useTranslation();
  const [opening, setOpening] = useState<Opening>({ kind: 'loading' });
  const preparation = useRef<Promise<StudioOpen> | undefined>(undefined);

  useEffect(() => {
    let live = true;
    const controller = new AbortController();
    preparation.current ??= (async () => {
      const info = await probeVideo(source, controller.signal);
      const assetId = asset.id.startsWith('garage:')
        ? await uploadSource(
            new File([source], asset.file_name, { type: asset.mime_type || source.type }),
          )
        : asset.id;
      const dot = asset.file_name.lastIndexOf('.');
      const projectName = dot > 0 ? asset.file_name.slice(0, dot) : asset.file_name;
      return {
        kind: 'project',
        project: projectFromClip(projectName, assetId, info, {
          start: 0,
          end: info.duration,
        }),
      } satisfies StudioOpen;
    })();
    preparation.current.then(
      (open) => {
        if (live) setOpening({ kind: 'ready', open });
      },
      (error: unknown) => {
        if (!live || controller.signal.aborted) return;
        setOpening({
          kind: 'failed',
          key: error instanceof CommandRefusal ? error.reasonKey : 'mediaEdit.loadFailed',
        });
      },
    );
    return () => {
      live = false;
      controller.abort();
    };
  }, [asset, source]);

  if (opening.kind === 'ready') {
    return <VideoStudio open={opening.open} onClose={onClose} />;
  }

  return (
    <MediaEditorLayer label={t('mediaEdit.editName', { name: asset.file_name })} onEscape={onClose}>
      <p
        role={opening.kind === 'failed' ? 'alert' : 'status'}
        className="flex flex-1 items-center justify-center p-6 text-sm text-text-muted"
      >
        {opening.kind === 'failed' ? t(opening.key) : t('mediaEdit.loading')}
      </p>
    </MediaEditorLayer>
  );
}
