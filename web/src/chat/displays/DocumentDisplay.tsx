import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import ReactMarkdown, { type Components } from 'react-markdown';
import {
  baseMarkdownComponents,
  buildRehypePlugins,
  remarkPlugins,
} from '../markdownConfig';
import type { DisplayDocument, DisplaySource } from './types';
import { DisplayCardShell } from './DisplayCardShell';
import { CitationBubble } from './CitationBubble';
import { useCopyAction } from './useCopyAction';
import {
  CITATION_NUMBER_ATTR,
  CITATION_REF_ATTR,
  indexSources,
  rehypeCitations,
} from './rehypeCitations';

// DocumentDisplay (D-10 / D-04): renders a web_fetch document's sanitized markdown
// through the SAME shared markdown pipeline as the chat lane (markdownConfig — the
// HARDEN-08 rehype-sanitize chokepoint), with two additions: rehypeCitations splices
// inline `[n]` chips at the claim position, and images render (elysia's prose-img:hidden
// is dropped). A Copy-text control copies the raw markdown. Citations resolve against
// the code-owned source registry (payload.sources); an unknown `[n]` stays literal text.

export interface DocumentDisplayProps {
  readonly payload: {
    readonly document?: DisplayDocument;
    readonly title?: string;
    readonly sources?: readonly DisplaySource[];
  };
  /** Forwarded to each citation chip — the host opens the Source Explorer. */
  readonly onOpenSource?: (refId: string) => void;
}

export function DocumentDisplay({ payload, onOpenSource }: DocumentDisplayProps) {
  const { t } = useTranslation();
  const { copied, copy } = useCopyAction();
  const doc = payload.document;
  const label = t('display.type.document');

  const byIndex = useMemo(() => indexSources(payload.sources), [payload.sources]);
  const byRefId = useMemo(() => {
    const map = new Map<string, DisplaySource>();
    for (const source of payload.sources ?? []) map.set(source.ref_id, source);
    return map;
  }, [payload.sources]);

  // The citation `span` renderer reads the data-* attrs the plugin spliced and
  // renders the accessible CitationBubble; a non-citation span passes through.
  const components = useMemo<Components>(
    () => ({
      ...baseMarkdownComponents,
      span: ({ node, children, ...spanProps }) => {
        const props = node?.properties ?? {};
        const refId = props[CITATION_REF_ATTR];
        const numberRaw = props[CITATION_NUMBER_ATTR];
        if (typeof refId === 'string') {
          const source = byRefId.get(refId);
          const number = Number(numberRaw);
          if (source !== undefined && Number.isFinite(number)) {
            return <CitationBubble number={number} source={source} {...(onOpenSource ? { onOpenSource } : {})} />;
          }
        }
        return <span {...spanProps}>{children}</span>;
      },
    }),
    [byRefId, onOpenSource],
  );

  const rehypePlugins = useMemo(
    () => buildRehypePlugins([[rehypeCitations, { byIndex }]]),
    [byIndex],
  );

  const content = doc?.content_md ?? '';
  const actions =
    content.length > 0 ? (
      <button
        type="button"
        onClick={() => {
          copy(content);
        }}
        aria-label={t('display.document.copyAria')}
        className="flex min-h-11 items-center rounded-[var(--radius-md)] border border-border px-2 text-[0.75rem] font-medium text-text-muted hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
      >
        {copied ? t('display.document.copied') : t('display.document.copy')}
      </button>
    ) : undefined;

  if (content.length === 0) {
    return (
      <DisplayCardShell label={label}>
        <div className="flex flex-col items-center gap-1 py-8 text-center">
          <p className="text-sm font-medium text-text">{t('display.document.emptyHeading')}</p>
          <p className="text-[0.75rem] text-text-faint">{t('display.document.emptyBody')}</p>
        </div>
      </DisplayCardShell>
    );
  }

  const title = doc?.title ?? payload.title;

  return (
    <DisplayCardShell label={label} {...(actions !== undefined ? { actions } : {})}>
      {title !== undefined && title.length > 0 ? (
        <p className="mb-2 font-display text-lg leading-tight text-text">{title}</p>
      ) : null}
      <div className="prose-sm max-w-none text-sm leading-relaxed text-text-muted [&_h1]:mb-2 [&_h1]:mt-3 [&_h1]:text-base [&_h1]:font-medium [&_h1]:text-text [&_h2]:mb-2 [&_h2]:mt-3 [&_h2]:text-sm [&_h2]:font-medium [&_h2]:text-text [&_li]:my-1 [&_ol]:my-2 [&_ol]:list-decimal [&_ol]:ps-5 [&_p]:my-2 [&_ul]:my-2 [&_ul]:list-disc [&_ul]:ps-5">
        <ReactMarkdown
          remarkPlugins={remarkPlugins}
          rehypePlugins={rehypePlugins}
          components={components}
        >
          {content}
        </ReactMarkdown>
      </div>
    </DisplayCardShell>
  );
}
