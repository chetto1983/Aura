import { expect, test, type Page, type TestInfo } from '@playwright/test';
import type { StudioModel, StudioModels, StudioRecord } from '../src/studio/studioApi';
import { gotoAuthenticated } from './auth';
import { sameOriginFetch } from './live';
import { paidRunSwitch, requireLiveOrigin } from './media-generation-live.helpers';

// The paid Studio acceptance run. Every case here spends real OpenRouter credit, so the file
// runs only for an explicitly approved run:
//
//   AURA_E2E_STUDIO=1 AURA_E2E_ORIGIN=https://localhost npx playwright test \
//     e2e/studio-live.spec.ts --project=chrome
//
// AURA_E2E_STUDIO is a TEST switch — no product code reads it. Without it the whole group is
// skipped, which is how CI and an ordinary local run stay unpaid; any other value throws at
// collection rather than disarming silently. With it, a missing origin fails the run in
// beforeAll, before a composer is ever opened.
//
// It generates EXACTLY ONCE PER KIND. The group is serial with retries pinned to zero, each
// Generate is a single click on a button the page holds shut while the request is open, and
// every wait polls a read route — never a resubmit. The history is read through the same API
// the panel reads, so a second record of the same kind is a failure with its own message
// rather than a silently doubled bill.

const studioEnabled = paidRunSwitch('AURA_E2E_STUDIO');

/** The clip model the plan priced, and the estimate it must show at the cheapest declared
 *  options. The literal is pinned because it is the spend the operator approved: a deployment
 *  whose catalog now prices this differently must stop the run, not quietly buy something
 *  else. */
const videoModelId = 'google/veo-3.1-lite';
const videoEstimate = '≈ $0.12';

/** Both prompts carry the run's own stamp, so the two rows this run adds are identifiable in
 *  the operator's history among every earlier acceptance run. */
const runStamp = new Date().toISOString();
const imagePrompt = `Studio acceptance ${runStamp}: a red wooden boat on a calm mountain lake.`;
const videoPrompt = `Studio acceptance ${runStamp}: waves rolling onto an empty beach at sunset.`;

const activeStatuses: readonly string[] = ['pending', 'in_progress'];

/** An image model the catalog priced. An unpriced row cannot be compared with the others, so
 *  "the cheapest" is chosen among the priced ones or not claimed at all. */
type PricedImageModel = StudioModel & { readonly image_min_usd: number };

function isPriced(model: StudioModel): model is PricedImageModel {
  return typeof model.image_min_usd === 'number';
}

function modelLabel(model: StudioModel): string {
  return model.name === undefined || model.name === '' ? model.id : model.name;
}

async function studioCatalog(page: Page, kind: 'image' | 'video'): Promise<StudioModels> {
  const response = await sameOriginFetch(page, `/api/studio/models?kind=${kind}`);
  if (response.status !== 200) {
    throw new Error(`/api/studio/models?kind=${kind} answered ${String(response.status)}`);
  }
  const catalog = JSON.parse(response.text) as StudioModels;
  if (catalog.models.length === 0) {
    throw new Error(`the ${kind} catalog is empty; the Studio is not configured here`);
  }
  return catalog;
}

/** The cheapest image model the deployment declares, read from the catalog the picker reads —
 *  never a hardcoded id, because the cheapest row moves with the provider's price list. */
function cheapestImageModel(catalog: StudioModels): PricedImageModel {
  const priced = catalog.models.filter(isPriced);
  const cheapest = priced.reduce<PricedImageModel | undefined>(
    (low, model) => (low === undefined || model.image_min_usd < low.image_min_usd ? model : low),
    undefined,
  );
  if (cheapest === undefined) {
    throw new Error('the image catalog prices no model, so the cheapest one cannot be chosen');
  }
  return cheapest;
}

function catalogModel(catalog: StudioModels, id: string): StudioModel {
  const model = catalog.models.find((row) => row.id === id);
  if (model === undefined) {
    throw new Error(
      `${id} is not in this deployment's video catalog: ${catalog.models
        .map((row) => row.id)
        .join(', ')}`,
    );
  }
  return model;
}

