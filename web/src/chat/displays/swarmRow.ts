// Pure swarm-row helpers for SwarmReportTable — extracted so the status mapping and
// the expand-field presence logic are directly unit/mutation-testable (the
// tableData.ts idiom). The .tsx keeps only rendering.

export type SwarmStatus = 'ok' | 'failed' | 'needs_user_input';

export const SWARM_DOT_CLASS: Record<SwarmStatus, string> = {
  ok: 'bg-success',
  failed: 'bg-danger',
  needs_user_input: 'bg-warning',
};

export const SWARM_STATUS_KEY: Record<SwarmStatus, string> = {
  ok: 'swarm.status.ok',
  failed: 'swarm.status.failed',
  needs_user_input: 'swarm.status.needs_user_input',
};

export function isSwarmStatus(value: string): value is SwarmStatus {
  return value === 'ok' || value === 'failed' || value === 'needs_user_input';
}

/** An out-of-enum status renders as a danger dot with the generic "Unknown" label. */
export function statusDotClass(status: string): string {
  return isSwarmStatus(status) ? SWARM_DOT_CLASS[status] : SWARM_DOT_CLASS.failed;
}

export function statusLabelKey(status: string): string {
  return isSwarmStatus(status) ? SWARM_STATUS_KEY[status] : 'swarm.status.unknown';
}

/** A field is shown in the row-expand only when present AND non-empty. */
export function hasField(value: string | undefined): value is string {
  return value !== undefined && value !== '';
}

/** Options are shown only when present AND non-empty. */
export function hasOptions(options: readonly string[] | undefined): options is readonly string[] {
  return options !== undefined && options.length > 0;
}
