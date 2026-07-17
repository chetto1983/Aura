import { setTimeout as delay } from 'node:timers/promises';
import { expect, test, type Request, type Route } from '@playwright/test';
import { gotoAuthenticated } from './auth';

const live = process.env.AURA_E2E_LIVE_CATALOG === '1';
const catalogPattern = '**/api/governance/skills/catalog?*';

interface CatalogRequestRecord {
  query: string;
  startedAt: number;
  status?: number;
  failed?: string;
}

interface HTTPProblem {
  url: string;
  status: number;
}

function json(body: unknown, status = 200) {
  return { status, contentType: 'application/json', body: JSON.stringify(body) };
}

function compactInstallCount(installs: number): string {
  const safe = Math.max(0, installs);
  if (safe < 1000) return String(safe);
  const divisor = safe >= 1_000_000 ? 1_000_000 : 1000;
  const suffix = divisor === 1_000_000 ? 'M' : 'K';
  const value = safe / divisor;
  const scale = value >= 100 ? 1 : 10;
  return `${String(Math.round(value * scale) / scale)}${suffix}`;
}

function catalogQuery(url: string): string {
  return new URL(url).searchParams.get('q') ?? '';
}

test.describe('live skill catalog performance score', () => {
  test.skip(!live, 'set AURA_E2E_LIVE_CATALOG=1 against a rebuilt Aura');

  test('passes the approved rubric at 10/10', async ({ page, request }, testInfo) => {
    test.setTimeout(45_000);
    const passed = new Set<number>();
    const catalogRequests: CatalogRequestRecord[] = [];
    const requestRecords = new Map<Request, CatalogRequestRecord>();
    const consoleProblems: string[] = [];
    const pageProblems: string[] = [];
    const sameOriginFailures: HTTPProblem[] = [];
    const failedRequests: { url: string; error: string }[] = [];
    let coldMs = 0;
    let warmMs = 0;
    let rapidDocxRequestCount = 0;

    page.on('console', (message) => {
      if (message.type() !== 'error') return;
      const location = message.location().url;
      consoleProblems.push(`${message.text()} ${location}`.trim());
    });
    page.on('pageerror', (error) => pageProblems.push(error.message));
    page.on('request', (browserRequest) => {
      const url = new URL(browserRequest.url());
      if (url.pathname !== '/api/governance/skills/catalog') return;
      const record = {
        query: url.searchParams.get('q') ?? '',
        startedAt: Date.now(),
      };
      catalogRequests.push(record);
      requestRecords.set(browserRequest, record);
    });
    page.on('response', (response) => {
      const record = requestRecords.get(response.request());
      if (record !== undefined) record.status = response.status();
      if (response.status() >= 400) {
        sameOriginFailures.push({ url: response.url(), status: response.status() });
      }
    });
    page.on('requestfailed', (browserRequest) => {
      const error = browserRequest.failure()?.errorText ?? 'request failed';
      const record = requestRecords.get(browserRequest);
      if (record !== undefined) record.failed = error;
      failedRequests.push({ url: browserRequest.url(), error });
    });

    let panel = page.locator('section').first();
    let search = page.getByPlaceholder('Search the skills.sh catalog');

    await test.step('1/10 opens the skill install catalog', async () => {
      await gotoAuthenticated(page, '/');
      await page.getByRole('button', { name: 'Governance', exact: true }).first().click();
      await page.getByRole('tab', { name: 'Skills' }).click();
      await page.getByRole('button', { name: 'Install skill' }).click();
      const heading = page.getByRole('heading', { name: 'Install skill' });
      await expect(heading).toBeVisible();
      panel = page.locator('section').filter({ has: heading }).last();
      search = panel.getByPlaceholder('Search the skills.sh catalog');
      await expect(search).toBeVisible();
      passed.add(1);
    });

    await test.step('2/10 one character emits no request', async () => {
      const before = catalogRequests.length;
      await search.fill('d');
      await delay(350);
      expect(catalogRequests.slice(before)).toHaveLength(0);
      await expect(panel.getByText('Type at least 2 characters.')).toBeVisible();
      passed.add(2);
    });

    let docxResult = panel.getByRole('button', { name: /anthropics\/skills@docx/ }).first();
    await test.step('3/10 rapid docx emits one final request', async () => {
      await search.fill('');
      const before = catalogRequests.length;
      await search.pressSequentially('doc', { delay: 50 });
      const responsePromise = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/api/governance/skills/catalog' &&
          catalogQuery(response.url()) === 'docx',
      );
      await search.press('x');
      const finalKeystrokeAt = Date.now();

      await test.step('4/10 loading status is accessible', async () => {
        await expect(
          panel.getByRole('status').filter({ hasText: 'Searching skills' }),
        ).toBeVisible();
        passed.add(4);
      });

      await test.step('5/10 cold result is under 1.5 seconds', async () => {
        docxResult = panel.getByRole('button', { name: /anthropics\/skills@docx/ }).first();
        await expect(docxResult).toBeVisible({ timeout: 1500 });
        coldMs = Date.now() - finalKeystrokeAt;
        expect(coldMs).toBeLessThan(1500);
        passed.add(5);
      });

      const response = await responsePromise;
      expect(response.status()).toBe(200);
      const emitted = catalogRequests.slice(before);
      rapidDocxRequestCount = emitted.length;
      expect(emitted).toHaveLength(1);
      expect(emitted[0]?.query).toBe('docx');
      passed.add(3);
    });

    await test.step('6/10 result matches the current official catalog', async () => {
      const officialResponse = await request.get('https://skills.sh/api/search?q=docx', {
        failOnStatusCode: false,
      });
      expect(officialResponse.ok()).toBe(true);
      const official = (await officialResponse.json()) as {
        skills?: { source?: string; skillId?: string; installs?: number }[];
      };
      const top = official.skills?.[0];
      expect(top?.source).toBe('anthropics/skills');
      expect(top?.skillId).toBe('docx');
      const expectedCount = compactInstallCount(top?.installs ?? -1);
      await expect(docxResult).toContainText(expectedCount);
      passed.add(6);
    });

    await test.step('7/10 selecting the hit fills the installable source', async () => {
      await docxResult.click();
      await expect(panel.getByLabel('Source')).toHaveValue('anthropics/skills@docx');
      passed.add(7);
    });

    await test.step('8/10 an abandoned request cannot render stale rows', async () => {
      const staleHandler = async (route: Route) => {
        const query = catalogQuery(route.request().url());
        if (query === 'slow') {
          await delay(700);
          await route
            .fulfill(
              json({
                enabled: true,
                query,
                hits: [{ source: 'owner/repo', skill: 'slow-only', installs: '1' }],
              }),
            )
            .catch(() => undefined);
          return;
        }
        if (query === 'latest') {
          await route.fulfill(
            json({
              enabled: true,
              query,
              hits: [{ source: 'owner/repo', skill: 'latest-only', installs: '2' }],
            }),
          );
          return;
        }
        await route.fallback();
      };
      await page.route(catalogPattern, staleHandler);
      const slowRequest = page.waitForRequest(
        (browserRequest) => catalogQuery(browserRequest.url()) === 'slow',
      );
      await search.fill('slow');
      await slowRequest;
      const latestRequest = page.waitForRequest(
        (browserRequest) => catalogQuery(browserRequest.url()) === 'latest',
      );
      await search.fill('latest');
      await latestRequest;
      await expect(panel.getByRole('button', { name: /owner\/repo@latest-only/ })).toBeVisible();
      await delay(800);
      await expect(panel.getByRole('button', { name: /owner\/repo@slow-only/ })).toHaveCount(0);
      await page.unroute(catalogPattern, staleHandler);
      passed.add(8);
    });

    await test.step('9/10 a 502 is visible and the next query recovers', async () => {
      let failedOnce = false;
      const errorHandler = async (route: Route) => {
        if (catalogQuery(route.request().url()) === 'fail' && !failedOnce) {
          failedOnce = true;
          await route.fulfill(json({ error: 'forced live rubric failure' }, 502));
          return;
        }
        await route.fallback();
      };
      await page.route(catalogPattern, errorHandler);
      await search.fill('fail');
      await expect(panel.getByText("Couldn't search the skills catalog. Try again.")).toBeVisible();
      await page.unroute(catalogPattern, errorHandler);
      await search.fill('xlsx');
      await expect(
        panel.getByRole('button', { name: /anthropics\/skills@xlsx/ }).first(),
      ).toBeVisible({ timeout: 2500 });
      await expect(panel.getByText("Couldn't search the skills catalog. Try again.")).toHaveCount(
        0,
      );
      passed.add(9);
    });

    await test.step('10/10 warm response is under 500ms and browser health is clean', async () => {
      await search.fill('');
      const warmResponse = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/api/governance/skills/catalog' &&
          catalogQuery(response.url()) === 'docx',
      );
      await search.fill('docx');
      const finalInputAt = Date.now();
      const response = await warmResponse;
      warmMs = Date.now() - finalInputAt;
      expect(response.status()).toBe(200);
      expect(warmMs).toBeLessThan(500);
      docxResult = panel.getByRole('button', { name: /anthropics\/skills@docx/ }).first();
      await expect(docxResult).toBeVisible();
      await docxResult.click();
      await expect(panel.getByRole('button', { name: /^Install$/ })).toBeEnabled();

      const appOrigin = new URL(page.url()).origin;
      const unexpectedHTTP = sameOriginFailures.filter((problem) => {
        const url = new URL(problem.url);
        if (url.origin !== appOrigin) return false;
        return !(
          url.pathname === '/api/governance/skills/catalog' &&
          url.searchParams.get('q') === 'fail' &&
          problem.status === 502
        );
      });
      const unexpectedFailed = failedRequests.filter((problem) => {
        const url = new URL(problem.url);
        return !(
          url.origin === appOrigin &&
          url.pathname === '/api/governance/skills/catalog' &&
          url.searchParams.get('q') === 'slow'
        );
      });
      const unexpectedConsole = consoleProblems.filter(
        (problem) =>
          !(problem.includes('/api/governance/skills/catalog') && problem.includes('502')),
      );
      expect(unexpectedHTTP).toEqual([]);
      expect(unexpectedFailed).toEqual([]);
      expect(unexpectedConsole).toEqual([]);
      expect(pageProblems).toEqual([]);
      passed.add(10);
    });

    expect([...passed].sort((a, b) => a - b)).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9, 10]);
    const evidence = {
      score: `${String(passed.size)}/10`,
      coldMs,
      warmMs,
      rapidDocxRequestCount,
      requests: catalogRequests,
      consoleProblems,
      pageProblems,
      sameOriginFailures,
      failedRequests,
    };
    await testInfo.attach('skill-catalog-score.json', {
      contentType: 'application/json',
      body: Buffer.from(JSON.stringify(evidence, null, 2)),
    });
    testInfo.annotations.push({ type: 'score', description: `${String(passed.size)}/10` });
  });
});
