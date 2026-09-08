import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { WorkerControls } from './WorkerControls';
import { workerControlKey, type WorkerReceipt } from './workerControlApi';
import type { WorkerStatus } from './workerStream';

const conversationId = '22222222-2222-2222-2222-222222222222';
const childId = 'w1-aabbccdd';
const runId = 'run-11111111-1111-1111-1111-111111111111';
const running: WorkerStatus = {
  child_id: childId,
  status: 'running',
  run_id: runId,
  can_steer: true,
  can_cancel: true,
  events: 3,
  duration_sec: 2,
  last_event_at: '2026-09-08T00:00:00Z',
};
let receipts: WorkerReceipt[];
let posts: { url: string; init: RequestInit }[];
let post: () => Promise<Response>;
let clients: QueryClient[];
let queuedState: Record<string, unknown>;

function postedBody(): unknown {
  const body = posts[0]?.init.body;
  if (typeof body !== 'string') throw new Error('Expected a JSON request body');
  return JSON.parse(body) as unknown;
}

function mount(status: WorkerStatus = running) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  clients.push(client);
  const content = (value: WorkerStatus) => (
    <QueryClientProvider client={client}>
      <WorkerControls conversationId={conversationId} childId={childId} status={value} />
    </QueryClientProvider>
  );
  const view = render(content(status));
  return {
    ...view,
    client,
    update: (value: WorkerStatus) => {
      view.rerender(content(value));
    },
  };
}

beforeEach(() => {
  receipts = [];
  posts = [];
  clients = [];
  queuedState = {};
  post = () => Promise.resolve(new Response('{}', { status: 202 }));
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init?: RequestInit) => {
      if (init?.method === 'POST') {
        posts.push({ url, init });
        return post();
      }
      return Promise.resolve(
        new Response(JSON.stringify({ receipts, ...queuedState }), { status: 200 }),
      );
    }),
  );
});

afterEach(() => {
  for (const client of clients) client.clear();
  vi.unstubAllGlobals();
});

