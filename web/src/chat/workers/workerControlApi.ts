import { errorDetail } from '../http';

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
): Promise<readonly WorkerReceipt[]> {
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
  return data.receipts.map((value: unknown) => {
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
}
