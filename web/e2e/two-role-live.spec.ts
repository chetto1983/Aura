import { randomUUID } from 'node:crypto';
import { expect, test } from '@playwright/test';
import { gotoAuthenticated } from './auth';
import {
  createIdentity,
  getJSON,
  identities,
  removeIdentity,
  runTurn,
  setSpendingCap,
  signInAs,
  skipFirstRunSetup,
} from './identities';
import { sameOriginFetch } from './live';

// The closing witness phase 02 asked for (.planning/phases/02-two-roles-and-a-budget/02-10-PLAN.md,
// RBAC-03 and RBAC-10): a member holds exactly the four capabilities every identity is provisioned
// with — agent.run, governance.read, governance.write, share.public — is refused the two it does
// not hold, and reads those refusals back out of its OWN audit feed. The grant is proven by USING
// it, not by reading the table it is stored in.
//
// Gated on an installed, set-up Aura: AURA_E2E_LIVE_TWO_ROLE=1. The member is created, credited,
// driven and removed inside the run, so the deployment is left as it was found.

const runLive = process.env.AURA_E2E_LIVE_TWO_ROLE === '1';
const memberCap = process.env.AURA_E2E_MEMBER_CAP_USD ?? '0.50';
const SHELL_WITNESS = 'AURA_E2E_SHELL_OK';

interface AuditEvent {
  readonly source: string;
  readonly action: string;
  readonly target: string;
  readonly detail?: string;
}

test.describe('live two-role capabilities', () => {
  test.skip(!runLive, 'set AURA_E2E_LIVE_TWO_ROLE=1 against an installed, set-up Aura');

  test('a member works, is refused the admin pair, and reads its own refusals', async ({
    page,
    browser,
  }, testInfo) => {
    test.skip(testInfo.project.name !== 'chrome', 'one live account witness is sufficient');
    test.setTimeout(900_000);
    const baseURL = String(testInfo.project.use.baseURL);

    await gotoAuthenticated(page, '/');
    const admin = await getJSON<{ identity_id: string }>(page, '/api/me');
    const email = `e2e-role-${String(Date.now())}@example.com`;
    const password = `E2e-${randomUUID()}`;
    await createIdentity(page, email, password);
    const member = (await identities(page)).find((identity) => identity.name === email);
    if (member === undefined) throw new Error(`${email} is not in the roster`);

    try {
      // A member is minted at a zero cap, so its first turn would be refused before the model.
      // The capability under test here is agent.run, not the budget, so it is credited first.
      await setSpendingCap(page, email, memberCap);
      const memberPage = await signInAs(browser, baseURL, email, password);
      await skipFirstRunSetup(memberPage);

      // governance.write — the cockpit hides the Governance surface from a non-admin
      // (web/src/shell/modes.ts), but the grant is real: the member writes a skill into its own
      // root through the same route the board posts to, and the board then lists it.
      const skill = `e2e-member-skill-${String(Date.now())}`;
      const written = await sameOriginFetch(memberPage, '/api/governance/skills', {
        method: 'POST',
        body: JSON.stringify({
          name: skill,
          description: 'Written by a member during the two-role live witness.',
          body: `# ${skill}\n\nThis skill exists to prove a member may write one. It asks for nothing.\n`,
          always: false,
        }),
      });
      expect(written.status, written.text).toBe(200);
      const listed = await getJSON<{ skills?: { name: string }[] }>(
        memberPage,
        '/api/governance/skills',
      );
      expect((listed.skills ?? []).map((entry) => entry.name)).toContain(skill);

      // agent.run — a real turn in the member's own box, running a real tool. The model decides
      // how to answer, so the run is retried until the witness string comes back.
      await expect(async () => {
        const frames = await runTurn(
          memberPage,
          `Esegui nella shell il comando \`echo ${SHELL_WITNESS}\` e riporta l'output esatto.`,
        );
        expect(frames.some((frame) => frame.type === 'RUN_ERROR')).toBe(false);
        expect(JSON.stringify(frames)).toContain(SHELL_WITNESS);
      }).toPass({ timeout: 420_000, intervals: [15_000] });

      // The two it does not hold. The capability gate answers before any body is read, so what
      // is refused here is the capability and not the shape of the request.
      const creation = await sameOriginFetch(memberPage, '/api/onboarding/start', {
        method: 'POST',
        body: JSON.stringify({ email: `never-created-${String(Date.now())}@example.com` }),
      });
      expect(creation.status, creation.text).toBe(403);
      const removal = await sameOriginFetch(
        memberPage,
        `/api/admin/identities/${admin.identity_id}`,
        { method: 'DELETE' },
      );
      expect(removal.status, removal.text).toBe(403);

      // RBAC-10: the member reads ITS OWN refusals, by its own query — not an inspection of the
      // database. The denial row is written on the refusal path with its own bounded timeout, so
      // the feed is polled rather than read once.
      await expect(async () => {
        const feed = await getJSON<{ events?: AuditEvent[] }>(
          memberPage,
          `/api/admin/audit?identity=${member.id}&limit=100`,
        );
        const refused = (feed.events ?? []).filter((event) => event.source === 'capability');
        expect(refused.map((event) => event.target)).toEqual(
          expect.arrayContaining(['identity.create', 'identity.delete']),
        );
      }).toPass({ timeout: 60_000, intervals: [5_000] });

      await memberPage.context().close();
    } finally {
      await removeIdentity(page, email);
    }
    expect((await identities(page)).some((identity) => identity.name === email)).toBe(false);
  });
});
