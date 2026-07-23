import { expect, type Page, type Route } from '@playwright/test';

export const CONV_ID = '88888888-8888-8888-8888-888888888888';
export const EMPTY_CONV_ID = '88888888-8888-8888-8888-888888888889';
export const ERROR_TOKEN = 'error-token';

export type CalmPrismTheme = 'dark' | 'light';

const LONG_QUESTION =
  'Review the deployment evidence for the international customer-reconciliation workflow and explain every unresolved operational risk before continuing.';
const LONG_TOOL =
  'workspace_dependency_inventory_with_an_intentionally_long_but_truthful_operation_name';

function conversationRecord(id: string, title: string) {
  return {
    ID: id,
    Title: title,
    TitleSet: true,
    IdentityID: 'operator',
    Status: 'active',
    Model: 'calm-prism-fixture',
    TotalInputTokens: 1240,
    TotalOutputTokens: 380,
    TotalCachedTokens: 720,
    TotalCostUSD: 0.0184,
    CreatedAt: '2026-07-15T09:00:00Z',
  };
}

function fixtureSnapshot() {
  return {
    type: 'MESSAGES_SNAPSHOT',
    messages: [
      {
        id: 'msg-1',
        role: 'user',
        content:
          'Compare the release evidence, the attached runbook, and the current operational constraints. Keep every recommendation traceable to a concrete result.',
      },
      {
        id: 'msg-2',
        role: 'assistant',
        content:
          'The evidence is organized as activity, typed result, and durable artifact. Long prose remains readable without constraining the richer result surfaces.',
      },
      {
        id: 'msg-3',
        role: 'assistant',
        content: '',
        toolCalls: [
          {
            id: 'running-tool',
            function: {
              name: LONG_TOOL,
              arguments: '{"scope":"all workspaces","include_transitive_dependencies":true}',
            },
          },
          {
            id: 'done-tool',
            function: {
              name: 'release_evidence_check',
              arguments: '{"release":"2026.07"}',
            },
          },
          {
            id: 'error-tool',
            function: { name: 'policy_boundary_probe', arguments: '{"target":"restricted"}' },
            display: {
              type: 'system_event',
              tool_call_id: 'error-tool',
              system: {
                class: 'network',
                reason: 'blocked_url',
                severity: 'error',
              },
            },
          },
          {
            id: 'artifact-tool',
            function: { name: 'send_file', arguments: '{"format":"xlsx"}' },
            display: {
              type: 'local_artifact',
              tool_call_id: 'artifact-tool',
              artifact: {
                filename:
                  'calm-prism-release-readiness-evidence-and-operational-risk-register.xlsx',
                size_bytes: 48291,
                asset_id: 'artifact-1',
                mime_type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
              },
            },
          },
        ],
      },
      { role: 'tool', toolCallId: 'done-tool', content: 'All required checks passed.' },
      { role: 'tool', toolCallId: 'error-tool', content: 'The target was blocked by policy.' },
      { role: 'tool', toolCallId: 'artifact-tool', content: 'Artifact delivered.' },
    ],
  };
}

function approvals() {
  return [
    {
      token: ERROR_TOKEN,
      conversation_id: CONV_ID,
      kind: 'input',
      question: LONG_QUESTION,
      priority: 0,
    },
    {
      token: 'generic-options-token',
      conversation_id: CONV_ID,
      kind: 'approval',
      question: 'Choose the rollout boundary only after reviewing the scope and consequence above.',
      options: ['Pilot workspace', 'All workspaces'],
      priority: 1,
    },
    {
      token: 'terminal-token',
      conversation_id: CONV_ID,
      kind: 'input',
      question: 'The previous operator request reached its terminal window.',
      priority: 2,
      terminal: true,
    },
  ];
}

function fixtureAssets() {
  return [
    {
      id: 'upload-1',
      source_kind: 'web',
      thread_id: CONV_ID,
      scope: 'thread',
      status: 'complete',
      modality: 'document',
      file_name: 'international-customer-reconciliation-operational-readiness-runbook-final.docx',
      mime_type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
      declared_size_bytes: 98304,
      size_bytes: 98304,
      summary: 'Indexed and ready for grounded analysis.',
    },
  ];
}

function sseBody(frames: readonly Record<string, unknown>[]) {
  return frames
    .map((frame) => `event: ${String(frame.type)}\ndata: ${JSON.stringify(frame)}\n\n`)
    .join('');
}

function reasoningTurn() {
  const timestamp = Date.parse('2026-07-15T09:01:00Z');
  return sseBody([
    { type: 'RUN_STARTED', threadId: CONV_ID, runId: 'visual-run' },
    { type: 'REASONING_START', messageId: 'reasoning-1', timestamp },
    { type: 'REASONING_MESSAGE_START', messageId: 'reasoning-1', timestamp },
    {
      type: 'REASONING_MESSAGE_CONTENT',
      messageId: 'reasoning-1',
      delta: 'Checking release evidence against the requested operating boundary.',
      timestamp,
    },
    { type: 'REASONING_MESSAGE_END', messageId: 'reasoning-1', timestamp },
    { type: 'REASONING_END', messageId: 'reasoning-1', timestamp },
    { type: 'TEXT_MESSAGE_START', messageId: 'answer-1', timestamp },
    {
      type: 'TEXT_MESSAGE_CONTENT',
      messageId: 'answer-1',
      delta: 'The review is ready for the operator decisions below.',
      timestamp,
    },
    { type: 'TEXT_MESSAGE_END', messageId: 'answer-1', timestamp },
    {
      type: 'STATE_DELTA',
      delta: [
        { op: 'replace', path: '/prompt_tokens', value: 320 },
        { op: 'replace', path: '/completion_tokens', value: 96 },
        { op: 'replace', path: '/cache_hit_tokens', value: 180 },
        { op: 'replace', path: '/cost_usd', value: 0.0062 },
      ],
    },
    { type: 'RUN_FINISHED', outcome: { type: 'success' } },
  ]);
}

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body),
  });
}

