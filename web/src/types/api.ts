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
  // Slice 11j — embedding cache hit/miss counters since process start.
  // Both zero when no cache is wired.
  embed_cache: { hits: number; misses: number };
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
  kind: 'pdf' | 'text' | 'url';
  filename: string;
  status: 'stored' | 'ocr_complete' | 'ingested' | 'failed';
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

export interface UpsertTaskRequest {
  name: string;
  kind: Task['kind'];
  payload?: string;
  recipient_id?: string;
  at?: string; // RFC3339 UTC
  daily?: string; // HH:MM (bot's local TZ)
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

export interface Task {
  name: string;
  kind: 'reminder' | 'wiki_maintenance';
  payload?: string;
  recipient_id?: string;
  schedule_kind: 'at' | 'daily';
  schedule_at?: string;
  schedule_daily?: string;
  next_run_at: string;
  last_run_at?: string;
  last_error?: string;
  status: 'active' | 'done' | 'cancelled' | 'failed';
  created_at: string;
  updated_at: string;
}
