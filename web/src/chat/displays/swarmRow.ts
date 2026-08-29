// Pure swarm-row helpers for SwarmReportTable — extracted so the status mapping and
// the expand-field presence logic are directly unit/mutation-testable (the
// tableData.ts idiom). The .tsx keeps only rendering.

export type SwarmStatus =
  | 'ok'
  | 'failed'
  | 'needs_user_input'
  | 'running'
  | 'stalled'
  | 'dead_letter';

export const SWARM_DOT_CLASS: Record<SwarmStatus, string> = {
  ok: 'bg-success',
  failed: 'bg-danger',
  needs_user_input: 'bg-warning',
  running: 'bg-info',
  stalled: 'bg-warning',
  dead_letter: 'bg-danger',
};

export const SWARM_STATUS_KEY: Record<SwarmStatus, string> = {
  ok: 'swarm.status.ok',
  failed: 'swarm.status.failed',
  needs_user_input: 'swarm.status.needs_user_input',
  running: 'swarm.status.running',
  stalled: 'swarm.status.stalled',
  dead_letter: 'swarm.status.dead_letter',
};

export function isSwarmStatus(value: string): value is SwarmStatus {
  return (
    value === 'ok' ||
    value === 'failed' ||
    value === 'needs_user_input' ||
    value === 'running' ||
    value === 'stalled' ||
    value === 'dead_letter'
  );
}

/** An out-of-enum status renders as a danger dot with the generic "Unknown" label. */
export function statusDotClass(status: string): string {
  return isSwarmStatus(status) ? SWARM_DOT_CLASS[status] : SWARM_DOT_CLASS.failed;
}

export function statusLabelKey(status: string): string {
  return isSwarmStatus(status) ? SWARM_STATUS_KEY[status] : 'swarm.status.unknown';
}

export type SwarmStatusIconName =
  | 'CircleCheck'
  | 'CircleX'
  | 'MessageCircleQuestion'
  | 'LoaderCircle'
  | 'Clock'
  | 'MailX'
  | 'TriangleAlert';

export function statusIconName(status: string): SwarmStatusIconName {
  switch (status) {
    case 'ok':
      return 'CircleCheck';
    case 'failed':
      return 'CircleX';
    case 'needs_user_input':
      return 'MessageCircleQuestion';
    case 'running':
      return 'LoaderCircle';
    case 'stalled':
      return 'Clock';
    case 'dead_letter':
      return 'MailX';
    default:
      return 'TriangleAlert';
  }
}

export function isTerminalSwarmStatus(status: string): boolean {
  return status === 'ok' || status === 'failed' || status === 'dead_letter';
}

/** A field is shown in the row-expand only when present AND non-empty. */
export function hasField(value: string | undefined): value is string {
  return value !== undefined && value !== '';
}

/** Options are shown only when present AND non-empty. */
export function hasOptions(options: readonly string[] | undefined): options is readonly string[] {
  return options !== undefined && options.length > 0;
}
