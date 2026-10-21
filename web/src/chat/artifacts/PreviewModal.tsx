import { Suspense, useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { FileDown } from 'lucide-react';
import type { Asset } from '../attachments/types';
import { previewKind } from './artifactMeta';
import { SaveFileLink } from './SaveFileLink';
import { PreviewLoading, type RendererProps } from './renderers/PreviewStatus';
import { PreviewByKind } from './renderers/previewDispatch';
import { useAssetSource } from './renderers/assetSourceContext';
import { PreviewActionsContext, previewActionClass } from './renderers/previewActionsContext';
import { PreviewCloseGuardContext } from './renderers/previewCloseGuardContext';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';

// PreviewModal (WEBART-05 / D-09): a ~90vw×90vh Radix Dialog that previews an agent-
// delivered asset in-cockpit. It dispatches by previewKind (plan 03, the pure MIME gate)
// to SUSPENSE-wrapped shared renderers, keeping native apps out of the modal's static import
// graph — or, for the download-only kinds (svg/legacy-ppt/unknown),
// shows a download card WITHOUT mounting any renderer. The header carries the filename and
// a download anchor resolved through useAssetSource() (the 37A auth route by default; a
// token-scoped public route once a provider is mounted — R-05); Radix owns focus-trap, Esc,
// backdrop, and the aria wiring (DialogTitle + DialogDescription). A preview holding an edit it
// has not saved (a Craft app's) holds the first close, and the next one drops the edit.

/** What a preview needs of an asset: the chat's artifact card has these, not a whole Asset. */
export type PreviewTarget = Pick<Asset, 'id' | 'file_name' | 'mime_type'>;

export interface PreviewModalProps {
  /** The asset to preview, or undefined when the modal is closed. */
  readonly active: PreviewTarget | undefined;
  /** Called when the user dismisses the modal (Esc, backdrop, or the close ✕). */
  readonly onClose: () => void;
}

function renderKind(active: PreviewTarget): React.ReactNode {
  const props: RendererProps = {
    assetId: active.id,
    mimeType: active.mime_type,
    fileName: active.file_name,
  };
  return (
    <PreviewByKind
      kind={previewKind(active.mime_type, active.file_name)}
      asset={props}
      downloadFallback={<DownloadCard active={active} />}
    />
  );
}

/** The affordance for the download-only kinds (svg/pptx/unknown, T-37B-05): no renderer is
 *  mounted — script-bearing or unpreviewable bytes never reach an executing context. */
function DownloadCard({ active }: { active: PreviewTarget }) {
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
      <SaveFileLink
        href={assetUrl(active.id)}
        fileName={active.file_name}
        mimeType={active.mime_type}
        label={t('artifacts.preview.downloadFallback')}
        className="inline-flex items-center gap-2 rounded-full border border-accent/40 bg-surface px-4 py-2 text-sm font-medium text-accent-text transition-colors hover:border-accent hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
      />
    </div>
  );
}

export function PreviewModal({ active, onClose }: PreviewModalProps) {
  const { t } = useTranslation();
  const { assetUrl } = useAssetSource();
  const [actions, setActions] = useState<HTMLElement | null>(null);
  const [unsaved, setUnsavedFlag] = useState(false);
  const [held, setHeld] = useState(false);
  const setUnsaved = useCallback((value: boolean) => {
    setUnsavedFlag(value);
    if (!value) setHeld(false);
  }, []);
  const guard = useMemo(() => ({ setUnsaved, held }), [setUnsaved, held]);
  return (
    <Dialog
      open={active !== undefined}
      onOpenChange={(open) => {
        if (open) return;
        if (unsaved && !held) {
          setHeld(true);
          return;
        }
        setHeld(false);
        onClose();
      }}
    >
      <DialogContent className="flex h-[90vh] w-[90vw] max-w-[90vw] flex-col gap-0 overflow-hidden p-0">
        {active !== undefined ? (
          <>
            <div
              data-slot="preview-header"
              className="flex shrink-0 items-center gap-3 border-b border-border bg-gradient-to-r from-surface-2/80 to-surface px-5 py-3 pr-14"
            >
              <DialogTitle className="min-w-0 flex-1 truncate font-mono text-[15px] font-medium">
                {active.file_name}
              </DialogTitle>
              <span ref={setActions} className="flex items-center gap-3" />
              <SaveFileLink
                href={assetUrl(active.id)}
                fileName={active.file_name}
                mimeType={active.mime_type}
                label={t('artifacts.preview.downloadFallback')}
                aria-label={t('artifacts.preview.download', { name: active.file_name })}
                className={previewActionClass}
              />
            </div>
            <DialogDescription className="sr-only">
              {t('artifacts.preview.description', { name: active.file_name })}
            </DialogDescription>
            <div className="min-h-0 flex-1 overflow-auto bg-surface">
              <PreviewActionsContext.Provider value={actions}>
                <PreviewCloseGuardContext.Provider value={guard}>
                  <Suspense fallback={<PreviewLoading />}>{renderKind(active)}</Suspense>
                </PreviewCloseGuardContext.Provider>
              </PreviewActionsContext.Provider>
            </div>
          </>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
