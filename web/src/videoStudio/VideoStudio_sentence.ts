import { reasonOf } from './analysisState';
import { CommandRefusal } from './commands';

// VideoStudio_sentence.ts — what the workspace tells the operator in its status line.

/** A string the operator will read, kept unresolved so the language can still change under it. */
export interface Sentence {
  readonly key: string;
  readonly values: Record<string, unknown>;
}

export function says(key: string, values: Record<string, unknown> = {}): Sentence {
  return { key, values };
}

/**
 * The sentence an error becomes. A refusal speaks for itself and is shown unchanged, whichever
 * of the seven it is; anything else — a caller out of step with the model, a dropped connection
 * — is worded by whoever was attempting it and carries the message it came with. Neither is
 * silenced, and neither is dressed as the other.
 */
export function failure(error: unknown, fallbackKey: string): Sentence {
  if (error instanceof CommandRefusal) return says(error.reasonKey);
  return says(fallbackKey, { reason: reasonOf(error) });
}
