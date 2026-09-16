import { useTranslation } from 'react-i18next';
import { useBlobPreview } from '../useBlobPreview';
import { useAssetSource } from './assetSourceContext';
import { PreviewError, PreviewLoading, type RendererProps } from './PreviewStatus';
import {
  ImageActions,
  ImageFilename,
  ImagePreview,
  ImageRoot,
  ImageZoom,
} from '@/components/image';

// An image delivered into the chat (spec §5): the Image elements fed by the relabelled
// object URL, with a full screen view and actions. SVG never gets here (previewKind gates it
// to download-only). Download is the active tier's asset URL, so a share page stays token
// scoped and no provider URL is ever fetched.
export default function GeneratedImagePreview({ assetId, mimeType, fileName }: RendererProps) {
  const { t } = useTranslation();
  const { assetUrl } = useAssetSource();
  const { url, error } = useBlobPreview(assetId, mimeType);
  if (error !== undefined) return <PreviewError detail={error} />;
  if (url === undefined) return <PreviewLoading />;
  return (
    <ImageRoot className="my-1">
      <ImageZoom
        src={url}
        alt={fileName}
        labels={{
          open: t('media.image.zoom', { name: fileName }),
          close: t('media.image.close'),
        }}
      >
        <ImagePreview src={url} alt={fileName} />
      </ImageZoom>
      <div className="flex min-w-0 items-center gap-2 border-t border-border py-1 pe-1 ps-3">
        <ImageFilename>{fileName}</ImageFilename>
        <ImageActions
          className="ms-auto shrink-0"
          src={url}
          downloadHref={assetUrl(assetId)}
          fileName={fileName}
          labels={{
            download: t('artifacts.preview.download', { name: fileName }),
            copy: t('media.image.copy'),
            copied: t('media.image.copied'),
            copyFailed: t('media.image.copyFailed'),
          }}
        />
      </div>
    </ImageRoot>
  );
}
