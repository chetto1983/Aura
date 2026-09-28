import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { expect, test, type Page, type Route } from '@playwright/test';
import { gotoAuthenticated } from './auth';

const CONV_ID = '77777777-7777-7777-7777-777777777777';
const image = readFileSync(resolve(process.cwd(), 'e2e/fixtures/media-edit/photo.png'));
const video = readFileSync(resolve(process.cwd(), 'e2e/fixtures/media-edit/clip.mp4'));
const audio = readFileSync(resolve(process.cwd(), 'e2e/fixtures/video-studio/audio/music.wav'));

function artifactCall(id: string, filename: string, mimeType: string) {
  return {
    id,
    function: { name: 'emit_artifact', arguments: '{}' },
    display: {
      type: 'local_artifact',
      tool_call_id: id,
      artifact: { asset_id: id, filename, mime_type: mimeType, size_bytes: 4096 },
    },
  };
}

function conversationSnapshot(): string {
  const groups = [
    [
      artifactCall('image-1', 'First.png', 'image/png'),
      artifactCall('image-2', 'Second.png', 'image/png'),
    ],
    [artifactCall('video-1', 'clip.mp4', 'video/mp4')],
    [artifactCall('audio-1', 'recording.wav', 'audio/wav')],
    [artifactCall('image-fail', 'broken.png', 'image/png')],
  ];
  const messages: Record<string, unknown>[] = [];
  groups.forEach((calls, index) => {
    messages.push({ id: `user-${String(index)}`, role: 'user', content: 'Show media' });
    messages.push({
      id: `assistant-${String(index)}`,
      role: 'assistant',
      content: '',
      toolCalls: calls,
    });
    for (const call of calls) {
      messages.push({
        id: `result-${call.id}`,
        role: 'tool',
        toolCallId: call.id,
        content: call.id,
      });
    }
  });
  return JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages });
}

async function installConversationRoutes(page: Page) {
  const record = {
    id: CONV_ID,
    title: 'Media displays',
    status: 'active',
    total_input_tokens: 0,
    total_output_tokens: 0,
    total_cached_tokens: 0,
    total_cost_usd: 0,
  };
  await page.route('**/api/conversations*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(
        route.request().url().includes(`/api/conversations/${CONV_ID}`) ? record : [record],
      ),
    }),
  );
  await page.route('**/api/conversations/*/rot-events', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.route('**/api/approvals', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.route('**/threads/*/messages', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: conversationSnapshot() }),
  );
}

async function fulfillMediaRange(route: Route, bytes: Buffer, contentType: string) {
  const range = route.request().headers().range;
  const match = /^bytes=(\d+)-(\d*)$/.exec(range ?? '');
  if (match === null) {
    await route.fulfill({
      status: 200,
      contentType,
      body: bytes,
      headers: { 'Accept-Ranges': 'bytes', 'X-Content-Type-Options': 'nosniff' },
    });
    return;
  }
  const start = Number(match[1]);
  const end = match[2] === '' ? bytes.length - 1 : Math.min(Number(match[2]), bytes.length - 1);
  if (start >= bytes.length) {
    await route.fulfill({
      status: 416,
      headers: { 'Content-Range': `bytes */${String(bytes.length)}` },
    });
    return;
  }
  await route.fulfill({
    status: 206,
    contentType,
    body: bytes.subarray(start, end + 1),
    headers: {
      'Accept-Ranges': 'bytes',
      'Content-Range': `bytes ${String(start)}-${String(end)}/${String(bytes.length)}`,
      'X-Content-Type-Options': 'nosniff',
    },
  });
}

