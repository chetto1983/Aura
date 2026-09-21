import { useQuery } from '@tanstack/react-query';
import { X } from 'lucide-react';
import { lazy, Suspense, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { getAsset } from '../chat/attachments/api';
import type { Asset } from '../chat/attachments/types';
import { formatSize } from '../chat/artifacts/artifactMeta';
import { useAssetContent } from '../chat/artifacts/renderers/useAssetContent';
import { directURL } from '../files/filesApi';
import { editableKind, type EditKind } from './editRules';
import type { AssetEditTarget, EditTarget, GarageEditTarget } from './mediaEditorContext';
import { MediaEditorLayer } from './MediaEditorLayer';
import { Button } from '@/components/ui/button';

const PhotoEditor = lazy(() => import('./PhotoEditor'));
const VideoEditor = lazy(() => import('./VideoEditor'));

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

function LargeVideoGate({
  kind,
  size,
  onClose,
  children,
}: {
  readonly kind: EditKind;
  readonly size: number;
  readonly onClose: () => void;
  readonly children: ReactNode;
}) {
  const { t } = useTranslation();
  const [accepted, setAccepted] = useState(false);
  if (kind !== 'video' || size <= LARGE_VIDEO_BYTES || accepted) return children;
  return (
    <Notice onClose={onClose}>
      <p>{t('mediaEdit.large.body', { size: formatSize(size, t) })}</p>
      <Button
        type="button"
        size="sm"
        onClick={() => {
          setAccepted(true);
        }}
      >
        {t('mediaEdit.large.confirm')}
      </Button>
    </Notice>
  );
}

function ReadyEditor({
  asset,
  source,
  onClose,
}: {
  readonly asset: Asset;
  readonly source: Blob;
  readonly onClose: () => void;
}) {
  const { t } = useTranslation();
  const Editor = editableKind(asset.mime_type) === 'image' ? PhotoEditor : VideoEditor;
  return (
    <Suspense fallback={<Notice onClose={onClose}>{t('mediaEdit.loading')}</Notice>}>
      <Editor asset={asset} source={source} onClose={onClose} />
    </Suspense>
  );
}

function DownloadedAssetEditor({
  asset,
  onClose,
}: {
  readonly asset: Asset;
  readonly onClose: () => void;
}) {
  const { t } = useTranslation();
  const { data, error } = useAssetContent(asset.id, 'blob');
  if (error !== undefined) return <Notice onClose={onClose}>{t('mediaEdit.loadFailed')}</Notice>;
  if (data === undefined) return <Notice onClose={onClose}>{t('mediaEdit.loading')}</Notice>;
  return <ReadyEditor asset={asset} source={data} onClose={onClose} />;
}

function AssetTargetEditor({
  target,
  onClose,
}: {
  readonly target: AssetEditTarget;
  readonly onClose: () => void;
}) {
  const { t } = useTranslation();
  const asset = useQuery({
    queryKey: ['media-edit', 'asset', target.assetId],
    queryFn: () => getAsset(target.assetId),
    retry: false,
  });

  if (asset.isPending) return <Notice onClose={onClose}>{t('mediaEdit.loading')}</Notice>;
  if (asset.isError) return <Notice onClose={onClose}>{t('mediaEdit.loadFailed')}</Notice>;
  if (editableKind(asset.data.mime_type) !== target.kind) {
    return <Notice onClose={onClose}>{t('mediaEdit.notEditable')}</Notice>;
  }
  return (
    <LargeVideoGate kind={target.kind} size={asset.data.size_bytes} onClose={onClose}>
      <DownloadedAssetEditor asset={asset.data} onClose={onClose} />
    </LargeVideoGate>
  );
}

function GarageTargetEditor({
  target,
  onClose,
}: {
  readonly target: GarageEditTarget;
  readonly onClose: () => void;
}) {
  return (
    <LargeVideoGate kind={target.kind} size={target.sizeBytes} onClose={onClose}>
      <GarageFileEditor target={target} onClose={onClose} />
    </LargeVideoGate>
  );
}

function GarageFileEditor({
  target,
  onClose,
}: {
  readonly target: GarageEditTarget;
  readonly onClose: () => void;
}) {
  const { t } = useTranslation();
  const file = useQuery({
    queryKey: ['media-edit', 'garage', target.garageObjectId],
    queryFn: async () => {
      const response = await fetch(directURL(target.garageObjectId, false));
      if (!response.ok) throw new Error(`HTTP ${String(response.status)}`);
      const source = await response.blob();
      const asset: Asset = {
        id: `garage:${target.garageObjectId}`,
        status: 'accepted',
        modality: target.kind,
        file_name: target.fileName,
        mime_type: source.type,
        declared_size_bytes: target.sizeBytes,
        size_bytes: source.size,
      };
      return { asset, source };
    },
    retry: false,
  });

  if (file.isPending) return <Notice onClose={onClose}>{t('mediaEdit.loading')}</Notice>;
  if (file.isError) return <Notice onClose={onClose}>{t('mediaEdit.loadFailed')}</Notice>;
  if (editableKind(file.data.asset.mime_type) !== target.kind) {
    return <Notice onClose={onClose}>{t('mediaEdit.notEditable')}</Notice>;
  }
  return <ReadyEditor asset={file.data.asset} source={file.data.source} onClose={onClose} />;
}

export default function MediaEditorHost({
  target,
  onClose,
}: {
  readonly target: EditTarget;
  readonly onClose: () => void;
}) {
  return 'assetId' in target ? (
    <AssetTargetEditor target={target} onClose={onClose} />
  ) : (
    <GarageTargetEditor target={target} onClose={onClose} />
  );
}
