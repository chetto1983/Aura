// governanceApi.ts is the Phase-28 data layer over the Plan-02 thin REST adapter
// (the six GET /api/governance/* reads). It uses the shared canonical getJSON (web/src/api/json.ts):
// ALWAYS credentials: 'same-origin' (the SPA is served by the same binary that exposes
// the routes, behind the Phase-24 RequireAuth whole-origin gate), Accept: application/json,
// and a NON-200 — INCLUDING 401 — THROWS `Error("HTTP <n>")` rather than returning a
// discriminated union. The board consumers drive these through a TanStack Query error path
// so an expired session surfaces a VISIBLE auth-error state, never a silent blank board
// (requirement GOV-01/02/03 state contract / threat T-28-03-04).
//
// Phase 29 adds the WRITE layer alongside the reads: postJSON/patchJSON/deleteJSON mirror
// getJSON EXACTLY (same-origin, Accept: application/json, a non-200-incl-401 THROWS
// `Error("HTTP <n>")`), and every `{name}` path segment is encodeURIComponent'd. The MCP
// probe is a per-server GET keyed by name so each row resolves independently (T-28-03-05).

import { getJSON, isTrue } from '../api/json';

export const GOV_MCP_PATH = '/api/governance/mcp';
export const GOV_SKILLS_PATH = '/api/governance/skills';
export const GOV_SKILLS_AUDIT_PATH = '/api/governance/skills/audit';
export const GOV_SCHEDULER_PATH = '/api/governance/scheduler';

// --- Response shapes (the exact Plan-02 JSON projections, 28-02-SUMMARY §Next Phase Readiness) ---

/** One redacted MCP env-KEY chip. The VALUE is never serialized by the backend
 * (T-28-02-01) — only the key name and a redacted flag reach the wire. */
export interface McpEnvChip {
  readonly key: string;
  readonly redacted: boolean;
}

/** One static MCP server row (GET /api/governance/mcp). */
export interface McpServerRow {
  readonly name: string;
  readonly source: string;
  readonly trust: string;
  readonly riskPolicy: string;
  readonly runtime: string;
  readonly startupState: string;
  readonly authStatus: string;
  readonly profiles: readonly string[];
  readonly networkAllowlist: readonly string[];
  readonly envKeys: readonly McpEnvChip[];
  readonly lastError?: string;
}

/** The live per-server probe result (GET /api/governance/mcp/{name}/probe). */
export interface McpProbeResult {
  readonly name: string;
  readonly ok: boolean;
  readonly tool_count: number;
  readonly detail: string;
  readonly err?: string;
}

/** One skills lifecycle row (GET /api/governance/skills?stage=…). There is NO
 * action/run field by construction — the row carries metadata, and the lifecycle
 * verbs live on the board (GOV-02). `always` marks a skill that enters the prompt
 * every turn; the server omits the key when false. */
export interface SkillRow {
  readonly name: string;
  readonly description: string;
  readonly type: string;
  readonly always?: boolean;
  readonly language?: string;
  readonly contentHash?: string;
}

/** One append-only skills-audit ledger row (GET /api/governance/skills/audit).
 * Sensitive resume/session internals are not part of the board DTO. */
export interface AuditRow {
  readonly ID: string;
  readonly CreatedAt: string;
  readonly ActorID: string;
  readonly SkillName: string;
  readonly Action: string;
  readonly ContentHash: string;
  readonly ApprovalSource: string;
  readonly GateRecommended: boolean;
  readonly GateTaken: boolean;
  readonly BlocklistOverride: boolean;
}

/** One scheduler task (GET /api/governance/scheduler). Prompt payloads and identity
 * linkage fields stay server-side. */
export interface SchedulerTask {
  readonly ID: string;
  readonly Kind: string;
  readonly ScheduleKind: string;
  readonly CronExpr: string;
  readonly EveryMinutes: number;
  readonly RunAt: string;
  readonly TZ: string;
  readonly StepBudget: number;
  readonly Status: string;
  readonly NextRunAt: string;
  readonly NotifyRoute: string;
  readonly CreatedAt: string;
  readonly UpdatedAt: string;
}

