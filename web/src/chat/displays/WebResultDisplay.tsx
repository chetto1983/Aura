import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { DisplaySource, WebItem } from './types';
import { DisplayCardShell } from './DisplayCardShell';
import { DisplayPagination } from './DisplayPagination';
import { CitationBubble } from './CitationBubble';

// WebResultDisplay (D-09 / DISP-03): the rich web_result card. Per WebItem it shows
// a domain chip + snippet + a relevance meter (INFO color, NOT accent — accent stays
// for citations) + published_at + a citation bubble, plus a thumbnail/favicon. The
// result groups paginate in-card (default 3/page).
//
// SECURITY — T-26-15 (client-side SSRF / tracking): every external image is fetched
// through the backend image-proxy (`/api/image-proxy?url=…`, SSRF-safe, 26-03), never
// a raw client `<img src="http…">` to an arbitrary host. The proxied <img> carries
// referrerpolicy="no-referrer" + loading="lazy"; a broken image degrades to alt +
// the domain chip (no toast).

const IMAGE_PROXY = '/api/image-proxy';

/** Build the proxied image src — the ONLY way an external image enters the DOM. */
function proxiedSrc(url: string): string {
  return `${IMAGE_PROXY}?url=${encodeURIComponent(url)}`;
}

function Thumbnail({ url, alt }: { readonly url: string; readonly alt: string }) {
  const [broken, setBroken] = useState(false);
  if (broken) return null; // degrade silently to the alt text + domain chip
  return (
    <img
      src={proxiedSrc(url)}
      alt={alt}
      loading="lazy"
      referrerPolicy="no-referrer"
      onError={() => {
        setBroken(true);
      }}
      className="h-16 w-16 shrink-0 rounded-[var(--radius-md)] border border-border object-cover"
    />
  );
}

/** A quiet INFO-tinted relevance meter (0..1 score → 0..100% width). Never accent. */
function RelevanceMeter({ score }: { readonly score: number }) {
  const pct = Math.max(0, Math.min(100, Math.round(score * 100)));
  return (
    <span aria-hidden="true" className="inline-flex h-1 w-12 overflow-hidden rounded-sm bg-surface-3">
      <span className="h-full bg-info" style={{ width: `${String(pct)}%` }} />
    </span>
  );
}

interface ResultRowProps {
  readonly item: WebItem;
  readonly source?: DisplaySource;
  readonly onOpenSource?: (refId: string) => void;
}

function ResultRow({ item, source, onOpenSource }: ResultRowProps) {
  const { t } = useTranslation();
  const domain = item.domain ?? (item.url.length > 0 ? safeHost(item.url) : '');

  return (
    <div className="flex gap-3 rounded-[var(--radius-md)] border border-border bg-surface p-3">
      {item.thumbnail !== undefined && item.thumbnail.length > 0 ? (
        <Thumbnail url={item.thumbnail} alt={item.title} />
      ) : null}
      <div className="flex min-w-0 flex-col gap-1">
        <div className="flex items-center gap-2">
          <a
            href={item.url}
            target="_blank"
            rel="noreferrer"
            aria-label={t('display.webResult.visitAria', { title: item.title })}
            className="truncate text-sm font-medium text-accent-text hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          >
            {item.title}
          </a>
          {source !== undefined && item.ref_id !== undefined ? (
            <CitationBubble
              number={source.index}
              source={source}
              {...(onOpenSource ? { onOpenSource } : {})}
            />
          ) : null}
        </div>
        {item.snippet !== undefined && item.snippet.length > 0 ? (
          <p className="line-clamp-2 text-sm leading-relaxed text-text-muted">{item.snippet}</p>
        ) : null}
        <div className="flex flex-wrap items-center gap-2 text-[0.75rem] text-text-faint">
          {domain.length > 0 ? (
            <span className="rounded-[var(--radius-pill)] bg-surface-2 px-1.5 py-0.5">{domain}</span>
          ) : null}
          {item.score !== undefined ? (
            <span className="inline-flex items-center gap-1">
              <RelevanceMeter score={item.score} />
              {t('display.webResult.relevance', { score: item.score.toFixed(2) })}
            </span>
          ) : null}
          {item.published_at !== undefined && item.published_at.length > 0 ? (
            <span>{t('display.webResult.published', { date: item.published_at })}</span>
          ) : null}
        </div>
      </div>
    </div>
  );
}

/** Best-effort hostname extraction; falls back to the raw string on a parse error. */
function safeHost(url: string): string {
  try {
    return new URL(url).hostname;
  } catch {
    return url;
  }
}

export interface WebResultDisplayProps {
  readonly payload: {
    readonly web_results?: readonly WebItem[];
    readonly sources?: readonly DisplaySource[];
  };
  /** Forwarded to each citation chip — the host opens the Source Explorer. */
  readonly onOpenSource?: (refId: string) => void;
}

export function WebResultDisplay({ payload, onOpenSource }: WebResultDisplayProps) {
  const { t } = useTranslation();
  const items = payload.web_results ?? [];
  const label = t('display.type.web_result');

  // Resolve each item's citation source by ref_id (the registry is the truth, D-05).
  const byRefId = useMemo(() => {
    const map = new Map<string, DisplaySource>();
    for (const source of payload.sources ?? []) map.set(source.ref_id, source);
    return map;
  }, [payload.sources]);

  if (items.length === 0) {
    return (
      <DisplayCardShell label={label}>
        <div className="flex flex-col items-center gap-1 py-8 text-center">
          <p className="text-sm font-medium text-text">{t('display.webResult.emptyHeading')}</p>
          <p className="text-[0.75rem] text-text-faint">{t('display.webResult.emptyBody')}</p>
        </div>
      </DisplayCardShell>
    );
  }

  return (
    <DisplayCardShell label={label} meta={t('display.table.rowCount', { count: items.length })}>
      <DisplayPagination>
        {items.map((item, i) => {
          const source = item.ref_id !== undefined ? byRefId.get(item.ref_id) : undefined;
          return (
            <ResultRow
              key={item.ref_id ?? `${item.url}-${String(i)}`}
              item={item}
              {...(source !== undefined ? { source } : {})}
              {...(onOpenSource ? { onOpenSource } : {})}
            />
          );
        })}
      </DisplayPagination>
    </DisplayCardShell>
  );
}
