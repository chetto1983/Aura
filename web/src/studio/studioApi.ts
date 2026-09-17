// studioApi.ts — the browser half of internal/agui/studio_api.go. Every type below mirrors a
// `json:` tag in internal/agui/studio_dto.go: a field the Go DTO marks `omitempty` is optional
// here, because an absent capability or price is unknown and must never read as zero or free.

/** The two kinds the catalog knows; the routes refuse a third. */
export type StudioKind = 'image' | 'video';

/** One cell of a video model's price matrix: what a second costs at this resolution with this
 *  audio choice. The composer prices the clip being paid for, so a single rate would be wrong
 *  for every other cell. */
export interface StudioPrice {
  readonly resolution: string;
  readonly audio: boolean;
  readonly usd_per_second: number;
}

/** One picker row (studioModelDTO). `audio` and `seed` are always present — false means the
 *  model declares neither, which is also what an image row reads. */
export interface StudioModel {
  readonly id: string;
  readonly name?: string;
  readonly description?: string;
  readonly durations?: readonly number[];
  readonly resolutions?: readonly string[];
  readonly aspect_ratios?: readonly string[];
  /** 'first_frame' and/or 'last_frame' — the frames this model accepts. */
  readonly frame_images?: readonly string[];
  readonly audio: boolean;
  readonly seed: boolean;
  readonly prices?: readonly StudioPrice[];
  readonly reference_max?: number;
  readonly image_min_usd?: number;
  readonly image_max_usd?: number;
  readonly image_token_min_per_1m?: number;
  readonly image_token_max_per_1m?: number;
}

export interface StudioModels {
  readonly default: string;
  readonly models: readonly StudioModel[];
}

/** What the provider was actually asked for, after the server's clamp. */
export interface StudioUsed {
  readonly duration?: number;
  readonly resolution?: string;
  readonly aspect_ratio?: string;
  readonly audio?: boolean;
  readonly seed?: number;
  readonly first_frame_asset_id?: string;
  readonly last_frame_asset_id?: string;
  readonly reference_asset_ids?: readonly string[];
}

/** One history row, and the answer to both create routes. */
export interface StudioRecord {
  readonly id: string;
  readonly kind: StudioKind;
  readonly status: string;
  readonly model: string;
  readonly prompt: string;
  readonly used: StudioUsed;
  readonly adjustments?: readonly string[];
  /** Absent while the cost is not known yet — never 0. */
  readonly cost_usd?: number;
  readonly asset_id?: string;
  readonly error?: { readonly code: string; readonly message: string };
  readonly created_at: string;
  readonly completed_at?: string;
}

/** One library row. The server answers more (modality, status, size, created_at); the composer
 *  only ever needs to name the image it is attaching. */
export interface StudioImageRef {
  readonly id: string;
  readonly file_name: string;
  readonly mime_type: string;
}

/** POST /api/studio/videos' body. The route strict-decodes, so an undeclared field is a 400 —
 *  which is why every optional axis is omitted rather than sent as null. */
export interface StudioVideoBody {
  readonly model: string;
  readonly prompt: string;
  readonly duration?: number;
  readonly resolution?: string;
  readonly aspect_ratio?: string;
  readonly audio?: boolean;
  readonly seed?: number;
  readonly first_frame_asset_id?: string;
  readonly last_frame_asset_id?: string;
}

/** POST /api/studio/images' body. */
export interface StudioImageBody {
  readonly model: string;
  readonly prompt: string;
  readonly aspect_ratio?: string;
  readonly reference_asset_ids?: readonly string[];
}

/** A refusal the operator can act on: the status the route chose and the code + sentence the
 *  server wrote. An infrastructure failure has no code, so `code` is empty and the page falls
 *  back to its generic sentence rather than inventing one. */
export class StudioError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'StudioError';
    this.status = status;
    this.code = code;
  }
}

/** One screen of history — the server's own default (studioHistoryDefault), named so a caller
 *  can tell a full page from the last one. */
export const STUDIO_HISTORY_LIMIT = 24;

function readInit(signal?: AbortSignal): RequestInit {
  const init: RequestInit = {
    method: 'GET',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  };
  if (signal !== undefined) init.signal = signal;
  return init;
}

function writeInit(body?: unknown): RequestInit {
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  const init: RequestInit = { method: 'POST', headers, credentials: 'same-origin' };
  if (body !== undefined) init.body = JSON.stringify(body);
  return init;
}

/** Read a Studio response, turning a failure into a StudioError.
 *
 *  A refused generation answers studioErrorDTO — `{code, error}`, the code in `code` and the
 *  sentence in `error`. The gate routes (unwired Studio, no principal, a body the route cannot
 *  decode) answer http.Error text instead, so a body that is not that object leaves the bare
 *  status and an empty code. */
async function studioJSON<T>(res: Response): Promise<T> {
  if (res.ok) return (await res.json()) as T;
  const fallback = `HTTP ${String(res.status)}`;
  const raw = await res.text().catch(() => '');
  let code = '';
  let message = fallback;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (parsed !== null && typeof parsed === 'object') {
      const body = parsed as { readonly code?: unknown; readonly error?: unknown };
      if (typeof body.code === 'string') code = body.code;
      if (typeof body.error === 'string' && body.error.length > 0) message = body.error;
    }
  } catch {
    // Not JSON: the route answered plain text, and the status is all there is to report.
  }
  throw new StudioError(res.status, code, message);
}

export async function fetchStudioModels(
  kind: StudioKind,
  signal?: AbortSignal,
): Promise<StudioModels> {
  const res = await fetch(`/api/studio/models?kind=${kind}`, readInit(signal));
  return studioJSON<StudioModels>(res);
}

export async function listStudioHistory(
  kind: StudioKind | undefined,
  before: string | undefined,
  signal?: AbortSignal,
): Promise<readonly StudioRecord[]> {
  const query = new URLSearchParams({ limit: String(STUDIO_HISTORY_LIMIT) });
  if (kind !== undefined) query.set('kind', kind);
  if (before !== undefined && before.length > 0) query.set('before', before);
  const res = await fetch(`/api/studio/history?${query.toString()}`, readInit(signal));
  const page = await studioJSON<{ readonly records?: readonly StudioRecord[] }>(res);
  return page.records ?? [];
}

export async function createStudioVideo(body: StudioVideoBody): Promise<StudioRecord> {
  const res = await fetch('/api/studio/videos', writeInit(body));
  return studioJSON<StudioRecord>(res);
}

export async function createStudioImage(body: StudioImageBody): Promise<StudioRecord> {
  const res = await fetch('/api/studio/images', writeInit(body));
  return studioJSON<StudioRecord>(res);
}

export async function listStudioLibrary(signal?: AbortSignal): Promise<readonly StudioImageRef[]> {
  const res = await fetch('/api/studio/library', readInit(signal));
  const page = await studioJSON<{ readonly assets?: readonly StudioImageRef[] }>(res);
  return page.assets ?? [];
}

export async function finalizeStudioUpload(id: string): Promise<StudioImageRef> {
  const res = await fetch(`/api/studio/uploads/${encodeURIComponent(id)}/finalize`, writeInit());
  return studioJSON<StudioImageRef>(res);
}

/** The owned, identity-scoped asset routes. The Studio never learns an object key: a result is
 *  read back through the same gate the chat attachments use. */
export function assetDownloadUrl(id: string): string {
  return `/api/assets/${encodeURIComponent(id)}/download`;
}

export function assetStreamUrl(id: string): string {
  return `/api/assets/${encodeURIComponent(id)}/stream`;
}
