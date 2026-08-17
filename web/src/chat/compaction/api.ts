import { HttpError, getJSON, postJSON } from '../../api/json';

// api.ts — the conversation-compaction client: the `/compact` command's POST and the read
// the chat lane draws its marker from.
//
// Unlike the composer skills client, this one does NOT degrade a failure to a silent empty
// value. A picker that quietly opens empty is a smaller lie than a command that reports
// nothing after the operator asked for the conversation to be condensed, so the POST throws
// a typed reason the composer can put on screen.

/** The server's compactionDTO. covers_through_seq is a conversation_turns.seq — the same
 * number the message snapshot carries as `backendSeq` — so the marker is positioned without
 * a second lookup. Zero means this conversation has never been compacted. */
export interface CompactionState {
  readonly covers_through_seq: number;
  readonly source_turns: number;
  readonly summary: string;
  /** Populated by the POST only: the stored row does not remember them. */
  readonly tokens_before?: number;
  readonly tokens_after?: number;
}

export const NO_COMPACTION: CompactionState = {
  covers_through_seq: 0,
  source_turns: 0,
  summary: '',
};

/** Why a requested compaction did not happen, as an i18n key under `chat.compaction`. Each
 * case is distinguishable at the wire and means a different thing to the operator: "there is
 * nothing behind this turn yet" and "a summary would be longer than the conversation" are
 * both facts rather than malfunctions, "compaction is switched off" is not something retrying
 * fixes, and everything else is. */
export type CompactionFailure = 'nothing' | 'notWorthwhile' | 'unavailable' | 'failed';

export class CompactionError extends Error {
  readonly reason: CompactionFailure;

  constructor(reason: CompactionFailure) {
    super(reason);
    this.name = 'CompactionError';
    this.reason = reason;
  }
}

/** The statuses that mean "understood, and it did not happen" — see the handler comment in
 * conversations_compaction_api.go for why the refusals are not one status. 502 (the
 * summarizer did not answer) is deliberately absent: it is a malfunction, not a refusal, and
 * falls through to `failed`. */
const REFUSALS: Record<number, CompactionFailure> = {
  409: 'nothing',
  422: 'notWorthwhile',
  503: 'unavailable',
};

function compactionPath(threadId: string, suffix: string): string {
  return `/api/conversations/${encodeURIComponent(threadId)}/${suffix}`;
}

/** GET the stored summary. A failure reads as "no compaction": the marker annotates the
 * transcript, and hiding the whole thread because an annotation is unreachable would be a
 * worse answer than drawing no marker. */
export async function fetchCompaction(
  threadId: string,
  signal?: AbortSignal,
): Promise<CompactionState> {
  try {
    return await getJSON<CompactionState>(compactionPath(threadId, 'compaction'), signal);
  } catch {
    return NO_COMPACTION;
  }
}

/** POST the compaction. Throws CompactionError with the reason the operator needs. */
export async function compactConversation(threadId: string): Promise<CompactionState> {
  try {
    return await postJSON<CompactionState>(compactionPath(threadId, 'compact'), {});
  } catch (error) {
    if (error instanceof HttpError) {
      const reason = REFUSALS[error.status];
      if (reason !== undefined) throw new CompactionError(reason);
    }
    throw new CompactionError('failed');
  }
}
