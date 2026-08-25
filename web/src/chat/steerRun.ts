import { errorDetail } from './http';

// steerRun — the cockpit's client for POST /agent/runs/{runID}/steer (amendment #132,
// D-01/D-02, contract read from internal/agui/server_run_steer.go as 52-04/52-05 left it,
// never inferred from this plan's own prose). The route is registered with a REQUIRED
// Idempotency-Key header (internal/agui/idempotency_http.go:agent_run_steer): the server is
// counting on the client to hold ONE key per logical send, because a fresh key per retry
// would enqueue the steer twice (T-52-60). installMutationIdempotency (web/src/api/idempotency.ts)
// is deliberately NOT relied on here — it mints a fresh uuid PER FETCH CALL, which is correct
// for a one-shot mutation and wrong for a retried logical send. sseResume.ts's postRun follows
// the identical discipline for /agent/run; steerRun mirrors it for the steer route.

export type SteerRefusalKind = 'invalid' | 'busy' | 'ended';

/** A refused steer, classified by the ratified refusal ladder (52-04/52-05): 400 (empty or
 *  oversize) -> invalid, 429 (queue full) -> busy, 410 (run already ended) -> ended. The
 *  caller renders per KIND, never by string-matching the body. */
export class SteerRefusal extends Error {
  readonly kind: SteerRefusalKind;
  readonly retryAfter: string | undefined;

  constructor(kind: SteerRefusalKind, message: string, retryAfter?: string) {
    super(message);
    this.name = 'SteerRefusal';
    this.kind = kind;
    this.retryAfter = retryAfter;
  }
}

export interface SteerSend {
  /** Performs the POST. Safe to call more than once for the SAME logical send — every call
   *  reuses the ONE Idempotency-Key minted when steerRun was invoked (a transport retry of
   *  the identical steer, never a fresh one). Resolves on 202; throws SteerRefusal on a
   *  classified refusal (400/429/410), or a plain Error carrying errorDetail(res) otherwise. */
  readonly send: () => Promise<void>;
}

/**
 * Build a steer send for `runId` carrying `text`. Mirrors cancelRun's shape in sseResume.ts —
 * a runId-scoped POST with no streaming reply — mints the Idempotency-Key ONCE here (not per
 * fetch), and classifies every response into the ratified refusal ladder.
 */
export function steerRun(runId: string, text: string): SteerSend {
  const idempotencyKey = crypto.randomUUID();
  const send = async (): Promise<void> => {
    const res = await fetch(`/agent/runs/${encodeURIComponent(runId)}/steer`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Idempotency-Key': idempotencyKey },
      credentials: 'same-origin',
      body: JSON.stringify({ text }),
    });
    if (res.status === 202) return;
    if (res.status === 400) throw new SteerRefusal('invalid', await errorDetail(res));
    if (res.status === 429) {
      throw new SteerRefusal(
        'busy',
        await errorDetail(res),
        res.headers.get('Retry-After') ?? undefined,
      );
    }
    if (res.status === 410) throw new SteerRefusal('ended', await errorDetail(res));
    throw new Error(await errorDetail(res));
  };
  return { send };
}
