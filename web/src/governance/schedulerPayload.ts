/**
 * payloadText unwraps the shape the scheduler stores: the sentence the operator dictated.
 *
 * It lives in its own module because BOTH the board row and the edit dialog need it — the row
 * to say what will fire, the dialog to open on those words instead of an empty box that
 * silently keeps them. Reported 2026-09-07: the API stripped the payload as "private prompt
 * material", so a reminder was unreadable until it arrived on Telegram and changing it meant
 * retyping it blind.
 *
 * It reads {text} for a reminder and {goal} for an agent_job, and answers '' for anything else:
 * a field prefilled with a JSON blob, or a row showing one, is worse than nothing.
 */
export function payloadText(payload: unknown): string {
  if (payload === null || typeof payload !== 'object') return '';
  const record = payload as Record<string, unknown>;
  for (const key of ['text', 'goal']) {
    const value = record[key];
    if (typeof value === 'string') return value;
  }
  return '';
}
