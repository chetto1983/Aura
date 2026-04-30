import type {
  HealthRollup,
  WikiPageSummary,
  WikiPage,
  Graph,
  SourceSummary,
  SourceDetail,
  Task,
  UploadResponse,
  IngestResponse,
  ReocrResponse,
  UpsertTaskRequest,
} from '@/types/api';

const BASE = '/api';
const TIMEOUT_MS = 8000;

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
    this.name = 'ApiError';
  }
}

async function get<T>(path: string): Promise<T> {
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), TIMEOUT_MS);
  let res: Response;
  try {
    res = await fetch(BASE + path, {
      credentials: 'same-origin',
      signal: ctrl.signal,
    });
  } catch (err) {
    clearTimeout(timer);
    if (err instanceof Error && err.name === 'AbortError') {
      throw new ApiError(0, `request timed out after ${TIMEOUT_MS}ms`);
    }
    throw new ApiError(0, err instanceof Error ? err.message : 'network error');
  }
  clearTimeout(timer);
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    let msg = text.slice(0, 200);
    try {
      const parsed = JSON.parse(text);
      if (parsed && typeof parsed.error === 'string') msg = parsed.error;
    } catch {
      // not JSON; use raw text
    }
    throw new ApiError(res.status, msg || res.statusText);
  }
  return res.json() as Promise<T>;
}

// post sends a JSON or empty body to a write endpoint and parses the
// response. Bypasses the 8s GET timeout because some endpoints (ingest,
// reocr) run OCR which can take minutes. Errors surface as ApiError
// with the server's `error` field when present.
async function post<T>(path: string, body?: unknown): Promise<T> {
  const init: RequestInit = {
    method: 'POST',
    credentials: 'same-origin',
  };
  if (body !== undefined) {
    init.headers = { 'Content-Type': 'application/json' };
    init.body = JSON.stringify(body);
  }
  let res: Response;
  try {
    res = await fetch(BASE + path, init);
  } catch (err) {
    throw new ApiError(0, err instanceof Error ? err.message : 'network error');
  }
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    let msg = text.slice(0, 200);
    try {
      const parsed = JSON.parse(text);
      if (parsed && typeof parsed.error === 'string') msg = parsed.error;
    } catch {
      // not JSON
    }
    throw new ApiError(res.status, msg || res.statusText);
  }
  return res.json() as Promise<T>;
}

function qs(params?: Record<string, string | undefined>): string {
  if (!params) return '';
  const entries = Object.entries(params).filter(([, v]) => v !== undefined && v !== '');
  if (entries.length === 0) return '';
  const sp = new URLSearchParams();
  for (const [k, v] of entries) sp.set(k, v as string);
  return '?' + sp.toString();
}

export const api = {
  health: () => get<HealthRollup>('/health'),
  wikiPages: () => get<WikiPageSummary[]>('/wiki/pages'),
  wikiPage: (slug: string) =>
    get<WikiPage>(`/wiki/page?slug=${encodeURIComponent(slug)}`),
  wikiGraph: () => get<Graph>('/wiki/graph'),
  sources: (q?: { status?: string; kind?: string }) =>
    get<SourceSummary[]>('/sources' + qs(q)),
  source: (id: string) => get<SourceDetail>(`/sources/${id}`),
  sourceOCR: (id: string) =>
    get<{ markdown: string }>(`/sources/${id}/ocr`),
  uploadSource: async (file: File): Promise<UploadResponse> => {
    const fd = new FormData();
    fd.append('file', file, file.name);
    // Multipart uploads can take minutes for OCR; bypass the 8s GET timeout.
    const res = await fetch(BASE + '/sources/upload', {
      method: 'POST',
      body: fd,
      credentials: 'same-origin',
    });
    if (!res.ok) {
      const text = await res.text().catch(() => '');
      let msg = text.slice(0, 200);
      try {
        const parsed = JSON.parse(text);
        if (parsed && typeof parsed.error === 'string') msg = parsed.error;
      } catch {
        // not JSON
      }
      throw new ApiError(res.status, msg || res.statusText);
    }
    return (await res.json()) as UploadResponse;
  },
  tasks: (q?: { status?: string }) =>
    get<Task[]>('/tasks' + qs(q)),
  task: (name: string) => get<Task>(`/tasks/${name}`),

  // ---- write actions (slice 10c, loopback-gated until 10d ships auth) ----
  ingestSource: (id: string) =>
    post<IngestResponse>(`/sources/${id}/ingest`),
  reocrSource: (id: string) =>
    post<ReocrResponse>(`/sources/${id}/reocr`),
  rebuildWikiIndex: () =>
    post<{ ok: boolean }>(`/wiki/index/rebuild`),
  appendWikiLog: (action: string, slug?: string) =>
    post<{ ok: boolean }>(`/wiki/log`, { action, slug }),
  upsertTask: (req: UpsertTaskRequest) =>
    post<Task>(`/tasks`, req),
  cancelTask: (name: string) =>
    post<Task>(`/tasks/${name}/cancel`),
};
