import { useState, useSyncExternalStore } from 'react';
import { useTranslation } from 'react-i18next';
import { Code2, Download, FileCode2, Maximize2, Play, X } from 'lucide-react';
import { ArtifactFrame, ArtifactSource } from './renderers/HtmlPreview';
import { useAssetSource } from './renderers/assetSourceContext';
import type { RendererProps } from './renderers/PreviewStatus';
import { ArtifactCopyButton } from './ArtifactCopyButton';
import { Button } from '@/components/ui/button';
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from '@/components/ui/resizable';

interface Props extends RendererProps {
  readonly expanded?: boolean;
  readonly onExpand?: () => void;
  readonly onClose?: () => void;
}

const narrowQuery = '(max-width: 639px)';
function subscribeNarrow(onChange: () => void) {
  const query = window.matchMedia(narrowQuery);
  query.addEventListener('change', onChange);
  return () => {
    query.removeEventListener('change', onChange);
  };
}
const isNarrow = () => window.matchMedia(narrowQuery).matches;
const serverNarrow = () => false;

export default function HtmlArtifactViewer({
  assetId,
  fileName,
  expanded = false,
  onExpand,
  onClose,
}: Props) {
  const { t } = useTranslation();
  const { assetUrl } = useAssetSource();
  const [showCode, setShowCode] = useState(false);
  const narrow = useSyncExternalStore(subscribeNarrow, isNarrow, serverNarrow);
  return (
    <section
      aria-label={t('artifacts.workspace.label', { name: fileName })}
      className={`flex min-h-0 min-w-0 flex-col overflow-hidden bg-surface ${expanded ? 'h-full' : 'h-[480px] max-h-[70dvh] rounded-[22px] border border-border'}`}
    >
      <header className="flex min-h-12 shrink-0 items-center gap-1 px-2 text-text">
        {expanded ? (
          <Button
            variant="ghost"
            size="icon"
            onClick={onClose}
            aria-label={t('artifacts.workspace.close')}
            title={t('artifacts.workspace.close')}
          >
            <X />
          </Button>
        ) : (
          <FileCode2 aria-hidden="true" className="mx-2 size-4 shrink-0" />
        )}
        {!expanded && (
          <span className="min-w-0 flex-1 truncate text-[13px] font-medium" title={fileName}>
            {fileName}
          </span>
        )}
        <Button
          variant="ghost"
          size={expanded ? 'sm' : 'icon'}
          onClick={() => {
            setShowCode(!showCode);
          }}
          aria-pressed={showCode}
          aria-label={t(showCode ? 'artifacts.workspace.hideCode' : 'artifacts.workspace.showCode')}
          title={t(showCode ? 'artifacts.workspace.hideCode' : 'artifacts.workspace.showCode')}
        >
          <Code2 />
          {expanded && (
            <span>
              {t(showCode ? 'artifacts.workspace.hideCode' : 'artifacts.workspace.showCode')}
            </span>
          )}
        </Button>
        {expanded ? (
          <>
            <span className="min-w-0 flex-1 truncate px-2 text-xs text-text-muted" title={fileName}>
              {fileName}
            </span>
            <ArtifactCopyButton assetId={assetId} />
            <Button asChild variant="ghost" size="icon">
              <a
                href={assetUrl(assetId)}
                download={fileName}
                aria-label={t('artifacts.preview.download', { name: fileName })}
              >
                <Download />
              </a>
            </Button>
          </>
        ) : (
          <>
            <Button
              variant="ghost"
              size="icon"
              onClick={() => {
                setShowCode(false);
              }}
              aria-pressed={!showCode}
              aria-label={t('artifacts.preview.tabRendered')}
              title={t('artifacts.preview.tabRendered')}
              className={!showCode ? 'rounded-full bg-surface-2' : ''}
            >
              <Play />
            </Button>
            {onExpand && (
              <Button
                variant="ghost"
                size="icon"
                onClick={onExpand}
                aria-label={t('artifacts.workspace.expand')}
                title={t('artifacts.workspace.expand')}
              >
                <Maximize2 />
              </Button>
            )}
          </>
        )}
      </header>
      {expanded ? (
        <ResizablePanelGroup
          orientation={narrow ? 'vertical' : 'horizontal'}
          className="min-h-0 flex-1"
          id={`artifact-${assetId}`}
        >
          {showCode && (
            <>
              <ResizablePanel id="artifact-source" defaultSize="48%" minSize="25%">
                <ArtifactSource assetId={assetId} />
              </ResizablePanel>
              <ResizableHandle aria-label={t('artifacts.workspace.resize')} />
            </>
          )}
          <ResizablePanel id="artifact-preview" minSize="25%">
            <div className="h-full overflow-hidden rounded-t-[22px] border border-border">
              <ArtifactFrame assetId={assetId} fileName={fileName} />
            </div>
          </ResizablePanel>
        </ResizablePanelGroup>
      ) : (
        <div className="relative min-h-0 flex-1 border-t border-border">
          {showCode && (
            <div className="absolute inset-0">
              <ArtifactSource assetId={assetId} />
            </div>
          )}
          <div className="h-full" hidden={showCode}>
            <ArtifactFrame assetId={assetId} fileName={fileName} />
          </div>
        </div>
      )}
    </section>
  );
}
