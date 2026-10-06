-- tool_search usage report, read from the append-only aura.tool_invocations ledger.
--
-- Answers the question a ranker change has to be priced against: how many real
-- discovery calls are ranked free text (the only path BM25/SPLADE quality touches),
-- and how often the tool a ranked search loaded is the one the model then calls.
-- Nothing new is recorded for it: the `end` row already carries the query (args_raw)
-- and the loaded tools in ranked order (meta.activated_tools, tools.MetaActivatedTools).
--
--   psql "$AURA_DB_MIGRATE_URL" -v days=30 -f scripts/tool_search_usage.sql
--
-- Run it as the table owner: aura.tool_invocations is under fail-closed RLS (0087),
-- so aura_app without an identity GUC reads zero rows and the report is silently empty.
--
-- Mode is reconstructed, not recorded:
--   select   query starts with "select:"
--   names    every token is a loaded tool, or the result reports an unregistered name
--            (ToolSearch.asNameList resolves a bare name list without ranking it)
--   ranked   everything else: the BM25 path
--   unparsed args_raw is not valid JSON (capped at 8 KiB or redacted by the ledger)
-- A bare name list naming only always-loaded tools activates nothing and so reads as
-- ranked; the ranked share is therefore an upper bound.
--
-- Follow-through is scoped to the same request_id (one user turn) and counts the
-- first later call of any loaded tool; its 1-based position in activated_tools is
-- the rank the ranker gave it.

\if :{?days}
\else
\set days 30
\endif

CREATE TEMP VIEW tool_search_calls AS
WITH raw AS (
    SELECT id, conversation_id, request_id, ts, seq, status, result_preview,
           CASE WHEN pg_input_is_valid(args_raw, 'jsonb')
                THEN btrim(args_raw::jsonb ->> 'query') END AS query,
           CASE WHEN jsonb_typeof(meta -> 'activated_tools') = 'array'
                THEN ARRAY(SELECT jsonb_array_elements_text(meta -> 'activated_tools'))
                ELSE '{}'::text[] END AS loaded
    FROM aura.tool_invocations
    WHERE tool_name = 'tool_search'
      AND event_kind = 'end'
      AND ts >= now() - make_interval(days => :days)
)
SELECT raw.*,
       CASE
           WHEN status = 'error' THEN 'error'
           WHEN query IS NULL THEN 'unparsed'
           WHEN query LIKE 'select:%' THEN 'select'
           WHEN result_preview LIKE '%is not a registered tool.%' THEN 'names'
           WHEN cardinality(loaded) > 0
                AND regexp_split_to_array(btrim(query, E', \t\n'), E'[,\\s]+') <@ loaded THEN 'names'
           ELSE 'ranked'
       END AS mode,
       CASE
           WHEN cardinality(loaded) > 0 THEN 'loaded'
           WHEN result_preview LIKE 'no matching tools.%' THEN 'no_match'
           WHEN result_preview LIKE 'Already active in your manifest%' THEN 'already_active'
           ELSE 'other'
       END AS outcome
FROM raw;

CREATE TEMP VIEW ranked_follow_through AS
SELECT s.*, used.tool_name AS used_tool,
       array_position(s.loaded, used.tool_name) AS used_rank,
       EXISTS (
           SELECT 1 FROM aura.tool_invocations r
           WHERE r.conversation_id = s.conversation_id AND r.request_id = s.request_id
             AND r.tool_name = 'tool_search' AND r.event_kind = 'end'
             AND (r.ts, r.seq) > (s.ts, s.seq)
             AND (used.ts IS NULL OR (r.ts, r.seq) < (used.ts, used.seq))
       ) AS searched_again_first
FROM tool_search_calls s
LEFT JOIN LATERAL (
    SELECT t.tool_name, t.ts, t.seq FROM aura.tool_invocations t
    WHERE t.conversation_id = s.conversation_id AND t.request_id = s.request_id
      AND t.event_kind = 'end' AND t.tool_name = ANY (s.loaded)
      AND (t.ts, t.seq) > (s.ts, s.seq)
    ORDER BY t.ts, t.seq
    LIMIT 1
) used ON true
WHERE s.mode = 'ranked' AND s.outcome = 'loaded';

\echo '== 1. Discovery calls by mode and outcome (last' :days 'days)'
SELECT mode, outcome, count(*) AS calls,
       round(100.0 * count(*) / sum(count(*)) OVER (), 1) AS pct_of_all
FROM tool_search_calls
GROUP BY mode, outcome
ORDER BY calls DESC;

\echo '== 2. Ranked searches that loaded tools: which one the turn used'
SELECT CASE
           WHEN used_rank = 1 THEN '1  used rank 1'
           WHEN used_rank <= 3 THEN '2  used rank 2-3'
           WHEN used_rank IS NOT NULL THEN '3  used rank 4+'
           WHEN searched_again_first THEN '4  searched again, none used'
           ELSE '5  none used'
       END AS follow_through,
       count(*) AS searches,
       round(100.0 * count(*) / sum(count(*)) OVER (), 1) AS pct
FROM ranked_follow_through
GROUP BY 1
ORDER BY 1;

\echo '== 3. Ranked queries (real traffic: the source for a held-out eval set)'
SELECT s.query, count(*) AS times, s.outcome,
       string_agg(DISTINCT f.used_tool || '@' || f.used_rank, ', ') AS used
FROM tool_search_calls s
LEFT JOIN ranked_follow_through f ON f.id = s.id
WHERE s.mode = 'ranked'
GROUP BY s.query, s.outcome
ORDER BY times DESC, s.query
LIMIT 200;