describe('worker controls', () => {
  it('stops queued work without a run ID and restores durable acceptance after remount', async () => {
    const queued: WorkerStatus = {
      child_id: childId,
      status: 'queued',
      events: 0,
      duration_sec: 0,
      last_event_at: '',
    };
    const target = { job_id: '44444444-4444-4444-4444-444444444444', attempt_count: 0 };
    queuedState = { queued_target: target };
    post = () => {
      queuedState = { cancel_requested: true };
      return Promise.resolve(new Response('{}', { status: 202 }));
    };
    const view = mount(queued);
    fireEvent.click(await screen.findByRole('button', { name: 'Stop agent' }));
    await screen.findByText('Stop requested. Waiting for the agent to finish.');
    expect(postedBody()).toEqual(target);
    expect(posts[0]?.url).toBe(`/api/conversations/${conversationId}/swarm/${childId}/cancel`);
    expect(screen.queryByRole('textbox')).toBeNull();
    view.unmount();
    mount(queued);
    expect(
      await screen.findByText('Stop requested. Waiting for the agent to finish.'),
    ).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Stop agent' })).toBeNull();
    expect(posts).toHaveLength(1);
  });

  it('drops a queued retry when the worker starts and never redirects it to that run', async () => {
    queuedState = {
      queued_target: { job_id: '44444444-4444-4444-4444-444444444444', attempt_count: 0 },
    };
    post = () => Promise.reject(new TypeError('network cut'));
    const view = mount({
      child_id: childId,
      status: 'queued',
      events: 0,
      duration_sec: 0,
      last_event_at: '',
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Stop agent' }));
    await screen.findByRole('button', { name: 'Retry delivery' });
    view.update(running);
    expect(screen.queryByRole('button', { name: 'Retry delivery' })).toBeNull();
    expect(posts).toHaveLength(1);
    fireEvent.click(screen.getByRole('button', { name: 'Stop agent' }));
    await waitFor(() => {
      expect(posts).toHaveLength(2);
    });
    const body = posts[1]?.init.body;
    if (typeof body !== 'string') throw new Error('Expected JSON request');
    expect(JSON.parse(body)).toEqual({ run_id: runId });
    expect(new Headers(posts[0]?.init.headers).get('Idempotency-Key')).not.toBe(
      new Headers(posts[1]?.init.headers).get('Idempotency-Key'),
    );
  });
  it('shows acceptance separately from application and addresses only the selected execution', async () => {
    post = () => {
      receipts = [
        {
          id: 'receipt-1',
          run_id: runId,
          text: 'Use JSON',
          status: 'accepted',
          created_at: '2026-09-08T00:00:00Z',
        },
      ];
      return Promise.resolve(new Response('{}', { status: 202 }));
    };
    const view = mount();
    fireEvent.change(screen.getByRole('textbox', { name: 'Direct this agent' }), {
      target: { value: 'Use JSON' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Send correction' }));
    expect(await screen.findByText('Received · awaiting application')).toBeTruthy();
    expect(screen.queryByText('Applied')).toBeNull();
    expect(posts[0]?.url).toBe(`/api/conversations/${conversationId}/swarm/${childId}/steer`);
    expect(postedBody()).toEqual({ run_id: runId, text: 'Use JSON' });
    expect(new Headers(posts[0]?.init.headers).get('Idempotency-Key')).toBeTruthy();
    receipts = receipts.map((receipt) => ({ ...receipt, status: 'applied' }));
    await act(async () => {
      await view.client.invalidateQueries({ queryKey: workerControlKey(conversationId, childId) });
    });
    expect(await screen.findByText('Applied')).toBeTruthy();
  });

  it('does not erase text edited while an earlier correction is sending', async () => {
    let resolve: ((response: Response) => void) | undefined;
    post = () =>
      new Promise<Response>((done) => {
        resolve = done;
      });
    mount();
    const input = screen.getByRole('textbox', { name: 'Direct this agent' });
    fireEvent.change(input, { target: { value: 'First correction' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send correction' }));
    fireEvent.change(input, { target: { value: 'Next correction' } });
    await act(async () => {
      resolve?.(new Response('{}', { status: 202 }));
      await Promise.resolve();
    });
    expect((input as HTMLTextAreaElement).value).toBe('Next correction');
  });

  it('aborts observation of an old request and never automatically sends it to a new run', async () => {
    let resolve: ((response: Response) => void) | undefined;
    post = () =>
      new Promise<Response>((done) => {
        resolve = done;
      });
    const view = mount();
    const input = screen.getByRole('textbox', { name: 'Direct this agent' });
    fireEvent.change(input, { target: { value: 'Old correction' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send correction' }));
    view.update({ ...running, run_id: 'run-33333333-3333-3333-3333-333333333333' });
    expect(posts[0]?.init.signal?.aborted).toBe(true);
    fireEvent.change(input, { target: { value: 'New correction' } });
    await act(async () => {
      resolve?.(new Response('{}', { status: 202 }));
      await Promise.resolve();
    });
    expect(posts).toHaveLength(1);
    expect((input as HTMLTextAreaElement).value).toBe('New correction');
    expect(screen.getByRole('button', { name: 'Send correction' }).hasAttribute('disabled')).toBe(
      false,
    );
  });

  it('retries an uncertain transport outcome with the same logical key and body', async () => {
    post = () => Promise.reject(new TypeError('network cut'));
    mount();
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'One logical correction' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send correction' }));
    const retry = await screen.findByRole('button', { name: 'Retry delivery' });
    post = () => Promise.resolve(new Response('{}', { status: 202 }));
    fireEvent.click(retry);
    await waitFor(() => {
      expect(posts).toHaveLength(2);
    });
    expect(new Headers(posts[0]?.init.headers).get('Idempotency-Key')).toBe(
      new Headers(posts[1]?.init.headers).get('Idempotency-Key'),
    );
    expect(posts[0]?.init.body).toBe(posts[1]?.init.body);
  });

  it('requests a stop without prematurely reporting the worker as canceled', async () => {
    mount();
    fireEvent.click(screen.getByRole('button', { name: 'Stop agent' }));
    expect(
      await screen.findByText('Stop requested. Waiting for the agent to finish.'),
    ).toBeTruthy();
    expect(posts[0]?.url).toBe(`/api/conversations/${conversationId}/swarm/${childId}/cancel`);
    expect(postedBody()).toEqual({ run_id: runId });
    expect(new Headers(posts[0]?.init.headers).get('Idempotency-Key')).toBeTruthy();
    expect(screen.queryByText('Canceled')).toBeNull();
  });
});
