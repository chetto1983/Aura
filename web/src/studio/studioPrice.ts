import type { StudioModel } from './studioApi';

// studioPrice.ts — the two numbers the bar shows about money: what a model charges, on its
// picker row, and what THIS request is about to cost, beside Generate. Both are read from the
// catalog and neither is ever invented: a price the catalog did not declare is absent, not
// zero, because a button reading $0.00 is a claim about the bill nobody can honour.

// Per-image and per-second prices live in the cents and below, where rounding to cents turns
// $0.039 into $0.04 and a $0.035–$0.039 range into one price. Four significant digits keep
// them apart and drop trailing zeros.
const USD = new Intl.NumberFormat('en-US', {
  maximumSignificantDigits: 4,
  useGrouping: false,
});

/** A catalog rate, at the precision the rates themselves are published in. */
export function formatUsd(value: number): string {
  return USD.format(value);
}

/** What one generation is about to cost. Two decimals is what a bill is read in, but only
 *  down to a cent: below that the rounding would print $0.00 over a request that is not free,
 *  so the rate's own precision is kept instead. */
export function formatEstimate(usd: number): string {
  return usd >= 0.01 ? usd.toFixed(2) : USD.format(usd);
}

/** A range shows only when both ends are known: filling in a missing end would state a span
 *  the catalog never published. */
function range(min: number | undefined, max: number | undefined): string | undefined {
  if (min === undefined || max === undefined) return undefined;
  const low = formatUsd(min);
  const high = formatUsd(max);
  return low === high ? low : `${low}–${high}`;
}

/** How a model's price is metered: per second of clip, per image, or per million output
 *  tokens for an image model billed that way. */
export type StudioPriceUnit = 'second' | 'image' | 'tokens';

export interface StudioModelPrice {
  readonly unit: StudioPriceUnit;
  readonly amount: string;
}

/** What a picker row can truthfully say this model costs, or undefined when the catalog
 *  priced neither form. A video model's rate spans its price matrix, because the cell that
 *  applies is not chosen until the resolution and the sound switch are. */
export function modelPrice(model: StudioModel): StudioModelPrice | undefined {
  const seconds = (model.prices ?? []).map((price) => price.usd_per_second);
  if (seconds.length > 0) {
    const amount = range(Math.min(...seconds), Math.max(...seconds));
    return amount === undefined ? undefined : { unit: 'second', amount };
  }
  const perImage = range(model.image_min_usd, model.image_max_usd);
  if (perImage !== undefined) return { unit: 'image', amount: perImage };
  const perMillion = range(model.image_token_min_per_1m, model.image_token_max_per_1m);
  if (perMillion !== undefined) return { unit: 'tokens', amount: perMillion };
  return undefined;
}
