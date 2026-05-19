// Hand-written to mirror internal/api/types.go. Drift detection is by feel
// — runtime parsing failures are visible because polling exercises every
// endpoint. If drift becomes painful, swap in tygo Go→TS codegen.

export interface HealthRollup {
  process: {
    version: string;
    git_revision?: string;
    started_at: string;
    uptime_seconds: number;
  };
  wiki: { pages: number; last_update: string };
  sources: { by_status: Record<string, number> };
  tasks: { by_status: Record<string, number> };
  scheduler: { next_run: string | null };
  sandbox: {
    enabled: boolean;
    available: boolean;
    runtime?: string;
    detail?: string;
  };
  // Slice 11j — embedding cache hit/miss counters since process start.
  // Both zero when no cache is wired.
  embed_cache: { hits: number; misses: number };
  // Phase-TJ — rule-driven output compaction stats. Always present (zero
  // values before any compaction occurs or when the flag is off).
  tokenjuice: {
    enabled: boolean;
    total_calls: number;
    total_bytes_saved: number;
    avg_ratio: number;
    top_rules_by_savings: Array<{ rule_id: string; bytes_saved: number }>;
  };
}

export interface WikiPageSummary {
  slug: string;
  title: string;
  category?: string;
  tags?: string[];
  updated_at: string;
}

export interface WikiPage {
  slug: string;
  title: string;
  body_md: string;
  frontmatter: Record<string, unknown>;
  /**
   * True when the most recent write to this page did not produce a git commit
   * (commit failed or git unavailable). The page IS saved on disk; only the
   * audit history is degraded. Auto-clears on next successful commit. GIT-01.
   *
   * Optional for deploy-window backward compat — old API responses may not
   * include it. The backend (Plan 07 Task 1) always includes it; new clients
   * reading `page.frontmatter?.unversioned` also work for zero-downtime deploys.
   */
  unversioned?: boolean;
}

export interface GraphNode {
  id: string;
  title: string;
  category?: string;
}

export interface GraphEdge {
  source: string;
  target: string;
  type: 'wikilink' | 'related';
}

export interface Graph {
  nodes: GraphNode[];
  edges: GraphEdge[];
}

export interface SourceSummary {
  id: string;
  // Generated artifacts share the source layout with uploaded files but skip
  // OCR actions. Text-like uploads normalize to extract.md/extract.json.
  kind: 'pdf' | 'text' | 'markdown' | 'json' | 'csv' | 'url' | 'xlsx' | 'docx' | 'pdf_generated' | 'sandbox_artifact';
  filename: string;
  status: 'stored' | 'extracting' | 'ocr_complete' | 'extract_complete' | 'ingested' | 'failed';
  created_at: string;
  page_count?: number;
  wiki_pages?: string[];
}

export interface SourceDetail extends SourceSummary {
  mime_type?: string;
  sha256: string;
  size_bytes: number;
  ocr_model?: string;
  error?: string;
}

export interface UploadResponse {
  id: string;
  status: SourceSummary['status'];
  duplicate: boolean;
  filename: string;
  page_count?: number;
  wiki_pages?: string[];
  ingest_note?: string;
  ocr_error?: string;
  note?: string;
}

export interface IngestResponse {
  id: string;
  status: SourceSummary['status'];
  filename: string;
  wiki_pages?: string[];
  ingest_note?: string;
  note?: string;
}

export interface ReocrResponse {
  id: string;
  status: SourceSummary['status'];
  filename: string;
  page_count?: number;
  wiki_pages?: string[];
  ingest_note?: string;
  ocr_error?: string;
  note?: string;
}

export interface SourceMarkdown {
  markdown: string;
  file: string;
}

export interface UpsertTaskRequest {
  name: string;
  kind: Task['kind'];
  payload?: string;
  recipient_id?: string;
  language?: 'en' | 'it';
  at?: string; // RFC3339 UTC
  daily?: string; // HH:MM (bot's local TZ)
  weekdays?: string; // optional daily filter: mon,tue,wed,thu,fri,sat,sun
  every_minutes?: number; // recurrence: fires every N minutes (>=1)
}

