# Aura Second Brain Consolidation Strategy

Last updated: 2026-04-29 — strategy. **Implementation status updated 2026-05-04.**

> **STATUS — 2026-05-04:** Treat the 2026-05-02 status line below as historical. Since then Aura also shipped `propose_wiki_change`, weekday/every-minute recurrence, propose-only `agent_job`, `daily_briefing`, AuraBot runtime settings diagnostics, and unified `search_memory`. Active legacy gaps are now `update_wiki`, richer proposal provenance/batch review, `link_wiki`, `graph_query`, skill-change proposals, and watcher-style autonomous maintenance.

> **STATUS — 2026-05-02: STRATEGY EXECUTED.** Most "Next Aura-native tools" have shipped: `ingest_source`, `list_wiki`, `lint_wiki`, `lint_sources`, `append_log`, `rebuild_index`, plus the source store (slice 5) and ingestion pipeline (slice 6). Web dashboard with source inbox, wiki browser, graph view, pending-user queue, tasks, skills, and MCP panels shipped in slices 10a–10e + 11a–11d + 11o. **Still to do**: `update_wiki`, `propose_wiki_change` / `apply_wiki_change` (review queue), `link_wiki`, `graph_query`, scheduled lint passes ("autonomous maintenance"). This document remains the north-star strategy; concrete state lives in [`prd.md`](../prd.md) and [`docs/implementation-tracker.md`](implementation-tracker.md).

Aura's direction is a standalone second brain that combines:

- Picobot's agentic tool loop: the model chooses tools, runs them, observes results, and iterates.
- The LLM Wiki pattern in `docs/llm-wiki.md`: raw sources are compiled into a maintained markdown wiki instead of re-derived from raw chunks on every query.
- Aura's own UI and maintenance workflows, so Obsidian becomes optional inspiration, not a dependency.

## Positioning

Most AI knowledge apps cluster around one of four ideas:

- Graph/backlinks and local files, as in Obsidian's graph view.
- AI search and chat over notes, as in Mem, Khoj, Notion AI, and AnythingLLM.
- Structured graph/database objects, as in Tana supertags.
- Visual cards/whiteboards and source-grounded study views, as in Heptabase and NotebookLM.

Aura should not compete by being another notes editor with chat. The unique product promise is:

> Aura is an agent-maintained knowledge compiler. It turns messy captures and source material into a versioned, linked, audited wiki that gets healthier over time.

The important distinction is maintenance. Retrieval apps answer from stored material. Aura should also update the knowledge base: create pages, link concepts, flag contradictions, refresh stale claims, append log entries, and propose next investigations.

## What To Build Into Aura

Aura should replace the Obsidian side of the LLM Wiki workflow with first-party screens and tools:

- Source inbox: captured URLs, files, Telegram notes, transcripts, and imported markdown waiting for triage.
- Wiki browser/editor: markdown pages with frontmatter, `[[slug]]` links, source references, backlinks, and page history.
- Graph view: global graph, local page graph, orphan filter, stale page filter, source coverage overlay.
- Review queue: proposed writes and edits shown as diffs before applying sensitive or high-impact changes.
- Timeline log: append-only history of ingests, queries, edits, lint passes, and rollbacks.
- Search/chat surface: natural language questions answered with `search_wiki`, `read_wiki`, `web_search`, and `web_fetch`.
- Health dashboard: metrics that show whether the brain is getting more useful or just larger.

## Consolidated Tool Map

Existing tools:

- `web_search`: find current external sources through Ollama's web API.
- `web_fetch`: fetch a specific URL for deeper reading.
- `write_wiki`: create or update durable markdown knowledge.
- `read_wiki`: read a specific wiki page by slug.
- `search_wiki`: retrieve saved wiki knowledge with Mistral embeddings.

Next Aura-native tools:

- `ingest_source`: turn a URL, file, or pasted note into one or more wiki updates with source metadata.
- `list_wiki`: enumerate pages by category, tag, recency, or missing metadata.
- `update_wiki`: patch an existing page with a reason, sources, and link updates.
- `link_wiki`: add or repair backlinks and related-page metadata without rewriting a full page.
- `append_log`: write parseable entries to `wiki/log.md`.
- `rebuild_index`: regenerate `wiki/index.md` from page frontmatter.
- `lint_wiki`: report broken links, orphans, missing sources, stale claims, duplicate pages, and contradictions.
- `propose_wiki_change`: stage a diff for user approval instead of writing immediately.
- `apply_wiki_change`: apply an approved staged change and record it in the log.
- `graph_query`: answer structural questions such as hubs, orphans, clusters, and paths between concepts.

