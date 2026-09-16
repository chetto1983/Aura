import { expect, test } from '@playwright/test';
import { gotoAuthenticated } from './auth';
import {
  countToolCalls,
  createMediaConversation,
  expectPromotedByToolSearch,
  expectToolResultJSON,
  lastToolCall,
  mediaGenerationEnabled,
  putSetting,
  deleteSetting,
  rangeProbe,
  requireLiveOrigin,
  requireMediaRuntime,
  sendPrompt,
  threadSnapshot,
  toolCallArguments,
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
// is skipped, which is how ordinary CI stays unpaid. With it, a missing origin, an
// unreachable media catalog or missing cockpit credentials FAIL the run: a paid acceptance
// batch that quietly skipped is reported as not executed, never as green.
//
// The suite asserts the real path, not a picture of it. Each case reads the thread the daemon
// persisted and requires that tool_search promoted the deferred media tool BEFORE the agent
// called it — a screenshot proves a pixel, the trace proves the tool ran. Nothing here
// injects an assistant message, calls a Go tool directly or mocks a generation response.
//
// Serial: the image the first test generates is the first frame the animation test asks for,
// and the clip the third test delivers is the one the player test streams. Paid work is
// reused across cases rather than repeated.

const imagePrompt = 'Generate an image of a red wooden boat on a calm mountain lake, 16:9.';
const videoPrompt =
  'Generate a five second 480p video of a red wooden boat drifting on a calm mountain lake.';
const animatePrompt = 'Animate this image into a five second 480p clip: let the water ripple.';

test.describe.serial('media generation (paid, real agent)', () => {
  test.skip(
    !mediaGenerationEnabled,
    'set AURA_E2E_MEDIA_GENERATION=1 for an approved paid acceptance run',
  );

  let conversationId = '';
  let imageAssetID = '';
  let videoAssetID = '';

  test('the real agent generates a visible image', async ({ page }, info) => {
    test.setTimeout(600_000);
    requireLiveOrigin();
    await gotoAuthenticated(page, '/');
    await requireMediaRuntime(page);
    conversationId = await createMediaConversation(
      page,
      `Media generation acceptance ${new Date().toISOString()}`,
    );
    await gotoAuthenticated(page, `/c/${encodeURIComponent(conversationId)}`);
    await sendPrompt(page, imagePrompt);

    await expect(page.getByTestId('generation-frame')).toBeVisible({ timeout: 120_000 });
    const image = page.locator('[data-slot="image-preview"] img').last();
    await expect(image).toBeVisible({ timeout: 300_000 });
    expect(await image.evaluate((el) => (el as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);

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
    await sendPrompt(page, videoPrompt);
    await expect(page.getByTestId('generation-frame')).toBeVisible({ timeout: 120_000 });

    videoAssetID = await waitForDeliveredAsset(page, conversationId, 'video_generate', 600_000);
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

    // Motion, not a file signature: the element plays and its clock advances.
    await video.evaluate(async (el) => {
      await (el as HTMLVideoElement).play();
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
    // A finished generation drops its frame, so the only frame on screen is this turn's.
    await expect(page.getByTestId('generation-frame')).toHaveCount(1, { timeout: 120_000 });

    await waitForDeliveredAsset(page, conversationId, 'video_generate', 600_000);
    const snapshot = await threadSnapshot(page, conversationId);
    expect(countToolCalls(snapshot, 'video_generate')).toBeGreaterThan(before);
    const animation = lastToolCall(snapshot, 'video_generate');
    expect(toolCallArguments(animation).first_frame_asset_id).toBe(imageAssetID);
    const result = expectToolResultJSON(animation);
    expect(String(result.mime_type)).toMatch(/^video\//);

    await info.attach('animation-tool-call', {
      body: animation.argsText,
      contentType: 'application/json',
    });
  });

  test('a detached clip replays a static frame and arrives on the wake', async ({ page }, info) => {
    test.setTimeout(1_200_000);
    await gotoAuthenticated(page, '/');
    const detachedId = await createMediaConversation(
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
      await info.attach('detached-collect-result', {
        body: lastToolCall(settled, 'video_generate').result,
        contentType: 'application/json',
      });
    } finally {
      await deleteSetting(page, videoInlineWaitKey);
    }
  });
});
