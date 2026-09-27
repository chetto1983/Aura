import { errorDetail } from '../chat/http';
import {
  elicitationQuestionOf,
  type ElicitationAction,
  type ElicitationQuestion,
} from '../chat/sseAdapter_elicitation';

// elicitationApi is the cockpit's side of a run's MCP forms (internal/agui/
// server_run_elicitation.go): POST /agent/runs/{runID}/elicitations/{id} answers one, and
// GET /agent/runs/{runID}/elicitations lists those still open. The POST requires an
// Idempotency-Key (idempotency_http.go: agent_run_elicitation_answer). The card mints one per
// submit, and nothing retries a submit, so a replay can only be the transport's own.

/** internal/elicit ProblemRequired: the one field problem the card has its own copy for. */
export const PROBLEM_REQUIRED = 'required';

export interface ElicitationAnswerBody {
  readonly action: ElicitationAction;
  readonly content?: Readonly<Record<string, unknown>>;
}

export type ElicitationAnswerResult =
  | { readonly kind: 'delivered' }
  | { readonly kind: 'invalid'; readonly errors: Readonly<Record<string, string>> }
  | { readonly kind: 'closed' }
  | { readonly kind: 'gone' };

function runPath(runId: string): string {
  return `/agent/runs/${encodeURIComponent(runId)}/elicitations`;
}

export async function postElicitationAnswer(
  runId: string,
  id: string,
  body: ElicitationAnswerBody,
  idempotencyKey: string,
): Promise<ElicitationAnswerResult> {
  const res = await fetch(`${runPath(runId)}/${encodeURIComponent(id)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Idempotency-Key': idempotencyKey },
    credentials: 'same-origin',
    body: JSON.stringify(body),
  });
  switch (res.status) {
    case 202:
      return { kind: 'delivered' };
    case 409:
      return { kind: 'closed' };
    case 410:
      return { kind: 'gone' };
    case 422:
      return { kind: 'invalid', errors: await fieldErrors(res) };
    default:
      throw new Error(await errorDetail(res));
  }
}

/**
 * The run's open forms. A reattach whose replay the ring can no longer serve gets a 410 on
 * /events and no frame at all, so the forms come from here. An entry that does not parse is
 * dropped, as the stream drops a frame it cannot trust.
 */
export async function fetchOpenElicitations(runId: string): Promise<ElicitationQuestion[]> {
  const res = await fetch(runPath(runId), { credentials: 'same-origin' });
  if (!res.ok) throw new Error(await errorDetail(res));
  const body: unknown = await res.json();
  const questions = (body as { questions?: unknown } | null)?.questions;
  if (!Array.isArray(questions)) return [];
  return questions.flatMap((value) => {
    const question = elicitationQuestionOf(value);
    return question === null ? [] : [question];
  });
}

async function fieldErrors(res: Response): Promise<Record<string, string>> {
  const body: unknown = await res.json().catch(() => null);
  if (typeof body !== 'object' || body === null) return {};
  const errors: unknown = (body as { errors?: unknown }).errors;
  if (typeof errors !== 'object' || errors === null) return {};
  return Object.fromEntries(
    Object.entries(errors).filter(
      (entry): entry is [string, string] => typeof entry[1] === 'string',
    ),
  );
}