/** GET /api/conversations answers a bare array, or JSON null for an identity with none. */
async function conversationCount(page: Page): Promise<number> {
  const response = await sameOriginFetch(page, '/api/conversations');
  if (response.status !== 200) {
    throw new Error(`/api/conversations answered ${String(response.status)}: ${response.text}`);
  }
  const rows: unknown = JSON.parse(response.text);
  if (rows === null) return 0;
  if (!Array.isArray(rows)) {
    throw new Error(`/api/conversations answered no array: ${response.text}`);
  }
  return (rows as readonly unknown[]).length;
}

async function studioHistory(page: Page): Promise<readonly StudioRecord[]> {
  const response = await sameOriginFetch(page, '/api/studio/history?limit=24');
  if (response.status !== 200) {
    throw new Error(`/api/studio/history answered ${String(response.status)}: ${response.text}`);
  }
  const body = JSON.parse(response.text) as { readonly records?: readonly StudioRecord[] };
  const records = body.records;
  if (records === undefined) {
    throw new Error(`/api/studio/history carried no records array: ${response.text}`);
  }
  return records;
}

async function historyIds(page: Page): Promise<ReadonlySet<string>> {
  return new Set((await studioHistory(page)).map((record) => record.id));
}

/**
 * The record this run's one click produced, once the provider has finished with it.
 *
 * It polls the read route — a generation has no push channel — and answers three ways rather
 * than one: the completed record, a throw naming the status a finished-but-not-completed
 * record carries, and a throw when the click produced more than one record of this kind,
 * which is the doubled bill this suite exists to make impossible.
 */
async function awaitOneRecord(
  page: Page,
  kind: 'image' | 'video',
  before: ReadonlySet<string>,
  timeoutMs: number,
): Promise<StudioRecord> {
  const refusal = page.getByRole('alert');
  const deadline = Date.now() + timeoutMs;
  let seen = 'none';
  for (;;) {
    const fresh = (await studioHistory(page)).filter(
      (record) => record.kind === kind && !before.has(record.id),
    );
    if (fresh.length > 1) {
      throw new Error(
        `one Generate click recorded ${String(fresh.length)} ${kind} generations: ${fresh
          .map((record) => record.id)
          .join(', ')}`,
      );
    }
    const record = fresh[0];
    if (record !== undefined) {
      if (record.status === 'completed') return record;
      if (!activeStatuses.includes(record.status)) {
        throw new Error(`the ${kind} generation ended ${record.status}: ${JSON.stringify(record)}`);
      }
      seen = record.status;
    } else if ((await refusal.count()) > 0) {
      throw new Error(
        `the Studio refused the ${kind} generation: ${await refusal.first().innerText()}`,
      );
    }
    if (Date.now() >= deadline) {
      throw new Error(
        `no completed ${kind} record within ${String(timeoutMs)}ms (last status ${seen})`,
      );
    }
    await page.waitForTimeout(5_000);
  }
}

/** The asset the record delivered. A completed record without one is the bug this asserts. */
function deliveredAssetId(record: StudioRecord): string {
  const assetId = record.asset_id;
  expect(assetId, `completed ${record.kind} record ${record.id} carries no asset_id`).toBeTruthy();
  expect(String(assetId)).toMatch(/^[0-9a-f-]{36}$/i);
  return String(assetId);
}

/** What the composer says this request will cost, in dollars, read from the bar beside
 *  Generate. `price not published` has no number and throws rather than reading as free. */
async function shownEstimateUsd(page: Page): Promise<number> {
  const text = (await page.getByTestId('studio-estimate').innerText()).trim();
  const shown = /\$\s*([\d.]+)/.exec(text);
  const amount = shown?.[1];
  if (amount === undefined) {
    throw new Error(`the composer published no price beside Generate: ${text}`);
  }
  return Number(amount);
}

