import {
  MarkdownTextPrimitive,
  type MarkdownTextPrimitiveProps,
} from '@assistant-ui/react-markdown';
import rehypeSanitize from 'rehype-sanitize';
import remarkGfm from 'remark-gfm';
import { markdownSanitizeSchema } from './markdownSanitize';

export function MarkdownText(
  props: Omit<MarkdownTextPrimitiveProps, 'remarkPlugins' | 'rehypePlugins'>,
) {
  return (
    <MarkdownTextPrimitive
      remarkPlugins={[remarkGfm]}
      rehypePlugins={[[rehypeSanitize, markdownSanitizeSchema]]}
      components={{
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
        table: ({ children }) => (
          <div className="my-3 overflow-x-auto rounded-[var(--radius-md)] border border-border">
            <table className="min-w-full border-collapse text-left text-sm">{children}</table>
          </div>
        ),
        th: ({ children, align }) => (
          <th
            align={align}
            className="border-b border-border bg-surface-2 px-3 py-2 text-[0.75rem] font-medium uppercase text-text-faint"
          >
            {children}
          </th>
        ),
        td: ({ children, align }) => (
          <td align={align} className="border-b border-border px-3 py-2 text-text-muted">
            {children}
          </td>
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
      }}
      {...props}
    />
  );
}
