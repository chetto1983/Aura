import { createHmac } from 'node:crypto';
import { existsSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { expect, type Page } from '@playwright/test';

function unquoteEnvValue(value: string): string {
  const trimmed = value.trim();
  if (trimmed.length >= 2) {
    const first = trimmed[0];
    const last = trimmed[trimmed.length - 1];
    if ((first === '"' && last === '"') || (first === "'" && last === "'")) {
      return trimmed.slice(1, -1);
    }
  }
  return trimmed;
}

function envFileValue(name: string): string | undefined {
  const envPath = resolve(process.cwd(), '..', '.env');
  if (!existsSync(envPath)) return undefined;
  const prefix = `${name}=`;
  const line = readFileSync(envPath, 'utf8')
    .split(/\r?\n/)
    .find((entry) => entry.trimStart().startsWith(prefix));
  if (line === undefined) return undefined;
  return unquoteEnvValue(line.slice(line.indexOf('=') + 1));
}

function envValue(...names: string[]): string | undefined {
  for (const name of names) {
    const fromFile = envFileValue(name);
    if (fromFile !== undefined && fromFile.trim() !== '') return fromFile;
    const fromProcess = process.env[name];
    if (fromProcess !== undefined && fromProcess.trim() !== '') return fromProcess;
  }
  return undefined;
}

interface AuthConfig {
  authBasePath: string;
  csrfHeaderName: string;
  csrfToken: string;
}

async function authConfig(page: Page): Promise<AuthConfig> {
  const res = await page.request.get('/api/auth/config', { failOnStatusCode: false });
  if (!res.ok()) {
    throw new Error(
      `E2E Authula auth failed: /api/auth/config returned HTTP ${String(res.status())}`,
    );
  }
  const raw = (await res.json().catch(() => ({}))) as Record<string, unknown>;
  if (raw.provider !== 'authula') {
    throw new Error(
      `E2E Authula auth required, but /api/auth/config returned provider ${String(raw.provider)}`,
    );
  }
  const csrfHeaderName =
    typeof raw.csrf_header_name === 'string' ? raw.csrf_header_name : 'X-AUTHULA-CSRF-TOKEN';
  const headers = res.headers();
  const bodyToken =
    typeof raw.csrf_token === 'string' && raw.csrf_token !== '' ? raw.csrf_token : undefined;
  return {
    authBasePath: typeof raw.auth_base_path === 'string' ? raw.auth_base_path : '/auth',
    csrfHeaderName,
    csrfToken: bodyToken ?? headers[csrfHeaderName.toLowerCase()] ?? headers[csrfHeaderName] ?? '',
  };
}

function decodeBase32(input: string): Buffer {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  const normalized = input.toUpperCase().replace(/[^A-Z2-7]/g, '');
  let bits = 0;
  let value = 0;
  const bytes: number[] = [];
  for (const char of normalized) {
    const index = alphabet.indexOf(char);
    if (index < 0) {
      throw new Error('invalid base32 TOTP secret');
    }
    value = (value << 5) | index;
    bits += 5;
    if (bits >= 8) {
      bytes.push((value >>> (bits - 8)) & 0xff);
      bits -= 8;
    }
  }
  return Buffer.from(bytes);
}

function totpCode(secret: string, now = Date.now()): string {
  const key = decodeBase32(secret);
  const counter = Math.floor(now / 1000 / 30);
  const msg = Buffer.alloc(8);
  msg.writeBigUInt64BE(BigInt(counter), 0);
  const digest = createHmac('sha1', key).update(msg).digest();
  const offset = (digest.at(-1) ?? 0) & 0x0f;
  const binary =
    (((digest[offset] ?? 0) & 0x7f) << 24) |
    (((digest[offset + 1] ?? 0) & 0xff) << 16) |
    (((digest[offset + 2] ?? 0) & 0xff) << 8) |
    ((digest[offset + 3] ?? 0) & 0xff);
  return String(binary % 1_000_000).padStart(6, '0');
}

function authulaTotpCode(): string | undefined {
  const explicit = envValue('AURA_E2E_AUTHULA_TOTP_CODE');
  if (explicit !== undefined) return explicit;
  const secret = envValue('AURA_E2E_AUTHULA_TOTP_SECRET', 'AURA_AUTHULA_OPERATOR_TOTP_SECRET');
  return secret === undefined ? undefined : totpCode(secret);
}

async function authenticateViaAuthula(page: Page, config: AuthConfig) {
  const email = envValue('AURA_E2E_AUTHULA_EMAIL', 'AURA_AUTHULA_OPERATOR_EMAIL');
  const password = envValue('AURA_E2E_AUTHULA_PASSWORD', 'AURA_AUTHULA_OPERATOR_PASSWORD');
  const code = authulaTotpCode();
  if (email === undefined || password === undefined) {
    throw new Error(
      'E2E Authula auth required, but AURA_E2E_AUTHULA_EMAIL/PASSWORD are missing from env/.env',
    );
  }
  if (config.csrfToken === '') {
    throw new Error('E2E Authula auth failed: /api/auth/config did not return a CSRF token');
  }

  const headers = { [config.csrfHeaderName]: config.csrfToken };
  const signIn = await page.request.post(`${config.authBasePath}/email-password/sign-in`, {
    data: { email, password },
    headers,
    failOnStatusCode: false,
  });
  if (!signIn.ok()) {
    throw new Error(`E2E Authula auth failed: sign-in returned HTTP ${String(signIn.status())}`);
  }
  const payload = (await signIn.json().catch(() => ({}))) as { totp_redirect?: unknown };
  if (payload.totp_redirect !== true) return;
  if (code === undefined) {
    throw new Error(
      'E2E Authula TOTP required, but AURA_E2E_AUTHULA_TOTP_CODE or TOTP_SECRET is missing',
    );
  }
  const verify = await page.request.post(`${config.authBasePath}/totp/verify`, {
    data: { code, trust_device: false },
    headers,
    failOnStatusCode: false,
  });
  if (!verify.ok()) {
    throw new Error(
      `E2E Authula auth failed: TOTP verify returned HTTP ${String(verify.status())}`,
    );
  }
}

async function authenticateViaApi(page: Page) {
  const config = await authConfig(page);
  await authenticateViaAuthula(page, config);
}

function isLoginUrl(url: string): boolean {
  return new URL(url).pathname === '/login';
}

export async function gotoAuthenticated(page: Page, path: string) {
  await page.addInitScript(() => {
    window.localStorage.setItem('aura.language', 'en');
  });

  await page.goto(path, { waitUntil: 'domcontentloaded' });
  if (isLoginUrl(page.url())) {
    await authenticateViaApi(page);
    await page.goto(path, { waitUntil: 'domcontentloaded' });
  }

  await expect(page).not.toHaveURL(/\/login(?:[?#]|$)/, { timeout: 10_000 });
}
