// Real-stack E2E of the live view (prd.md §12): a signed-in operator opens /browser/<session> in
// the cockpit and logs into the fixture site running in their own box, by clicking and typing
// in the cockpit only. The agent's side (open the login page) is played with `docker exec`,
// exactly the command the agent runs through shell_exec.
//
// Not a CI spec: it needs a running `aura serve`, the aura-sandbox image with agent-browser,
// and the seeded Authula operator. Run from web/ with live_view.config.ts (see FINDINGS.md).
import { execFileSync } from 'node:child_process';
import { expect, test } from '@playwright/test';
import { gotoAuthenticated } from '../../web/e2e/auth';

const box = `aura-box-${process.env.AURA_E2E_IDENTITY ?? '00000000-0000-0000-0000-000000000001'}`;
// A fresh session name per run, so a browser left open by an earlier run cannot answer.
const session = `portal-${Date.now().toString(36)}`;
const inBox = (cmd: string) =>
  execFileSync('docker', ['exec', '-w', '/workspace', box, 'sh', '-c', cmd], { encoding: 'utf8' });

// Each open session is its own Chromium, about 142 tasks against the box's 512-pid cap (measured
// 2026-09-26): a run that leaves its browser open starves the next one.
test.afterEach(() => {
  if (!process.env.AURA_E2E_KEEP_SESSION) inBox(`agent-browser --session ${session} --restore close || true`);
});

test('the operator logs into a site through the live view, never through the chat', async ({ page }) => {
  // First visit: the stream route resolves (creates) the box; no browser is open yet.
  await gotoAuthenticated(page, `/browser/${session}`);
  await expect(page.getByRole('status')).toContainText(/no browser open under this name|not running/i);

  // The agent's turn: start the site and open its login page in this run's session.
  execFileSync('docker', ['cp', `${__dirname}/fixture_site.py`, `${box}:/workspace/fixture_site.py`]);
  inBox(
    'curl -sf -o /dev/null http://127.0.0.1:8765/login || { FIXTURE_STATE=/workspace/sessions.json nohup python3 fixture_site.py 8765 >>/workspace/site.log 2>&1 & sleep 1; }',
  );
  const grantedSessions = () =>
    Number(inBox(`python3 -c "import json,os;print(len(json.load(open('sessions.json'))) if os.path.exists('sessions.json') else 0)"`));
  const before = grantedSessions();
  inBox(`agent-browser --session ${session} --restore open http://127.0.0.1:8765/login`);

  await page.reload();
  const frame = page.getByRole('img', { name: /sandbox browser/i });
  await expect(frame).toBeVisible({ timeout: 15_000 });

  // Clicks land where agent-browser says the field is, scaled onto the displayed frame. The
  // field is found by its label: refs are renumbered after every navigation.
  const clickField = async (label: string) => {
    const snapshot = inBox(`agent-browser --session ${session} --restore snapshot -i`);
    const ref = new RegExp(`textbox "${label}" \\[ref=(e\\d+)\\]`).exec(snapshot)?.[1];
    if (!ref) throw new Error(`no "${label}" textbox in:\n${snapshot}`);
    const b = JSON.parse(inBox(`agent-browser --session ${session} --restore get box @${ref} --json`)).data;
    const shown = await frame.boundingBox();
    if (!shown) throw new Error('frame not laid out');
    const scale = shown.width / 1280;
    await page.mouse.click(shown.x + (b.x + b.width / 2) * scale, shown.y + (b.y + b.height / 2) * scale);
  };

  await clickField('Email');
  await page.keyboard.type('alice@example.test', { delay: 20 });
  await clickField('Password');
  await page.keyboard.type('Sp1ke-Passw0rd!x7', { delay: 20 });
  await page.keyboard.press('Enter');
  // The box browser is the source of truth; the cockpit's URL bar must then follow it.
  const boxUrl = () => inBox(`agent-browser --session ${session} --restore get url`).trim();
  await expect.poll(boxUrl, { timeout: 15_000 }).toContain('/otp');
  await expect(page.locator('code')).toContainText('/otp', { timeout: 5_000 });

  // Second factor, typed by the human in the live view like the password.
  const code = inBox('python3 fixture_site.py totp').trim();
  await clickField('Code');
  await page.keyboard.type(code, { delay: 20 });
  await page.keyboard.press('Enter');
  await expect.poll(boxUrl, { timeout: 15_000 }).toContain('/docs');
  await expect(page.locator('code')).toContainText('/docs', { timeout: 5_000 });

  // The site granted exactly one new session, and the agent's own browser now holds it.
  expect(grantedSessions()).toBe(before + 1);
});
