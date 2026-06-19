import { isDisplayPayload } from './types';
import type { DisplaySource } from './types';

// answerSources — the pure aggregation behind the answer-level "Sources (N)"
// affordance (D-13). An assistant turn can emit several tool displays, each
// carrying the per-turn source registry on its DisplayPayload; the answer-level
// button shows ONE deduped list across them all. Kept off the component so it is
// directly unit- and mutation-testable.
//
// Dedupe is by ref_id (the registry's stable key, D-05); the first occurrence
// wins so the displayed index/cited flag matches the registry the citations use.
// A cited entry that re-appears as consulted keeps its cited status (cited OR'd in).

interface MaybeDisplayPart {
  readonly type?: unknown;
  readonly display?: unknown;
}

/** Collect + dedupe the source registry across an assistant message's tool parts. */
export function aggregateAnswerSources(content: unknown): DisplaySource[] {
  if (!Array.isArray(content)) return [];
  const byRefId = new Map<string, DisplaySource>();
  for (const part of content as readonly MaybeDisplayPart[]) {
    if (part.type !== 'tool-call' || !isDisplayPayload(part.display)) continue;
    for (const source of part.display.sources ?? []) {
      const existing = byRefId.get(source.ref_id);
      if (existing === undefined) {
        byRefId.set(source.ref_id, source);
      } else if (source.cited && !existing.cited) {
        // Promote to cited if any occurrence cited it (the citation is the signal).
        byRefId.set(source.ref_id, { ...existing, cited: true });
      }
    }
  }
  return [...byRefId.values()].sort((a, b) => a.index - b.index);
}