Keep the Picobot memory tools as reference, but translate them into Aura's wiki model. Daily memory becomes journal/source inbox behavior. Long-term memory becomes structured wiki pages. Edit/delete memory becomes reviewed wiki patching with audit history.

## Unique Product Mechanics

- Memory transactions: every durable write has intent, source, diff, actor, timestamp, and rollback path.
- Source-grounded synthesis: answers cite wiki pages and original sources, not just vector chunks.
- Compounding pages: useful answers can be filed back into the wiki as first-class pages.
- Autonomous maintenance: scheduled lint passes identify contradictions, stale pages, orphan clusters, and missing links.
- Graph plus semantics: combine `[[slug]]` links, frontmatter metadata, full-text search, and embeddings.
- Local-first durability: markdown remains inspectable and git-friendly, while Aura provides the graph UI and agent workflows.
- Natural prompt testing: every tool should be testable by asking the LLM a normal user request, not only by unit tests.

## Roadmap

1. Stabilize the current tool base.
   - Keep `write_wiki`, `read_wiki`, `search_wiki`, `web_search`, and `web_fetch` covered by unit tests and `cmd/debug_tools`.
   - Keep LLM keys and embedding keys separate. Embeddings use Mistral settings only.

2. Add wiki maintenance tools.
   - Implement `list_wiki`, `append_log`, `rebuild_index`, and `lint_wiki`.
   - Add metrics for broken links, orphan pages, stale pages, pages without sources, duplicate titles, and indexing freshness.

3. Add source ingestion.
   - Store immutable raw source records separately from wiki pages.
   - Let the agent propose page updates from each source, then apply or stage them.

4. Build the standalone second-brain UI.
   - Start with a simple web dashboard: search, page reader, graph, source inbox, review queue, and health metrics.
   - Telegram remains a capture and chat channel, not the only product surface.

5. Add proactive intelligence.
   - Scheduled lint and review jobs.
   - "What changed?", "What should I revisit?", "What contradicts what?", and "What source should I read next?" workflows.

## Metrics

Tool metrics:

- Natural-prompt pass rate by tool.
- Tool-call success rate and average latency.
- Max-tool-iteration stops per 100 conversations.
- Tool error rate by name.

Wiki health metrics:

- Total pages, sources, links, backlinks, and categories.
- Orphan page percentage.
- Broken link count.
- Pages without sources.
- Stale page count by age and category.
- Duplicate or near-duplicate page count.
- Contradiction candidates found per lint pass.
- Search index freshness and failed reindex count.

User value metrics:

- Capture-to-wiki time.
- Query-to-cited-answer time.
- Percentage of answers grounded in wiki pages and original sources.
- Number of useful answers filed back into the wiki.
- Review approval, edit, and rejection rates.

## Research Notes

- Obsidian proves the value of graph/backlink browsing, including global and local graph views, filters, and orphan visibility: https://obsidian.md/help/plugins/graph
- NotebookLM shows that source-grounded visual summaries are valuable for learning and exploration: https://support.google.com/notebooklm/answer/16212283
- Notion AI Connectors show the market expectation for cross-app search with cited sources: https://www.notion.com/help/notion-ai-connectors
- Khoj validates open-source, self-hostable "second brain" positioning with chat over local files and web access: https://docs.khoj.dev/
- Tana shows the power of graph-native structured objects through supertags and fields: https://outliner.tana.inc/docs/supertags
- Heptabase MCP shows a natural-language tool interface for saving cards, appending journals, semantic search, and whiteboard lookup: https://support.heptabase.com/en/articles/12679581-how-to-use-heptabase-mcp
- Mem's API and search docs show expected tool coverage for notes: list, search, create, update, attachments, and recovery: https://docs.mem.ai/mcp/supported-tools
- AnythingLLM shows the broader AI workspace baseline: RAG, agents, web browsing, scraping, document tools, vector databases, and scheduled jobs: https://docs.anythingllm.com/
