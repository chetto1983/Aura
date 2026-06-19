import type { Components } from 'react-markdown';
import type { PluggableList } from 'unified';
import rehypeSanitize, { type Options as SanitizeOptions } from 'rehype-sanitize';
import remarkGfm from 'remark-gfm';
import { markdownSanitizeSchema } from './markdownSanitize';
import { CITATION_NUMBER_ATTR, CITATION_REF_ATTR } from './displays/rehypeCitations';

// Shared markdown pipeline config (the HARDEN-08 chokepoint): the sanitize schema,
// the base component renderers, and the rehype-plugin assembly are defined ONCE
// here so the streaming chat host (MarkdownText) and the standalone-string document
// renderer (DocumentDisplay) render identically — same sanitize allow-list, same
// chrome, same citation injection. This is a SINGLE source of truth, not a forked
// markdown component (T-26-17): rehype-sanitize ALWAYS runs LAST in buildRehypePlugins,
// so an extra plugin can never bypass the chokepoint.

// The citation chip rides through sanitize as an empty <span> carrying two inert
// data-* attributes; allow exactly those. This WIDENS the committed schema only by
// those inert markers — it never weakens an existing allow.
export const sanitizeSchema: SanitizeOptions = {
  ...markdownSanitizeSchema,
  attributes: {
    ...markdownSanitizeSchema.attributes,
    span: [
      ...(markdownSanitizeSchema.attributes?.span ?? []),
      CITATION_REF_ATTR,
      CITATION_NUMBER_ATTR,
    ],
  },
};

export const remarkPlugins: PluggableList = [remarkGfm];

/** Assemble the rehype plugin list with sanitize ALWAYS last (chokepoint guard). */
export function buildRehypePlugins(extra?: PluggableList): PluggableList {
  return [...(extra ?? []), [rehypeSanitize, sanitizeSchema]];
}

export const baseMarkdownComponents = {
  a: ({ href, children, ...linkProps }) => (
    <a
      {...linkProps}
      href={href}
      rel="noreferrer"
      target="_blank"
      className="text-accent-text underline decoration-accent-muted underline-offset-2 hover:text-accent"
    >
      {children}
    </a>
  ),
  // Images render (DocumentDisplay drops elysia's prose-img:hidden, D-04 fix 2).
  // src is sanitize-restricted to http/https by the committed schema.
  img: ({ src, alt }) => (
    <img
      src={typeof src === 'string' ? src : undefined}
      alt={alt ?? ''}
      loading="lazy"
      referrerPolicy="no-referrer"
      className="my-3 max-w-full rounded-[var(--radius-md)] border border-border"
    />
  ),
  table: ({ children }) => (
    <div className="my-3 overflow-x-auto rounded-[var(--radius-md)] border border-border">
      <table className="min-w-full border-collapse text-left text-sm">{children}</table>
    </div>
  ),
  th: ({ children }) => (
    <th className="border-b border-border bg-surface-2 px-3 py-2 text-[0.75rem] font-medium uppercase text-text-faint">
      {children}
    </th>
  ),
  td: ({ children }) => (
    <td className="border-b border-border px-3 py-2 text-text-muted">{children}</td>
  ),
  pre: ({ children }) => (
    <pre className="my-3 overflow-x-auto rounded-[var(--radius-md)] border border-border bg-surface px-3 py-2 font-mono text-xs leading-relaxed text-text-muted">
      {children}
    </pre>
  ),
  code: ({ children, className }) => (
    <code
      className={
        className ??
        'rounded-[var(--radius-sm)] bg-surface-2 px-1 py-0.5 font-mono text-[0.8125rem] text-accent-text'
      }
    >
      {children}
    </code>
  ),
} satisfies Components;
