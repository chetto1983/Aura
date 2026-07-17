import { Suspense, lazy, useEffect, useMemo, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { useParams } from 'react-router-dom';
import { Download, Share2, type LucideIcon } from 'lucide-react';
import {
  categoryIcon,
  categoryLabel,
  formatSize,
  previewKind,
  type PreviewKind,
} from '../chat/artifacts/artifactMeta';
import {
  AssetSourceContext,
  useAssetSource,
  type AssetSource,
} from '../chat/artifacts/renderers/assetSourceContext';
import { PreviewLoading, type RendererProps } from '../chat/artifacts/renderers/PreviewStatus';
import type { Snapshot, SnapshotArtifact, SnapshotTurn } from '../chat/share/shareTypes';

// SharePage (WEBSHARE-02/03, ADR 0039): the read-only snapshot renderer for BOTH
// share tiers at once — ONE component, TWO routes, never a forked pair. `tier` is
// a prop set by the two <Route> elements in main.tsx (the caller already knows
// which route matched; this file never re-derives it from the URL shape). Every
// renderer, the not-found body, and the no-identity posture below are IDENTICAL
// for both tiers — only the fetch URL and the AssetSourceContext value differ
// (dataRequestFor / assetSourceFor below, read as one inverse pair). If this file
// ever grows a second exported component, that is the D-07 one-canonical-snapshot
// rule breaking: a forked page forks the redaction and the escaping right along
// with it. Every 37B renderer (HtmlPreview included) is reused completely
// unedited through the asset-source seam plan 37F-05 built for exactly this.
const ImagePreview = lazy(() => import('../chat/artifacts/renderers/ImagePreview'));
const PdfPreview = lazy(() => import('../chat/artifacts/renderers/PdfPreview'));
const TextPreview = lazy(() => import('../chat/artifacts/renderers/TextPreview'));
const HtmlPreview = lazy(() => import('../chat/artifacts/renderers/HtmlPreview'));
const DocxPreview = lazy(() => import('../chat/artifacts/renderers/DocxPreview'));
const XlsxPreview = lazy(() => import('../chat/artifacts/renderers/XlsxPreview'));

export type ShareTier = 'public' | 'internal';

export interface SharePageProps {
  readonly tier: ShareTier;
}

type PageState =
  | { readonly kind: 'loading' }
  | { readonly kind: 'error' }
  | { readonly kind: 'notFound' }
  | { readonly kind: 'ready'; readonly snapshot: Snapshot };

interface DataRequest {
  readonly url: string;
  readonly credentials: RequestCredentials;
}

/** The D-10 inverse fetch rule — read the two branches TOGETHER, never in isolation:
 *  - public   (`/s/{token}`):   `credentials: 'omit'`        — no session exists to send.
 *  - internal (`/shared/{id}`): `credentials: 'same-origin'` — the RequireAuth gate needs
 *    the cookie to resolve at all.
 *  A future "simplification" onto one shared credentials value silently breaks whichever
 *  tier loses — that is precisely why the two live here, side by side, in one function. */
function dataRequestFor(tier: ShareTier, key: string): DataRequest {
  if (tier === 'public') {
    return { url: `/s/${encodeURIComponent(key)}/data`, credentials: 'omit' };
  }
  return { url: `/api/shares/${encodeURIComponent(key)}/data`, credentials: 'same-origin' };
}

/** The tier-scoped asset provider (the R-05 seam, plan 37F-05): every 37B renderer —
 *  HtmlPreview included — resolves bytes through this value with ZERO edits to any of
 *  them. Exact inverse pairing with dataRequestFor above; the id is percent-encoded,
 *  never interpolated raw into a path segment. */
function assetSourceFor(tier: ShareTier, key: string): AssetSource {
  if (tier === 'public') {
    return {
      assetUrl: (assetId) => `/s/${encodeURIComponent(key)}/asset/${encodeURIComponent(assetId)}`,
      credentials: 'omit',
    };
  }
  return {
    assetUrl: (assetId) =>
      `/api/shares/${encodeURIComponent(key)}/asset/${encodeURIComponent(assetId)}`,
    credentials: 'same-origin',
  };
}

function formatSnapshotDate(iso: string): string {
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? iso : date.toLocaleString();
}

export function SharePage({ tier }: SharePageProps) {
  const params = useParams<{ token?: string; id?: string }>();
  const routeKey = (tier === 'public' ? params.token : params.id) ?? '';
  const { t } = useTranslation();
  const [state, setState] = useState<PageState>({ kind: 'loading' });

  useEffect(() => {
    // An empty routeKey has nothing to fetch; the render below handles it directly
    // (a derived render-time branch, not a state transition) rather than dispatching
    // a synchronous setState from inside this effect.
    if (routeKey === '') return;
    const { url, credentials } = dataRequestFor(tier, routeKey);
    const controller = new AbortController();
    void (async () => {
      // Reset to 'loading' from inside the async callback (not synchronously at the
      // top of the effect body): a token/id change re-triggers this effect and this
      // is the re-fetch's own first move, not a side effect of the effect running.
      setState({ kind: 'loading' });
      let res: Response;
      try {
        res = await fetch(url, { credentials, signal: controller.signal });
      } catch {
        if (!controller.signal.aborted) setState({ kind: 'error' });
        return;
      }
      // Unknown / expired / revoked / a foreign-tier id / an anonymous 401 on the
      // internal route ALL collapse into this ONE state (D-13, T-37F-50): a
      // distinguishable body here would be exactly the oracle the server already
      // refuses to be. Never branch this into a second, more specific state, and
      // never read the status code beyond ok/not-ok.
      if (!res.ok) {
        setState({ kind: 'notFound' });
        return;
      }
      try {
        const snapshot = (await res.json()) as Snapshot;
        setState({ kind: 'ready', snapshot });
      } catch {
        setState({ kind: 'error' });
      }
    })();
    return () => {
      controller.abort();
    };
  }, [tier, routeKey]);

  const assetSource = useMemo(() => assetSourceFor(tier, routeKey), [tier, routeKey]);

  if (routeKey === '') {
    return <StatusPage message={t('share.public.notFound')} />;
  }
  if (state.kind === 'loading') {
    return <StatusPage role="status" message={t('share.public.loading')} />;
  }
  if (state.kind === 'error') {
    return <StatusPage role="alert" message={t('share.public.error')} />;
  }
  if (state.kind === 'notFound') {
    return <StatusPage message={t('share.public.notFound')} />;
  }

  return (
    <AssetSourceContext.Provider value={assetSource}>
      <SnapshotView snapshot={state.snapshot} />
    </AssetSourceContext.Provider>
  );
}

/** The loading / network-error / not-found shell: no identity, no navigation, no
 *  affordance beyond a single centred message — the same shell for all three states,
 *  distinguished only by the ARIA role a live-region announcer needs. */
function StatusPage({
  role,
  message,
}: {
  readonly role?: 'status' | 'alert';
  readonly message: string;
}) {
  return (
    <main className="grid h-dvh place-items-center bg-bg px-6 text-center text-text">
      <div className="flex max-w-sm flex-col items-center gap-3">
        <span className="grid size-12 place-items-center rounded-2xl bg-surface-2/70 text-accent-text ring-1 ring-border">
          <Share2 aria-hidden="true" className="size-5" strokeWidth={1.75} />
        </span>
        <p role={role} className="text-sm text-text-muted">
          {message}
        </p>
      </div>
    </main>
  );
}

function SnapshotView({ snapshot }: { readonly snapshot: Snapshot }) {
  const { t } = useTranslation();
  return (
    <main className="relative min-h-dvh overflow-hidden bg-bg px-4 py-10 text-text sm:px-8">
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-x-0 top-0 -z-10 h-72 bg-gradient-to-b from-accent/10 via-transparent to-transparent"
      />
      <div className="mx-auto flex max-w-3xl flex-col gap-6">
        <header className="animate-in fade-in-0 slide-in-from-bottom-1 fill-mode-backwards flex flex-col gap-1.5 border-b border-border pb-6">
          <h1 className="font-display text-3xl font-medium text-text">{snapshot.title}</h1>
          <p className="text-sm text-text-muted">
            {t('share.public.snapshotDate', { date: formatSnapshotDate(snapshot.snapshot_at) })}
          </p>
        </header>

        <section className="flex flex-col gap-4">
          {snapshot.turns.map((turn, index) => (
            <TurnCard key={turn.seq} turn={turn} index={index} />
          ))}
        </section>

        {snapshot.artifacts.length > 0 ? (
          <section className="flex flex-col gap-3 border-t border-border pt-6">
            <h2 className="text-[13px] font-semibold uppercase tracking-[0.14em] text-text-faint">
              {t('share.public.artifacts.heading')}
            </h2>
            <div className="flex flex-col gap-4">
              {snapshot.artifacts.map((artifact) => (
                <ArtifactBlock key={artifact.asset_id} artifact={artifact} />
              ))}
            </div>
          </section>
        ) : null}

        <footer className="flex items-center justify-center gap-1.5 border-t border-border pt-6 text-[11px] font-medium uppercase tracking-[0.16em] text-text-faint">
          <Share2 aria-hidden="true" className="size-3" />
          {t('share.public.mark')}
        </footer>
      </div>
    </main>
  );
}

function TurnCard({ turn, index }: { readonly turn: SnapshotTurn; readonly index: number }) {
  const { t } = useTranslation();
  const isUser = turn.role === 'user';
  return (
    <article
      data-role={turn.role}
      className="animate-in fade-in-0 slide-in-from-bottom-1 fill-mode-backwards flex flex-col gap-1.5 rounded-lg border border-border bg-surface-2/40 p-4"
      style={{ animationDelay: `${String(Math.min(index, 8) * 40)}ms` }}
    >
      <span className="text-[11px] font-semibold uppercase tracking-[0.14em] text-accent-text">
        {isUser ? t('share.public.turn.user') : t('share.public.turn.assistant')}
      </span>
      <p className="whitespace-pre-wrap break-words text-sm text-text">{turn.text}</p>
      {turn.tool_names !== undefined && turn.tool_names.length > 0 ? (
        <ul aria-label={t('share.public.turn.toolsUsed')} className="flex flex-wrap gap-1.5 pt-1">
          {turn.tool_names.map((name, toolIndex) => (
            <li
              key={`${name}-${String(toolIndex)}`}
              className="rounded-md border border-border bg-surface px-2 py-0.5 font-mono text-[11px] text-text-faint"
            >
              {name}
            </li>
          ))}
        </ul>
      ) : null}
    </article>
  );
}

function ArtifactBlock({ artifact }: { readonly artifact: SnapshotArtifact }) {
  const { t } = useTranslation();
  const { assetUrl } = useAssetSource();
  const Icon = categoryIcon(artifact.mime_type, artifact.filename);
  const rendererProps: RendererProps = {
    assetId: artifact.asset_id,
    mimeType: artifact.mime_type,
    fileName: artifact.filename,
  };

  return (
    <div className="overflow-hidden rounded-lg border border-border bg-surface">
      <div className="flex items-center gap-2.5 border-b border-border bg-surface-2/50 px-3 py-2">
        <ArtifactGlyph Icon={Icon} />
        <span
          className="min-w-0 flex-1 truncate font-mono text-[13px] text-text"
          title={artifact.filename}
        >
          {artifact.filename}
        </span>
        <span className="shrink-0 text-[11px] uppercase tracking-wide text-text-faint">
          {categoryLabel(artifact.mime_type, artifact.filename, t)} ·{' '}
          {formatSize(artifact.size_bytes, t)}
        </span>
        <a
          href={assetUrl(artifact.asset_id)}
          download={artifact.filename}
          aria-label={t('share.public.artifacts.download', { name: artifact.filename })}
          className="grid size-7 shrink-0 place-items-center rounded-full text-text-faint transition-colors hover:bg-surface-3 hover:text-accent-text"
        >
          <Download aria-hidden="true" className="size-3.5" />
        </a>
      </div>
      <div className="min-h-32 max-h-[70vh]" data-artifact-preview="">
        <Suspense fallback={<PreviewLoading />}>
          {renderArtifactKind(previewKind(artifact.mime_type, artifact.filename), rendererProps)}
        </Suspense>
      </div>
    </div>
  );
}

/** The category glyph, split into its own component exactly as ArtifactRow.tsx's
 *  IconTile does: the PARENT resolves `categoryIcon` (a stable, already-declared
 *  Lucide export, never a fresh component) and hands the result down as a plain
 *  prop; this component only ever RECEIVES it and renders it as the JSX tag. It
 *  never calls `categoryIcon` itself — that split is what keeps
 *  react-hooks/static-components from reading the icon selection as "a component
 *  created during render". */
function ArtifactGlyph({ Icon }: { readonly Icon: LucideIcon }) {
  return (
    <Icon aria-hidden="true" className="size-4 shrink-0 text-accent-text" strokeWidth={1.75} />
  );
}

function renderArtifactKind(kind: PreviewKind, props: RendererProps): ReactNode {
  switch (kind) {
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
      return <DownloadOnly />;
  }
}

/** The affordance for the download-only kinds (SVG chief among them, T-37B-05): no
 *  renderer is mounted — the header's download link above is the only way out. */
function DownloadOnly() {
  const { t } = useTranslation();
  return (
    <div className="grid h-32 place-items-center px-4 text-center text-[12px] text-text-faint">
      {t('share.public.artifacts.previewUnavailable')}
    </div>
  );
}
