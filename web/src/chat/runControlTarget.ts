export interface WorkerControlTarget {
  readonly conversationId: string;
  readonly childId: string;
}

export function runControlURL(
  runId: string,
  action: 'steer' | 'cancel',
  target?: WorkerControlTarget,
): string {
  if (target === undefined) return `/agent/runs/${encodeURIComponent(runId)}/${action}`;
  return `/api/conversations/${encodeURIComponent(target.conversationId)}/swarm/${encodeURIComponent(target.childId)}/${action}`;
}
