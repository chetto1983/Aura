-- Source: PRD §Slice 4 (KV cache builder) + phase 06-SPEC / 06-CONTEXT (D-02, D-02a).
-- Per-turn KV-cache metrics: one append-only row per completed assistant turn, from
-- the already-computed llm.Usage (prompt/cached tokens + OpenRouter cost). Backs the
-- time-windowed `aura cache-stats --since=<window>` query (06-04), replacing the
-- debug-only in-memory stats (overrides PRD OQ2, amended in 06-01).
--
-- Append-only by design (T-06-04 role separation): aura_app gets SELECT, INSERT only
-- (no UPDATE/DELETE) — metrics rows are immutable observations, never mutated. DDL is
-- reserved for aura_migrate (AURA_DB_MIGRATE_URL, amendment #17). Mirrors 0005.

CREATE TABLE aura.cache_metrics (
    conversation_id uuid           NOT NULL REFERENCES aura.conversations (id) ON DELETE CASCADE,
    seq             integer        NOT NULL,
    ts              timestamptz    NOT NULL DEFAULT now(),
    prompt_tokens   integer        NOT NULL DEFAULT 0,
    cached_tokens   integer        NOT NULL DEFAULT 0,
    cost_usd        numeric(10, 4) NOT NULL DEFAULT 0,
    PRIMARY KEY (conversation_id, seq)
);

-- Plain index build on a fresh empty table inside the multi-statement implicit
-- migration tx (Pitfall 4 / golang-migrate v4.19.1 forbids the concurrent variant in
-- a tx block). The window queries are `WHERE ts >= $1 ORDER BY ts ASC` (ListSince +
-- AggregateSince); a plain ascending (ts) index serves BOTH the range predicate and
-- the ASC ordering without a reverse-scan / sort step (WR-04 — a DESC index served
-- only the predicate, not the sort).
CREATE INDEX cache_metrics_ts_idx ON aura.cache_metrics (ts);

GRANT SELECT, INSERT ON aura.cache_metrics TO aura_app;
GRANT ALL            ON aura.cache_metrics TO aura_migrate;

COMMENT ON TABLE aura.cache_metrics IS
    'Per-turn KV-cache metrics (Slice 4 / Phase 6, D-02). Append-only: one row per completed assistant turn from llm.Usage (token counts + cost only, no message content).';
