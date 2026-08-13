# Spike — is the conversation chain-of-thought worth indexing as searchable memory?

Date: 2026-08-13. Run on the live stack (Postgres `aura`, `aura-llama-embed` EmbeddingGemma
768d). Corpus: the **real** operator CoT already in `aura.conversation_turns.reasoning` —
62 turns across 18 conversations, median 965 chars, 97,178 chars total (~24K tokens).

## Why this spike existed

Three sources were on the table for the durable-compaction slice (1b):

- **hermes-agent** — `active`/`compacted` flags + `archive_and_compact` in one transaction.
- **neo4j-labs/agent-memory** — short-term `Session -> Message` with hybrid search.
- **OpenClaw** — a memory-flush *before* compaction (`compaction.memoryFlush.enabled`).

The proposal under test was: persist the whole CoT and index it, so a compacted round stays
recallable. Before building that, two things had to be measured rather than assumed —
whether the CoT carries information its own answer does not, and whether it is actually
retrievable.

Note on what the sources do NOT say: OpenClaw's docs never claim reasoning is persisted; its
leverage is extracting salient facts at the compaction boundary. neo4j's
`get_conversation_summary` is a read-only reporting call — it returns a `ConversationSummary`
and writes nothing, so there is no durable-compaction mechanism there to port.

## Method

`probe.py` embeds every turn's reasoning, its visible answer, and the real user message that
produced it, using the repo's own asymmetric prefixes (`title: none | text: ` for documents,
`task: search result | query: ` for queries — inventing different ones would measure a stack
Aura does not run). Ground truth is never invented: the pairing *this message produced this
answer and this reasoning* is a fact of the transcript.

The corpus is real operator conversation data and is deliberately NOT committed. Pass its
path as the first argument.

```
python probe.py <corpus.json> --out results.txt
```

## Results

```
M1 cos(reasoning_i, answer_i)   min=0.263  p25=0.510  median=0.587  p75=0.734  max=0.859
M2 recall@1  answers=0.371  reasoning=0.694      recall@3  answers=0.548  reasoning=0.823
M3 echo_rows=38  noecho_rows=24
   noecho: answers=0.375   reasoning=0.375      echo: reasoning=0.895
M4 substantive no-echo (n=20):  answers=0.450   reasoning=0.400
```

**M1 — the CoT is not a paraphrase of its answer.** Median cosine 0.587 between a turn's
reasoning and the answer it produced: related, clearly not the same document. There is
distinct content in there.

**M2 looked like a 2x win for reasoning, and it was an artifact.** Reading the text first
(rather than the number) showed CoT habitually opening by restating the prompt — *"The user
just greeted me and asked «cosa sai fare?»"*. So a question retrieving its own reasoning may
be retrieving nothing but its own copy.

**M3 confirms exactly that.** 38 of 62 reasoning texts quote a >=15-char literal run of the
question. On those, recall@1 is 0.895. On the 24 that do not, reasoning and answers score
**identically, 0.375 vs 0.375**. The entire headline gap was quotation coming back.

**M4 removes the degenerate queries too** (`grazie`, `quindi?` — no surface can retrieve
those) and the remaining 20 substantive no-echo queries give answers 0.450 vs reasoning
0.400. At n=20 that is not evidence the answer is *better*; it is enough to say the CoT is
not better.

## Verdict

**Do not index the CoT as a retrieval surface.** It is not redundant (M1), but it is not
more findable than the answer text already is (M3, M4), and the intuition that it would be
came from a measurement artifact. Indexing it costs an embed call per turn and buys no
measured recall.

This leaves OpenClaw's shape as the better-supported one: extract the salient content at the
compaction boundary, rather than archive the whole trace and hope search finds it.

## What this does NOT prove

- It does **not** test the actual use case — a *new, later* question recalling a *past*
  conversation's reasoning. M2-M4 are self-retrieval (does a question find its own turn),
  which is a necessary condition, not the product one. There is no ground truth for
  cross-conversation recall in this corpus, and inventing labels for it is the exact trap
  that wasted a day on the RAG eval.
- n is small: 62 turns, 18 conversations, and only 20 rows survive into M4.
- Only 62 of 690 turns carry reasoning at all (9%) — the corpus reflects whichever
  model/effort was live, not a steady state.
- It says nothing about a CoT indexed **jointly** with its answer, or chunked.
- Retrieval here is pure vector cosine in Python. It is NOT ArcadeDB's hybrid
  (`vector.fuse` + FULL_TEXT), so these numbers are a floor for the semantic leg alone,
  not a measurement of the engine Aura would actually query. The hybrid leg was not run
  deliberately: full-text rewards exact term overlap, which is precisely the quotation the
  CoT already carries, so it would *amplify* the M3 artifact rather than correct it — it
  cannot rescue a surface that ties on the rows where the quotation is absent. Measuring it
  would buy fidelity for a thing this spike concluded not to build.

## Spike B — the archive UPDATE's trigger amplification (measured, disposable schema)

`aura.conversation_turns` carries `conversation_turns_snapshot_bump`, a **BEFORE INSERT OR
UPDATE FOR EACH ROW** trigger (migration 0047) that hermes' SQLite has no equivalent of. A
literal port of `archive_and_compact` fires it once per archived row.

Reproduced on a throwaway schema (created and dropped in the same script; the live tables
were never touched):

```
400 inserts             -> snapshot_version = 400
mass archive UPDATE     -> snapshot_version = 800   (+400, one bump per row)
UPDATE wall-clock       -> 21.6 ms for 400 rows
```

The amplification is real and exactly N, and it is **not** a problem: 21.6 ms for a
400-turn conversation, and `snapshot_version` is documented in `store.go:159` as an internal
monotonic export-delete concurrency token compared for equality — nothing reads its
magnitude, so a jump of N instead of 1 changes no behaviour beyond making a concurrent
export-delete retry, which is correct because the conversation did change.

Read from source, not measured here: the same trigger path raises SQLSTATE `55006` when the
conversation is reserved for export-delete (0047 lines 53-57). A compaction must degrade to
the deterministic L2.5 drop on that error rather than propagate it.