export interface WhoamiResponse {
  user_id: string;
}

// Slice 11b — skills + MCP read surfaces.

export interface SkillSummary {
  name: string;
  description?: string;
}

export interface SkillDetail {
  name: string;
  description?: string;
  content: string;
  truncated?: boolean;
}

export interface MCPToolInfo {
  name: string;
  description?: string;
  input_schema?: Record<string, unknown>;
}

export interface MCPServerSummary {
  name: string;
  transport: 'stdio' | 'http';
  tool_count: number;
  tools: MCPToolInfo[];
}

export interface ConnectorCapability {
  id: string;
  label: string;
  description?: string;
  enabled: boolean;
  review_required: boolean;
}

export interface ConnectorRiskBadge {
  id: string;
  label: string;
  level: 'low' | 'medium' | 'high';
}

export interface ConnectorProviderSummary {
  id: string;
  name: string;
  kind: 'mail' | 'database';
  profile: 'personal' | 'business';
  description: string;
  status: 'not_configured' | 'configured' | 'ready' | 'enabled' | 'failed';
  runtime_type: 'container' | 'stdio' | 'remote_http';
  repository_url?: string;
  homepage_url?: string;
  mcp_server_names?: string[];
  capabilities: ConnectorCapability[];
  risk_badges: ConnectorRiskBadge[];
  required_secrets?: string[];
  approved_tools?: string[];
  blocked_tools?: string[];
  setup_hints?: string[];
}

export interface ConnectorProbeResponse {
  ok: boolean;
  provider_id: string;
  server_name?: string;
  capabilities_ready?: string[];
  missing_capabilities?: string[];
  approved_tools_advertised?: string[];
  blocked_tools_advertised?: string[];
  error?: string;
}

export type MailSetupProvider = 'gmail' | 'outlook' | 'imap';

export interface MailSetupStatus {
  configured: boolean;
  connected: boolean;
  needs_restart: boolean;
  restart_required: boolean;
  can_restart: boolean;
  binary_present: boolean;
  command?: string;
  provider?: MailSetupProvider;
  account_id?: string;
  email?: string;
  configured_email?: string;
  imap_host?: string;
  imap_port?: number;
  imap_secure?: boolean;
  smtp_host?: string;
  smtp_port?: number;
  smtp_secure?: boolean;
  enable_smtp?: boolean;
  enable_imap_mutations?: boolean;
  secret_configured?: boolean;
  updated_at?: string;
  error?: string;
}

export interface MailSetupRequest {
  provider: MailSetupProvider;
  account_id?: string;
  email: string;
  imap_host?: string;
  imap_port?: number;
  imap_secure?: boolean;
  smtp_host?: string;
  smtp_port?: number;
  smtp_secure?: boolean;
  app_password?: string;
  enable_smtp?: boolean;
  enable_imap_mutations?: boolean;
}

export interface MailSetupResponse {
  ok: boolean;
  status: MailSetupStatus;
}

export type DatabaseSetupProvider = 'sqlite' | 'postgresql' | 'mysql' | 'sqlserver';

export interface DatabaseSetupStatus {
  configured: boolean;
  connected: boolean;
  needs_restart: boolean;
  restart_required: boolean;
  can_restart: boolean;
  binary_present: boolean;
  command?: string;
  provider?: DatabaseSetupProvider;
  sqlite_path?: string;
  host?: string;
  port?: number;
  database?: string;
  user?: string;
  ssl?: boolean;
  password_configured?: boolean;
  error?: string;
}

export interface DatabaseSetupRequest {
  provider: DatabaseSetupProvider;
  sqlite_path?: string;
  host?: string;
  port?: number;
  database?: string;
  user?: string;
  password?: string;
  ssl?: boolean;
}

export interface DatabaseSetupResponse {
  ok: boolean;
  status: DatabaseSetupStatus;
}

