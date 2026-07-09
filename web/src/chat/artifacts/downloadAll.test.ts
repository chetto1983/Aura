import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { MockInstance } from 'vitest';
import type { Asset, AssetStatus } from '../attachments/types';
import { downloadAll } from './downloadAll';

// downloadAll — the sequential, throttled "Scarica tutto" loop (D-13/WEBART-06).
// Pure DOM logic driven with fake timers + a spy on HTMLAnchorElement.click, so the
// sequencing / throttle / skip-degraded / abort behaviour is pinned exactly (Stryker
// mutation target). The negative assertion proves only the asset id ever reaches the
// href — never a host/container path or a storage key (T-37B-06).

function asset(id: string, file_name: string, status: AssetStatus = 'accepted'): Asset {
  return {
    id,
    file_name,
    status,
    modality: 'document',
    mime_type: 'application/octet-stream',
    declared_size_bytes: 0,
    size_bytes: 0,
  };
}

let links: { href: string; download: string }[];
let clickSpy: MockInstance<() => void>;

beforeEach(() => {
  vi.useFakeTimers();
  links = [];
  clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
    this: HTMLAnchorElement,
  ) {
    links.push({
      href: this.getAttribute('href') ?? '',
      download: this.getAttribute('download') ?? '',
    });
  });
});

afterEach(() => {
  clickSpy.mockRestore();
  vi.useRealTimers();
});

describe('downloadAll', () => {
  it('downloads each accepted asset once, throttled ~500ms with no trailing delay', async () => {
    const onProgress = vi.fn();
    const p = downloadAll(
      [asset('a1', 'f1.pdf'), asset('a2', 'f2.csv'), asset('a3', 'f3.png')],
      onProgress,
    );
    // First iteration runs synchronously up to the first await.
    expect(links).toHaveLength(1);
    // No second click until the ~500ms throttle elapses.
    await vi.advanceTimersByTimeAsync(499);
    expect(links).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(links).toHaveLength(2);
    // Third (last) click, then resolve with NO trailing timer scheduled.
    await vi.advanceTimersByTimeAsync(500);
    await p;
    expect(links).toHaveLength(3);
    expect(vi.getTimerCount()).toBe(0);
    expect(links.map((l) => l.href)).toEqual([
      '/api/assets/a1/download',
      '/api/assets/a2/download',
      '/api/assets/a3/download',
    ]);
    expect(links.map((l) => l.download)).toEqual(['f1.pdf', 'f2.csv', 'f3.png']);
    expect(onProgress.mock.calls).toEqual([
      [1, 3],
      [2, 3],
      [3, 3],
    ]);
  });

  it('skips degraded (non-accepted) rows and excludes them from the total', async () => {
    const onProgress = vi.fn();
    const p = downloadAll(
      [asset('a1', 'f1.pdf'), asset('bad', 'f2.pdf', 'failed'), asset('a3', 'f3.pdf')],
      onProgress,
    );
    expect(links).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(500);
    await p;
    expect(links.map((l) => l.href)).toEqual([
      '/api/assets/a1/download',
      '/api/assets/a3/download',
    ]);
    expect(onProgress.mock.calls).toEqual([
      [1, 2],
      [2, 2],
    ]);
  });

  it('performs no downloads when the signal is already aborted', async () => {
    const onProgress = vi.fn();
    const controller = new AbortController();
    controller.abort();
    await downloadAll([asset('a1', 'f1.pdf'), asset('a2', 'f2.pdf')], onProgress, {
      signal: controller.signal,
    });
    expect(links).toHaveLength(0);
    expect(onProgress).not.toHaveBeenCalled();
  });

  it('stops early when the signal aborts mid-run', async () => {
    const onProgress = vi.fn();
    const controller = new AbortController();
    const p = downloadAll(
      [asset('a1', 'f1.pdf'), asset('a2', 'f2.pdf'), asset('a3', 'f3.pdf')],
      onProgress,
      { signal: controller.signal },
    );
    expect(links).toHaveLength(1);
    controller.abort();
    await vi.advanceTimersByTimeAsync(500);
    await p;
    expect(links).toHaveLength(1);
    expect(onProgress.mock.calls).toEqual([[1, 3]]);
  });

  it('honors a custom delayMs between downloads', async () => {
    const onProgress = vi.fn();
    const p = downloadAll([asset('a1', 'f1.pdf'), asset('a2', 'f2.pdf')], onProgress, {
      delayMs: 1000,
    });
    expect(links).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(999);
    expect(links).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(1);
    await p;
    expect(links).toHaveLength(2);
  });

  it('never emits a host path, bucket, or object key in the href — only the asset id', async () => {
    const onProgress = vi.fn();
    const p = downloadAll([asset('id-42', 'report.pdf')], onProgress);
    await p;
    const href = links[0]?.href ?? '';
    expect(href).toBe('/api/assets/id-42/download');
    expect(href).not.toMatch(/object_key|object_bucket/);
    expect(href).not.toMatch(/^[a-z]+:\/\//i); // no absolute scheme (http/https/file)
    expect(href).not.toMatch(/\/run\/|[A-Za-z]:\\/); // no container/host path
  });
});
