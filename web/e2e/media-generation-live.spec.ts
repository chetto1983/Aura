import { expect, test, type Locator, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';
import { createConversation } from './live';
import {
  countToolCalls,
  expectPromotedByToolSearch,
  expectToolCallAt,
  expectToolResultJSON,
  mediaGenerationEnabled,
  putSetting,
  deleteSetting,
  rangeProbe,
  requireLiveOrigin,
  requireMediaRuntime,
  sendPrompt,
  threadSnapshot,
  toolCallArguments,
  toolCallsAfter,
  videoInlineWaitKey,
  waitForDeliveredAsset,
} from './media-generation-live.helpers';

// The paid media-generation acceptance suite (spec §2/§3/§5). Every test here spends real
// OpenRouter credit, so the file runs only for an explicitly approved run:
//
//   AURA_E2E_MEDIA_GENERATION=1 AURA_E2E_ORIGIN=https://localhost npx playwright test \
//     e2e/media-generation-live.spec.ts --project=chrome
//
// AURA_E2E_MEDIA_GENERATION is a TEST switch — no product code reads it. Without it the file
// is skipped, which is how ordinary CI stays unpaid; any other value throws rather than
// disarming silently. With it, a missing origin, an unreachable media catalog or missing
// cockpit credentials FAIL the run in beforeAll — before the first prompt reaches a composer,
// and whichever test a --grep happens to select.
//
// The suite asserts the real path, not a picture of it. Each case reads the thread the daemon
// persisted and requires that tool_search promoted the deferred media tool BEFORE the agent
// called it — a screenshot proves a pixel, the trace proves the tool ran. Nothing here
// injects an assistant message, calls a Go tool directly or mocks a generation response.
//
// Serial, and the conversation is shared: the image the first test generates is the first
// frame the animation test asks for, and the clip the second delivers is the one the player
// test streams. That reuse is why every wait counts the tool's calls BEFORE its prompt and
// asserts only on what the turn added — a thread-wide first match would answer with the
// previous test's clip and wait for nothing.

const imagePrompt = 'Generate an image of a red wooden boat on a calm mountain lake, 16:9.';
const videoPrompt =
  'Generate a five second 480p video of a red wooden boat drifting on a calm mountain lake.';
const animatePrompt = 'Animate this image into a five second 480p clip: let the water ripple.';

function generationFrame(page: Page): Locator {
  return page.getByTestId('generation-frame');
}

/**
 * The running frame is not a checkpoint: a fast turn can deliver before it paints, and a
 * missing frame there is not a regression. Gate on the frame OR the delivered media.
 */
function frameOrResult(page: Page, result: Locator): Locator {
  return generationFrame(page).or(result).first();
}

test.describe.serial('media generation (paid, real agent)', () => {
  test.skip(
    !mediaGenerationEnabled,
    'set AURA_E2E_MEDIA_GENERATION=1 for an approved paid acceptance run',
  );

  let conversationId = '';
  let imageAssetID = '';
  let videoAssetID = '';

  test.beforeAll(async ({ browser }) => {
    // Guarded because a skipped group must not be able to fail the unpaid run this hook is
    // there to protect.
    if (!mediaGenerationEnabled) return;
    requireLiveOrigin();
    const page = await browser.newPage();
    try {
      await gotoAuthenticated(page, '/');
      await requireMediaRuntime(page);
      conversationId = await createConversation(
        page,
        `Media generation acceptance ${new Date().toISOString()}`,
      );
    } finally {
      await page.close();
    }
  });

  test('the real agent generates a visible image', async ({ page }, info) => {
    test.setTimeout(600_000);
    await gotoAuthenticated(page, `/c/${encodeURIComponent(conversationId)}`);
    const image = page.locator('[data-slot="image-preview"] img');
    await sendPrompt(page, imagePrompt);

    await expect(frameOrResult(page, image)).toBeVisible({ timeout: 120_000 });
    await expect(image.last()).toBeVisible({ timeout: 300_000 });
    expect(
      await image.last().evaluate((el) => (el as HTMLImageElement).naturalWidth),
    ).toBeGreaterThan(0);

    const snapshot = await threadSnapshot(page, conversationId);
    const call = expectPromotedByToolSearch(snapshot, 'image_generate');
    const result = expectToolResultJSON(call);
    expect(String(result.mime_type)).toMatch(/^image\//);
    imageAssetID = String(result.asset_id);
    expect(imageAssetID).toMatch(/^[0-9a-f-]{36}$/i);

    await info.attach('image-tool-result', { body: call.result, contentType: 'application/json' });
    await info.attach('generated-image', {
      body: await page.screenshot(),
      contentType: 'image/png',
    });
  });

  test('the real agent delivers a clip in the same turn', async ({ page }, info) => {
    test.setTimeout(900_000);
    await gotoAuthenticated(page, `/c/${encodeURIComponent(conversationId)}`);
    const before = countToolCalls(await threadSnapshot(page, conversationId), 'video_generate');
    await sendPrompt(page, videoPrompt);
    await expect(frameOrResult(page, page.locator('video'))).toBeVisible({ timeout: 120_000 });

    videoAssetID = await waitForDeliveredAsset(
      page,
      conversationId,
      'video_generate',
      600_000,
      before,
    );
    const snapshot = await threadSnapshot(page, conversationId);
    const call = expectPromotedByToolSearch(snapshot, 'video_generate');
    const result = expectToolResultJSON(call);
    // The same-turn claim is exactly this: the FIRST video_generate answered with the asset,
    // not with {"status":"in_progress"}. A provider slower than the inline wait must be
    // recorded as that observation, never relabelled as a same-turn pass.
    expect(result.status, call.result).toBeUndefined();
    expect(String(result.asset_id)).toBe(videoAssetID);
    expect(String(result.mime_type)).toMatch(/^video\//);

    await expect(page.locator(`video[src="/api/assets/${videoAssetID}/stream"]`)).toBeVisible({
      timeout: 180_000,
    });
    await info.attach('video-tool-result', { body: call.result, contentType: 'application/json' });
  });

  test('the cockpit streams the delivered clip over HTTP Range', async ({ page }) => {
    test.setTimeout(300_000);
    await gotoAuthenticated(page, `/c/${encodeURIComponent(conversationId)}`);
    const streamURL = `/api/assets/${videoAssetID}/stream`;
    const video = page.locator(`video[src="${streamURL}"]`);
    await expect(video).toBeVisible({ timeout: 180_000 });

    const probe = await rangeProbe(page, streamURL, 'bytes=0-1023');
    expect(probe.status).toBe(206);
    expect(probe.contentRange).toMatch(/^bytes 0-1023\/\d+$/);
    expect(probe.acceptRanges).toBe('bytes');
    expect(probe.contentType).toMatch(/^video\//);
    expect(probe.bytes).toBe(1024);

    // Motion, not a file signature: the element plays and its clock advances. Muted first, so
    // Chromium's autoplay policy cannot reject play() and read as a streaming regression.
    await video.evaluate(async (el) => {
      const player = el as HTMLVideoElement;
      player.muted = true;
      await player.play();
    });
    await expect
      .poll(async () => video.evaluate((el) => (el as HTMLVideoElement).currentTime), {
        timeout: 30_000,
      })
      .toBeGreaterThan(0);
  });

  test('a first frame animation reuses the generated image', async ({ page }, info) => {
    test.setTimeout(900_000);
    await gotoAuthenticated(page, `/c/${encodeURIComponent(conversationId)}`);
    const before = countToolCalls(await threadSnapshot(page, conversationId), 'video_generate');
    await sendPrompt(page, animatePrompt);
    await expect(frameOrResult(page, page.locator('video').nth(before))).toBeVisible({
      timeout: 120_000,
    });

    await waitForDeliveredAsset(page, conversationId, 'video_generate', 600_000, before);
    const fresh = toolCallsAfter(
      await threadSnapshot(page, conversationId),
      'video_generate',
      before,
    );
    // The submit carries the arguments; a detached turn's later collect does not, so the
    // first_frame claim is asserted on the call that made it.
    const submit = expectToolCallAt(fresh, 0, 'the animation submit');
    expect(toolCallArguments(submit).first_frame_asset_id).toBe(imageAssetID);
    const delivered = expectToolCallAt(fresh, fresh.length - 1, 'the animation delivery');
    expect(String(expectToolResultJSON(delivered).mime_type)).toMatch(/^video\//);

    await info.attach('animation-tool-call', {
      body: submit.argsText,
      contentType: 'application/json',
    });
  });

  test('a detached clip replays a static frame and arrives on the wake', async ({ page }, info) => {
    test.setTimeout(1_200_000);
    await gotoAuthenticated(page, '/');
    const detachedId = await createConversation(
      page,
      `Media generation detached ${new Date().toISOString()}`,
    );
    // Inline wait 1s forces the detach deterministically; the row is removed again below so
    // the deployment returns to its configured wait whatever this test does.
    await putSetting(page, videoInlineWaitKey, '1');
    try {
      await gotoAuthenticated(page, `/c/${encodeURIComponent(detachedId)}`);
      await sendPrompt(page, videoPrompt);

      const staticFrame = page.locator('[data-testid="generation-frame"][data-generating="false"]');
      await expect(staticFrame).toBeVisible({ timeout: 300_000 });
      await expect(staticFrame.locator('[role="timer"]')).toHaveCount(0);

      const pending = await threadSnapshot(page, detachedId);
      const submit = expectPromotedByToolSearch(pending, 'video_generate');
      const submitted = expectToolResultJSON(submit);
      expect(submitted.status, submit.result).toBe('in_progress');
      expect(String(submitted.job_id)).toMatch(/^[0-9a-f-]{36}$/i);

      const detachedAssetID = await waitForDeliveredAsset(
        page,
        detachedId,
        'video_generate',
        900_000,
      );
      await gotoAuthenticated(page, `/c/${encodeURIComponent(detachedId)}`);
      await expect(page.locator(`video[src="/api/assets/${detachedAssetID}/stream"]`)).toBeVisible({
        timeout: 180_000,
      });
      // The replayed submit frame stays static and unduplicated once the clip has landed.
      await expect(staticFrame).toHaveCount(1);
      await expect(staticFrame.locator('[role="timer"]')).toHaveCount(0);

      const settled = await threadSnapshot(page, detachedId);
      expect(countToolCalls(settled, 'video_generate')).toBe(2);
      const collect = expectToolCallAt(
        toolCallsAfter(settled, 'video_generate', 1),
        0,
        'the wake collect',
      );
      await info.attach('detached-collect-result', {
        body: collect.result,
        contentType: 'application/json',
      });
    } finally {
      await deleteSetting(page, videoInlineWaitKey);
    }
  });
});
