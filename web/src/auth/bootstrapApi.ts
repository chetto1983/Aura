import { readJSON } from './authConfig';

export interface BootstrapCreateRequest {
  readonly email: string;
  readonly password: string;
  readonly securityQuestion: string;
  readonly securityAnswer: string;
}

export interface BootstrapCreateResponse {
  readonly identityId: string;
}

export async function createFirstOperator(
  req: BootstrapCreateRequest,
): Promise<BootstrapCreateResponse> {
  const res = await fetch('/api/auth/bootstrap/operator', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
    credentials: 'same-origin',
  });
  if (!res.ok) {
    throw new Error(`bootstrap failed: ${String(res.status)}`);
  }
  const raw = await readJSON(res);
  if (!isBootstrapResponse(raw)) {
    throw new Error('bootstrap response invalid');
  }
  return raw;
}

function isBootstrapResponse(value: unknown): value is BootstrapCreateResponse {
  return (
    typeof value === 'object' &&
    value !== null &&
    typeof (value as { identityId?: unknown }).identityId === 'string'
  );
}
