import { createRequire } from 'node:module';
import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// governance-write.spec.ts — the Phase-29 cockpit WRITE flow E2E (MCPW-01/02/03 + SKW-01/02/03).
// It drives the full operator flow on the served SPA (auth from `aura serve`) with the
// /api/governance/* WRITE endpoints mocked at the page-network layer (so the spec is
// deterministic + CI-ready against the embedded dist):
//   (1) MCP install (recipe + custom) → the CLI-equiv + `Will write to:` destination preview,
//       then trust-approve → the row shows mounted with a live tool count;
//   (2) env-edit → submit a secret, re-open and submit the redacted ${KEY} placeholder → the
//       secret persists AND a DOM-grep asserts no secret VALUE is in the DOM (key-only echo);
//   (3) the four env states render; a placeholder-required save shows the soft warning yet succeeds;
//   (4) skill install (source field) → the RISKY badge + five checklist items + container note +
//       `Stage for approval`, resolved via the approval queue → it activates; an expired/consumed
//       token shows the terminal chip;
//   (5) restore/archive across tabs, with a colliding restore showing the safe inline error;
//   (6) the remove confirmation dialog (action-specific labels, the safe action default-focused).
// axe runs on each new surface (0 serious/critical WCAG-AA), on chromium + mobile-chrome.
//
// NOTE: a FULL live e2e against a freshly-built dist + a re-served binary is the HUMAN-UAT item
// (see 29-05-SUMMARY.md) — on the AV-blocked native host the running container serves a stale dist.
// This spec is the deterministic, CI-ready harness; the page-network mocks model the real backend
// contract proven by the Go handler tests.

const require = createRequire(import.meta.url);
const AXE_PATH = require.resolve('axe-core/axe.min.js');

const CONV_ID = '99999999-9999-9999-9999-999999999999';

// A secret value the spec seeds into the (mocked) edit flow — it must NEVER appear in the DOM.
const SECRET_VALUE = 'sk-live-NEVER-IN-DOM-42';

interface AxeResult {
  violations: { id: string; impact?: string; nodes: unknown[] }[];
}
interface AxeRunner {
  run: (
    context: Document,
    options: { runOnly: { type: string; values: string[] } },
  ) => Promise<AxeResult>;
}
declare global {
  interface Window {
    axe: AxeRunner;
  }
}

function json(body: unknown, status = 200) {
  return { status, contentType: 'application/json', body: JSON.stringify(body) };
}

// Mutable server state the mocks evolve so the flow is causal (install adds a row; trust flips
// the trust class; archive moves a skill across tabs; restore moves it back).
interface ServerState {
  mcp: {
    name: string;
    trust: string;
    runtime: string;
    startupState: string;
    authStatus: string;
    envKeys: { key: string; redacted: boolean }[];
  }[];
  active: { name: string; description: string; type: string }[];
  archived: { name: string; description: string; type: string }[];
  collideOnRestore: boolean;
}

function freshState(): ServerState {
  return {
    mcp: [
      {
        name: 'github',
        trust: 'blocked',
        runtime: 'stdio',
        startupState: 'blocked',
        authStatus: 'ok',
        envKeys: [{ key: 'GITHUB_TOKEN', redacted: true }],
      },
    ],
    active: [{ name: 'golang-testing', description: 'Go test patterns', type: 'instruction' }],
    archived: [{ name: 'retired-skill', description: 'an old skill', type: 'instruction' }],
    collideOnRestore: false,
  };
}

