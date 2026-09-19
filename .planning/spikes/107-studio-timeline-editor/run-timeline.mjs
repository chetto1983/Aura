// Q2: drives both timeline shells (dnd-timeline, @xzdarcy/react-timeline-editor) over the same
// VideoFlow project + immer history + DomRenderer preview. Desktop (mouse) and a 390×844 phone
// (isMobile, touch through CDP Input.dispatchTouchEvent). Checks: drag, trim both edges, snap, zoom,
// ruler→preview seek, preview→playhead, one undo step per gesture, preview in step with the model.
// Writes out/timeline/<lab>-<device>.png and out/timeline-report.json. Usage: node run-timeline.mjs
import { mkdirSync, writeFileSync } from 'node:fs';
import { chromium } from 'file:///D:/Repo/Aura/web/node_modules/playwright/index.mjs';

const BASE = 'http://localhost:5207';
mkdirSync('out/timeline', { recursive: true });
const browser = await chromium.launch({ channel: 'chrome' });
const DEVICES = {
  desktop: { viewport: { width: 1280, height: 900 } },
  phone: { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 3 },
};
const snapped = (v) => Math.abs(v * 4 - Math.round(v * 4)) < 1e-6;
const report = [];

for (const lab of ['dnd', 'xz']) {
  for (const [device, options] of Object.entries(DEVICES)) {
    const context = await browser.newContext(options);
    const page = await context.newPage();
    page.setDefaultTimeout(30_000);
    const issues = [];
    page.on('console', (m) => ['error', 'warning'].includes(m.type()) && issues.push(`${m.type()}: ${m.text().slice(0, 200)}`));
    page.on('pageerror', (e) => issues.push(`pageerror: ${e.message.slice(0, 200)}`));
    const cdp = device === 'phone' ? await context.newCDPSession(page) : null;
    await page.goto(`${BASE}/?tab=${lab}`);
    await page.waitForFunction(() => window.__spike?.previewReady && window.__spike?.editor);
    await page.waitForTimeout(500);

    const box = async (name) => {
      const sel = lab === 'xz' ? `.timeline-editor-action:has([data-testid="item-${name}"])` : `[data-testid="item-${name}"]`;
      return page.locator(sel).boundingBox();
    };
    const state = () => page.evaluate(() => window.__spike.editor.state);
    const layerOf = (s, name) => s.layers.find((l) => l.settings.name === name);
    const endOf = (l) => l.settings.startTime + l.settings.sourceDuration;

    async function gesture(from, to) {
      const steps = 12;
      if (!cdp) {
        await page.mouse.move(from.x, from.y);
        await page.mouse.down();
        for (let i = 1; i <= steps; i++) await page.mouse.move(from.x + ((to.x - from.x) * i) / steps, from.y + ((to.y - from.y) * i) / steps);
        await page.mouse.up();
      } else {
        const point = (p) => [{ x: p.x, y: p.y, id: 1, radiusX: 4, radiusY: 4, force: 1 }];
        await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: point(from) });
        for (let i = 1; i <= steps; i++) {
          await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: point({ x: from.x + ((to.x - from.x) * i) / steps, y: from.y + ((to.y - from.y) * i) / steps }) });
          await page.waitForTimeout(16);
        }
        await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
      }
      await page.waitForTimeout(400);
    }
    const tap = async (p) => (cdp ? gesture(p, p) : page.mouse.click(p.x, p.y));

    const r = { lab, device, checks: {} };
    const initial = await state();
    // react-timeline-editor has a fixed px-per-second scale: on a phone the 4 s clip overflows the
    // viewport, so zoom out until it fits (dnd-timeline fits its range to the width by itself).
    const vw = options.viewport.width;
    r.zoomOutsToFit = 0;
    while ((await box('clip')).x + (await box('clip')).width > vw - 8 && r.zoomOutsToFit < 4) {
      await page.getByTestId('zoom-out').click();
      await page.waitForTimeout(200);
      r.zoomOutsToFit++;
    }
    const clip0 = await box('clip');
    const pxPerSec = clip0.width / 4;
    r.pxPerSec = +pxPerSec.toFixed(1);

    // Ruler/time area → preview.
    const rulerSel = lab === 'xz' ? '.timeline-editor-time-area' : '[data-testid="ruler"]';
    const ruler = await page.locator(rulerSel).boundingBox();
    await tap({ x: clip0.x + 2 * pxPerSec, y: ruler.y + ruler.height / 2 });
    await page.waitForTimeout(300);
    const seekFrame = await page.evaluate(() => window.__spike.player.currentFrame);
    r.checks.rulerSeeksPreview = { frame: seekFrame, expected: 48, ok: Math.abs(seekFrame - 48) <= 2 };

    // Preview → playhead.
    await page.evaluate(() => window.__spike.player.seek(72));
    await page.waitForTimeout(300);
    const cursorSec = lab === 'xz'
      ? await page.evaluate(() => window.__spike.xzCursor())
      : ((await page.locator('[data-testid="playhead"]').boundingBox()).x - clip0.x) / pxPerSec;
    r.checks.previewMovesPlayhead = { cursorSec: +cursorSec.toFixed(3), expected: 3, ok: Math.abs(cursorSec - 3) < 0.1 };

    // Play 1 s: the playhead follows the player.
    // play() resolves only when playback ends, so it is started and not awaited.
    await page.evaluate(async () => { await window.__spike.player.seek(0); window.__spike.player.play(); });
    await page.waitForTimeout(1000);
    const during = await page.evaluate(() => ({ t: window.__spike.player.currentTime }));
    const cursorDuring = lab === 'xz'
      ? await page.evaluate(() => window.__spike.xzCursor())
      : ((await page.locator('[data-testid="playhead"]').boundingBox()).x - clip0.x) / pxPerSec;
    await page.evaluate(() => window.__spike.player.stop());
    r.checks.playheadFollowsPlayback = { playerSec: +during.t.toFixed(2), cursorSec: +cursorDuring.toFixed(2), ok: during.t > 0.3 && Math.abs(cursorDuring - during.t) < 0.25 };

    // Drag the title right by ~0.9 s.
    const title = await box('title');
    await gesture({ x: title.x + title.width / 2, y: title.y + title.height / 2 }, { x: title.x + title.width / 2 + 0.9 * pxPerSec, y: title.y + title.height / 2 });
    let s = await state();
    const t = layerOf(s, 'title').settings.startTime;
    r.checks.drag = { startTime: t, snapped: snapped(t), ok: t > 0.4 && t < 1.4 && snapped(t) };

    // Trim the photo's right edge left by ~1.1 s.
    const photo = await box('photo');
    await gesture({ x: photo.x + photo.width - 3, y: photo.y + photo.height / 2 }, { x: photo.x + photo.width - 3 - 1.1 * pxPerSec, y: photo.y + photo.height / 2 });
    s = await state();
    const pEnd = endOf(layerOf(s, 'photo'));
    r.checks.trimEnd = { end: pEnd, snapped: snapped(pEnd), ok: pEnd > 2.4 && pEnd < 3.4 && snapped(pEnd) };

    // Trim the clip's left edge right by ~0.6 s: sourceStart must follow startTime.
    const clip = await box('clip');
    await gesture({ x: clip.x + 3, y: clip.y + clip.height / 2 }, { x: clip.x + 3 + 0.6 * pxPerSec, y: clip.y + clip.height / 2 });
    s = await state();
    const c = layerOf(s, 'clip').settings;
    r.checks.trimStart = { startTime: c.startTime, sourceStart: c.sourceStart, sourceDuration: c.sourceDuration, ok: c.startTime > 0.2 && Math.abs(c.sourceStart - c.startTime) < 1e-9 && Math.abs(c.sourceDuration - (4 - c.startTime)) < 1e-9 };

    // The preview renderer holds the same settings as the model (DomRenderer's private layer map).
    r.checks.previewInStep = await page.evaluate(() => {
      const p = window.__spike.player;
      const rows = window.__spike.editor.state.layers.map((l) => ({ id: l.id, model: l.settings, preview: p.layerById?.get(l.id)?.json.settings }));
      return { ok: rows.every((x) => x.preview && x.preview.startTime === x.model.startTime && x.preview.sourceStart === x.model.sourceStart && x.preview.sourceDuration === x.model.sourceDuration), lastSyncMs: window.__spike.lastSyncMs };
    });

    // Undo/redo through the buttons.
    const edited = await state();
    const depth = await page.evaluate(() => window.__spike.editor.depth);
    const gestures = ['drag', 'trimEnd', 'trimStart'].filter((k) => r.checks[k].ok).length;
    for (let i = 0; i < depth.past; i++) await page.getByTestId('undo').click();
    const undone = await state();
    for (let i = 0; i < depth.past; i++) await page.getByTestId('redo').click();
    const redone = await state();
    r.checks.history = { entries: depth.past, gestures, undoToInitial: JSON.stringify(undone) === JSON.stringify(initial), redoToEdited: JSON.stringify(redone) === JSON.stringify(edited) };
    r.checks.history.ok = depth.past === gestures && r.checks.history.undoToInitial && r.checks.history.redoToEdited;

    // Zoom: the clip gets twice as wide.
    const before = (await box('clip')).width;
    await page.getByTestId('zoom-in').click();
    await page.waitForTimeout(300);
    const after = (await box('clip')).width;
    r.checks.zoom = { before: +before.toFixed(1), after: +after.toFixed(1), ok: Math.abs(after / before - 2) < 0.1 };
    await page.getByTestId('zoom-out').click();

    r.horizontalOverflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
    r.issues = [...new Set(issues)];
    r.pass = Object.values(r.checks).every((x) => x.ok);
    await page.screenshot({ path: `out/timeline/${lab}-${device}.png`, fullPage: true });
    report.push(r);
    console.log(`${lab} ${device}: ${r.pass ? 'PASS' : 'FAIL'}`, Object.entries(r.checks).map(([k, v]) => `${k}=${v.ok ? '✓' : '✗'}`).join(' '), `overflow=${r.horizontalOverflow}`, `issues=${r.issues.length}`);
    for (const [k, v] of Object.entries(r.checks)) if (!v.ok) console.log('   ✗', k, JSON.stringify(v));
    await context.close();
  }
}
writeFileSync('out/timeline-report.json', JSON.stringify(report, null, 2));
await browser.close();
