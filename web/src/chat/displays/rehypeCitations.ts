import { visit } from 'unist-util-visit';
import type { Element, Root, Text } from 'hast';
import type { DisplaySource } from './types';

// rehypeCitations (D-04 / DISP-03): a rehype plugin that finds inline `[n]`
// citation markers IN the model's text and splices a citation-chip element at the
// EXACT match position (NOT end-appended). It ports the elysia walker
// (MarkdownFormat.tsx:39-111) and DELIBERATELY DROPS its two bugs:
//   1. elysia's `processTextWithCitations` end-append fallback — the model already
//      places `[n]` inline next to the claim (D-05), so we splice in place only.
//   2. nothing here touches images (elysia's `prose-img:hidden`) — DocumentDisplay
//      renders images via the sanitized MarkdownText.
//
// SECURITY — T-26-18 (spoofing): a `[n]` whose number is NOT in the code-owned
// source registry resolves to NOTHING and is left as the literal `[n]` text — no
// live chip. The model cannot fabricate a citation target; only registry-backed
// numbers become chips. The splice emits a neutral `<span data-ref-id …>` whose
// children are empty; the CitationBubble component (the `span` renderer on
// MarkdownText) reads the data-* attributes and renders the accessible chip. The
// span carries no executable content and rides through rehype-sanitize via the
// schema's data-* allow (the sanitize chokepoint stays intact, T-26-17).

const CITATION_RE = /\[(\d+)\]/g;

export interface RehypeCitationsOptions {
  /** The source registry indexed by its 1-based citation number (`source.index`). */
  readonly byIndex: ReadonlyMap<number, DisplaySource>;
}

/** The data attribute marking a spliced citation chip (read by the span renderer). */
export const CITATION_REF_ATTR = 'data-ref-id';
export const CITATION_NUMBER_ATTR = 'data-citation-number';

function chipElement(refId: string, citationNumber: number): Element {
  return {
    type: 'element',
    tagName: 'span',
    properties: {
      [CITATION_REF_ATTR]: refId,
      [CITATION_NUMBER_ATTR]: String(citationNumber),
    },
    children: [],
  };
}

/**
 * Build the rehype plugin. Returns a transformer that, for each text node holding
 * one or more `[n]` markers backed by the registry, replaces the node with the
 * surrounding text plus an inline chip element at each matched position.
 */
export function rehypeCitations({ byIndex }: RehypeCitationsOptions) {
  return (tree: Root): void => {
    visit(tree, 'text', (node: Text, index, parent) => {
      if (!parent || typeof index !== 'number') return;
      const value = node.value;
      const matches = [...value.matchAll(CITATION_RE)];
      if (matches.length === 0) return;

      // Skip entirely if NONE of the matched numbers are registry-backed — leaves
      // the original text untouched so an all-unknown line keeps its literal `[n]`s.
      const hasKnown = matches.some((m) => byIndex.has(Number(m[1])));
      if (!hasKnown) return;

      const replacement: (Element | Text)[] = [];
      let lastIndex = 0;

      for (const match of matches) {
        const full = match[0];
        const number = Number(match[1]);
        const at = match.index;
        const source = byIndex.get(number);

        // Text before this marker.
        if (at > lastIndex) {
          replacement.push({ type: 'text', value: value.slice(lastIndex, at) });
        }

        if (source !== undefined) {
          // Registry-backed → splice the inline chip element at the claim position.
          replacement.push(chipElement(source.ref_id, number));
        } else {
          // Unknown number → keep the literal `[n]` text (no live chip, T-26-18).
          replacement.push({ type: 'text', value: full });
        }

        lastIndex = at + full.length;
      }

      // Trailing text after the last marker.
      if (lastIndex < value.length) {
        replacement.push({ type: 'text', value: value.slice(lastIndex) });
      }

      parent.children.splice(index, 1, ...replacement);
    });
  };
}

/** Build the index-keyed lookup map the plugin consumes from a sources list. */
export function indexSources(
  sources: readonly DisplaySource[] | undefined,
): Map<number, DisplaySource> {
  const byIndex = new Map<number, DisplaySource>();
  for (const source of sources ?? []) {
    byIndex.set(source.index, source);
  }
  return byIndex;
}
