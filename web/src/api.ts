import type {
  HealthRollup,
  WikiPageSummary,
  WikiPage,
  Graph,
  GodNodesResponse,
  SourceSummary,
  SourceDetail,
  SourceMarkdown,
  Task,
  UploadResponse,
  IngestResponse,
  ReocrResponse,
  UpsertTaskRequest,
  WhoamiResponse,
  SkillSummary,
  SkillDetail,
  MCPServerSummary,
  ConnectorProviderSummary,
  ConnectorProbeResponse,
  DatabaseSetupRequest,
  DatabaseSetupResponse,
  DatabaseSetupStatus,
  MailSetupRequest,
  MailSetupResponse,
  MailSetupStatus,
  SkillCatalogItem,
  SkillInstallRequest,
  SkillInstallResponse,
  SkillDeleteResponse,
  MCPInvokeResponse,
  PendingUserSummary,
  PendingDecisionResponse,
  ConversationTurn,
  ConversationDetail,
  ProposedUpdate,
  SummaryBatchResponse,
  WikiIssue,
  AuthzResponse,
  ToolAttemptsResponse,
  SettingItem,
  SettingsUpdateResponse,
  RestartResponse,
  BackupExportResponse,
  BackupListResponse,
  SwarmRunDetail,
  SwarmRunSummary,
  SwarmTask,
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
// token and bounces the user to /login. Stashes the current location
// in sessionStorage so Login can restore the user there after sign-in.
function handle401(): void {
  clearToken();
  if (typeof window === 'undefined') return;
  if (window.location.pathname.startsWith('/login')) return;
  try {
    const returnTo = window.location.pathname + window.location.search + window.location.hash;
    if (returnTo && returnTo !== '/') {
      window.sessionStorage.setItem('aura_return_to', returnTo);
    }
  } catch {
    // sessionStorage unavailable (private mode, quota); fall through.
  }
  const encodedReturnTo = encodeURIComponent(window.location.pathname + window.location.search + window.location.hash);
  window.location.href = `/login?expired=1&returnTo=${encodedReturnTo}`;
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
  return write<T>('POST', path, body);
}

async function put<T>(path: string, body?: unknown): Promise<T> {
  return write<T>('PUT', path, body);
}

async function del<T>(path: string, body?: unknown): Promise<T> {
  return write<T>('DELETE', path, body);
}

async function write<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = {
    method,
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
  // 204 / empty body — return {} so callers don't choke on undefined.
  if (res.status === 204) return {} as T;
  const text = await res.text();
  if (!text) return {} as T;
  return JSON.parse(text) as T;
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
  wikiGodNodes: (topK = 10) =>
    get<GodNodesResponse>(`/wiki/godnodes?top_k=${topK}`),
  sources: (q?: { status?: string; kind?: string }) =>
    get<SourceSummary[]>('/sources' + qs(q)),
  source: (id: string) => get<SourceDetail>(`/sources/${id}`),
  sourceOCR: (id: string) =>
    get<{ markdown: string }>(`/sources/${id}/ocr`),
  sourceMarkdown: (id: string) =>
    get<SourceMarkdown>(`/sources/${id}/markdown`),
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

  // ---- file manager (multi-root) ----
  filesTree: (root: string, path = '', recursive = false) =>
    get<Array<{ name: string; type: 'file' | 'dir'; size?: number; modified?: string }>>(
      `/files/${root}/tree` + qs({ path, recursive: recursive ? 'true' : undefined }),
    ),
  filesRead: (root: string, path: string) =>
    get<{ root: string; path: string; size: number; modified: string; encoding: 'utf-8' | 'base64'; content: string }>(
      `/files/${root}/file` + qs({ path }),
    ),
  filesWrite: (root: string, path: string, content: string, encoding: 'utf-8' | 'base64' = 'utf-8') =>
    put<{ root: string; path: string; size: number }>(
      `/files/${root}/file` + qs({ path }),
      { encoding, content },
    ),
  filesDelete: (root: string, path: string) =>
    del<{ root: string; path: string; deleted: boolean }>(`/files/${root}/file` + qs({ path })),
  // filesBulkDelete cascades a list of root-relative paths through the
  // wiki-aware delete pipeline in one round-trip. Used by the FilesPanel
  // multi-select flow so the operator can clean up the 876 phantom row
  // pages produced by the broken xlsx converter (now fixed at the source
  // in commit ac5e1670) without 876 sequential HTTP requests.
  filesBulkDelete: (root: string, paths: string[]) =>
    post<{
      root: string;
      deleted: string[];
      failed?: Array<{ path: string; error: string }>;
    }>(`/files/${root}/delete-many`, { paths }),
  filesMkdir: (root: string, path: string) =>
    post<{ root: string; path: string; created: boolean }>(`/files/${root}/mkdir` + qs({ path })),
  filesRename: (root: string, from: string, to: string) =>
    post<{ root: string; from: string; to: string }>(`/files/${root}/rename`, { from, to }),
  deleteSource: (id: string) =>
    del<{
      id: string;
      status: string;
      memory_purged: boolean;
      memory_purge_warning?: string;
      wiki_pages_deleted?: string[];
      wiki_pages_failed?: string[];
      orphan_references?: string[];
      orphan_scan_error?: string;
    }>(`/sources/${id}`),
  // sourceDerived previews what DELETE /sources/{id} will touch — used by
  // FilesPanel before showing the cascade confirmation modal so the
  // operator sees the wiki page count before committing.
  sourceDerived: (id: string) =>
    get<{
      id: string;
      filename?: string;
      tracked_pages: string[];
      referencing_pages: string[];
    }>(`/sources/${id}/derived`),

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
  deleteTask: (name: string) =>
    post<{ ok: boolean; name: string; deleted: boolean }>(`/tasks/${name}/delete`),

  // ---- auth (slice 10d) ----
  whoami: () => get<WhoamiResponse>(`/auth/whoami`),
  logout: () => post<{ ok: boolean }>(`/auth/logout`),

  // ---- skills + MCP (slice 11b) ----
  skills: () => get<SkillSummary[]>(`/skills`),
  skill: (name: string) =>
    get<SkillDetail>(`/skills/${encodeURIComponent(name)}`),
  mcpServers: () => get<MCPServerSummary[]>(`/mcp/servers`),
  mcpProviders: () => get<ConnectorProviderSummary[]>(`/mcp/providers`),
  probeMCPProvider: (id: string) =>
    post<ConnectorProbeResponse>(`/mcp/providers/${encodeURIComponent(id)}/actions/probe`),
  mailSetupStatus: () => get<MailSetupStatus>(`/mcp/setup/mail`),
  saveMailSetup: (req: MailSetupRequest) =>
    post<MailSetupResponse>(`/mcp/setup/mail`, req),
  databaseSetupStatus: () => get<DatabaseSetupStatus>(`/mcp/setup/database`),
  saveDatabaseSetup: (req: DatabaseSetupRequest) =>
    post<DatabaseSetupResponse>(`/mcp/setup/database`, req),

  // ---- skills.sh catalog + admin install/delete (slice 11c) ----
  skillsCatalog: (q?: string, limit?: number) =>
    get<SkillCatalogItem[]>('/skills/catalog' + qs({ q, limit: limit?.toString() })),
  installSkill: (req: SkillInstallRequest) =>
    post<SkillInstallResponse>('/skills/install', req),
  deleteSkill: (name: string) =>
    post<SkillDeleteResponse>(`/skills/${encodeURIComponent(name)}/delete`),

  // ---- mcp tool invocation (slice 11d) ----
  invokeMCPTool: (server: string, tool: string, args: Record<string, unknown>) =>
    post<MCPInvokeResponse>(
      `/mcp/${encodeURIComponent(server)}/tools/${encodeURIComponent(tool)}`,
      args,
    ),

  // ---- pending access requests ----
  pendingUsers: () => get<PendingUserSummary[]>('/pending-users'),
  approvePendingUser: (id: string) =>
    post<PendingDecisionResponse>(`/pending-users/${encodeURIComponent(id)}/approve`),
  denyPendingUser: (id: string) =>
    post<PendingDecisionResponse>(`/pending-users/${encodeURIComponent(id)}/deny`),

  // ---- conversation archive (slice 12j) ----
  conversations: (chatId?: number, limit?: number, hasTools?: boolean) =>
    get<ConversationTurn[]>(
      '/conversations' +
        qs({
          chat_id: chatId !== undefined ? String(chatId) : undefined,
          limit: limit !== undefined ? String(limit) : undefined,
          has_tools: hasTools ? 'true' : undefined,
        }),
    ),
  conversation: (id: number) =>
    get<ConversationDetail>(`/conversations/${id}`),

  // ---- summaries review queue (slice 12k) ----
  summaries: (status?: string) =>
    get<ProposedUpdate[]>('/summaries' + qs({ status })),
  approveSummary: (id: number) =>
    post<ProposedUpdate>(`/summaries/${id}/approve`),
  rejectSummary: (id: number) =>
    post<ProposedUpdate>(`/summaries/${id}/reject`),
  approveSummaries: (ids: number[]) =>
    post<SummaryBatchResponse>(`/summaries/batch/approve`, { ids }),
  rejectSummaries: (ids: number[]) =>
    post<SummaryBatchResponse>(`/summaries/batch/reject`, { ids }),
  quarantine: () =>
    get<ProposedUpdate[]>('/quarantine'),
  approveQuarantine: (id: number) =>
    post<ProposedUpdate>(`/quarantine/${id}/approve`),
  deleteQuarantine: (id: number) =>
    post<{ ok: boolean; id: number }>(`/quarantine/${id}/delete`),

  // ---- maintenance issue queue (slice 12l) ----
  maintenanceIssues: (status?: string, severity?: string) =>
    get<WikiIssue[]>('/maintenance/issues' + qs({ status, severity })),
  resolveIssue: (id: number) =>
    post<WikiIssue>(`/maintenance/issues/${id}/resolve`),
  maintenanceAuthz: () =>
    get<AuthzResponse>('/maintenance/authz'),
  maintenanceToolAttempts: () =>
    get<ToolAttemptsResponse>('/maintenance/tool-attempts'),

  // ---- conversation cleanup (slice 14) ----
  conversationStats: () =>
    get<{ total_rows: number; oldest_at?: string; newest_at?: string; distinct_chats: number }>(
      '/conversations/stats',
    ),
  cleanupConversations: (sel: { chat_id?: number; older_than_days?: number; all?: boolean }) => {
    const q: Record<string, string> = {};
    if (sel.chat_id !== undefined) q.chat_id = String(sel.chat_id);
    if (sel.older_than_days !== undefined) q.older_than_days = String(sel.older_than_days);
    if (sel.all) q.all = 'true';
    return post<{ ok: boolean; deleted: number }>('/conversations/cleanup' + qs(q));
  },

  // ---- runtime settings (slice 14d) ----
  settings: () => get<{ items: SettingItem[] }>(`/settings`),
  updateSettings: (updates: Record<string, string>) =>
    post<SettingsUpdateResponse>(`/settings`, { updates }),
  restart: () => post<RestartResponse>('/restart'),
  testProvider: (baseURL: string, apiKey: string, probePath?: string) =>
    post<{ ok: boolean; error?: string; models?: string[] }>(`/settings/test`, {
      base_url: baseURL,
      api_key: apiKey,
      probe_path: probePath,
    }),

  // ---- Garage artifact vault ----
  backups: () => get<BackupListResponse>('/backups'),
  exportBackups: () => post<BackupExportResponse>('/backups/export'),

  // ---- AuraBot swarm observability (slice 17d) ----
  swarmRuns: (limit?: number) =>
    get<SwarmRunSummary[]>('/swarm/runs' + qs({ limit: limit?.toString() })),
  swarmRun: (id: string) =>
    get<SwarmRunDetail>(`/swarm/runs/${encodeURIComponent(id)}`),
  swarmTask: (id: string) =>
    get<SwarmTask>(`/swarm/tasks/${encodeURIComponent(id)}`),
};
