import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Download } from 'lucide-react';
import { EditMediaButton } from '../../../mediaEdit/EditMediaButton';
import { useBlobPreview } from '../useBlobPreview';
import { useAssetSource } from './assetSourceContext';
import { PreviewError, PreviewLoading, type RendererProps } from './PreviewStatus';
import { ImageActions, ImageFilename, ImageRoot, ImageZoom } from '@/components/image';
import { ToolImage } from '@/components/tool-ui-image';

// An image delivered into the chat (spec §5): the Image elements fed by the relabelled
// object URL, with a full screen view and actions. SVG never gets here (previewKind gates it
// to download-only). Download is the active tier's asset URL, so a share page stays token
// scoped and no provider URL is ever fetched.
export default function GeneratedImagePreview({ assetId, mimeType, fileName }: RendererProps) {
  const { t } = useTranslation();
  const { assetUrl } = useAssetSource();
  const { url, error } = useBlobPreview(assetId, mimeType);
  const [failedUrl, setFailedUrl] = useState<string>();
  if (error !== undefined || (url !== undefined && failedUrl === url)) {
    return (
      <div className="flex flex-col gap-2">
        <PreviewError {...(error === undefined ? {} : { detail: error })} />
        <a
          href={assetUrl(assetId)}
          download={fileName}
          data-required-touch-target
          className="inline-flex min-h-11 min-w-11 w-fit items-center gap-2 rounded-[var(--radius-sm)] border border-border px-3 text-sm text-accent-text focus-visible:outline-2 focus-visible:outline-accent"
          aria-label={t('artifacts.preview.download', { name: fileName })}
        >
          <Download aria-hidden className="size-4" />
          {t('display.artifact.download')}
        </a>
      </div>
    );
  }
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
        <ToolImage
          id={assetId}
          src={url}
          alt={fileName}
          ratio="auto"
          onError={() => {
            setFailedUrl(url);
          }}
        />
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
          extra={
            <EditMediaButton
              assetId={assetId}
              kind="image"
              mimeType={mimeType}
              fileName={fileName}
              compact
            />
          }
        />
      </div>
    </ImageRoot>
  );
}
