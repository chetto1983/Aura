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

function envFileWebAuthSecret(): string | undefined {
  const envPath = resolve(process.cwd(), '..', '.env');
  if (!existsSync(envPath)) return undefined;
  const line = readFileSync(envPath, 'utf8')
    .split(/\r?\n/)
    .find((entry) => entry.trimStart().startsWith('AURA_WEB_AUTH_SECRET='));
  if (line === undefined) return undefined;
  return unquoteEnvValue(line.slice(line.indexOf('=') + 1));
}

function webAuthSecret(): string | undefined {
  const fromFile = envFileWebAuthSecret();
  if (fromFile !== undefined && fromFile.trim() !== '') return fromFile;
  const fromProcess = process.env.AURA_WEB_AUTH_SECRET;
  if (fromProcess !== undefined && fromProcess.trim() !== '') return fromProcess;
  return undefined;
}

async function authenticateViaApi(page: Page) {
  const passphrase = webAuthSecret();
  if (passphrase === undefined) {
    throw new Error('E2E auth required, but AURA_WEB_AUTH_SECRET is missing from env/.env');
  }

  const res = await page.request.post('/login', {
    form: { passphrase },
    maxRedirects: 0,
  });
  if (res.status() !== 303) {
    throw new Error(`E2E auth failed: POST /login returned HTTP ${String(res.status())}`);
  }
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
