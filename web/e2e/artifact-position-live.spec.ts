import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';
import { sameOriginFetch } from './live';

// artifact-position-live.spec.ts drives the REAL cockpit against a live stack and a real agent,
// no mocked route: a greeting turn, then a turn whose send_file delivers a file, then a reload.
//
// The defect it pins (2026-09-11, "Ciao, presentazione dell'assistente"): on reload the file
// rendered under the greeting, because nothing recorded which call had delivered it. The asset
// now names its call (migration 0126) and the snapshot puts the card back on that call.
//
// Gated because it needs a running stack and a model that calls tools:
//   AURA_E2E_LIVE_ARTIFACT_POSITION=1

const live = process.env.AURA_E2E_LIVE_ARTIFACT_POSITION === '1';
const fileName = 'artifact-position.txt';

interface SnapshotToolCall {
  readonly id: string;
  readonly function?: { readonly name?: string };
  readonly display?: {
    readonly type?: string;
    readonly artifact?: { readonly filename?: string; readonly asset_id?: string };
  };
}

interface SnapshotMessage {
  readonly role: string;
  readonly content?: string;
  readonly toolCalls?: readonly SnapshotToolCall[];
}

interface ThreadAsset {
  readonly id: string;
  readonly file_name: string;
  readonly tool_call_id?: string;
}

async function snapshot(page: Page, threadID: string): Promise<readonly SnapshotMessage[]> {
  const response = await sameOriginFetch(page, `/threads/${threadID}/messages`);
  expect(response.status, response.text).toBe(200);
  return (JSON.parse(response.text) as { messages: SnapshotMessage[] }).messages;
}

function answered(messages: readonly SnapshotMessage[], afterUserTurns: number): boolean {
  let users = 0;
  return messages.some((message) => {
    if (message.role === 'user') users += 1;
    return (
      users >= afterUserTurns &&
      message.role === 'assistant' &&
      (message.content ?? '').trim() !== ''
    );
  });
}

function deliveredCall(messages: readonly SnapshotMessage[]): SnapshotToolCall | undefined {
  return messages
    .flatMap((message) => message.toolCalls ?? [])
    .find(
      (call) =>
        call.function?.name === 'send_file' &&
        call.display?.type === 'local_artifact' &&
        call.display.artifact?.filename === fileName,
    );
}

test.describe('live artifact position', () => {
  test.skip(!live, 'set AURA_E2E_LIVE_ARTIFACT_POSITION=1 against a live stack');

  test('a file delivered in a later turn stays on that turn after a reload', async ({ page }) => {
    test.setTimeout(300_000);
    await gotoAuthenticated(page, '/');
    const input = page.getByRole('textbox', { name: 'Ask Aura', exact: true });

    await input.fill('Ciao! Rispondi solo con un saluto breve.');
    await input.press('Enter');
    await expect(page).toHaveURL(/\/c\/[0-9a-f-]{36}/, { timeout: 60_000 });
    const threadID = /\/c\/([0-9a-f-]{36})/.exec(page.url())?.[1] ?? '';
    await expect
      .poll(async () => answered(await snapshot(page, threadID), 1), { timeout: 120_000 })
      .toBe(true);

    await input.fill(
      `Crea il file /workspace/${fileName} che contiene la parola ciao e consegnamelo con send_file. Poi rispondi solo "fatto".`,
    );
    await input.press('Enter');
    await expect
      .poll(
        async () => {
          const messages = await snapshot(page, threadID);
          return deliveredCall(messages) !== undefined && answered(messages, 2);
        },
        { timeout: 240_000 },
      )
      .toBe(true);

    // The durable record: the asset names the call, and the snapshot carries its card there.
    const call = deliveredCall(await snapshot(page, threadID));
    expect(call?.display?.artifact?.asset_id).toBeTruthy();
    const assets = await sameOriginFetch(
      page,
      `/api/assets?thread_id=${encodeURIComponent(threadID)}`,
    );
    expect(assets.status, assets.text).toBe(200);
    const delivered = (JSON.parse(assets.text) as ThreadAsset[]).find(
      (asset) => asset.id === call?.display?.artifact?.asset_id,
    );
    expect(delivered?.tool_call_id).toBe(call?.id);

    // What the person sees after a reload: one card in the transcript, not under the greeting.
    // Scoped to the chat because the Artifacts panel lists the same file with its own download.
    await page.reload();
    const chat = page.getByRole('region', { name: 'Chat', exact: true });
    const download = chat.getByRole('link', { name: `Download ${fileName}`, exact: true });
    await expect(download).toHaveCount(1, { timeout: 30_000 });
    const greeting = chat.locator('[data-message-role="assistant"]').first();
    await expect(greeting.getByRole('link', { name: `Download ${fileName}` })).toHaveCount(0);
  });
});
