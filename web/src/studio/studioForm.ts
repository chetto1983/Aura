import type {
  StudioImageBody,
  StudioImageRef,
  StudioKind,
  StudioModel,
  StudioRecord,
  StudioVideoBody,
} from './studioApi';

// studioForm.ts — the Studio composer as pure functions, so the page can be rendered from a
// draft and the rules can be tested without a DOM.
//
// The defaults here agree with mediagen.CheapestVideoInput on the server: an option the body
// omits is filled with the cheapest the model declares, because the provider's own default is
// whatever it sells best — the long, high, audible clip. The two must say the same thing, or
// the price beside Generate is not the price that gets paid.

export interface StudioOptions {
  readonly resolution: string;
  readonly duration: number | undefined;
  readonly aspectRatio: string;
  readonly audio: boolean;
  readonly seed: number | undefined;
}

export interface StudioDraft {
  readonly kind: StudioKind;
  readonly model: string;
  readonly prompt: string;
  readonly options: StudioOptions;
  /** The reference images for an image model; the start frame for a video one. */
  readonly images: readonly StudioImageRef[];
  readonly endFrame: StudioImageRef | undefined;
}

/** The ratio to prefer when a model offers it: the one every catalog declares and the one a
 *  clip is watched in. */
const PREFERRED_RATIO = '16:9';

const K_HEIGHTS: Readonly<Record<string, number>> = { '1k': 1024, '2k': 2048, '4k': 4096 };

/** Measure "<n>p" and the 1K/2K/4K labels by pixel height, as mediagen's clamp does
 *  (internal/mediagen/clamp.go). A label neither form describes has no height, and is never
 *  silently preferred to one that has. */
function resolutionHeight(label: string): number | undefined {
  const normalized = label.trim().toLowerCase();
  const known = K_HEIGHTS[normalized];
  if (known !== undefined) return known;
  if (!normalized.endsWith('p')) return undefined;
  const digits = normalized.slice(0, -1);
  if (!/^\d+$/.test(digits)) return undefined;
  const height = Number(digits);
  return height > 0 ? height : undefined;
}

function lowestResolution(declared: readonly string[] | undefined): string {
  let lowest = '';
  let lowestHeight = 0;
  for (const candidate of declared ?? []) {
    const height = resolutionHeight(candidate);
    if (height === undefined) continue;
    if (lowest === '' || height < lowestHeight) {
      lowest = candidate;
      lowestHeight = height;
    }
  }
  return lowest;
}

/** The least expensive request this model can be asked for: the shortest clip, the lowest
 *  resolution, silence, and no seed. An image model declares none of those axes, so it keeps
 *  only its ratio. */
export function cheapestOptions(model: StudioModel): StudioOptions {
  const durations = model.durations ?? [];
  const ratios = model.aspect_ratios ?? [];
  return {
    resolution: lowestResolution(model.resolutions),
    duration: durations.length > 0 ? Math.min(...durations) : undefined,
    aspectRatio: ratios.includes(PREFERRED_RATIO) ? PREFERRED_RATIO : (ratios[0] ?? ''),
    audio: false,
    seed: undefined,
  };
}

/** How many images this model accepts: its reference ceiling for an image, the one start frame
 *  for a video. An undeclared ceiling means none, not unlimited — the server would refuse the
 *  extra ones anyway, and an operator who attached five should see four survive the switch. */
export function maxImages(model: StudioModel, kind: StudioKind): number {
  if (kind === 'image') return Math.max(model.reference_max ?? 0, 0);
  return (model.frame_images ?? []).includes('first_frame') ? 1 : 0;
}

function keepOrCheapest<T>(current: T, declared: readonly T[] | undefined, cheapest: T): T {
  return (declared ?? []).includes(current) ? current : cheapest;
}

/** Re-aim a draft at a model: every axis the model still declares survives, everything else
 *  falls back to the cheapest it does declare. Called when the operator switches models, so
 *  the form never carries an option the new model would refuse. */
export function reconcileDraft(draft: StudioDraft, model: StudioModel): StudioDraft {
  const cheapest = cheapestOptions(model);
  const images = draft.images.slice(0, maxImages(model, draft.kind));
  const takesEndFrame = (model.frame_images ?? []).includes('last_frame');
  return {
    ...draft,
    // The body must name the model whose capabilities shaped it.
    model: model.id,
    options: {
      resolution: keepOrCheapest(draft.options.resolution, model.resolutions, cheapest.resolution),
      duration:
        draft.options.duration !== undefined &&
        (model.durations ?? []).includes(draft.options.duration)
          ? draft.options.duration
          : cheapest.duration,
      aspectRatio: keepOrCheapest(
        draft.options.aspectRatio,
        model.aspect_ratios,
        cheapest.aspectRatio,
      ),
      audio: model.audio && draft.options.audio,
      seed: model.seed ? draft.options.seed : undefined,
    },
    images,
    // An end frame with nothing to start from is not a clip the provider can make.
    endFrame: takesEndFrame && images.length > 0 ? draft.endFrame : undefined,
  };
}

