import { Suspense, lazy } from 'react';
import { useTranslation } from 'react-i18next';
import { Download, FileDown } from 'lucide-react';
import type { Asset } from '../attachments/types';
import { previewKind } from './artifactMeta';
import { PreviewLoading, type RendererProps } from './renderers/PreviewStatus';
import { useAssetSource } from './renderers/assetSourceContext';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';

// PreviewModal (WEBART-05 / D-09): a ~90vw×90vh Radix Dialog that previews an agent-
// delivered asset in-cockpit. It dispatches by previewKind (plan 03, the pure MIME gate)
// to one of six SUSPENSE-wrapped lazy renderer chunks — so docx-preview/read-excel-file never enter
// this modal's static import graph — or, for the download-only kinds (svg/pptx/unknown),
// shows a download card WITHOUT mounting any renderer. The header carries the filename and
// a download anchor resolved through useAssetSource() (the 37A auth route by default; a
// token-scoped public route once a provider is mounted — R-05); Radix owns focus-trap, Esc,
// backdrop, and the aria wiring (DialogTitle + DialogDescription).

// Each renderer is its own lazy boundary (D-08): the dynamic import() keeps its bytes — and,
// for docx/xlsx, the heavy parser deps — out of the main + modal chunks until first preview.
const ImagePreview = lazy(() => import('./renderers/ImagePreview'));
const PdfPreview = lazy(() => import('./renderers/PdfPreview'));
const TextPreview = lazy(() => import('./renderers/TextPreview'));
const HtmlPreview = lazy(() => import('./renderers/HtmlPreview'));
const DocxPreview = lazy(() => import('./renderers/DocxPreview'));
const XlsxPreview = lazy(() => import('./renderers/XlsxPreview'));

export interface PreviewModalProps {
  /** The asset to preview, or undefined when the modal is closed. */
  readonly active: Asset | undefined;
  /** Called when the user dismisses the modal (Esc, backdrop, or the close ✕). */
  readonly onClose: () => void;
}

function renderKind(active: Asset): React.ReactNode {
  const props: RendererProps = {
    assetId: active.id,
    mimeType: active.mime_type,
    fileName: active.file_name,
  };
  switch (previewKind(active.mime_type, active.file_name)) {
    case 'image':
      return <ImagePreview {...props} />;
    case 'pdf':
      return <PdfPreview {...props} />;
    case 'text':
      return <TextPreview {...props} />;
    case 'html':
      return <HtmlPreview {...props} />;
    case 'docx':
      return <DocxPreview {...props} />;
    case 'xlsx':
      return <XlsxPreview {...props} />;
    case 'download':
      return <DownloadCard active={active} />;
  }
}

/** The affordance for the download-only kinds (svg/pptx/unknown, T-37B-05): no renderer is
 *  mounted — script-bearing or unpreviewable bytes never reach an executing context. */
function DownloadCard({ active }: { active: Asset }) {
  const { t } = useTranslation();
  const { assetUrl } = useAssetSource();
  return (
    <div className="flex h-full flex-col items-center justify-center gap-5 p-10 text-center">
      <span className="grid size-20 place-items-center rounded-2xl bg-surface-2 text-accent-text ring-1 ring-border">
        <FileDown className="size-9" strokeWidth={1.5} />
      </span>
      <div className="flex flex-col gap-1">
        <p className="max-w-[36ch] truncate font-mono text-sm text-text" title={active.file_name}>
          {active.file_name}
        </p>
        <p className="text-[13px] text-text-muted">{t('artifacts.preview.unsupported')}</p>
      </div>
      <a
        href={assetUrl(active.id)}
        download={active.file_name}
        className="inline-flex items-center gap-2 rounded-full border border-accent/40 bg-surface px-4 py-2 text-sm font-medium text-accent-text transition-colors hover:border-accent hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
      >
        <Download className="size-4" />
        {t('artifacts.preview.downloadFallback')}
      </a>
    </div>
  );
}

export function PreviewModal({ active, onClose }: PreviewModalProps) {
  const { t } = useTranslation();
  const { assetUrl } = useAssetSource();
  return (
    <Dialog
      open={active !== undefined}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="flex h-[90vh] w-[90vw] max-w-[90vw] flex-col gap-0 overflow-hidden p-0">
        {active !== undefined ? (
          <>
            <div className="flex shrink-0 items-center gap-3 border-b border-border bg-gradient-to-r from-surface-2/80 to-surface px-5 py-3 pr-14">
              <DialogTitle className="min-w-0 flex-1 truncate font-mono text-[15px] font-medium">
                {active.file_name}
              </DialogTitle>
              <a
                href={assetUrl(active.id)}
                download={active.file_name}
                aria-label={t('artifacts.preview.download', { name: active.file_name })}
                className="inline-flex h-9 items-center gap-2 rounded-full border border-border bg-surface px-3.5 text-[13px] font-medium text-accent-text transition-colors hover:border-accent hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              >
                <Download className="size-4" />
                {t('artifacts.preview.downloadFallback')}
              </a>
            </div>
            <DialogDescription className="sr-only">
              {t('artifacts.preview.description', { name: active.file_name })}
            </DialogDescription>
            <div className="min-h-0 flex-1 overflow-auto bg-surface">
              <Suspense fallback={<PreviewLoading />}>{renderKind(active)}</Suspense>
            </div>
          </>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
