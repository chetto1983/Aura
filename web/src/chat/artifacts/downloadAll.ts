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

/** One same-origin download. An empty fileName leaves the name to the response's
 *  Content-Disposition, which wins over the attribute anyway. */
export interface DownloadLink {
  readonly href: string;
  readonly fileName: string;
}

/** Sequentially download every link, reporting `(done, total)` progress; no delay
 *  follows the final click. The throttled loop behind every multi-file download. */
export async function downloadLinks(
  links: readonly DownloadLink[],
  onProgress?: (done: number, total: number) => void,
  opts?: DownloadAllOptions,
): Promise<void> {
  const delay = opts?.delayMs ?? 500;
  const total = links.length;
  let done = 0;
  for (const { href, fileName } of links) {
    if (opts?.signal?.aborted) break;
    const link = document.createElement('a');
    link.href = href;
    link.download = fileName;
    document.body.appendChild(link);
    link.click();
    link.remove();
    done += 1;
    onProgress?.(done, total);
    if (done < total) {
      await new Promise((resolve) => setTimeout(resolve, delay));
    }
  }
}

/** Sequentially download every accepted asset, reporting `(done, total)` progress.
 *  Total counts only accepted rows; no delay follows the final click. */
export function downloadAll(
  assets: readonly Asset[],
  onProgress: (done: number, total: number) => void,
  opts?: DownloadAllOptions,
): Promise<void> {
  const links = assets
    .filter((a) => a.status === 'accepted')
    .map((a) => ({
      href: `/api/assets/${encodeURIComponent(a.id)}/download`,
      fileName: a.file_name,
    }));
  return downloadLinks(links, onProgress, opts);
}