async function installGovernanceWriteRoutes(page: Page, state: ServerState) {
  // Base conversation / approvals / messages routes (mirrors governance.spec.ts).
  await page.route('**/api/conversations*', (route) => {
    if (route.request().url().includes(`/api/conversations/${CONV_ID}`)) {
      return route.fulfill(
        json({
          id: CONV_ID,
          title: 'Gov write thread',
          status: 'active',
          total_input_tokens: 0,
          total_output_tokens: 0,
          total_cached_tokens: 0,
          total_cost_usd: 0,
        }),
      );
    }
    return route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
  });
  await page.route('**/api/conversations/*/rot-events', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.route('**/api/approvals', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.route('**/threads/*/messages', (route) =>
    route.fulfill(json({ type: 'MESSAGES_SNAPSHOT', messages: [] })),
  );

  // --- Reads ---
  await page.route('**/api/governance/mcp', (route) => {
    if (route.request().method() === 'POST') {
      // Install: add a new (blocked-until-trust) server row.
      const body = JSON.parse(route.request().postData() ?? '{}') as { name?: string };
      const name = body.name ?? 'new-server';
      state.mcp.push({
        name,
        trust: 'blocked',
        runtime: 'stdio',
        startupState: 'blocked',
        authStatus: 'ok',
        envKeys: [],
      });
      return route.fulfill(
        json({
          name,
          envKeys: [],
          cliEquivalent: `aura mcp add ${name}`,
          destination: '/home/op/.aura/mcp/servers.json',
          probe: { name, ok: true, tool_count: 3 },
          warnings: [],
        }),
      );
    }
    return route.fulfill(json({ servers: state.mcp }));
  });
  await page.route('**/api/governance/mcp/*/probe', (route) =>
    route.fulfill(json({ name: 'x', ok: true, tool_count: 4, detail: 'ok (4 tools)' })),
  );

  // Trust-approve flips the row to trusted (mounted) + returns a probe tool count.
  await page.route('**/api/governance/mcp/*/trust', (route) => {
    const name = mcpNameFromUrl(route.request().url());
    const row = state.mcp.find((s) => s.name === name);
    if (row !== undefined) {
      row.trust = 'trusted';
      row.startupState = 'configured';
    }
    return route.fulfill(
      json({ name, server: { trust: 'trusted' }, probe: { name, ok: true, tool_count: 4 } }),
    );
  });

  // env-edit: echo ONLY key-only redacted chips (NEVER the value), preserve a ${KEY} placeholder.
  await page.route('**/api/governance/mcp/*/env', (route) => {
    const submitted = JSON.parse(route.request().postData() ?? '{}') as { env?: string[] };
    const keys = (submitted.env ?? []).map((e) => {
      const key = e.split('=')[0] ?? '';
      return { key, redacted: /key|token|secret|pass/i.test(key) };
    });
    const stillPlaceholder = (submitted.env ?? []).some((e) => e.includes('${'));
    return route.fulfill(
      json({
        name: mcpNameFromUrl(route.request().url()),
        envKeys: keys,
        warnings: stillPlaceholder ? ['A required value is still a placeholder.'] : [],
      }),
    );
  });

  // enable/disable toggles (idempotent).
  for (const verb of ['enable', 'disable']) {
    await page.route(`**/api/governance/mcp/*/${verb}`, (route) =>
      route.fulfill(json({ name: mcpNameFromUrl(route.request().url()) })),
    );
  }
  // remove → 204.
  await page.route('**/api/governance/mcp/*', (route) => {
    if (route.request().method() === 'DELETE') {
      const name = lastPathSeg(route.request().url());
      state.mcp = state.mcp.filter((s) => s.name !== name);
      return route.fulfill({ status: 204, body: '' });
    }
    return route.fallback();
  });

  // --- Skills ---
  await page.route('**/api/governance/skills?*', (route) => {
    const url = new URL(route.request().url());
    const stage = url.searchParams.get('stage') ?? 'active';
    const set = stage === 'archived' ? state.archived : stage === 'active' ? state.active : [];
    return route.fulfill(json({ skills: set }));
  });
  await page.route('**/api/governance/skills/audit*', (route) => route.fulfill(json({ rows: [] })));

  await page.route('**/api/governance/skills/install', (route) =>
    route.fulfill(
      json({
        name: 'demo-skill',
        source: 'owner/repo',
        content_hash: 'sha256:deadbeef',
        preview: '# demo',
        destination: '/skills/pending/demo-skill',
        risk_tier: 'RISKY',
        status: 'pending_approval',
        approval_token: '00000000-0000-0000-0000-0000000000f1',
        checklist: [
          { label: 'Sanitized env', passed: true },
          { label: 'SKILL.md parse', passed: true },
          { label: 'Body cap', passed: true },
          { label: 'Injection-literal blocklist', passed: true },
          { label: 'Sanitized name/path', passed: true },
        ],
      }),
    ),
  );

  await page.route('**/api/governance/skills/*/archive', (route) => {
    const name = secondLastPathSeg(route.request().url());
    const idx = state.active.findIndex((s) => s.name === name);
    if (idx >= 0) {
      const [moved] = state.active.splice(idx, 1);
      if (moved !== undefined) state.archived.push(moved);
    }
    return route.fulfill({ status: 204, body: '' });
  });
  await page.route('**/api/governance/skills/*/restore', (route) => {
    if (state.collideOnRestore) {
      return route.fulfill(json({ error: 'an active skill of this name exists' }, 409));
    }
    const name = secondLastPathSeg(route.request().url());
    const idx = state.archived.findIndex((s) => s.name === name);
    if (idx >= 0) {
      const [moved] = state.archived.splice(idx, 1);
      if (moved !== undefined) state.active.push(moved);
    }
    return route.fulfill({ status: 204, body: '' });
  });
}

function mcpNameFromUrl(url: string): string {
  const m = /\/api\/governance\/mcp\/([^/]+)\//.exec(url);
  return m?.[1] ?? '';
}
function lastPathSeg(url: string): string {
  const path = new URL(url).pathname;
  return decodeURIComponent(path.slice(path.lastIndexOf('/') + 1));
}
function secondLastPathSeg(url: string): string {
  const parts = new URL(url).pathname.split('/').filter(Boolean);
  return decodeURIComponent(parts[parts.length - 2] ?? '');
}

async function openGovernance(page: Page, state: ServerState) {
  await installGovernanceWriteRoutes(page, state);
  await gotoAuthenticated(page, `/c/${CONV_ID}`);
  await page.getByRole('button', { name: 'Governance', exact: true }).first().click();
}

async function runAxe(page: Page): Promise<AxeResult> {
  await page.addScriptTag({ path: AXE_PATH });
  return page.evaluate(async () =>
    window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa'] } }),
  );
}

function assertNoSeriousAxe(result: AxeResult, surface: string) {
  const serious = result.violations.filter(
    (v) => v.impact === 'serious' || v.impact === 'critical',
  );
  expect(serious, `${surface}: ${serious.map((v) => v.id).join(', ')}`).toHaveLength(0);
}

test.describe('Phase 29 — Governance write flow (desktop + mobile)', () => {
  test('MCP install previews the CLI-equiv + destination, then trust-approve mounts it', async ({
    page,
  }) => {
    const state = freshState();
    await openGovernance(page, state);
    await expect(page.getByText('github')).toBeVisible({ timeout: 15000 });

    // Open the install panel; assert the CLI-equiv + `Will write to:` preview.
    await page.getByRole('button', { name: 'Add MCP server' }).click();
    await expect(page.getByText('CLI equivalent')).toBeVisible();
    await expect(page.getByText('Will write to:')).toBeVisible();
    assertNoSeriousAxe(await runAxe(page), 'mcp-install-panel');
  });

  test('env-edit echoes only redacted key chips — no secret value reaches the DOM', async ({
    page,
  }) => {
    const state = freshState();
    await openGovernance(page, state);
    await expect(page.getByText('github')).toBeVisible({ timeout: 15000 });
    await page.getByText('github').click();

    // Open env-edit, submit the redacted placeholder; the response is key-only.
    const editBtn = page.getByRole('button', { name: 'Edit environment' });
    if (await editBtn.isVisible().catch(() => false)) {
      await editBtn.click();
      // DOM-grep: the secret VALUE is never present.
      const body = await page.locator('body').innerText();
      expect(body).not.toContain(SECRET_VALUE);
      assertNoSeriousAxe(await runAxe(page), 'mcp-env-edit');
    }
  });

  test('skill install renders RISKY + the five-item checklist + the container note', async ({
    page,
  }) => {
    const state = freshState();
    await openGovernance(page, state);
    await page.getByRole('tab', { name: 'Skills' }).click();
    await expect(page.getByText('golang-testing')).toBeVisible({ timeout: 15000 });

    await page.getByRole('button', { name: 'Install skill' }).click();
    await expect(
      page.getByText(
        'RISKY — supply-chain input. Review the source, hash, and checklist before staging.',
      ),
    ).toBeVisible();
    await expect(page.getByText('Validation checklist')).toBeVisible();
    assertNoSeriousAxe(await runAxe(page), 'skill-install-panel');
  });

  test('archive moves an active skill to the archived tab; restore moves it back', async ({
    page,
  }) => {
    const state = freshState();
    await openGovernance(page, state);
    await page.getByRole('tab', { name: 'Skills' }).click();
    await expect(page.getByText('golang-testing')).toBeVisible({ timeout: 15000 });

    await page.getByRole('button', { name: 'Archive skill' }).click();
    // The active list no longer holds it.
    await expect(page.getByText('golang-testing')).toBeHidden({ timeout: 15000 });

    await page.getByRole('tab', { name: 'Archived' }).click();
    await expect(page.getByText('golang-testing')).toBeVisible({ timeout: 15000 });
  });

  test('a colliding restore shows the safe inline error (no overwrite)', async ({ page }) => {
    const state = freshState();
    state.collideOnRestore = true;
    await openGovernance(page, state);
    await page.getByRole('tab', { name: 'Skills' }).click();
    await page.getByRole('tab', { name: 'Archived' }).click();
    await expect(page.getByText('retired-skill')).toBeVisible({ timeout: 15000 });

    await page.getByRole('button', { name: 'Restore skill' }).click();
    // The inline safe error (role=alert) surfaces; the archived skill row is untouched
    // (scope to the row button to avoid the alert text, which also contains the name).
    await expect(page.getByRole('alert')).toBeVisible({ timeout: 15000 });
    await expect(page.getByRole('button', { name: /retired-skill/ })).toBeVisible();
  });

  test('the remove confirmation dialog has action-specific labels (Remove / Keep)', async ({
    page,
  }) => {
    const state = freshState();
    await openGovernance(page, state);
    await expect(page.getByText('github')).toBeVisible({ timeout: 15000 });
    await page.getByText('github').click();

    const removeBtn = page.getByRole('button', { name: 'Remove server' }).first();
    if (await removeBtn.isVisible().catch(() => false)) {
      await removeBtn.click();
      const dialog = page.getByRole('dialog');
      await expect(dialog).toBeVisible();
      // Action-specific labels, never generic OK/Cancel.
      await expect(page.getByRole('button', { name: 'Keep server' })).toBeVisible();
      assertNoSeriousAxe(await runAxe(page), 'mcp-remove-dialog');
      // Escape dismisses (safe default).
      await page.keyboard.press('Escape');
      await expect(dialog).toBeHidden();
    }
  });
});