/** One scheduler run-history row (GET /api/governance/scheduler/{id}/runs). */
export interface SchedulerRun {
  readonly ID: string;
  readonly TaskID: string;
  readonly Status: string;
  readonly StepBudget: number;
  readonly StartedAt: string;
  readonly LastHeartbeatAt: string;
  readonly CompletedWithHash: string;
  readonly Summary: string;
  readonly LastError: string;
  readonly MissedSince: string;
  readonly CompletedAt: string;
}

/** The two lifecycle stages a skill can be in. Amendment #97 retired 'pending': a
 * written skill is live, so there is no longer a stage where a skill exists but does
 * not work. The server 400s the retired name rather than quietly answering. */
export type SkillStage = 'active' | 'archived';

// --- Phase-29 write layer (mirrors getJSON; never a discriminated union — a non-200,
// INCLUDING 401/404/409, THROWS `Error("HTTP <n>")` so the mutation surfaces a visible
// error/auth state, never a silent no-op). encodeURIComponent on every `{name}` is the
// caller's job at the URL-build site below. ---

async function sendJSON<T>(method: string, url: string, body?: unknown): Promise<T> {
  const init: RequestInit = {
    method,
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    credentials: 'same-origin',
  };
  if (body !== undefined) {
    init.body = JSON.stringify(body);
  }
  const res = await fetch(url, init);
  if (!res.ok) {
    throw new Error(`HTTP ${String(res.status)}`);
  }
  // A 204 No Content (remove/restore/archive) has an empty body — coerce to {} so the
  // generic T resolves without a JSON-parse throw on the empty stream.
  if (res.status === 204) {
    return {} as T;
  }
  return (await res.json()) as T;
}

export function postJSON<T>(url: string, body?: unknown): Promise<T> {
  return sendJSON<T>('POST', url, body);
}

export function patchJSON<T>(url: string, body?: unknown): Promise<T> {
  return sendJSON<T>('PATCH', url, body);
}

export function deleteJSON<T>(url: string): Promise<T> {
  return sendJSON<T>('DELETE', url);
}

