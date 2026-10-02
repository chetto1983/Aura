import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import type { Locator, Page } from '@playwright/test';
import { expect, readAsset, test } from './support/assetCleanup';
import { uploadAsset } from './support/assetUpload';
import { meanLevel, readFrames } from './support/frames';
import {
  containerFacts,
  editorOnSeededClip,
  exportTo,
  FIXTURES,
  reopen,
  silentFilm,
  uploadClip,
} from './support/videoStudio';

// video-studio-export.spec.ts — what the Studio's files must never do quietly, each found by the
// render spikes (spikes/video-mcp-render/FINDINGS.md): drop a clip's fade (S1.3), turn a source it
// could not fetch or read into black frames that download as a success (S1.4), fetch a source
// twice (S1.4), or become a searchable document when saved (spec §Project indexing). The file and
// the key are the same at every width, so each is measured once, on the desktop project.

const PHONE =
  'the exported file and the saved key do not depend on the width; the desktop run measures them';

/** Presses Export and answers with what happened first: a download, or `sentence` on screen. */
async function exportOutcome(
  page: Page,
  editor: Locator,
  sentence: string,
): Promise<'downloaded' | 'refused'> {
  const downloaded = page
    .waitForEvent('download', { timeout: 120_000 })
    .then(() => 'downloaded' as const);
  const refused = editor
    .getByText(sentence)
    .waitFor({ timeout: 120_000 })
    .then(() => 'refused' as const);
  await editor.getByRole('button', { name: 'Export', exact: true }).click();
  return await Promise.race([downloaded, refused]);
}

test('a clip that fades out darkens in the exported file, as far as its fade has gone', async ({
  page,
}, info) => {
  test.skip(info.project.name.startsWith('mobile'), PHONE);
  test.setTimeout(10 * 60_000);
  const clip = await uploadClip(page);
  // Three muted 4 s clips of one source: each replays the first frame for frame, so the film at
  // t + 4 s shows the source frame it showed at t. Clip 2 fades out; clip 3 is the control, drawn
  // at a static opacity of 0.4, which the renderer honoured before and after the fix: whatever the
  // decode does to brightness, it does to both.
  const film = silentFilm(clip, 'fade check', 3);
  const editor = await reopen(
    page,
    {
      ...film,
      video: film.video.map((item, index) =>
        index === 1 ? { ...item, fadeOut: true } : index === 2 ? { ...item, opacity: 0.4 } : item,
      ),
    },
    clip,
  );
  await expect(editor.getByRole('button', { name: 'Clip 3' })).toBeVisible({ timeout: 60_000 });

  // The clip's bytes, fetched by the export itself: the context sees a worker's requests too. The
  // Stage already holds them in the page's media cache, so a fixed export reads them from there.
  let fetchedByExport = 0;
  let exporting = false;
  page.context().on('request', (request) => {
    if (
      exporting &&
      request.method() === 'GET' &&
      request.url().includes(`/api/assets/${clip}/download`)
    ) {
      fetchedByExport += 1;
    }
  });
  exporting = true;
  const mp4 = readFileSync(await exportTo(page, editor, info, 'fade.mp4'));
  exporting = false;
  // Before the fade (7.2 s), 0.3 s into its last half second (7.8 s), and the control (11.8 s),
  // each beside its twin.
  const [early, earlyTwin, late, lateTwin, control] = await readFrames(
    page,
    mp4,
    [3.2, 7.2, 3.8, 7.8, 11.8],
  );
  if (
    early === undefined ||
    earlyTwin === undefined ||
    late === undefined ||
    lateTwin === undefined ||
    control === undefined
  ) {
    throw new Error('the export answered fewer frames than it was asked for');
  }
  // The opacity the renderer drew the presented frame at: 1 until 7.5 s, then down to 0 at 8 s.
  const expected = Math.min(1, (8 - lateTwin.mediaTime) / 0.5);
  const measured = {
    beforeFade: meanLevel(earlyTwin) / meanLevel(early),
    inFade: meanLevel(lateTwin) / meanLevel(late),
    control: meanLevel(control) / meanLevel(late),
    expected,
    presentedAt: lateTwin.mediaTime,
    fetchedByExport,
  };
  // Written beside the exported file, so a run with a private `--output` keeps both to read.
  const levels = info.outputPath('fade-levels.json');
  writeFileSync(levels, JSON.stringify(measured, null, 2));
  await info.attach('fade-levels', { contentType: 'application/json', path: levels });

  expect(measured.beforeFade).toBeGreaterThan(0.9);
  expect(measured.control).toBeLessThan(0.7);
  // S1.3's frame at this point of its fade read RGB (233, 100, 90), where 0.4 over black allows
  // 102. Measured against the control, scaled to the fade's own opacity at the presented frame.
  expect(Math.abs(measured.inFade - (measured.control * expected) / 0.4)).toBeLessThan(0.1);
  // S1.4 measured two GETs per source: VideoFlow's own, then the pre-decode's.
  expect(fetchedByExport).toBe(0);
});