// Slice 11c — skills.sh catalog + admin-gated install/delete.

export interface SkillCatalogItem {
  source: string;
  skill_id?: string;
  name: string;
  installs: number;
  install_command?: string;
}

export interface SkillInstallRequest {
  source: string;
  skill_id?: string;
}

export interface SkillInstallResponse {
  ok: boolean;
  output?: string;
  error?: string;
}

export interface SkillDeleteResponse {
  ok: boolean;
  name: string;
}

// Slice 11d — invoke MCP tools from the dashboard.
export interface MCPInvokeResponse {
  ok: boolean;
  is_error?: boolean;
  output?: string;
  error?: string;
}

// Pending access requests. The list endpoint is gated by the dashboard
// bearer token, so by the time the frontend sees these the requester has
// already been queued by the bot's /start handler.
export interface PendingUserSummary {
  user_id: string;
  username?: string;
  requested_at: string;
}

export interface PendingDecisionResponse {
  ok: boolean;
  user_id: string;
}

// Slice 12j — conversation archive read surface.

export interface ConversationTurn {
  id: number;
  chat_id: number;
  user_id: number;
  turn_index: number;
  role: string;
  content: string;
  tool_calls?: string;
  tool_call_id?: string;
  llm_calls?: number;
  tool_calls_count?: number;
  elapsed_ms?: number;
  tokens_in?: number;
  tokens_out?: number;
  created_at: string;
}

export interface ConversationDetail {
  id: number;
  chat_id: number;
  user_id: number;
  turn_index: number;
  role: string;
  content: string;
  tool_calls?: string;
  tool_call_id?: string;
  llm_calls?: number;
  tool_calls_count?: number;
  elapsed_ms?: number;
  tokens_in?: number;
  tokens_out?: number;
  created_at: string;
}

// Slice 12l — wiki maintenance issue queue.

export interface WikiIssue {
  id: number;
  kind: string;
  severity: 'high' | 'medium' | 'low';
  slug?: string;
  broken_link?: string;
  message?: string;
  status: 'open' | 'resolved';
  created_at: string;
  resolved_at?: string;
}

// Tool attempts observability (US-T05).

export interface ToolOutcomeCounts {
  ok: number;
  recoverable: number;
  blocked: number;
  fatal: number;
  cancelled: number;
}

export interface ToolBudgetRow {
  tool_name: string;
  attempts_today: number;
  budget: number;
  percent: number;
}

export interface ToolAttemptsResponse {
  per_tool: Record<string, ToolOutcomeCounts>;
  retry_budget_consumption: ToolBudgetRow[];
}

// Authz decisions observability (US-T04).

export interface CapabilityCount {
  capability: string;
  count: number;
}

export interface RecentDenial {
  actor_id: string;
  capability: string;
  reason: string;
  created_at: string;
}

export interface AuthzResponse {
  denial_rate_24h: number;
  top_denied_capabilities: CapabilityCount[];
  recent_denials: RecentDenial[];
}

// Proposed updates review queue.

export interface ProposedUpdate {
  id: number;
  chat_id: number;
  fact: string;
  action: 'new' | 'patch' | 'skip';
  target_slug?: string;
  similarity: number;
  source_turn_ids: number[];
  category?: string;
  related_slugs: string[];
  provenance?: ProposalProvenance;
  status: 'pending' | 'approved' | 'rejected' | 'quarantine' | 'accepted';
  kind?: string;
  signature_hash?: string;
  created_at: string;
}

export interface ProposalEvidenceRef {
  kind: string;
  id: string;
  title?: string;
  page?: number;
  snippet?: string;
}

export interface ProposalProvenance {
  origin_tool?: string;
  origin_reason?: string;
  evidence?: ProposalEvidenceRef[];
  agent_job_id?: string;
  swarm_run_id?: string;
  swarm_task_id?: string;
}

export interface SummaryBatchFailure {
  id: number;
  error: string;
}

export interface SummaryBatchResponse {
  updated: ProposedUpdate[];
  failed: SummaryBatchFailure[];
}

