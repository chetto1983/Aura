import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Code2, Eye } from 'lucide-react';
import { useAssetContent } from './useAssetContent';
import { useAssetSource } from './assetSourceContext';
import { PreviewError, PreviewLoading, type RendererProps } from './PreviewStatus';

// text/html (D-07 / WEBART-07 / T-37B-08): an agent-written page is untrusted markup, so it
// renders in a NULL-ORIGIN iframe. sandbox="allow-scripts" WITHOUT the same-origin token is
// what makes the origin null — scripts run, but document.cookie is empty, window.parent access
// throws (cross-origin), and fetch('/api/…') carries no ambient session, so the content can
// reach neither our cookies nor our DOM. Granting the same-origin token here is forbidden: it
// would let the sandboxed script drop its own sandbox.
//
// The frame is loaded through src= (the sealed /api/assets/{id}/render document), NOT srcdoc.
// The reason is a property of srcdoc rather than a preference: a srcdoc document INHERITS the
// embedder's Content-Security-Policy and has no base URL of its own, so the artifact could
// never carry a policy and the cockpit could never tighten its own without blanking every
// artifact. A document fetched from a URL carries its own response headers — which is where
// internal/agui/assets_render_api.go puts the sealed policy, the no-script exfiltration scrub
// (base/meta-refresh/link-preload) and the opaque-origin storage+clipboard shims.
//
// Note the interaction with the anchors the server retargets to target="_blank": this sandbox
// grants no allow-popups, so such a click is inert. That is the intended end state — the point
// of retargeting is to stop a remote page REPLACING the artifact inside the frame, not to open
// it somewhere else.
//
// The two-tab shape (Source / Preview) is the artifact-viewer convention: the rendered page is
// the answer, the markup behind it is one click away, and neither is the raw payload dumped
// into the conversation.

/** The sealed document lane: the server renders, we only frame it. */
function RenderedFrame({ src, title }: { readonly src: string; readonly title: string }) {
  return (
    <iframe
      src={src}
      sandbox="allow-scripts"
      title={title}
      className="h-full w-full border-0 bg-white"
    />
  );
}

/** The fallback lane for a tier with no render route (the share pages): the bytes are fetched
 *  and inlined via srcdoc. Same null-origin sandbox, but the document inherits the embedder's
 *  CSP and gets no scrub — the pre-existing behavior, kept rather than removed so those pages
 *  do not regress while they wait for a sealed route of their own. */
function SrcdocFrame({ assetId, title }: { readonly assetId: string; readonly title: string }) {
  const { data, error } = useAssetContent(assetId, 'text');
  if (error !== undefined) return <PreviewError detail={error} />;
  if (data === undefined) return <PreviewLoading />;
  return (
    <iframe
      srcDoc={data}
      sandbox="allow-scripts"
      title={title}
      className="h-full w-full border-0 bg-white"
    />
  );
}

/** The markup behind the page, React-escaped in a <pre> exactly like TextPreview — the bytes
 *  are shown, never parsed. Mounted ONLY while its tab is active, so opening an artifact does
 *  not fetch a large body a second time just to have it available. */
function ArtifactSource({ assetId }: { readonly assetId: string }) {
  const { data, error } = useAssetContent(assetId, 'text');
  if (error !== undefined) return <PreviewError detail={error} />;
  if (data === undefined) return <PreviewLoading />;
  return (
    <pre className="h-full overflow-auto whitespace-pre-wrap break-words p-4 font-mono text-sm text-text">
      {data}
    </pre>
  );
}

type ArtifactTab = 'rendered' | 'source';

export default function HtmlPreview({ assetId, fileName }: RendererProps) {
  const { t } = useTranslation();
  const { renderUrl } = useAssetSource();
  // Rendered first: the operator opened an artifact to look at it. Source is the affordance
  // for "what is this actually doing", not the landing state.
  const [tab, setTab] = useState<ArtifactTab>('rendered');

  const tabs: readonly {
    readonly id: ArtifactTab;
    readonly label: string;
    readonly Icon: typeof Eye;
  }[] = [
    { id: 'rendered', label: t('artifacts.preview.tabRendered'), Icon: Eye },
    { id: 'source', label: t('artifacts.preview.tabSource'), Icon: Code2 },
  ];

  return (
    <div className="flex h-full flex-col">
      <div role="tablist" aria-label={fileName} className="flex shrink-0 border-b border-border">
        {tabs.map(({ id, label, Icon }) => (
          <button
            key={id}
            type="button"
            role="tab"
            aria-selected={tab === id}
            data-active={tab === id}
            onClick={() => {
              setTab(id);
            }}
            className="inline-flex min-h-[44px] flex-1 items-center justify-center gap-2 px-4 text-sm font-medium text-text-muted transition-colors hover:text-text data-[active=true]:bg-surface-2 data-[active=true]:text-accent-text"
          >
            <Icon aria-hidden="true" className="size-4" />
            {label}
          </button>
        ))}
      </div>
      <div className="min-h-0 flex-1">
        {tab === 'source' ? (
          <ArtifactSource assetId={assetId} />
        ) : renderUrl === undefined ? (
          <SrcdocFrame assetId={assetId} title={fileName} />
        ) : (
          <RenderedFrame src={renderUrl(assetId)} title={fileName} />
        )}
      </div>
    </div>
  );
}
