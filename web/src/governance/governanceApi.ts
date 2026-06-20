// governanceApi.ts is the Phase-28 data layer over the Plan-02 thin REST adapter
// (the six GET /api/governance/* reads). It mirrors graphApi.ts's getJSON EXACTLY:
// ALWAYS credentials: 'same-origin' (the SPA is served by the same binary that exposes
// the routes, behind the Phase-24 RequireAuth whole-origin gate), Accept: application/json,
// and a NON-200 — INCLUDING 401 — THROWS `Error("HTTP <n>")` rather than returning a
// discriminated union. The board consumers drive these through a TanStack Query error path
// so an expired session surfaces a VISIBLE auth-error state, never a silent blank board
// (requirement GOV-01/02/03 state contract / threat T-28-03-04).
//
// The boards are READ-ONLY (all writes are Phase 29) — there is no postJSON here. The MCP
// probe is a per-server GET keyed by name so each row resolves independently (T-28-03-05).

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
 * action/run field by construction — a pending row is non-runnable (GOV-02). */
export interface SkillRow {
  readonly name: string;
  readonly description: string;
  readonly type: string;
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

export type SkillStage = 'active' | 'pending' | 'archived';

async function getJSON<T>(url: string): Promise<T> {
  const res = await fetch(url, {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  if (!res.ok) {
    throw new Error(`HTTP ${String(res.status)}`);
  }
  return (await res.json()) as T;
}

/** GET /api/governance/mcp — the static MCP registry rows (always renders; the live probe
 * is a separate per-row call). Rejects on a non-200 (incl. 401) so the board shows the
 * auth/error state, never a blank list (T-28-03-04). */
export async function fetchMcpServers(): Promise<readonly McpServerRow[]> {
  const body = await getJSON<{ servers?: readonly McpServerRow[] }>(GOV_MCP_PATH);
  return body.servers ?? [];
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
