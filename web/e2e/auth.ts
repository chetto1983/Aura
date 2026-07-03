import { createHash, createHmac } from 'node:crypto';
import {
  closeSync,
  existsSync,
  mkdirSync,
  openSync,
  readFileSync,
  rmSync,
  statSync,
  unlinkSync,
  writeFileSync,
} from 'node:fs';
import { dirname, resolve } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { expect, type Page } from '@playwright/test';

type BrowserContext = ReturnType<Page['context']>;
type BrowserStorageState = Awaited<ReturnType<BrowserContext['storageState']>>;

const authStateVersion = 1;
const authLockStaleMs = 120_000;
const authLockPollMs = 250;

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

function authStatePath(): string {
  const origin =
    process.env.AURA_E2E_HTTPS_ORIGIN ??
    process.env.AURA_E2E_ORIGIN ??
    envFileValue('AURA_E2E_ORIGIN') ??
    'http://127.0.0.1:9080';
  const digest = createHash('sha256').update(origin).digest('hex').slice(0, 16);
  return resolve(process.cwd(), 'test-results', '.auth', `authula-${digest}.json`);
}

function readAuthState(): BrowserStorageState | undefined {
  const path = authStatePath();
  if (!existsSync(path)) return undefined;
  try {
    const raw = JSON.parse(readFileSync(path, 'utf8')) as {
      version?: unknown;
      state?: unknown;
    };
    if (raw.version !== authStateVersion || typeof raw.state !== 'object' || raw.state === null) {
      return undefined;
    }
    const state = raw.state as BrowserStorageState;
    const now = Date.now() / 1000;
    const hasLiveSession = state.cookies.some(
      (cookie) =>
        /session/i.test(cookie.name) && (cookie.expires === -1 || cookie.expires > now + 30),
    );
    return hasLiveSession ? state : undefined;
  } catch {
    return undefined;
  }
}

function writeAuthState(state: BrowserStorageState) {
  const path = authStatePath();
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, JSON.stringify({ version: authStateVersion, state }), { encoding: 'utf8' });
}

function clearAuthState() {
  rmSync(authStatePath(), { force: true });
}

function asError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error));
}

async function withAuthStateLock<T>(fn: () => Promise<T>): Promise<T> {
  const path = authStatePath();
  mkdirSync(dirname(path), { recursive: true });
  const lockPath = `${path}.lock`;
  let fd: number | undefined;
  while (fd === undefined) {
    try {
      fd = openSync(lockPath, 'wx');
    } catch (error) {
      const code = (error as NodeJS.ErrnoException).code;
      if (code !== 'EEXIST') throw error;
      try {
        const ageMs = Date.now() - statSync(lockPath).mtimeMs;
        if (ageMs > authLockStaleMs) {
          rmSync(lockPath, { force: true });
          continue;
        }
      } catch {
        continue;
      }
      await delay(authLockPollMs);
    }
  }

  let result: T | undefined;
  let fnError: unknown;
  try {
    result = await fn();
  } catch (error) {
    fnError = error;
  }

  let cleanupError: unknown;
  try {
    closeSync(fd);
    try {
      unlinkSync(lockPath);
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== 'ENOENT') {
        cleanupError = error;
      }
    }
  } catch (error) {
    cleanupError = error;
  }

  if (fnError !== undefined) throw asError(fnError);
  if (cleanupError !== undefined) throw asError(cleanupError);
  return result as T;
}

async function applyAuthState(page: Page): Promise<boolean> {
  const state = readAuthState();
  if (state === undefined) return false;
  if (state.cookies.length > 0) {
    await page.context().addCookies(state.cookies);
  }
  if (state.origins.length > 0) {
    await page.addInitScript((origins) => {
      for (const originState of origins) {
        if (originState.origin !== window.location.origin) continue;
        for (const item of originState.localStorage) {
          window.localStorage.setItem(item.name, item.value);
        }
      }
    }, state.origins);
  }
  return true;
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

async function ensureAuthState(page: Page) {
  if (await applyAuthState(page)) return;
  await withAuthStateLock(async () => {
    if (await applyAuthState(page)) return;
    await authenticateViaApi(page);
    writeAuthState(await page.context().storageState());
  });
}

function isLoginUrl(url: string): boolean {
  return new URL(url).pathname === '/login';
}

async function installCompletedProfileFixture(page: Page) {
  await page.route('**/api/onboarding/status', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ required: false, completed: true, skipped: false }),
    }),
  );
}

// installRuntimeHealthFixture pins the shell's runtime-health poll to a healthy 200. The
// ShellHeader RuntimeStatusChip (useRuntimeHealth, D-07) polls /healthz + /readyz every 5s
// from EVERY authenticated page. The E2E stack boots `aura serve` over Postgres + Garage
// with NO Neo4j, so the LIVE /readyz Neo4j probe returns 503 — a genuine "degraded in this
// env" signal, not a chat-flow bug — which otherwise leaks a "Failed to load resource: 503"
// console error and trips chat.spec.ts's consoleProblems assertion. Mocking here (a page
// route, not the beforeAll `request` liveness probe) keeps the shell hermetic; a spec that
// wants to exercise the degraded chip can re-route /readyz after gotoAuthenticated (later
// page.route wins).
async function installRuntimeHealthFixture(page: Page) {
  await page.route('**/healthz', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ ok: true }),
    }),
  );
  await page.route('**/readyz', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ ready: true, deps: { postgres: 'ok', neo4j: 'ok' } }),
    }),
  );
}

export async function gotoAuthenticated(page: Page, path: string) {
  await page.addInitScript(() => {
    window.localStorage.setItem('aura.language', 'en');
  });
  await installCompletedProfileFixture(page);
  await installRuntimeHealthFixture(page);

  await applyAuthState(page);
  const firstResponse = await page.goto(path, { waitUntil: 'domcontentloaded' });
  if (isLoginUrl(page.url()) || firstResponse?.status() === 401) {
    clearAuthState();
    await ensureAuthState(page);
    await page.goto(path, { waitUntil: 'domcontentloaded' });
  }

  await expect(page).not.toHaveURL(/\/login(?:[?#]|$)/, { timeout: 10_000 });
}
