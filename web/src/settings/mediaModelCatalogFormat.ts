import type { ImageCatalogModel, ModelRow, VideoCatalogModel } from './mediaModelCatalog';
import { formatRate, modelMeta } from './modelCatalogFormat';

/** The localized words of a media row; the numbers and units are written here. */
export interface MediaLabels {
  readonly references: (max: number) => string;
  readonly duration: (min: number, max: number) => string;
  readonly imageToVideo: string;
  /** The per-million output-token rate of a token-billed image model, already formatted. */
  readonly imageTokens: (price: string) => string;
}

// Per-image and per-second prices sit in the cents and below, where rounding to cents would
// turn $0.039 into $0.04 and a $0.035–$0.039 range into one price; four significant digits
// keep them apart, and trailing zeros are dropped.
const MEDIA_PRICE = new Intl.NumberFormat('en-US', {
  maximumSignificantDigits: 4,
  useGrouping: false,
});

// A price shows only when both ends are known: filling in a missing end would state a range
// the catalogue never published.
function priceRange(min: number | undefined, max: number | undefined): string | undefined {
  if (min === undefined || max === undefined) return undefined;
  const low = `$${MEDIA_PRICE.format(min)}`;
  const high = `$${MEDIA_PRICE.format(max)}`;
  return low === high ? low : `${low}–${high}`;
}

// A token-billed model's rate, per million like an LLM row's, in the same $0.00 form. Both
// ends are required for the same reason as priceRange.
function tokenRange(min: number | undefined, max: number | undefined): string | undefined {
  if (min === undefined || max === undefined) return undefined;
  const low = formatRate(min);
  const high = formatRate(max);
  return low === high ? low : `${low}–${high}`;
}

/**
 * An image row: its price first, so it survives a truncated row, then its reference limit.
 * A per-image price wins; a token-billed model shows its output-token rate instead, since its
 * cost per image depends on size and quality and must not be written as one.
 */
export function imageModelMeta(model: ImageCatalogModel, labels: MediaLabels): string {
  const parts: string[] = [];
  const price = model.has_price ? priceRange(model.image_min_usd, model.image_max_usd) : undefined;
  const tokens = tokenRange(model.image_token_min_per_1m, model.image_token_max_per_1m);
  if (price !== undefined) parts.push(`${price}/image`);
  else if (tokens !== undefined) parts.push(labels.imageTokens(tokens));
  if (model.reference_max !== undefined && model.reference_max > 0) {
    parts.push(labels.references(model.reference_max));
  }
  return parts.join(' · ');
}

/** A video row: the $/s range, the duration range, the resolutions and image-to-video. */
export function videoModelMeta(model: VideoCatalogModel, labels: MediaLabels): string {
  const parts: string[] = [];
  const price = model.has_price
    ? priceRange(model.second_min_usd, model.second_max_usd)
    : undefined;
  if (price !== undefined) parts.push(`${price}/s`);
  if (model.duration_min !== undefined && model.duration_max !== undefined) {
    parts.push(labels.duration(model.duration_min, model.duration_max));
  }
  if (model.resolutions !== undefined && model.resolutions.length > 0) {
    parts.push(model.resolutions.join(', '));
  }
  if (model.image_to_video === true) parts.push(labels.imageToVideo);
  return parts.join(' · ');
}

/** Any picker row: an LLM row keeps its context and token-rate label. */
export function modelRowMeta(row: ModelRow, freeLabel: string, labels: MediaLabels): string {
  if (!('kind' in row)) return modelMeta(row, freeLabel);
  switch (row.kind) {
    case 'image':
      return imageModelMeta(row, labels);
    case 'video':
      return videoModelMeta(row, labels);
    default:
      // A voice row is its id: its rate is published without a unit.
      return '';
  }
}
