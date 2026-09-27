import { writeFileSync } from 'node:fs';
import { openProbe } from './lib/browser.mjs';

const FULL_WIDTH = 800;

async function handleSizes(page) {
  const sizes = [];
  for (const handle of await page.locator('[data-testid="waveform"] svg ellipse').all()) {
    const b = await handle.boundingBox();
    sizes.push(b === null ? null : `${Math.round(b.width)}x${Math.round(b.height)}`);
  }
  return sizes.join(' ');
}

// Zooms the same item to a `start`–`end` range and reports the redraw that follows it.
async function zoom(page, start, end, shot) {
  await page.evaluate(([from, to]) => window.s2Zoom(from, to), [start, end]);
  await page.waitForTimeout(800);
  const log = await page.evaluate(() => window.s2Log.slice());
  const started = log.filter((e) => e.event === 'zoom').at(-1).at;
  const redraw = log.find((e) => e.event === 'redrawcomplete' && e.at > started);
  await page.screenshot({ path: `out/${shot}-zoom-${start}-${end}.png` });
  return {
    width: redraw?.wrapperWidth ?? null,
    redrawMs: redraw === undefined ? null : Math.round(redraw.at - started),
    handles: await handleSizes(page),
  };
}

async function run(guard, mobile, layout, stroke) {
  const probe = await openProbe(`s2.html?guard=${guard}&layout=${layout}&stroke=${stroke}`, { mobile });
  const { page } = probe;
  try {
    // The first draw that counts is the one at the item's real width.
    await page.waitForFunction(
      (width) => window.s2Log.some((e) => e.event === 'redrawcomplete' && e.wrapperWidth >= width),
      FULL_WIDTH,
      { timeout: 30_000 },
    );
    const handlesAtRest = await handleSizes(page);
    // The second envelope point sits at t=120 of a 300 s item that starts at 20 on a 0–400 range.
    // The envelope draws its points as <ellipse> inside wavesurfer's open shadow root.
    const box = await page.locator('[data-testid="waveform"] svg ellipse').nth(1).boundingBox();
    if (box === null) throw new Error('no envelope point drawn');
    const x = box.x + box.width / 2;
    const y = box.y + box.height / 2;
    await page.mouse.move(x, y);
    await page.mouse.down();
    for (const dy of [4, 8, 12, 16]) await page.mouse.move(x, y + dy, { steps: 3 });
    await page.mouse.up();
    await page.waitForTimeout(300);
    const afterPoint = await page.evaluate(() => window.s2Log.slice());
    // Then a drag on the item body, away from any point: this one MUST move the item.
    const item = await page.locator('[data-testid="item"]').boundingBox();
    const bx = item.x + 30;
    const by = item.y + 6;
    await page.mouse.move(bx, by);
    await page.mouse.down();
    for (const dx of [10, 30, 60, 90]) await page.mouse.move(bx + dx, by, { steps: 3 });
    await page.mouse.up();
    await page.waitForTimeout(300);
    const all = await page.evaluate(() => window.s2Log.slice());
    const shot = `s2-${layout}-${stroke}-${guard}-${mobile ? 'mobile' : 'desktop'}`;
    await page.screenshot({ path: `out/${shot}.png` });
    const zoom200 = await zoom(page, 0, 200, shot);
    const zoom20 = await zoom(page, 100, 120, shot);
    return {
      layout,
      stroke,
      guard,
      mobile,
      renderMs: all.find((e) => e.event === 'redrawcomplete' && e.wrapperWidth >= FULL_WIDTH)?.ms,
      pointDragMovedPoint: afterPoint.some((e) => e.event === 'points-change'),
      pointDragMovedItem: afterPoint.some((e) => e.event === 'item-drag-end'),
      bodyDragMovedItem: all.slice(afterPoint.length).some((e) => e.event === 'item-drag-end'),
      handlesAtRest,
      zoom200: `${zoom200.width}px ${zoom200.redrawMs}ms ${zoom200.handles}`,
      zoom20: `${zoom20.width}px ${zoom20.redrawMs}ms ${zoom20.handles}`,
      trace: all
        .filter((e) => e.event !== 'redrawcomplete' && !e.event.startsWith('geometry'))
        .map((e) => (e.event === 'press' ? `press:${e.on}` : e.event))
        .join(' '),
    };
  } finally {
    await probe.close();
  }
}

const results = [];
for (const mobile of [false, true]) {
  for (const guard of ['none', 'svg', 'point']) results.push(await run(guard, mobile, 'span', 'fixed'));
}
results.push(await run('point', false, 'span', 'raw'));
results.push(await run('point', false, 'content', 'raw'));
writeFileSync(new URL('out/s2.json', import.meta.url), JSON.stringify(results, null, 2));
console.table(results.map(({ trace, ...row }) => row));
for (const r of results) console.log(`${r.guard}/${r.mobile ? 'mobile' : 'desktop'}: ${r.trace}`);
