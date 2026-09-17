// generationState — the pure gate between an image_generate / video_generate tool part
// and the generation frame (spec §5 Cockpit). Arguments and results are untrusted model and
// tool text: they are parsed as JSON only, and an aspect ratio outside the tools' enum never
// reaches CSS.

/** The exact suffix internal/gateway/reserve.go (replayedMarker) appends to a replayed
 *  tool result preview. It must be removed before the preview is parsed as JSON. */
export const REPLAYED_RESULT_MARKER =
  '\n\n[replayed: this result is from a prior dispatch of this call, not a fresh execution]';

export type GenerationState = 'running' | 'deferred' | 'fallback';

export type MediaKind = 'image' | 'video';

const MEDIA_TOOLS = new Set(['image_generate', 'video_generate']);

// Both statuses answer a job that has not finished: video_generate_collect.go returns the
// stored job status, and a job the provider queued is stored pending.
const ACTIVE_JOB_STATUSES = new Set<unknown>(['pending', 'in_progress']);

// The union of the aspect_ratio enums in internal/agent/tools/image_generate.go and
// video_generate.go, as CSS aspect-ratio values. A Map, so an inherited key is never a hit.
const ASPECT_RATIOS = new Map([
  ['1:1', '1 / 1'],
  ['16:9', '16 / 9'],
  ['9:16', '9 / 16'],
  ['4:3', '4 / 3'],
  ['3:4', '3 / 4'],
  ['3:2', '3 / 2'],
  ['2:3', '2 / 3'],
  ['21:9', '21 / 9'],
  ['9:21', '9 / 21'],
]);

const SQUARE = '1 / 1';

function asObject(value: unknown): Record<string, unknown> | undefined {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

function parseObject(text: string): Record<string, unknown> | undefined {
  try {
    return asObject(JSON.parse(text));
  } catch {
    return undefined;
  }
}

function resultObject(result: unknown): Record<string, unknown> | undefined {
  if (typeof result !== 'string') return asObject(result);
  return parseObject(
    result.endsWith(REPLAYED_RESULT_MARKER)
      ? result.slice(0, -REPLAYED_RESULT_MARKER.length)
      : result,
  );
}

export function generationState(
  toolName: string,
  statusType: string | undefined,
  result: unknown,
): GenerationState {
  if (!MEDIA_TOOLS.has(toolName)) return 'fallback';
  if (statusType === 'running') return 'running';
  if (toolName !== 'video_generate') return 'fallback';
  return ACTIVE_JOB_STATUSES.has(resultObject(result)?.status) ? 'deferred' : 'fallback';
}

/** The job id an in-progress video_generate result carries, empty when it carries none. */
export function deferredJobId(result: unknown): string | undefined {
  const jobID = resultObject(result)?.job_id;
  return typeof jobID === 'string' && jobID !== '' ? jobID : undefined;
}

/**
 * collectedJobIds is every job id a video_generate call names in this thread — that is, every
 * detached job the model has already asked back for.
 *
 * A detached job's card is what the tool answered BEFORE the clip existed ("arriving in this
 * chat"). Once the clip has been collected, the delivered clip further down is the answer, and
 * the card above it is a second, empty copy of the same generation. Measured in the cockpit on
 * 2026-09-17: both were on screen at once.
 *
 * Reads the stored parts defensively: content may be a string, a part may carry its arguments
 * as text (the stream reducer) or as an object (a snapshot), and either may be malformed.
 */
export function collectedJobIds(messages: readonly unknown[]): ReadonlySet<string> {
  const ids = new Set<string>();
  for (const message of messages) {
    const content = asObject(message)?.content;
    if (!Array.isArray(content)) continue;
    for (const part of content) {
      const row = asObject(part);
      if (row?.type !== 'tool-call' || row.toolName !== 'video_generate') continue;
      const args =
        typeof row.argsText === 'string' ? parseObject(row.argsText) : asObject(row.args);
      const jobID = args?.job_id;
      if (typeof jobID === 'string' && jobID !== '') ids.add(jobID);
    }
  }
  return ids;
}

export function generationArgs(argsText: string | undefined): {
  prompt: string;
  aspectRatio: string;
} {
  const args = argsText === undefined ? undefined : parseObject(argsText);
  const prompt = args?.prompt;
  const ratio = args?.aspect_ratio;
  return {
    prompt: typeof prompt === 'string' ? prompt : '',
    aspectRatio: (typeof ratio === 'string' ? ASPECT_RATIOS.get(ratio) : undefined) ?? SQUARE,
  };
}
