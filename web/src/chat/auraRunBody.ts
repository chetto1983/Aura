import type { StreamRunOptions } from './sseAdapter';

// buildAuraRunBody assembles the POST /agent/run body from a streamRun request. It folds the
// optional attachment ids AND the optional pinned skill (37D) into ONE `aura` object — the
// server decodes both off `req.Aura` — and emits NO `aura` key when neither is set, so a plain
// turn is byte-identical to the pre-37D body. Extracted out of sseAdapter.ts (which sits at the
// 600-LOC cap) so the `skill` fold lands here without pushing that file over.
export function buildAuraRunBody(id: string, opts: StreamRunOptions): Record<string, unknown> {
  const aura = {
    ...(opts.attachmentIds !== undefined && opts.attachmentIds.length > 0
      ? { attachment_ids: opts.attachmentIds }
      : {}),
    ...(opts.skill !== undefined && opts.skill.length > 0 ? { skill: opts.skill } : {}),
  };
  return {
    threadId: opts.threadId,
    messages: [{ id, role: 'user', content: opts.userText }],
    ...(Object.keys(aura).length > 0 ? { aura } : {}),
  };
}
