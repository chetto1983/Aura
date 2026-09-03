import type { LLMCatalogModel } from './settingsApi';

// How a catalogue row's two decision-grade numbers are written: the context window and,
// where the provider charges per token, its input/output rate. Kept out of the component
// so both the picker and its tests can read them without importing React.

/** 204800 reads as "205K", 1000000 as "1M": exact token counts are noise at this size. */
export function formatContext(tokens: number): string {
  if (tokens >= 1_000_000) {
    const millions = tokens / 1_000_000;
    return `${millions >= 10 ? millions.toFixed(0) : millions.toFixed(millions % 1 === 0 ? 0 : 1)}M`;
  }
  if (tokens >= 1000) return `${Math.round(tokens / 1000).toString()}K`;
  return tokens.toString();
}

/**
 * $0.14 stays $0.14. A rate under a cent would round to $0.00 and read as free, so it
 * keeps the digits it needs instead.
 */
export function formatRate(perMillion: number): string {
  if (perMillion === 0) return '$0';
  if (perMillion < 0.01) return `$${perMillion.toPrecision(2)}`;
  return `$${perMillion.toFixed(2)}`;
}

/** The right-hand column of a catalogue row. */
export function modelMeta(model: LLMCatalogModel, freeLabel: string): string {
  const parts: string[] = [];
  if (model.context_window !== undefined && model.context_window > 0) {
    parts.push(`${formatContext(model.context_window)} ctx`);
  }
  if (model.has_price) {
    parts.push(`${formatRate(model.input_per_1m ?? 0)} / ${formatRate(model.output_per_1m ?? 0)}`);
  } else {
    parts.push(freeLabel);
  }
  return parts.join(' · ');
}
