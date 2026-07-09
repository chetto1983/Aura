import type { Asset } from '../attachments/types';

// downloadAll — the "Scarica tutto" control (D-13/WEBART-06). A sequential,
// throttled loop of same-origin `<a download>` clicks: each accepted asset is
// fetched through the 37A-proven auth route GET /api/assets/{id}/download, so the
// session cookie rides the request and the server streams the forced attachment.
// The ~500ms inter-click delay avoids Chromium's multi-download burst block;
// degraded (non-accepted) rows are skipped (D-18); the run is abortable. Only the
// asset id ever reaches the href — never a host/container path or a storage key.

export interface DownloadAllOptions {
  /** Delay between clicks (ms). Default 500 — inside the 400–600ms burst-safe band. */
  readonly delayMs?: number;
  /** Aborts the loop before the next click (button-disable / thread-switch). */
  readonly signal?: AbortSignal;
}

/** Sequentially download every accepted asset, reporting `(done, total)` progress.
 *  Total counts only accepted rows; no delay follows the final click. */
export async function downloadAll(
  assets: readonly Asset[],
  onProgress: (done: number, total: number) => void,
  opts?: DownloadAllOptions,
): Promise<void> {
  const delay = opts?.delayMs ?? 500;
  const rows = assets.filter((a) => a.status === 'accepted');
  const total = rows.length;
  let done = 0;
  for (const a of rows) {
    if (opts?.signal?.aborted) break;
    const link = document.createElement('a');
    link.href = `/api/assets/${encodeURIComponent(a.id)}/download`;
    link.download = a.file_name;
    document.body.appendChild(link);
    link.click();
    link.remove();
    done += 1;
    onProgress(done, total);
    if (done < total) {
      await new Promise((resolve) => setTimeout(resolve, delay));
    }
  }
}