/** The composer pill's text: `16:9 · 720p · 4s` for a clip, `1:1` for an image. An axis the
 *  model never declared is absent from the draft, so it is absent from the summary rather
 *  than shown blank — the pill has to read as the request that is about to be sent.
 *
 *  It lives here rather than beside the popover that renders it because a .tsx exporting a
 *  pure function next to a component loses fast refresh (react-refresh/only-export-components,
 *  the same rule collectedJobsContext.ts documents). */
export function optionsSummary(
  options: StudioOptions,
  formatSeconds: (seconds: number) => string,
): string {
  return [
    options.aspectRatio,
    options.resolution,
    options.duration === undefined ? '' : formatSeconds(options.duration),
  ]
    .filter((part) => part !== '')
    .join(' · ');
}

/** What this request will cost, or undefined when the catalog never priced it.
 *
 *  Undefined is not zero. A price the catalog did not declare is unknown, and a Generate button
 *  reading $0.00 is a claim about the bill that nobody can honour. */
export function estimateCost(model: StudioModel, options: StudioOptions): number | undefined {
  const row = model.prices?.find(
    (price) => price.resolution === options.resolution && price.audio === options.audio,
  );
  if (row !== undefined) {
    return options.duration === undefined ? undefined : row.usd_per_second * options.duration;
  }
  return model.image_min_usd;
}

/** A body together with the route it belongs to. The two body shapes overlap structurally —
 *  both are `{model, prompt}` plus optional fields — so a bare union lets a video body be
 *  handed to the image mutation without a word from the compiler. Tagging it means the wrong
 *  pairing cannot typecheck. */
export type StudioRequest =
  | { readonly kind: 'video'; readonly body: StudioVideoBody }
  | { readonly kind: 'image'; readonly body: StudioImageBody };

/** The body for this draft, carrying only the fields the model declares. The create routes
 *  strict-decode, and an axis the model does not offer earns a "not supported" adjustment about
 *  a choice the operator never made — so an undeclared field is absent, not zeroed. */
export function requestBody(draft: StudioDraft, model: StudioModel): StudioRequest {
  const { options } = draft;
  const ratio = options.aspectRatio === '' ? {} : { aspect_ratio: options.aspectRatio };
  if (draft.kind === 'image') {
    const ids = draft.images.map((image) => image.id);
    const image: StudioImageBody = {
      model: model.id,
      prompt: draft.prompt,
      ...ratio,
      ...(ids.length > 0 ? { reference_asset_ids: ids } : {}),
    };
    return { kind: 'image', body: image };
  }
  const firstFrame = draft.images[0];
  const video: StudioVideoBody = {
    model: model.id,
    prompt: draft.prompt,
    ...(options.duration === undefined ? {} : { duration: options.duration }),
    ...(options.resolution === '' ? {} : { resolution: options.resolution }),
    ...ratio,
    ...(model.audio ? { audio: options.audio } : {}),
    ...(model.seed && options.seed !== undefined ? { seed: options.seed } : {}),
    ...(firstFrame === undefined ? {} : { first_frame_asset_id: firstFrame.id }),
    ...(draft.endFrame === undefined ? {} : { last_frame_asset_id: draft.endFrame.id }),
  };
  return { kind: 'video', body: video };
}

/** Every asset id a record asked the provider to use. Reuse has to resolve all of them, and
 *  the count is how a dropped image is told from a record that never had one. */
export function inputAssetIds(record: StudioRecord): readonly string[] {
  const used = record.used;
  return [
    ...(used.reference_asset_ids ?? []),
    used.first_frame_asset_id,
    used.last_frame_asset_id,
  ].filter((id): id is string => id !== undefined && id !== '');
}

/** A generation the history should keep watching. */
export function isActive(record: Pick<StudioRecord, 'status'>): boolean {
  return record.status === 'pending' || record.status === 'in_progress';
}

/** Rebuild the draft that produced a record, for Reuse.
 *
 *  The options come from `used` — what the provider was actually asked for after the server's
 *  clamp — so reusing a narrowed request reuses the narrowed one, not the one that was refused.
 *  An id the library page no longer carries is dropped rather than turned into a reference to
 *  an image the operator cannot see. */
export function draftFromRecord(
  record: StudioRecord,
  images: readonly StudioImageRef[],
): StudioDraft {
  const resolve = (id: string | undefined): StudioImageRef | undefined =>
    id === undefined ? undefined : images.find((image) => image.id === id);
  const used = record.used;
  const attached =
    record.kind === 'image'
      ? (used.reference_asset_ids ?? []).map(resolve)
      : [resolve(used.first_frame_asset_id)];
  return {
    kind: record.kind,
    model: record.model,
    prompt: record.prompt,
    options: {
      resolution: used.resolution ?? '',
      duration: used.duration,
      aspectRatio: used.aspect_ratio ?? '',
      audio: used.audio ?? false,
      seed: used.seed,
    },
    images: attached.filter((image) => image !== undefined),
    endFrame: resolve(used.last_frame_asset_id),
  };
}