export interface Task {
  name: string;
  kind: 'reminder' | 'wiki_maintenance' | 'agent_job';
  payload?: string;
  recipient_id?: string;
  schedule_kind: 'at' | 'daily' | 'every';
  schedule_at?: string;
  schedule_daily?: string;
  schedule_weekdays?: string;
  schedule_every_minutes?: number;
  next_run_at: string;
  last_run_at?: string;
  last_error?: string;
  last_output?: string;
  last_metrics_json?: string;
  wake_signature?: string;
  status: 'active' | 'done' | 'cancelled' | 'failed';
  created_at: string;
  updated_at: string;
}

export interface TaskCounts {
  total: number;
  pending: number;
  running: number;
  completed: number;
  failed: number;
}

export interface RunMetrics {
  llm_calls: number;
  tool_calls: number;
  tokens_prompt: number;
  tokens_completion: number;
  tokens_total: number;
  task_elapsed_ms: number;
  wall_ms: number;
  speedup: number;
}

export interface SwarmRunSummary {
  id: string;
  goal: string;
  status: 'pending' | 'running' | 'completed' | 'failed';
  created_by?: string;
  created_at: string;
  updated_at: string;
  completed_at?: string;
  last_error?: string;
  task_counts: TaskCounts;
  metrics: RunMetrics;
}

export interface SwarmTask {
  id: string;
  run_id: string;
  parent_id?: string;
  role: string;
  subject?: string;
  status: 'pending' | 'running' | 'completed' | 'failed';
  depth: number;
  attempts: number;
  tool_allowlist?: string[];
  blocked_by?: string[];
  result?: string;
  last_error?: string;
  llm_calls: number;
  tool_calls: number;
  tokens_prompt: number;
  tokens_completion: number;
  tokens_total: number;
  elapsed_ms: number;
  created_at: string;
  started_at?: string;
  completed_at?: string;
}

export interface SwarmRunDetail extends SwarmRunSummary {
  tasks: SwarmTask[];
}

// Slice 14d — runtime settings page.
//
// `source` tells the UI where the effective `value` came from:
//   - "db"      : a row exists in aura.db (this is what the dashboard owns).
//   - "env"     : the bot loaded it from .env at startup; saving here
//                  promotes it to a DB row that overrides env on next boot.
//   - "default" : neither set; the bot is using the in-code default.
//
// `kind` hints which input control to render. Defaults to "text".
export type SettingKind = 'text' | 'bool' | 'int' | 'float' | 'enum' | 'url';

export interface SettingItem {
  key: string;
  value: string;
  source: 'db' | 'env' | 'default';
  active_value: string;
  restart_required: boolean;
  is_secret: boolean;
  read_only: boolean;
  kind?: SettingKind;
  options?: string[]; // present when kind === 'enum'
  min?: number;
  max?: number;
  label?: string;
  hint?: string;
  group?: 'runtime' | 'provider' | 'search' | 'storage' | 'embeddings' | 'ocr' | 'sandbox' | 'budget' | 'aurabot' | 'agent' | 'other';
}

export interface SettingsUpdateResponse {
  ok: boolean;
  applied?: string[];
  errors?: string[];
  runtime_applied?: boolean;
  runtime_error?: string;
}

export interface RestartResponse {
  ok: boolean;
  restarting: boolean;
}

export interface BackupObject {
  key: string;
  category: 'full_restore' | 'source_originals' | 'extractions' | 'memory_snapshot' | 'embedding_index' | 'audit_bundle' | 'manifest' | 'artifact';
  timestamp?: string;
  size_bytes: number;
  last_modified: string;
}

export interface BackupListResponse {
  bucket: string;
  objects: BackupObject[];
}

export interface BackupExportObject {
  category: BackupObject['category'];
  key: string;
  size_bytes: number;
  files: number;
}

export interface BackupExportResponse {
  bucket: string;
  timestamp: string;
  objects: BackupExportObject[];
}
