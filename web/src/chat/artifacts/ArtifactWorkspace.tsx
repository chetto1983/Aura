import { lazy, Suspense, useEffect, useRef, useState, type ReactNode } from 'react';
import { ArtifactWorkspaceContext } from './artifactWorkspaceContext';
import { PreviewLoading, type RendererProps } from './renderers/PreviewStatus';

const HtmlArtifactViewer = lazy(() => import('./HtmlArtifactViewer'));

export function ArtifactWorkspace({
  children,
  onExpand,
  scopeKey = '',
}: {
  readonly children: ReactNode;
  readonly onExpand: () => void;
  readonly scopeKey?: string;
}) {
  const [active, setActive] = useState<RendererProps | null>(null);
  const [scope, setScope] = useState(scopeKey);
  const trigger = useRef<HTMLElement | null>(null);
  const expanded = useRef<HTMLDivElement>(null);

  if (scope !== scopeKey) {
    setScope(scopeKey);
    setActive(null);
  }

  function close() {
    setActive(null);
    requestAnimationFrame(() => trigger.current?.focus());
  }

  useEffect(() => {
    if (!active) return;
    expanded.current?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.stopPropagation();
        setActive(null);
        requestAnimationFrame(() => trigger.current?.focus());
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [active]);

  return (
    <ArtifactWorkspaceContext.Provider
      value={(artifact) => {
        trigger.current =
          document.activeElement instanceof HTMLElement ? document.activeElement : null;
        onExpand();
        setActive(artifact);
      }}
    >
      <div className="relative h-full min-h-0 min-w-0 overflow-hidden">
        <div className="h-full min-h-0 overflow-hidden" hidden={active !== null}>
          {children}
        </div>
        {active && (
          <div
            ref={expanded}
            role="region"
            aria-label={active.fileName}
            tabIndex={-1}
            className="absolute inset-0 min-h-0 bg-bg outline-none"
          >
            <Suspense fallback={<PreviewLoading />}>
              <HtmlArtifactViewer key={active.assetId} {...active} expanded onClose={close} />
            </Suspense>
          </div>
        )}
      </div>
    </ArtifactWorkspaceContext.Provider>
  );
}
