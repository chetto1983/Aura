import {
  MarkdownTextPrimitive,
  type MarkdownTextPrimitiveProps,
} from '@assistant-ui/react-markdown';
import type { PluggableList } from 'unified';
import {
  baseMarkdownComponents,
  buildRehypePlugins,
  remarkPlugins,
} from './markdownConfig';

// MarkdownText: the streaming chat host (the assistant-ui MarkdownTextPrimitive
// bound to the current message part). It renders through the shared markdownConfig
// pipeline (HARDEN-08 chokepoint) so the chat lane and the standalone DocumentDisplay
// render identically. The citation document path injects rehypeCitations via
// `extraRehypePlugins` + the citation `span` renderer via `extraComponents` — an
// internal MERGE, not a forked component (T-26-17): buildRehypePlugins keeps
// rehype-sanitize LAST so an extra plugin can never bypass sanitization.

export type ExtraMarkdownComponents = NonNullable<MarkdownTextPrimitiveProps['components']>;

export interface MarkdownTextProps
  extends Omit<MarkdownTextPrimitiveProps, 'remarkPlugins' | 'rehypePlugins' | 'components'> {
  /** Extra rehype plugins (e.g. rehypeCitations) merged BEFORE the sanitize pass. */
  readonly extraRehypePlugins?: PluggableList;
  /** Extra component renderers merged over the defaults (e.g. the citation span). */
  readonly extraComponents?: ExtraMarkdownComponents;
}

export function MarkdownText({
  extraRehypePlugins,
  extraComponents,
  ...props
}: MarkdownTextProps) {
  const components = (
    extraComponents !== undefined
      ? { ...baseMarkdownComponents, ...extraComponents }
      : baseMarkdownComponents
  ) as ExtraMarkdownComponents;

  return (
    <MarkdownTextPrimitive
      remarkPlugins={remarkPlugins}
      rehypePlugins={buildRehypePlugins(extraRehypePlugins)}
      components={components}
      {...props}
    />
  );
}