test('a source the export cannot fetch stops it by name, and nothing downloads', async ({
  page,
}, info) => {
  test.skip(info.project.name.startsWith('mobile'), PHONE);
  test.setTimeout(5 * 60_000);
  const clip = await uploadClip(page);
  const lost = await uploadAsset(page, resolve(FIXTURES, 'clip-b.mp4'), 'clip-b.mp4', 'video/mp4', {
    use: 'media',
  });
  const film = silentFilm(clip, 'lost source check');
  // A Playwright stub, not the server: the library still lists the second clip and its row is
  // complete, so the editor opens and the export is allowed to start (projectStore `sourceIsGone`
  // reads the row, not the bytes); only the bytes stop answering.
  let refused = 0;
  await page.route(`**/api/assets/${lost}/download`, (route) => {
    refused += 1;
    return route.fulfill({ status: 404, body: '' });
  });
  const editor = await reopen(
    page,
    {
      ...film,
      sources: [
        ...film.sources,
        {
          id: 'src-b',
          assetId: lost,
          kind: 'video',
          duration: 4,
          size: { width: 320, height: 180 },
          hasAudio: true,
        },
      ],
      video: film.video.map((item, index) => (index === 1 ? { ...item, sourceId: 'src-b' } : item)),
    },
    clip,
  );
  await expect(editor.getByRole('button', { name: 'Clip 2' })).toBeVisible({ timeout: 60_000 });

  // S1.4 measured the old answer: a black, silent MP4 in 4.9 s, reported as a success. The
  // sentence names the file as the library lists it, read from the asset row.
  const outcome = await exportOutcome(
    page,
    editor,
    'The export stopped: clip-b.mp4 could not be fetched, so no file was written.',
  );
  // The stub really answered: the sentence is about the bytes it refused, not a route it missed.
  expect(refused).toBeGreaterThan(0);
  // Kept beside the run for a person to look at: the sentence where the operator reads it.
  await page.screenshot({ path: info.outputPath('lost-source.png') });
  expect(outcome).toBe('refused');
});

test('a source the renderer cannot read stops the export by name, and nothing downloads', async ({
  page,
}, info) => {
  test.skip(info.project.name.startsWith('mobile'), PHONE);
  test.setTimeout(5 * 60_000);
  const clip = await uploadClip(page);
  // MPEG-4 Part 2 in an MP4: it parses, and no browser holds a decoder for it
  // (video-studio.spec.ts proves the editor refuses it at the picker). Uploaded past the picker,
  // the way an agent's project can name any asset.
  const path = resolve(FIXTURES, 'unplayable.mp4');
  const facts = await containerFacts(path);
  const unplayable = await uploadAsset(page, path, 'unplayable.mp4', 'video/mp4', { use: 'media' });
  const film = silentFilm(clip, 'undecodable source check');
  const editor = await reopen(
    page,
    {
      ...film,
      sources: [
        ...film.sources,
        {
          id: 'src-u',
          assetId: unplayable,
          kind: 'video',
          duration: facts.duration,
          size: { width: facts.width, height: facts.height },
          hasAudio: facts.hasAudio,
        },
      ],
      video: film.video.map((item, index) =>
        index === 1 ? { ...item, sourceId: 'src-u', duration: facts.duration } : item,
      ),
    },
    clip,
  );
  await expect(editor.getByRole('button', { name: 'Clip 2' })).toBeVisible({ timeout: 60_000 });

  const outcome = await exportOutcome(
    page,
    editor,
    'The export stopped: the renderer could not read unplayable.mp4, so no file was written.',
  );
  await page.screenshot({ path: info.outputPath('undecodable-source.png') });
  expect(outcome).toBe('refused');
});

test('a saved project keeps its Studio suffix in the store, where the ingest skips it', async ({
  page,
}, info) => {
  test.skip(info.project.name.startsWith('mobile'), PHONE);
  const editor = await editorOnSeededClip(page, 'saved project check');

  await editor.getByRole('button', { name: 'Save the project' }).click();
  await expect(editor.getByText('Project saved.')).toBeVisible({ timeout: 60_000 });

  const saved = await page.evaluate(() =>
    window.localStorage.getItem('aura.videoStudio.lastSavedProject'),
  );
  if (saved === null) throw new Error('the save remembered no project');
  // The key is all the ingest's path matcher sees (services/ingest/source.py): the suffix has to
  // be in it, and the name — which rides in the object's metadata — must not. Its uuid is the one
  // Presign minted for the key, never the asset's own id (internal/assets/service.go `Presign`).
  expect((await readAsset(page, saved)).object_key).toMatch(
    /^chat\/[0-9a-f-]{36}\.aura-video\.json$/,
  );
});
