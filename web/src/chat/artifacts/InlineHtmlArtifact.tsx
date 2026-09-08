import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Download, FileCode2 } from 'lucide-react';
import { useArtifactWorkspace } from './artifactWorkspaceContext';
import { useAssetSource } from './renderers/assetSourceContext';
import type { RendererProps } from './renderers/PreviewStatus';
import HtmlArtifactViewer from './HtmlArtifactViewer';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';

export default function InlineHtmlArtifact(props: RendererProps) {
  const { t } = useTranslation();
  const openWorkspace = useArtifactWorkspace();
  const { assetUrl } = useAssetSource();
  const [expanded, setExpanded] = useState(false);
  function open() {
    if (openWorkspace) openWorkspace(props);
    else setExpanded(true);
  }
  return (
    <div className="my-3 flex w-full min-w-0 max-w-[768px] flex-col gap-4">
      <HtmlArtifactViewer {...props} onExpand={open} />
      <div className="flex w-full max-w-[480px] items-center rounded-2xl border border-border bg-surface px-3">
        <button
          type="button"
          onClick={open}
          className="flex min-h-[60px] min-w-0 flex-1 items-center gap-3 rounded-xl text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          <FileCode2 aria-hidden="true" className="size-5 shrink-0 text-text-muted" />
          <span className="flex min-w-0 flex-col gap-0.5">
            <span className="truncate text-[13px] font-medium">{props.fileName}</span>
            <span className="text-xs text-text-muted">HTML</span>
          </span>
        </button>
        <a
          href={assetUrl(props.assetId)}
          download={props.fileName}
          aria-label={t('artifacts.preview.download', { name: props.fileName })}
          className="grid size-11 shrink-0 place-items-center rounded-full text-text-muted hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          <Download className="size-4" />
        </a>
      </div>
      {expanded && (
        <Dialog open onOpenChange={setExpanded}>
          <DialogContent className="h-[96dvh] w-[96vw] max-w-[96vw] overflow-hidden p-0 [&>button]:hidden">
            <DialogTitle className="sr-only">{props.fileName}</DialogTitle>
            <DialogDescription className="sr-only">
              {t('artifacts.preview.description', { name: props.fileName })}
            </DialogDescription>
            <HtmlArtifactViewer
              {...props}
              expanded
              onClose={() => {
                setExpanded(false);
              }}
            />
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}
