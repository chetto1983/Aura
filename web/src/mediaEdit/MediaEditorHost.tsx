import { useQuery } from '@tanstack/react-query';
import { X } from 'lucide-react';
import { lazy, Suspense, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { getAsset } from '../chat/attachments/api';
import type { Asset } from '../chat/attachments/types';
import { formatSize } from '../chat/artifacts/artifactMeta';
import { useAssetContent } from '../chat/artifacts/renderers/useAssetContent';
import { editableKind, type EditKind } from './editRules';
import { MediaEditorLayer } from './MediaEditorLayer';
import { Button } from '@/components/ui/button';

// MediaEditorHost — resolves what the asset is (the Studio stage does not know its MIME type or
// file name; getAsset does on every surface), loads its bytes, then picks the editor.

const PhotoEditor = lazy(() => import('./PhotoEditor'));
const VideoEditor = lazy(() => import('./VideoEditor'));

// Source and output both sit in memory while a clip is edited; past this size the operator is
// asked first. The server's own video limit already bounds what can exist at all.
const LARGE_VIDEO_BYTES = 500 * 1024 * 1024;

function Notice({
  onClose,
  children,
}: {
  readonly onClose: () => void;
  readonly children: ReactNode;
}) {
  const { t } = useTranslation();
  return (
    <MediaEditorLayer label={t('mediaEdit.title')} onEscape={onClose}>
      <header className="flex justify-end border-b border-border px-4 py-2">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-label={t('mediaEdit.close')}
          onClick={onClose}
        >
          <X aria-hidden="true" className="size-4" />
        </Button>
      </header>
      <div
        role="status"
        className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center text-sm text-text-muted"
      >
        {children}
      </div>
    </MediaEditorLayer>
  );
}

function LoadedEditor({ asset, onClose }: { readonly asset: Asset; readonly onClose: () => void }) {
  const { t } = useTranslation();
  const { data, error } = useAssetContent(asset.id, 'blob');
  if (error !== undefined) return <Notice onClose={onClose}>{t('mediaEdit.loadFailed')}</Notice>;
  if (data === undefined) return <Notice onClose={onClose}>{t('mediaEdit.loading')}</Notice>;
  const Editor = editableKind(asset.mime_type) === 'image' ? PhotoEditor : VideoEditor;
  return (
    <Suspense fallback={<Notice onClose={onClose}>{t('mediaEdit.loading')}</Notice>}>
      <Editor asset={asset} source={data} onClose={onClose} />
    </Suspense>
  );
}

export default function MediaEditorHost({
  assetId,
  kind,
  onClose,
}: {
  readonly assetId: string;
  readonly kind: EditKind;
  readonly onClose: () => void;
}) {
  const { t } = useTranslation();
  const [largeAccepted, setLargeAccepted] = useState(false);
  const asset = useQuery({
    queryKey: ['media-edit', 'asset', assetId],
    queryFn: () => getAsset(assetId),
    retry: false,
  });

  if (asset.isPending) return <Notice onClose={onClose}>{t('mediaEdit.loading')}</Notice>;
  if (asset.isError) return <Notice onClose={onClose}>{t('mediaEdit.loadFailed')}</Notice>;
  if (editableKind(asset.data.mime_type) !== kind) {
    return <Notice onClose={onClose}>{t('mediaEdit.notEditable')}</Notice>;
  }
  if (kind === 'video' && asset.data.size_bytes > LARGE_VIDEO_BYTES && !largeAccepted) {
    return (
      <Notice onClose={onClose}>
        <p>{t('mediaEdit.large.body', { size: formatSize(asset.data.size_bytes, t) })}</p>
        <Button
          type="button"
          size="sm"
          onClick={() => {
            setLargeAccepted(true);
          }}
        >
          {t('mediaEdit.large.confirm')}
        </Button>
      </Notice>
    );
  }
  return <LoadedEditor asset={asset.data} onClose={onClose} />;
}