/**
 * Open the Studio the way the plan describes: the shell's surface intent and both remembered
 * model choices are preset in localStorage before the first paint, so the page opens on the
 * models this run means to pay for rather than on the deployment's defaults.
 */
async function openStudio(page: Page, imageModelId: string): Promise<void> {
  await page.addInitScript(
    ({ image, video }) => {
      window.localStorage.setItem('aura.shell.surface', 'studio');
      window.localStorage.setItem('aura.studio.model.image', image);
      window.localStorage.setItem('aura.studio.model.video', video);
      // The history panel is what step 4 reads back; an operator's stored "closed" must not
      // decide whether this run can see it.
      window.localStorage.setItem('aura.studio.history-open', '1');
    },
    { image: imageModelId, video: videoModelId },
  );
  await gotoAuthenticated(page, '/');
  await expect(page.getByRole('region', { name: 'Studio', exact: true })).toBeVisible({
    timeout: 60_000,
  });
  await expect(page.getByRole('textbox', { name: 'Prompt', exact: true })).toBeVisible({
    timeout: 60_000,
  });
}

async function attachRecord(info: TestInfo, name: string, record: StudioRecord): Promise<void> {
  info.annotations.push({
    type: name,
    description: `${record.id} asset=${record.asset_id ?? 'none'} model=${record.model} cost=${
      record.cost_usd === undefined ? 'unknown' : `$${String(record.cost_usd)}`
    }`,
  });
  await info.attach(name, {
    body: JSON.stringify(record, null, 2),
    contentType: 'application/json',
  });
}