export function stringValue(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

function stringList(value: unknown): readonly string[] {
  if (!Array.isArray(value)) return [];
  return value.filter((entry): entry is string => typeof entry === 'string');
}

function envChipList(value: unknown): readonly McpEnvChip[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((entry): McpEnvChip[] => {
    if (entry === null || typeof entry !== 'object') return [];
    const raw = entry as Record<string, unknown>;
    const key = stringValue(raw.key);
    if (key === '') return [];
    return [{ key, redacted: raw.redacted === true }];
  });
}

function normalizeMcpServerRow(row: McpServerRow): McpServerRow {
  const raw = row as unknown as Record<string, unknown>;
  const normalized: McpServerRow = {
    name: stringValue(raw.name),
    source: stringValue(raw.source),
    trust: stringValue(raw.trust),
    riskPolicy: stringValue(raw.riskPolicy),
    runtime: stringValue(raw.runtime),
    startupState: stringValue(raw.startupState),
    authStatus: stringValue(raw.authStatus),
    profiles: stringList(raw.profiles),
    networkAllowlist: stringList(raw.networkAllowlist),
    envKeys: envChipList(raw.envKeys),
  };
  return typeof raw.lastError === 'string'
    ? { ...normalized, lastError: raw.lastError }
    : normalized;
}

/** GET /api/governance/mcp — the static MCP registry rows (always renders; the live probe
 * is a separate per-row call). Rejects on a non-200 (incl. 401) so the board shows the
 * auth/error state, never a blank list (T-28-03-04). */
export async function fetchMcpServers(): Promise<readonly McpServerRow[]> {
  const body = await getJSON<{ servers?: readonly McpServerRow[] }>(GOV_MCP_PATH);
  return (body.servers ?? []).map(normalizeMcpServerRow);
}

/** GET /api/governance/mcp/{name}/probe — the bounded LIVE probe for ONE server. Each row
 * fires its own query so a hung/dead server resolves to its own timed-out/error result
 * without stalling siblings (T-28-03-05). */
export function probeMcpServer(name: string): Promise<McpProbeResult> {
  return getJSON<McpProbeResult>(`${GOV_MCP_PATH}/${encodeURIComponent(name)}/probe`);
}

/** GET /api/governance/skills?stage=… — the lifecycle list for one stage. Pending/archived
 * rows carry NO action field (GOV-02). */
export async function fetchSkills(stage: SkillStage): Promise<readonly SkillRow[]> {
  const body = await getJSON<{ skills?: readonly SkillRow[] }>(
    `${GOV_SKILLS_PATH}?stage=${encodeURIComponent(stage)}`,
  );
  return body.skills ?? [];
}

/** GET /api/governance/skills/{name}/body — the markdown body of ONE ACTIVE skill:
 * what it actually tells the agent to do. An archived or unknown name throws
 * `Error("HTTP 404")` — only the active set is readable. */
export async function fetchSkillBody(name: string): Promise<string> {
  const body = await getJSON<{ body?: string }>(
    `${GOV_SKILLS_PATH}/${encodeURIComponent(name)}/body`,
  );
  return body.body ?? '';
}

/** GET /api/governance/skills/audit — the append-only ledger, newest-first. */
export async function fetchSkillsAudit(): Promise<readonly AuditRow[]> {
  const body = await getJSON<{ rows?: readonly AuditRow[] }>(GOV_SKILLS_AUDIT_PATH);
  return body.rows ?? [];
}

/** GET /api/governance/scheduler — the active scheduled tasks. */
export async function fetchSchedulerTasks(): Promise<readonly SchedulerTask[]> {
  const body = await getJSON<{ tasks?: readonly SchedulerTask[] }>(GOV_SCHEDULER_PATH);
  return body.tasks ?? [];
}

/** GET /api/governance/scheduler/{id}/runs — the paginated, newest-first run history for one
 * task. limit/offset drive the Show-more pagination (default page 25). */
export async function fetchSchedulerRuns(
  taskId: string,
  limit: number,
  offset: number,
): Promise<readonly SchedulerRun[]> {
  const params = new URLSearchParams({ limit: String(limit), offset: String(offset) });
  const body = await getJSON<{ runs?: readonly SchedulerRun[] }>(
    `${GOV_SCHEDULER_PATH}/${encodeURIComponent(taskId)}/runs?${params.toString()}`,
  );
  return body.runs ?? [];
}

// === GOV-03 scheduler write surface (approve / run / cancel / reschedule) ===
// Owner verbs over a scheduled task, each behind RequireCapability(governance.write). A
// non-200 THROWS `Error("HTTP <n>")` so the row surfaces a visible error, never a silent
// no-op: 403 = a system-seeded sweep (not operator-mutable), 404 = gone, 409 = wrong status
// (e.g. approve on an already-active task), 400 = an invalid reschedule grammar.

/** The user-managed task kinds (mirror of cron.IsUserManageableKind): only these expose the
 * approve/run/edit/delete verbs — a system-seeded sweep is read-only in the cockpit. */
export const USER_MANAGEABLE_KINDS: ReadonlySet<string> = new Set([
  'reminder',
  'agent_job',
  'backup_postgres',
  'backup_neo4j',
]);

export function isUserManageableKind(kind: string): boolean {
  return USER_MANAGEABLE_KINDS.has(kind);
}

/** POST /api/governance/scheduler/{id}/approve — flip a pending_approval task to active. */
export async function approveSchedulerTask(id: string): Promise<void> {
  await postJSON<unknown>(`${GOV_SCHEDULER_PATH}/${encodeURIComponent(id)}/approve`);
}

/** POST /api/governance/scheduler/{id}/run — run an active task now (next fire → now). */
export async function runSchedulerTask(id: string): Promise<void> {
  await postJSON<unknown>(`${GOV_SCHEDULER_PATH}/${encodeURIComponent(id)}/run`);
}

/** DELETE /api/governance/scheduler/{id} — cancel a task (soft, status='cancelled'). */
export async function cancelSchedulerTask(id: string): Promise<void> {
  await deleteJSON<unknown>(`${GOV_SCHEDULER_PATH}/${encodeURIComponent(id)}`);
}

/** The reschedule/re-payload request (PATCH /api/governance/scheduler/{id}). An omitted
 * payload keeps the task's current one; notify '' resets to the default route. */
export interface SchedulerEditRequest {
  readonly schedule_kind: string;
  readonly cron?: string;
  readonly at?: string;
  readonly every_minutes?: number;
  readonly tz?: string;
  readonly payload?: unknown;
  readonly notify?: string;
}

/** PATCH /api/governance/scheduler/{id} — reschedule + re-payload a user task. A bad grammar
 * throws `Error("HTTP 400")`; a task no longer editable throws `Error("HTTP 409")`. */
export async function editSchedulerTask(id: string, req: SchedulerEditRequest): Promise<void> {
  await patchJSON<unknown>(`${GOV_SCHEDULER_PATH}/${encodeURIComponent(id)}`, req);
}

// === Phase-29 MCP write surface (MCPW-01/02/03) ===
// The request/response shapes mirror internal/agui/governance_write_seam.go +
// governance_write_api.go (29-02). Env values NEVER cross the wire — the response carries
// ONLY key-only redacted chips (McpEnvChip). The install request is either a `recipe` name
// (resolved against the built-in catalog) or a custom command/url; `env` rows are submitted
// as `KEY=value` strings (a secret may be left as its redacted `${KEY}` placeholder, which
// the backend preserves).

/** POST /api/governance/mcp install request (recipe OR custom). */
export interface McpInstallRequest {
  readonly name: string;
  readonly recipe?: string;
  readonly command?: string;
  readonly args?: readonly string[];
  readonly url?: string;
  readonly type?: string;
  readonly env?: readonly string[];
}

/** The common MCP write response (install/env/trust/lifecycle). EnvKeys are key-only
 * redacted chips; cliEquivalent + destination preview the install; warnings carry the soft
 * required-var-still-placeholder list; probe is the post-write live tool count (fail-soft). */
export interface McpWriteResponse {
  readonly name: string;
  readonly envKeys: readonly McpEnvChip[];
  readonly cliEquivalent?: string;
  readonly destination?: string;
  readonly warnings?: readonly string[];
  readonly probe?: McpProbeResult;
}

/** POST /api/governance/mcp — install a recipe/custom MCP server. A duplicate name throws
 * `Error("HTTP 409")` (the caller surfaces the inline duplicate-name field error). */
export function installMcpServer(req: McpInstallRequest): Promise<McpWriteResponse> {
  return postJSON<McpWriteResponse>(GOV_MCP_PATH, req);
}

/** PATCH /api/governance/mcp/{name}/env — apply the submitted env rows over the stored entry.
 * A secret left as its redacted `${KEY}` placeholder is preserved by the backend merge; a real
 * value rotates; a non-secret edits/clears in place. No value is echoed back (key-only). */
export function setMcpServerEnv(name: string, env: readonly string[]): Promise<McpWriteResponse> {
  return patchJSON<McpWriteResponse>(`${GOV_MCP_PATH}/${encodeURIComponent(name)}/env`, { env });
}

/** POST /api/governance/mcp/{name}/trust — operator-direct trust-approve with a reason. */
export function trustMcpServer(
  name: string,
  trustClass: string,
  reason: string,
): Promise<McpWriteResponse> {
  return postJSON<McpWriteResponse>(`${GOV_MCP_PATH}/${encodeURIComponent(name)}/trust`, {
    class: trustClass,
    reason,
  });
}

/** POST /api/governance/mcp/{name}/(enable|disable) — the reversible lifecycle toggle. */
export function setMcpServerEnabled(name: string, enabled: boolean): Promise<McpWriteResponse> {
  const verb = enabled ? 'enable' : 'disable';
  return postJSON<McpWriteResponse>(`${GOV_MCP_PATH}/${encodeURIComponent(name)}/${verb}`);
}

/** DELETE /api/governance/mcp/{name} — remove the server (returns 204). */
export async function removeMcpServer(name: string): Promise<void> {
  await deleteJSON<unknown>(`${GOV_MCP_PATH}/${encodeURIComponent(name)}`);
}

// === Phase-29 skills write surface (SKW-01/02/03) ===
// The shapes mirror internal/agui/governance_write_seam.go (SkillsInstallInfo /
// SkillsCatalogResult / SkillsCheckItem). The current operator contract installs directly
// after the server-side validation/write-boundary checks; legacy risk/checklist fields may
// still ride the response, but the cockpit does not render an approval ceremony.

/** One validation-checklist item returned by the backend for legacy/audit context. */
export interface SkillsCheckItem {
  readonly label: string;
  readonly passed: boolean;
}

/** POST /api/governance/skills/install response — source/hash/preview/destination plus
 * direct activation status. */
export interface SkillsInstallInfo {
  readonly name: string;
  readonly source: string;
  readonly content_hash: string;
  readonly preview: string;
  readonly destination: string;
  readonly risk_tier: string;
  readonly status: string;
  readonly checklist: readonly SkillsCheckItem[];
}

/** One parsed skills.sh catalog hit. */
export interface SkillsCatalogHit {
  readonly source: string;
  readonly skill: string;
  readonly installs: string;
}

/** GET /api/governance/skills/catalog response. `enabled` reflects the server
 * AURA_SKILLS_EXTERNAL_DISCOVERY explicit deployment opt-out; `hits` is empty when
 * disabled OR when the search found nothing. */
export interface SkillsCatalogResult {
  readonly enabled: boolean;
  readonly query: string;
  readonly hits: readonly SkillsCatalogHit[];
}

/** POST /api/governance/skills/install — install and activate a validated skill. An
 * empty/invalid source throws `Error("HTTP 400")`. */
export function installSkill(source: string): Promise<SkillsInstallInfo> {
  return postJSON<SkillsInstallInfo>(`${GOV_SKILLS_PATH}/install`, { source });
}

/** GET /api/governance/skills/catalog?q= — the flag-gated external discovery search. */
export async function searchSkillCatalog(
  query: string,
  signal?: AbortSignal,
): Promise<SkillsCatalogResult> {
  return getJSON<SkillsCatalogResult>(
    `${GOV_SKILLS_PATH}/catalog?q=${encodeURIComponent(query)}`,
    signal,
  );
}

/** POST /api/governance/skills/{name}/restore — restore an archived skill. A name collision
 * with an active skill throws `Error("HTTP 409")` (the inline colliding-restore error). */
export async function restoreSkill(name: string): Promise<void> {
  await postJSON<unknown>(`${GOV_SKILLS_PATH}/${encodeURIComponent(name)}/restore`);
}

/** POST /api/governance/skills/{name}/archive — archive an active skill (reversible). */
export async function archiveSkill(name: string): Promise<void> {
  await postJSON<unknown>(`${GOV_SKILLS_PATH}/${encodeURIComponent(name)}/archive`);
}

/** The draft an operator is authoring in the editor. `always` puts the skill in the
 * prompt every turn; the type is instruction (a snippet needs a language and is
 * authored through `aura skills snippet`). */
export interface SkillDraft {
  readonly name: string;
  readonly description: string;
  readonly body: string;
  readonly always: boolean;
}

/** The write-boundary verdict for a draft. `field` anchors the message to an input
 * ('name' | 'body'); empty means show it at form level. */
export interface SkillValidation {
  readonly ok: boolean;
  readonly field?: string;
  readonly message?: string;
}

/** POST /api/governance/skills/validate — dry-run the write boundary. It writes nothing
 * and answers 200 even for a rejected draft: a half-typed skill is not a bad request. */
export function validateSkill(draft: SkillDraft): Promise<SkillValidation> {
  return postJSON<SkillValidation>(`${GOV_SKILLS_PATH}/validate`, draft);
}

/** POST /api/governance/skills — create a skill. It is live when this resolves. */
export function createSkill(draft: SkillDraft): Promise<{ status: string }> {
  return postJSON<{ status: string }>(GOV_SKILLS_PATH, draft);
}

/** PATCH /api/governance/skills/{name} — update a skill in place. */
export function updateSkill(draft: SkillDraft): Promise<{ status: string }> {
  return patchJSON<{ status: string }>(
    `${GOV_SKILLS_PATH}/${encodeURIComponent(draft.name)}`,
    draft,
  );
}

/** DELETE /api/governance/skills/{name} — remove the skill for good (204, no body).
 * Unlike archive this is irreversible, which is why the board puts it behind a
 * confirm dialog and never on the row. */
export async function deleteSkill(name: string): Promise<void> {
  await deleteJSON<unknown>(`${GOV_SKILLS_PATH}/${encodeURIComponent(name)}`);
}

// === Cockpit "Connect" — WhatsApp device-linking (connect_api.go) ===
// The three /api/connect/whatsapp/* routes proxy the aura-whatsapp bridge management REST
// behind the SAME governance.write gate. They reuse the getJSON/sendJSON helpers (same-origin,
// Accept: application/json, a non-200 THROWS `Error("HTTP <n>")`) so a bridge-offline state
// surfaces as a VISIBLE error in the connect section, never a silent blank. The qr.png is an
// <img> source (a binary PNG rendered server-side), so its path is exported rather than fetched.

/** The exact qr.png route. The connect UI appends a cache-bust query (`?ts=…`) so each ~3s
 * tick re-fetches the rotated QR; Cache-Control: no-store on the response is the belt. */
export const WHATSAPP_CONNECT_QR_PATH = '/api/connect/whatsapp/qr.png';

/** GET /api/connect/whatsapp/status — the bridge link state passthrough. */
export interface WhatsAppConnectStatus {
  readonly state: string;
  readonly paired: boolean;
  readonly jid: string;
  readonly connected: boolean;
  readonly qrAvailable: boolean;
}

/** GET /api/connect/whatsapp/status — the device-link state. Rejects on a non-200 (incl. 401/
 * 503 bridge-unconfigured) so the connect section shows the offline/error note. */
export async function whatsappConnectStatus(): Promise<WhatsAppConnectStatus> {
  const raw = await getJSON<Record<string, unknown>>('/api/connect/whatsapp/status');
  return {
    state: stringValue(raw.state),
    paired: isTrue(raw.paired),
    jid: stringValue(raw.jid),
    connected: isTrue(raw.connected),
    qrAvailable: isTrue(raw.qr_available),
  };
}

/** POST /api/connect/whatsapp/logout — drop the paired session (operator Unlink). */
export async function whatsappConnectLogout(): Promise<void> {
  await postJSON<unknown>('/api/connect/whatsapp/logout');
}

/** isWhatsAppServer detects the WhatsApp MCP server by recipe/source (preferred) with a
 * name/source-substring fallback, so the detail pane knows when to render the connect section.
 * Pure; lives here (not the component file) so WhatsAppConnect.tsx exports only components. */
export function isWhatsAppServer(server: {
  readonly name: string;
  readonly source: string;
}): boolean {
  const source = server.source.toLowerCase();
  const name = server.name.toLowerCase();
  return name === 'whatsapp' || source === 'recipe:whatsapp' || source.includes('whatsapp');
}

// Cockpit "Connect" — calendar/PIM account-linking moved to ./pimApi.ts (governanceApi.ts crossed
// the 600-LOC cap). WhatsApp connect stays here; the PIM connect concern is its own module.
