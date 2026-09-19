// Does the browser's own <video> honour the edit list of a copy-trimmed MP4? Plays each output at
// t=0 and screenshots the frame: the burned-in stamp must read 00:00:02.500 f75.
import { readFileSync } from 'node:fs';
import { chromium, firefox } from 'file:///D:/Repo/Aura/web/node_modules/playwright/index.mjs';
const files = ['clip-h264-aac-expand-2.5-6.mp4', 'clip-h264-aac-exact-2.5-6.mp4', 'clip-hevc-aac-expand-2.5-6.mp4'];
for (const channel of ['chrome', 'msedge', 'firefox']) {
  const browser = channel === 'firefox' ? await firefox.launch() : await chromium.launch({ channel });
  const page = await browser.newPage({ viewport: { width: 700, height: 420 } });
  await page.goto('http://localhost:5199/');
  for (const f of files) {
    const b64 = readFileSync(`out/chrome/${f}`).toString('base64');
    const state = await page.evaluate(async (data) => {
      document.body.innerHTML = '<video id="v" muted style="width:640px"></video>';
      const v = document.getElementById('v');
      const bytes = Uint8Array.from(atob(data), (c) => c.charCodeAt(0));
      v.src = URL.createObjectURL(new Blob([bytes], { type: 'video/mp4' }));
      const ok = await new Promise((res) => { v.onloadeddata = () => res(true); v.onerror = () => res(false); });
      if (!ok) return { ok, error: v.error?.message ?? v.error?.code };
      v.currentTime = 0;
      await new Promise((res) => (v.onseeked = res));
      return { ok, duration: v.duration, width: v.videoWidth, height: v.videoHeight };
    }, b64);
    const shot = `out/frames/play-${channel}-${f.replace('.mp4', '')}.png`;
    if (state.ok) await page.locator('#v').screenshot({ path: shot });
    console.log(channel, f, JSON.stringify(state));
  }
  await browser.close();
}
