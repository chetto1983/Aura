import type { StreamRunOptions } from './sseAdapter';

// buildAuraRunBody assembles the POST /agent/run body from a streamRun request. It folds the
// optional attachment ids, the optional pinned skill (37D), AND the optional reasoning-effort
// symbol (37E) into ONE `aura` object — the server decodes all three off `req.Aura` — and emits NO
// `aura` key when none is set, so a plain turn is byte-identical to the pre-37D body. The effort is
// folded ONLY for the six fixed levels and OMITTED for 'auto' (the adaptive default is the absence
// of the field, WEBMODEL-03). Extracted out of sseAdapter.ts (which sits at the 600-LOC cap).
export function buildAuraRunBody(id: string, opts: StreamRunOptions): Record<string, unknown> {
  const aura = {
    ...(opts.attachmentIds !== undefined && opts.attachmentIds.length > 0
      ? { attachment_ids: opts.attachmentIds }
      : {}),
    ...(opts.skill !== undefined && opts.skill.length > 0 ? { skill: opts.skill } : {}),
    ...(opts.effort !== undefined && opts.effort !== 'auto' && opts.effort.length > 0
      ? { effort: opts.effort }
      : {}),
  };
  return {
    threadId: opts.threadId,
    messages: [{ id, role: 'user', content: opts.userText }],
    ...(Object.keys(aura).length > 0 ? { aura } : {}),
  };
}
