import { randomUUID } from 'node:crypto';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect, type Locator, type Page, type TestInfo } from '@playwright/test';
import { ALL_FORMATS, FilePathSource, Input } from 'mediabunny';
import type { StudioRecord } from '../../src/studio/studioApi';
import { gotoAuthenticated } from '../auth';
import { uploadAsset, uploadBytes } from './assetUpload';

// videoStudio.ts — driving the multi-track editor the way an operator does, shared by the Studio
// specs: the editor opened on a seeded clip, the add actions at both widths, inspector fields,
// pointer drags on a timeline with a real scale, and the network watch that insists nothing left
// the appliance.

export const FIXTURES = resolve(
  dirname(fileURLToPath(import.meta.url)),
  '../fixtures/video-studio',
);

export const AUDIO_FIXTURES = resolve(FIXTURES, 'audio');

const FRAME = { width: 320, height: 180 };

/** Muted 4 s clips of `clip-a.mp4`: the only sound in the export is the one the test puts there. */
export function silentFilm(clip: string, name: string, clips = 2) {
  return {
    id: randomUUID(),
    name,
    size: FRAME,
    fps: 30,
    sources: [
      { id: 'src-a', assetId: clip, kind: 'video', duration: 4, size: FRAME, hasAudio: true },
    ],
    video: Array.from({ length: clips }, (_, index) => ({
      id: `clip-${String(index + 1)}`,
      sourceId: 'src-a',
      duration: 4,
      sourceStart: 0,
      muted: true,
    })),
    overlays: [],
  };
}

export function videoRecord(assetId: string, prompt: string): StudioRecord {
  return {
    id: 'rec-video-studio',
    kind: 'video',
    status: 'completed',
    model: 'test/model',
    prompt,
    used: {},
    asset_id: assetId,
    created_at: new Date().toISOString(),
  };
}

export interface NetworkLog {
  /** Every http(s) origin touched so far, in order of first sight. */
  origins(): readonly string[];
  /** A point in that list, so a later call can ask what a single gesture added. */
  mark(): number;
  since(mark: number): readonly string[];
  /** The object-store origins the appliance's OWN presign answers named, once they have been
   *  read. An empty set means the session never asked to upload anything. */
  store(): Promise<readonly string[]>;
}

/**
 * Every http(s) origin the session touched, from before the first navigation. Both the page and
 * its context are listened to: the export runs `worker: true`, and a dedicated worker's requests
 * reach the context even where they do not surface on the page.
 *
 * The second half of this is what makes the claim portable without weakening it. An upload goes
 * to a presigned URL, and which origin that is belongs to the DEPLOYMENT, not to the editor: on
 * the appliance Caddy fronts the store on the browsing origin itself, while CI presigns to
 * garage on another port. So the allowed set is not a list written here — it is exactly what the
 * cockpit's own `/api/assets/presign` answer named, and nothing else is tolerated.
 */
export function watchNetwork(page: Page): NetworkLog {
  const seen: string[] = [];
  const presigned = new Set<string>();
  const reading: Promise<void>[] = [];
  const record = (url: string) => {
    if (!url.startsWith('http:') && !url.startsWith('https:')) return;
    const origin = new URL(url).origin;
    if (!seen.includes(origin)) seen.push(origin);
  };
  page.on('request', (request) => {
    record(request.url());
  });
  page.context().on('request', (request) => {
    record(request.url());
  });
  page.on('response', (response) => {
    if (!response.url().includes('/api/assets/presign')) return;
    reading.push(
      response
        .json()
        .then((body: { upload?: { upload_url?: unknown } }) => {
          const url = body.upload?.upload_url;
          if (typeof url === 'string') presigned.add(new URL(url).origin);
        })
        .catch(() => undefined),
    );
  });
  return {
    origins: () => [...seen],
    mark: () => seen.length,
    since: (mark) => seen.slice(mark),
    store: async () => {
      await Promise.all(reading);
      return [...presigned].sort();
    },
  };
}

/**
 * Nothing left the appliance. Every origin is either the cockpit the operator is looking at or
 * the object store the cockpit itself presigned an upload to — no font CDN, no telemetry, no
 * third party of any kind.
 */