async function installShellRoutes(page: Page) {
  await page.route('**/api/onboarding/status', (route) =>
    json(route, { required: false, completed: true, skipped: false }),
  );
  await page.route('**/healthz', (route) => json(route, { ok: true }));
  await page.route('**/readyz', (route) =>
    json(route, { ready: true, deps: { postgres: 'ok', neo4j: 'ok' } }),
  );
  await page.route('**/api/me', (route) =>
    json(route, { identity_id: 'operator', capabilities: [] }),
  );
  await page.route('**/api/voice/capabilities', (route) => json(route, { tts: true, stt: true }));
  await page.route('**/api/composer/skills', (route) => json(route, { skills: [] }));
  await page.route('**/api/composer/reasoning-capabilities', (route) =>
    json(route, { levels: ['auto', 'off'], default: 'auto', detected: false }),
  );
}

export async function installCalmPrismFixture(
  page: Page,
  theme: CalmPrismTheme,
  empty = false,
  includeApprovals = true,
) {
  const id = empty ? EMPTY_CONV_ID : CONV_ID;
  let matrixReady = false;
  await page.clock.setFixedTime(new Date('2026-07-16T12:00:00Z'));
  await page.addInitScript(
    ({ selectedTheme }) => {
      const refreshGlobals = globalThis as typeof globalThis & {
        $RefreshReg$?: () => void;
        $RefreshSig$?: () => (type: unknown) => unknown;
      };
      refreshGlobals.$RefreshReg$ = () => undefined;
      refreshGlobals.$RefreshSig$ = () => (type) => type;
      localStorage.setItem('aura.language', 'en');
      localStorage.setItem('aura.theme', selectedTheme);
      localStorage.setItem('aura.density', 'operator');
    },
    { selectedTheme: theme },
  );
  await page.emulateMedia({ reducedMotion: 'reduce', colorScheme: theme });
  await installShellRoutes(page);
  await page.route('**/api/conversations*', (route) => {
    const url = new URL(route.request().url());
    const record = conversationRecord(id, empty ? 'Starter fixture' : 'Calm Prism review');
    return json(route, url.pathname.endsWith(`/${id}`) ? record : [record]);
  });
  await page.route(`**/api/conversations/${id}`, (route) =>
    json(route, conversationRecord(id, empty ? 'Starter fixture' : 'Calm Prism review')),
  );
  await page.route('**/threads/*/messages', (route) =>
    json(route, empty ? { type: 'MESSAGES_SNAPSHOT', messages: [] } : fixtureSnapshot()),
  );
  await page.route('**/api/conversations/*/rot-events', (route) => json(route, []));
  await page.route(/\/api\/assets\?thread_id=/, (route) =>
    json(route, empty ? [] : fixtureAssets()),
  );
  await page.route('**/api/approvals', (route) =>
    json(route, !empty && includeApprovals && matrixReady ? approvals() : []),
  );
  await page.route('**/api/approvals/*/resolve', (route) => route.fulfill({ status: 204 }));
  await page.route(`**/api/approvals/${ERROR_TOKEN}/resolve`, (route) =>
    json(route, { error: 'already resolved' }, 409),
  );
  await page.route('**/agent/run', (route) => {
    matrixReady = true;
    return route.fulfill({
      status: 200,
      headers: { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' },
      body: reasoningTurn(),
    });
  });
}

export async function openCalmPrism(page: Page, empty = false, verifyContract = true) {
  await page.goto('/', { waitUntil: 'domcontentloaded' });
  await page.evaluate(
    (path) => {
      history.pushState({}, '', path);
      dispatchEvent(new PopStateEvent('popstate'));
    },
    `/c/${empty ? EMPTY_CONV_ID : CONV_ID}`,
  );
  await page.evaluate(() => document.fonts.ready);
  if (verifyContract) await expect(page.locator('[data-chat-workspace-controls]')).toBeVisible();
}

export async function revealCalmPrismMatrix(page: Page, includeApprovals = true) {
  await expect(page.getByText('release_evidence_check')).toBeVisible({ timeout: 15_000 });
  const composer = page.getByPlaceholder('Ask Aura');
  await composer.fill('Render the deterministic Calm Prism review state.');
  await composer.press('Enter');
  await expect(page.getByText('The review is ready for the operator decisions below.')).toBeVisible(
    {
      timeout: 15_000,
    },
  );
  if (includeApprovals) {
    await expect(page.getByText(LONG_QUESTION)).toBeVisible({ timeout: 7_000 });
  }
}
