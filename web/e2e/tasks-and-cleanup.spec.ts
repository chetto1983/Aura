import { test, expect } from './fixtures';

/**
 * Slice 14 follow-up: task delete, recurring (every_minutes), and
 * conversation cleanup. The slices were driven by user reports:
 *   - "/tasks can not delete task"
 *   - "/conversations db will be full with no control"
 *   - "can not schedule recurrent task"
 *
 * Tests run end-to-end against the live install — they create real
 * test rows then immediately delete them so the running bot is left
 * in the same state as before.
 */

const TOKEN = process.env.AURA_E2E_TOKEN!;
const HEADERS = { Authorization: `Bearer ${TOKEN}` };

test.describe('task delete + recurrence (14)', () => {
  const TEST_NAME = `e2e-recur-${Date.now()}`;

  test('POST /api/tasks accepts every_minutes', async ({ request }) => {
    const res = await request.post('/api/tasks', {
      headers: HEADERS,
      data: { name: TEST_NAME, kind: 'wiki_maintenance', every_minutes: 60 },
    });
    expect(res.status()).toBe(200);
    const body = await res.json();
    expect(body.schedule_kind).toBe('every');
    expect(body.schedule_every_minutes).toBe(60);
  });

  test('POST /api/tasks/{name}/delete hard-removes the row', async ({ request }) => {
    const del = await request.post(`/api/tasks/${TEST_NAME}/delete`, { headers: HEADERS });
    expect(del.status()).toBe(200);
    const body = await del.json();
    expect(body.deleted).toBe(true);

    // Row should be gone — second delete returns 404 (not 200, not 500).
    const second = await request.post(`/api/tasks/${TEST_NAME}/delete`, { headers: HEADERS });
    expect(second.status()).toBe(404);
  });

  test('rejects every_minutes < 1', async ({ request }) => {
    const res = await request.post('/api/tasks', {
      headers: HEADERS,
      data: { name: 'e2e-bad-interval', kind: 'wiki_maintenance', every_minutes: 0 },
    });
    expect(res.status()).toBe(400);
  });

  test('rejects multiple schedule selectors', async ({ request }) => {
    const res = await request.post('/api/tasks', {
      headers: HEADERS,
      data: {
        name: 'e2e-conflict',
        kind: 'wiki_maintenance',
        every_minutes: 60,
        daily: '03:00',
      },
    });
    expect(res.status()).toBe(400);
  });

  test('TasksPanel renders the "Every N minutes" radio in the New Task dialog', async ({ authedPage: page }) => {
    await page.goto('/tasks');
    await page.getByRole('button', { name: /new task/i }).click();
    await expect(page.getByLabel(/every n minutes/i)).toBeVisible({ timeout: 5_000 });
    await expect(page.getByLabel(/daily at hh:mm/i)).toBeVisible();
    await expect(page.getByLabel(/once at/i)).toBeVisible();
  });
});

test.describe('conversation cleanup (14)', () => {
  test('GET /api/conversations/stats returns row count', async ({ request }) => {
    const res = await request.get('/api/conversations/stats', { headers: HEADERS });
    expect(res.status()).toBe(200);
    const body = await res.json();
    expect(typeof body.total_rows).toBe('number');
    expect(typeof body.distinct_chats).toBe('number');
  });

  test('POST /api/conversations/cleanup rejects no selector', async ({ request }) => {
    const res = await request.post('/api/conversations/cleanup', { headers: HEADERS });
    expect(res.status()).toBe(400);
  });

  test('POST /api/conversations/cleanup rejects multiple selectors', async ({ request }) => {
    const res = await request.post('/api/conversations/cleanup?chat_id=1&all=true', {
      headers: HEADERS,
    });
    expect(res.status()).toBe(400);
  });

  test('older_than_days requires a positive integer', async ({ request }) => {
    const res = await request.post('/api/conversations/cleanup?older_than_days=0', {
      headers: HEADERS,
    });
    expect(res.status()).toBe(400);
  });

  test('older_than_days=99999 is a no-op when the archive is fresh', async ({ request }) => {
    // Archive only has recent test data — purging anything older than
    // 99999 days deletes 0 rows but should still return 200.
    const res = await request.post('/api/conversations/cleanup?older_than_days=99999', {
      headers: HEADERS,
    });
    expect(res.status()).toBe(200);
    const body = await res.json();
    expect(body.ok).toBe(true);
    expect(body.deleted).toBe(0);
  });

  test('ConversationsPanel shows the cleanup toolbar', async ({ authedPage: page }) => {
    await page.goto('/conversations');
    await expect(page.getByRole('button', { name: /purge older than/i })).toBeVisible({ timeout: 5_000 });
    await expect(page.getByRole('button', { name: /wipe all/i })).toBeVisible();
  });
});
