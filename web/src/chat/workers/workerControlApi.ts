import { errorDetail } from '../http';
import type { QueuedWorkerTarget } from '../runControlTarget';

export interface WorkerControlState {
  readonly receipts: readonly WorkerReceipt[];
  readonly queued_target?: QueuedWorkerTarget;
  readonly cancel_requested?: boolean;
}

export interface WorkerReceipt {
  readonly id: string;
  readonly run_id: string;
  readonly text: string;
  readonly status: 'accepted' | 'applied' | 'rejected';
  readonly reason?: string;
  readonly created_at: string;
  readonly applied_at?: string;
}

export const workerControlKey = (conversationId: string, childId: string) =>
  ['worker-controls', conversationId, childId] as const;

export async function workerControlHistory(
  conversationId: string,
  childId: string,
  signal: AbortSignal,
): Promise<WorkerControlState> {
  const res = await fetch(
    `/api/conversations/${encodeURIComponent(conversationId)}/swarm/${encodeURIComponent(childId)}/controls`,
    {
      credentials: 'same-origin',
      signal,
    },
  );
  if (!res.ok) throw new Error(await errorDetail(res));
  const data: unknown = await res.json();
  if (
    typeof data !== 'object' ||
    data === null ||
    !('receipts' in data) ||
    !Array.isArray(data.receipts)
  ) {
    throw new Error('Invalid worker receipt response');
  }
  const receipts = data.receipts.map((value: unknown) => {
    if (
      typeof value !== 'object' ||
      value === null ||
      !('id' in value) ||
      typeof value.id !== 'string' ||
      !('run_id' in value) ||
      typeof value.run_id !== 'string' ||
      !('text' in value) ||
      typeof value.text !== 'string' ||
      !('created_at' in value) ||
      typeof value.created_at !== 'string' ||
      !('status' in value) ||
      typeof value.status !== 'string' ||
      !['accepted', 'applied', 'rejected'].includes(value.status)
    ) {
      throw new Error('Invalid worker receipt');
    }
    return value as WorkerReceipt;
  });
  const queued = 'queued_target' in data ? data.queued_target : undefined;
  if (
    queued !== undefined &&
    (typeof queued !== 'object' ||
      queued === null ||
      !('job_id' in queued) ||
      typeof queued.job_id !== 'string' ||
      queued.job_id === '' ||
      !('attempt_count' in queued) ||
      typeof queued.attempt_count !== 'number' ||
      !Number.isInteger(queued.attempt_count) ||
      queued.attempt_count < 0 ||
      queued.attempt_count > 2147483647)
  )
    throw new Error('Invalid worker queued target');
  const cancelling = 'cancel_requested' in data ? data.cancel_requested : undefined;
  if (cancelling !== undefined && typeof cancelling !== 'boolean')
    throw new Error('Invalid worker cancellation state');
  return {
    receipts,
    ...(queued === undefined ? {} : { queued_target: queued as QueuedWorkerTarget }),
    ...(cancelling === undefined ? {} : { cancel_requested: cancelling }),
  };
}