test.describe('Studio (paid, real generations)', () => {
  // Serial so a failed image never buys a clip, and retries pinned to zero so no failure can
  // ever re-run a paid case — the config retries once under CI, which this must not inherit.
  test.describe.configure({ mode: 'serial', retries: 0 });
  test.skip(!studioEnabled, 'set AURA_E2E_STUDIO=1 for an approved paid Studio run');

  let imageModel: PricedImageModel | undefined;
  let videoModel: StudioModel | undefined;
  let imageRecord: StudioRecord | undefined;
  let videoRecord: StudioRecord | undefined;
  let conversationsBefore = -1;

  test.beforeAll(async ({ browser }) => {
    // Guarded because a skipped group must not be able to fail the unpaid run this hook exists
    // to protect.
    if (!studioEnabled) return;
    requireLiveOrigin('AURA_E2E_STUDIO');
    const page = await browser.newPage();
    try {
      await gotoAuthenticated(page, '/');
      // Read before the first spend, so step 5's comparison spans both generations.
      conversationsBefore = await conversationCount(page);
      imageModel = cheapestImageModel(await studioCatalog(page, 'image'));
      videoModel = catalogModel(await studioCatalog(page, 'video'), videoModelId);
    } finally {
      await page.close();
    }
  });

  test('generates one image on the cheapest declared model', async ({ page }, info) => {
    test.setTimeout(900_000);
    const model = imageModel;
    if (model === undefined) throw new Error('the image catalog was never read');
    await openStudio(page, model.id);

    await page.getByRole('radio', { name: 'Image', exact: true }).click();
    await expect(page.getByRole('textbox', { name: 'Prompt', exact: true })).toHaveAttribute(
      'placeholder',
      'Describe the image you want to generate',
    );
    // The page opened on the model this run chose from the catalog, not on a default.
    await expect(page.getByRole('combobox', { name: 'Model', exact: true })).toContainText(
      modelLabel(model),
    );

    // The price is on screen BEFORE the money is spent, and it is the one the catalog declares
    // for this model. Two decimals: the bar rounds a rate to the cent once it reaches one.
    const estimate = await shownEstimateUsd(page);
    expect(estimate, `the bar priced ${model.id} at $${String(estimate)}`).toBeCloseTo(
      model.image_min_usd,
      2,
    );

    const before = await historyIds(page);
    await page.getByRole('textbox', { name: 'Prompt', exact: true }).fill(imagePrompt);
    // The one paid click of this test. There is no retry and no loop above it.
    await page.getByRole('button', { name: /Generate/ }).click();

    const record = await awaitOneRecord(page, 'image', before, 600_000);
    const assetId = deliveredAssetId(record);
    expect(record.model).toBe(model.id);
    imageRecord = record;

    // The stage renders the delivered picture, not a placeholder: a decoded image has width.
    const preview = page.locator('figure img');
    await expect(preview).toBeVisible({ timeout: 180_000 });
    expect(
      await preview.evaluate((element) => (element as HTMLImageElement).naturalWidth),
    ).toBeGreaterThan(0);

    await attachRecord(info, 'studio-image-record', record);
    info.annotations.push({
      type: 'studio-image-estimate',
      description: `shown ≈ $${String(estimate)}, delivered asset ${assetId}`,
    });
  });

  test(`generates one clip on ${videoModelId} at ${videoEstimate}`, async ({ page }, info) => {
    test.setTimeout(1_500_000);
    const model = videoModel;
    const image = imageModel;
    if (model === undefined || image === undefined) throw new Error('the catalogs were never read');
    await openStudio(page, image.id);

    // The Studio opens on Video, and the preset put this run's clip model under the pill.
    await expect(page.getByRole('textbox', { name: 'Prompt', exact: true })).toHaveAttribute(
      'placeholder',
      'Describe the video scene you want to generate',
    );
    await expect(page.getByRole('combobox', { name: 'Model', exact: true })).toContainText(
      modelLabel(model),
    );
    // The Options pill reads what will be sent: a ratio, then the resolution, then the length
    // the estimate below is a multiple of. The axes are what the composer reconciled to this
    // model's cheapest, which is what makes that estimate the cheapest it can be asked for.
    await expect(page.getByRole('button', { name: 'Options', exact: true })).toHaveText(
      /^\d+:\d+\b.*\d+s$/,
    );
    await expect(page.getByTestId('studio-estimate')).toHaveText(videoEstimate);

    const before = await historyIds(page);
    await page.getByRole('textbox', { name: 'Prompt', exact: true }).fill(videoPrompt);
    // The one paid click of this test.
    await page.getByRole('button', { name: /Generate/ }).click();

    const record = await awaitOneRecord(page, 'video', before, 1_200_000);
    const assetId = deliveredAssetId(record);
    expect(record.model).toBe(videoModelId);
    videoRecord = record;

    // The clip plays from the identity-scoped Range route, which is the only way the cockpit
    // ever names an asset.
    await expect(page.locator(`video[src="/api/assets/${assetId}/stream"]`)).toBeVisible({
      timeout: 300_000,
    });

    await attachRecord(info, 'studio-video-record', record);
  });

  test('both survive a reload in History, and no conversation was touched', async ({
    page,
  }, info) => {
    test.setTimeout(300_000);
    const image = imageRecord;
    const video = videoRecord;
    const model = imageModel;
    if (image === undefined || video === undefined || model === undefined) {
      throw new Error('neither generation completed, so there is nothing to find in History');
    }
    await openStudio(page, model.id);
    await page.reload({ waitUntil: 'domcontentloaded' });
    await expect(page.getByRole('region', { name: 'Studio', exact: true })).toBeVisible({
      timeout: 60_000,
    });

    const card = (id: string) =>
      page.locator(`[data-testid="studio-history-card"][data-record-id="${id}"]`);
    await expect(card(image.id)).toBeVisible({ timeout: 60_000 });
    await expect(card(video.id)).toBeVisible({ timeout: 60_000 });

    // The Studio is not a conversation: two paid generations must leave the thread list exactly
    // as long as it was before the first one.
    expect(await conversationCount(page), 'the Studio created or removed a conversation').toBe(
      conversationsBefore,
    );

    info.annotations.push({
      type: 'studio-records',
      description: `image ${image.id} (${image.asset_id ?? 'none'}), video ${video.id} (${
        video.asset_id ?? 'none'
      })`,
    });
    await info.attach('studio-records', {
      body: JSON.stringify({ image, video, conversations: conversationsBefore }, null, 2),
      contentType: 'application/json',
    });
  });
});