export async function expectNothingLeftTheAppliance(page: Page, network: NetworkLog) {
  const here = new URL(page.url()).origin;
  const store = await network.store();
  const strangers = network
    .origins()
    .filter((origin) => origin !== here && !store.includes(origin));
  expect(strangers, `off-origin requests (store origins: ${store.join(', ') || 'none'})`).toEqual(
    [],
  );
}

/**
 * The Studio with one completed video record pointing at a real uploaded asset. Only the Studio's
 * own routes are stubbed — a CI deployment has no OpenRouter key and serves them 503 — and the
 * asset those routes name is the fixture, uploaded through the real presign/PUT/finalize path.
 */
export async function openStudioWith(page: Page, assetId: string, prompt: string) {
  await page.route('**/api/studio/models?**', (route) =>
    route.fulfill({
      json: { default: 'test/model', models: [{ id: 'test/model', audio: false, seed: false }] },
    }),
  );
  await page.route('**/api/studio/history?**', (route) =>
    route.fulfill({ json: { records: [videoRecord(assetId, prompt)] } }),
  );
  await page.route('**/api/studio/library**', (route) => route.fulfill({ json: { assets: [] } }));
  await page.addInitScript(() => {
    window.localStorage.setItem('aura.shell.surface', 'studio');
  });
  await gotoAuthenticated(page, '/');
}

/** Puts a project file in the library and opens the Studio on it, the way a reload does. */
export async function reopen(page: Page, project: object, clipAsset: string): Promise<Locator> {
  const fileId = await uploadBytes(
    page,
    Buffer.from(JSON.stringify(project)),
    'project.json',
    'application/json',
  );
  await page.addInitScript((id) => {
    window.localStorage.setItem('aura.videoStudio.lastSavedProject', id);
  }, fileId);
  await openStudioWith(page, clipAsset, 'audio lane check');
  await page.getByRole('button', { name: 'Reopen the last project you saved here' }).click();
  return page.getByRole('dialog', { name: 'Video editor' });
}

/** Signs in and puts `clip-a.mp4` (4 s, 320×180, a tone) in the library through the media door. */
export async function uploadClip(page: Page): Promise<string> {
  await gotoAuthenticated(page, '/');
  return uploadAsset(page, resolve(FIXTURES, 'clip-a.mp4'), 'clip-a.mp4', 'video/mp4', {
    use: 'media',
  });
}

/** Presses Export and keeps the file with the run's other output. */
export async function exportTo(
  page: Page,
  editor: Locator,
  info: TestInfo,
  fileName: string,
): Promise<string> {
  const downloading = page.waitForEvent('download', { timeout: 10 * 60_000 });
  await editor.getByRole('button', { name: 'Export', exact: true }).click();
  const path = info.outputPath(fileName);
  await (await downloading).saveAs(path);
  return path;
}

/** Signs in, puts `clip-a.mp4` in the library, and opens the editor on it as the Studio would. */
export async function editorOnSeededClip(page: Page, prompt: string): Promise<Locator> {
  await gotoAuthenticated(page, '/');
  const seeded = await uploadAsset(
    page,
    resolve(FIXTURES, 'clip-a.mp4'),
    'clip-a.mp4',
    'video/mp4',
  );
  await openStudioWith(page, seeded, prompt);
  await page.getByRole('button', { name: 'Open in the video editor' }).click();
  const editor = page.getByRole('dialog', { name: 'Video editor' });
  await expect(editor.getByRole('button', { name: 'Clip 1' })).toBeVisible({ timeout: 60_000 });
  // The project is named after the prompt. At phone width the layout hides the name to make room
  // for the tools (video-studio-mobile.css, max-width 540px), so there it is only in the DOM.
  const title = editor.getByRole('heading', { name: prompt, includeHidden: true });
  await expect(title).toBeAttached();
  if ((page.viewportSize()?.width ?? Number.POSITIVE_INFINITY) > 540) {
    await expect(title).toBeVisible();
  }
  return editor;
}