test('trusted images group while audio, video and failed previews keep asset controls', async ({
  page,
}) => {
  await installConversationRoutes(page);
  await page.route('**/api/assets/*/download', async (route) => {
    const id = new URL(route.request().url()).pathname.split('/')[3];
    if (id === 'image-fail') {
      await route.fulfill({ status: 404 });
    } else if (id === 'image-1' || id === 'image-2') {
      await route.fulfill({ status: 200, contentType: 'image/png', body: image });
    } else {
      await route.continue();
    }
  });
  await page.route('**/api/assets/audio-1/stream', (route) =>
    fulfillMediaRange(route, audio, 'audio/wav'),
  );
  await page.route('**/api/assets/video-1/stream', (route) =>
    fulfillMediaRange(route, video, 'video/mp4'),
  );
  await gotoAuthenticated(page, `/c/${CONV_ID}`);

  const gallery = page.locator('[data-slot="image-gallery"]');
  await expect(gallery).toBeVisible();
  await gallery.getByRole('button', { name: 'Open Second.png' }).click();
  const lightbox = page.getByRole('dialog', { name: 'Second.png' });
  await expect(lightbox).toBeVisible();
  await expect(lightbox.getByRole('link', { name: 'Download Second.png' })).toHaveAttribute(
    'href',
    '/api/assets/image-2/download',
  );
  await page.keyboard.press('Escape');
  await expect(lightbox).toHaveCount(0);

  const videoElement = page.locator('video[aria-label="clip.mp4"]');
  await expect(videoElement).toHaveAttribute('src', '/api/assets/video-1/stream');
  await expect(videoElement).not.toHaveAttribute('autoplay');
  await expect(videoElement).toHaveClass(/h-auto/);

  const audioElement = page.locator('audio[src$="/api/assets/audio-1/stream"]');
  await expect(audioElement).toHaveCount(1);
  await expect(audioElement).not.toHaveAttribute('autoplay');
  const seek = page.getByRole('slider', { name: 'Seek' });
  await expect(seek).not.toHaveAttribute('max', '0');
  await seek.focus();
  await seek.press('ArrowRight');
  const firstSeek = await seek.inputValue();
  await seek.press('ArrowRight');
  expect(Number(await seek.inputValue())).toBeGreaterThan(Number(firstSeek));
  await expect(page.getByRole('link', { name: 'Download recording.wav' })).toHaveAttribute(
    'href',
    '/api/assets/audio-1/download',
  );
  await expect(page.getByRole('link', { name: 'Download broken.png' })).toHaveAttribute(
    'href',
    '/api/assets/image-fail/download',
  );

  await page.reload();
  await expect(gallery).toBeVisible();
  await expect(videoElement).not.toHaveAttribute('autoplay');
  await expect(audioElement).not.toHaveAttribute('autoplay');
});

test('public share audio seeks on its token-scoped stream without a login', async ({ page }) => {
  await page.route('**/s/media-token/data', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        schema_version: 1,
        title: 'Shared audio',
        model: '',
        created_at: '2026-09-28T00:00:00Z',
        snapshot_at: '2026-09-28T00:00:00Z',
        turns: [],
        artifacts: [
          {
            asset_id: 'audio-1',
            filename: 'recording.wav',
            mime_type: 'audio/wav',
            size_bytes: audio.length,
          },
        ],
      }),
    }),
  );
  const streamRequests: string[] = [];
  await page.route('**/s/media-token/asset/audio-1/stream', (route) => {
    streamRequests.push(route.request().headers().range ?? '');
    return fulfillMediaRange(route, audio, 'audio/wav');
  });
  await page.goto('/s/media-token');
  const player = page.locator('[data-slot="audio-player"]');
  await expect(player).toBeVisible();
  await expect(player.locator('audio')).toHaveAttribute(
    'src',
    /\/s\/media-token\/asset\/audio-1\/stream$/,
  );
  await expect(player.locator('audio')).not.toHaveAttribute('autoplay');
  const seek = page.getByRole('slider', { name: 'Seek' });
  await expect(seek).not.toHaveAttribute('max', '0');
  await seek.focus();
  await seek.press('ArrowRight');
  await seek.press('ArrowRight');
  expect(Number(await seek.inputValue())).toBeGreaterThan(0);
  expect(streamRequests.length).toBeGreaterThan(0);
  await expect(page.getByRole('link', { name: 'Download recording.wav' })).toHaveAttribute(
    'href',
    '/s/media-token/asset/audio-1',
  );
});
