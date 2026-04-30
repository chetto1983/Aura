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
  WhoamiResponse,
  SkillSummary,
  SkillDetail,
  MCPServerSummary,
  SkillCatalogItem,
  SkillInstallRequest,
  SkillInstallResponse,
  SkillDeleteResponse,
} from '@/types/api';
import { getToken, clearToken } from '@/lib/auth';

const BASE = '/api';
const TIMEOUT_MS = 8000;

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
    this.name = 'ApiError';
  }
}

// authHeaders attaches the bearer token, when present, to every outbound
// request. The login flow is the only path that doesn't go through this —
// since /api/auth/login doesn't exist (tokens are minted via Telegram),
// the Login UI just stores the pasted token then calls /auth/whoami to
// confirm it's valid.
function authHeaders(extra?: HeadersInit): HeadersInit {
  const tok = getToken();
  const base: Record<string, string> = {};
  if (tok) base.Authorization = `Bearer ${tok}`;
  if (!extra) return base;
  return { ...base, ...(extra as Record<string, string>) };
}

// handle401 is called when a request returns 401. Clears the stored
// token and bounces the user to /login. Also accepts a hint param so
// the login screen can explain why the user landed there.
function handle401(): void {
  clearToken();
  // Avoid a redirect loop if we're already on /login.
  if (typeof window !== 'undefined' && !window.location.pathname.startsWith('/login')) {
    window.location.href = '/login?expired=1';
  }
}

async function readError(res: Response): Promise<string> {
  const text = await res.text().catch(() => '');
  let msg = text.slice(0, 200);
  try {
    const parsed = JSON.parse(text);
    if (parsed && typeof parsed.error === 'string') msg = parsed.error;
  } catch {
    // not JSON; fall through with raw text
  }
  return msg || res.statusText;
}

async function get<T>(path: string): Promise<T> {
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), TIMEOUT_MS);
  let res: Response;
  try {
    res = await fetch(BASE + path, {
      credentials: 'same-origin',
      headers: authHeaders(),
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
  if (res.status === 401) {
    handle401();
    throw new ApiError(401, 'unauthorized');
  }
  if (!res.ok) {
    throw new ApiError(res.status, await readError(res));
  }
  return res.json() as Promise<T>;
}

// post sends a JSON or empty body to a write endpoint and parses the
// response. Bypasses the 8s GET timeout because some endpoints (ingest,
// reocr) run OCR which can take minutes.
async function post<T>(path: string, body?: unknown): Promise<T> {
  const init: RequestInit = {
    method: 'POST',
    credentials: 'same-origin',
  };
  if (body !== undefined) {
    init.headers = authHeaders({ 'Content-Type': 'application/json' });
    init.body = JSON.stringify(body);
  } else {
    init.headers = authHeaders();
  }
  let res: Response;
  try {
    res = await fetch(BASE + path, init);
  } catch (err) {
    throw new ApiError(0, err instanceof Error ? err.message : 'network error');
  }
  if (res.status === 401) {
    handle401();
    throw new ApiError(401, 'unauthorized');
  }
  if (!res.ok) {
    throw new ApiError(res.status, await readError(res));
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
      headers: authHeaders(),
    });
    if (res.status === 401) {
      handle401();
      throw new ApiError(401, 'unauthorized');
    }
    if (!res.ok) {
      throw new ApiError(res.status, await readError(res));
    }
    return (await res.json()) as UploadResponse;
  },
  tasks: (q?: { status?: string }) =>
    get<Task[]>('/tasks' + qs(q)),
  task: (name: string) => get<Task>(`/tasks/${name}`),

  // ---- write actions (slice 10c) ----
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

  // ---- auth (slice 10d) ----
  whoami: () => get<WhoamiResponse>(`/auth/whoami`),
  logout: () => post<{ ok: boolean }>(`/auth/logout`),

  // ---- skills + MCP (slice 11b) ----
  skills: () => get<SkillSummary[]>(`/skills`),
  skill: (name: string) =>
    get<SkillDetail>(`/skills/${encodeURIComponent(name)}`),
  mcpServers: () => get<MCPServerSummary[]>(`/mcp/servers`),

  // ---- skills.sh catalog + admin install/delete (slice 11c) ----
  skillsCatalog: (q?: string, limit?: number) =>
    get<SkillCatalogItem[]>('/skills/catalog' + qs({ q, limit: limit?.toString() })),
  installSkill: (req: SkillInstallRequest) =>
    post<SkillInstallResponse>('/skills/install', req),
  deleteSkill: (name: string) =>
    post<SkillDeleteResponse>(`/skills/${encodeURIComponent(name)}/delete`),
};
