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
