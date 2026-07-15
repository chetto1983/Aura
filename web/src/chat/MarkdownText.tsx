import {
  MarkdownTextPrimitive,
  type MarkdownTextPrimitiveProps,
} from '@assistant-ui/react-markdown';
import type { Element } from 'hast';
import type { PluggableList } from 'unified';
import { baseMarkdownComponents, buildRehypePlugins, remarkPlugins } from './markdownConfig';

// MarkdownText: the streaming chat host (the assistant-ui MarkdownTextPrimitive
// bound to the current message part). It renders through the shared markdownConfig
// pipeline (HARDEN-08 chokepoint) so the chat lane and the standalone DocumentDisplay
// render identically. The citation document path injects rehypeCitations via
// `extraRehypePlugins` + the citation `span` renderer via `extraComponents` — an
// internal MERGE, not a forked component (T-26-17): buildRehypePlugins keeps
// rehype-sanitize LAST so an extra plugin can never bypass sanitization.

export type ExtraMarkdownComponents = NonNullable<MarkdownTextPrimitiveProps['components']>;

export interface MarkdownTextProps extends Omit<
  MarkdownTextPrimitiveProps,
  'remarkPlugins' | 'rehypePlugins' | 'components'
> {
  /** Constrain prose blocks without capping wide Markdown nodes such as tables. */
  readonly constrainProse?: boolean;
  /** Extra rehype plugins (e.g. rehypeCitations) merged BEFORE the sanitize pass. */
  readonly extraRehypePlugins?: PluggableList;
  /** Extra component renderers merged over the defaults (e.g. the citation span). */
  readonly extraComponents?: ExtraMarkdownComponents;
}

const PROSE_BLOCK_CLASS = 'w-full max-w-[48rem] [overflow-wrap:anywhere]';

function containsTable(node: Element | undefined): boolean {
  return (
    node?.children.some(
      (child) => child.type === 'element' && (child.tagName === 'table' || containsTable(child)),
    ) ?? false
  );
}

function structuralProseProps(node: Element | undefined) {
  return containsTable(node) ? {} : { 'data-message-prose': true, className: PROSE_BLOCK_CLASS };
}

const constrainedProseComponents = {
  p: ({ children }) => (
    <p data-message-prose className={PROSE_BLOCK_CLASS}>
      {children}
    </p>
  ),
  h1: ({ children }) => (
    <h1 data-message-prose className={PROSE_BLOCK_CLASS}>
      {children}
    </h1>
  ),
  h2: ({ children }) => (
    <h2 data-message-prose className={PROSE_BLOCK_CLASS}>
      {children}
    </h2>
  ),
  h3: ({ children }) => (
    <h3 data-message-prose className={PROSE_BLOCK_CLASS}>
      {children}
    </h3>
  ),
  h4: ({ children }) => (
    <h4 data-message-prose className={PROSE_BLOCK_CLASS}>
      {children}
    </h4>
  ),
  h5: ({ children }) => (
    <h5 data-message-prose className={PROSE_BLOCK_CLASS}>
      {children}
    </h5>
  ),
  h6: ({ children }) => (
    <h6 data-message-prose className={PROSE_BLOCK_CLASS}>
      {children}
    </h6>
  ),
  blockquote: ({ node, children }) => (
    <blockquote {...structuralProseProps(node)}>{children}</blockquote>
  ),
  ol: ({ node, children }) => <ol {...structuralProseProps(node)}>{children}</ol>,
  ul: ({ node, children }) => <ul {...structuralProseProps(node)}>{children}</ul>,
} satisfies ExtraMarkdownComponents;

export function MarkdownText({
  constrainProse = false,
  extraRehypePlugins,
  extraComponents,
  ...props
}: MarkdownTextProps) {
  const components = {
    ...baseMarkdownComponents,
    ...(constrainProse ? constrainedProseComponents : {}),
    ...extraComponents,
  } as ExtraMarkdownComponents;

  return (
    <MarkdownTextPrimitive
      remarkPlugins={remarkPlugins}
      rehypePlugins={buildRehypePlugins(extraRehypePlugins)}
      components={components}
      {...props}
    />
  );
}