/**
 * Presses one of the editor's add actions. On a desktop they sit in the rail. At phone width the
 * rail is gone and the tool bar offers them only while nothing is selected, and the editor opens
 * with the first clip selected, so the bar's back button goes first: it closes an open panel,
 * then clears the selection (VideoStudio_mobile.tsx, video-studio-mobile.css).
 */
export async function pressAddAction(editor: Locator, name: string) {
  const action = editor.getByRole('button', { name });
  const back = editor.getByRole('button', { name: 'Close tool panel' });
  for (let press = 0; press < 2 && !(await action.isVisible()); press += 1) {
    await back.click();
  }
  await action.click();
}

/** Presses an Add action whose panel offers the device and the library, and answers the panel.
 *  Waits for the editor's second clip first: `pressAddAction` looks for the action once, and while
 *  the editor is still loading it would reach for the phone's tool bar instead. */
async function openPanel(page: Page, editor: Locator, action: string, title: string) {
  await expect(editor.getByRole('button', { name: 'Clip 2' })).toBeVisible({ timeout: 60_000 });
  await pressAddAction(editor, action);
  const panel = page.getByRole('dialog', { name: title });
  await expect(panel).toBeVisible();
  return panel;
}

export function openAudioPanel(page: Page, editor: Locator): Promise<Locator> {
  return openPanel(page, editor, 'Add audio', 'Add a sound');
}

export function openClipPanel(page: Page, editor: Locator): Promise<Locator> {
  return openPanel(page, editor, 'Add a clip', 'Add a clip');
}

/** Picks a file through the editor's own button, the way an operator does: Add a clip, then the
 *  panel's Upload a clip. */
export async function addClip(page: Page, editor: Locator, file: string) {
  const chooser = page.waitForEvent('filechooser');
  await pressAddAction(editor, 'Add a clip');
  await page
    .getByRole('dialog', { name: 'Add a clip' })
    .getByRole('button', { name: 'Upload a clip' })
    .click();
  await (await chooser).setFiles(resolve(FIXTURES, file));
}

/** Commits one inspector field: type, then blur, which is what the fields listen for. */
export async function setField(inspector: Locator, label: string, value: string) {
  const field = inspector.getByLabel(label, { exact: true });
  await field.fill(value);
  await field.blur();
}

/** A lane item prints its own length as `mm:ss.s`; this reads that back as a number. */
export function seconds(timecode: string): number {
  const [minutes, rest] = timecode.trim().split(':');
  if (minutes === undefined || rest === undefined) {
    throw new Error(`not a timecode: ${timecode}`);
  }
  return Number(minutes) * 60 + Number(rest);
}

/** The layout box of a locator, or a failure that says which one had none. */
export async function boxOf(target: Locator, what: string) {
  const box = await target.boundingBox();
  if (box === null) throw new Error(`${what} has no layout box`);
  return box;
}

/**
 * A pointer drag with intermediate moves, pressing at `anchor` across the target's width.
 * dnd-kit's PointerSensor activates on distance, so a single jump from press to release never
 * crosses it: the gesture has to be made of steps. The anchor matters for a trim — dnd-timeline
 * reads the press position against the item's rect to tell a resize from a move.
 */
export async function dragBy(page: Page, target: Locator, what: string, dx: number, anchor = 0.5) {
  const box = await boxOf(target, what);
  const from = { x: box.x + box.width * anchor, y: box.y + box.height / 2 };
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  for (const step of [0.1, 0.35, 0.6, 0.85, 1]) {
    await page.mouse.move(from.x + dx * step, from.y, { steps: 4 });
  }
  await page.mouse.up();
}

/** What the container says, read with Mediabunny in Node — no decoder needed for any of it. */
export async function containerFacts(path: string) {
  const input = new Input({ source: new FilePathSource(path), formats: ALL_FORMATS });
  try {
    const video = await input.getPrimaryVideoTrack();
    if (video === null) throw new Error('the export has no video track');
    const audio = await input.getPrimaryAudioTrack();
    return {
      duration: await input.computeDuration(),
      width: await video.getDisplayWidth(),
      height: await video.getDisplayHeight(),
      hasAudio: audio !== null,
    };
  } finally {
    input.dispose();
  }
}
