# Embedding model change — documents family (plan 4 of 5) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Document retrieval ranks a query's vector only against a library wholly in that vector's space. When it cannot, it answers from the full-text indexes instead of returning nothing. It reads its key live and names the floors that were never calibrated for its space. A healthy library keeps exactly today's answers.

**Architecture:**
- **`internal/arcadedb`.**
  - A documents gate on the tenant client, beside memory's: one count per type over `Passage` and `IndexedDocument`, cached 30 s.
  - The two dense document statements filter every candidate by `embed_space = :space` and take their floors from a per-space table (§9).
  - New lexical statements query the three full-text indexes with Lucene's own Snowball Italian and English stopwords removed, and apply a BM25 floor of 2.
- **`internal/documents`.**
  - `HostRetriever` takes a `QueryEmbedder` that embeds and names its space.
  - It asks the gate before embedding and reads the space again after.
  - Any failure on that path answers lexically, with status `lexical_only` and the reason.
- **`cmd/aura`.**
  - The document query embedder is an `embeddings.Route` at the documents' width.
  - Its key is read from the running LLM profile, wired into the registry's retriever once the runtime exists. The reasoning classifier shares the route.

**Tech Stack:** Go 1.26; ArcadeDB 26.9.1 (`SEARCH_INDEX` with BM25, `vector.neighbors` / `fuse` / `rerank`); Lucene 10.5.1's Snowball stop lists (`lucene-analysis-common-10.5.1.jar`, the jar ArcadeDB 26.9.1 runs).

**Spec:** `docs/superpowers/specs/2026-09-23-embedding-model-change-design.md`:
- §3, the documents family;
- §8 (all of it);
- §9 (all of it);
- §5's "the key is read live", for the daemon's document query embedder and its reasoning classifier;
- "Fix on touch", the `document_cards.go` comment;
- "Testing and acceptance", the documents line of `arcadedb_integration`.

Plan 1 (`…-embedding-route-and-identity.md`) delivered `embeddings.Route`, `SpaceFor`, `RouteSpace` and `AttestLocal`. Plan 2 (`…-embedding-memory-family.md`) delivered the memory gate, `denseSpaceFilter` and the live memory key. Plan 3 (`…-embedding-ingest.md`, last commit `39b54419e`) stamps every `Passage` and `IndexedDocument` with the supervisor's space.

## Global Constraints

- **Do not break what works** (operator, 2026-09-24: "i documenti adesso lavorano abbastanza bene vedi di non rompere nulla").
  - With the gate open and the space calibrated, a dense retrieval returns exactly what it returns today. The statements are the same plus the space predicate. The floors stay 0.72 / 0.32, the query-embedding timeout stays `documentHTTPClient`'s, and ranking, grouping, the card lane and neighbours are untouched.
  - Only the path that today returns zero documents (a failed query embedding) changes behaviour on a healthy library.
  - Every task runs the whole unit suite of every package it touches, not only its new tests. Tasks 4 and 6 also run `./cmd/aura/`'s document tests and `./internal/agent/tools/`'s `document_search` tests.
  - Task 1 records the dense baseline on the VM, and plan 5's E2E compares against it. What lexical mode answers on that same library is already measured: spec §8, stopwords removed.
- **No bespoke beyond the one measured gap** (operator, 2026-09-24: "una soluzione industriale e funzionante senza inutile bespoke").
  - Every piece reuses what exists:
    - the gate is plan 2's memory gate, generalized;
    - the dense filter is `denseSpaceFilter`;
    - fusion and rerank are the engine's;
    - lexical mode is `SEARCH_INDEX`/BM25 with memory's `escapeLucene` and `lexicalScoreFloor`;
    - grouping and ranking are `rankDocuments`;
    - the embedder is plan 1's `embeddings.Route`.
  - The only custom logic is removing stopwords from the query. ArcadeDB has no query-side option for it, and an engine-side analyzer would be custom Java plus an index rebuild. Its lists are Lucene's own files.
  - No committed code that nothing runs.
- **Where things run.**
  - Go tests run in WSL, never as a Windows `.exe`: `wsl -e bash -c 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go test <pkgs> -run "<regex>" -count=1'`. The single quotes keep Git Bash from expanding `$HOME`.
  - VM reads go through `$SCRATCH/r.sh`, run as `MSYS_NO_PATHCONV=1 wsl -e bash r.sh <script>` from `$SCRATCH`. `$SCRATCH` is `C:\Users\Davide\AppData\Local\Temp\claude\d--Aura\abc1caa3-c3d2-41b8-981d-d9633500c340\scratchpad`, and `r.sh` is `exec sshpass -p aura ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=8 aura@192.168.101.158 'bash -s' < "$1"`.
  - The live `arcadedb_integration` tier runs against the VM's ArcadeDB through an SSH tunnel (Task 8 Step 1). Local Docker is down, and E2E and data measurements run on the VM, never on a local container. Each live test creates and drops its own database; nothing else on the VM is written.
  - Never print a secret.
- **Test scope.** Each task runs the packages it names. `go vet ./...`, race, deadcode, coverage, the full `make quality-full` and mutation run at the end of plan 5 or in CI; mutation never runs locally.
- **Git.**
  - `--no-verify` is forbidden.
  - Another session shares the index: `git add` only new files the task names, and commit with `git commit -m "…" -- <paths>`.
  - Run gofmt on the task's Go files before committing: `wsl -e bash -c 'export PATH=$HOME/go/bin:$PATH; cd /mnt/d/Aura && $(go env GOROOT)/bin/gofmt -l <files>'` prints nothing when clean.
  - Master-direct. No push until plans 1–5 are done.
  - Commit messages end with `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
- **Files.** No file above 600 lines. `cmd/aura/main.go` is 597 today and `internal/arcadedb/memory_recall.go` 588: both must stay at or below 600. Every touched file loses its dead code in the same commit.
- **Measured values this plan pins** (lab VM, 2026-09-24):
  - The local sidecar attests `embeddinggemma-300M-Q8_0.gguf|size=327060480|params=307581696|embd=768|ftype=Q8_0`, which is space `es1-e0aa6accf0b79c6b`.
  - The stop lists from the ArcadeDB 26.9.1 container's `lucene-analysis-common-10.5.1.jar`:
    - `org/apache/lucene/analysis/snowball/italian_stop.txt`: 4903 bytes, sha256 `23e3d7c9d977756e5dd8ae9a120b51a468f904e79ea12bd084dcccfbba70ac5f`, 279 words;
    - `org/apache/lucene/analysis/snowball/english_stop.txt`: 4945 bytes, sha256 `c8d811c265112dc3e6c2f12bbc59af49f34c874be16436ca9ac7c56dd62a24f8`, 174 words;
    - 449 words in their union.
  - The lexical floor is `2` on `$score`, and `0` for a query of one remaining word (memory's `lexicalScoreFloor` rule).
- **`AURA_EMBED_DIMENSIONS` is environment-only in both processes** (`internal/settings/settings.go:4` keeps it out of `AllowedKeys`). The daemon's document space and the supervisor's stamp are therefore computed at the same width by the same `embeddings.RouteSpace`, and Task 7 pins it.

## Review Focus

1. **A question made only of stopwords or punctuation** ("chi è il?", "what is it", "?"), in lexical mode.
   - Expected: it abstains, sends no statement, and raises neither an error nor a Lucene parse error.
   - Pinned: Task 5 `TestLexicalReadsSendNothingForAStopwordOnlyQuery`, and Task 8's live "chi è il?" row.
2. **The embedding model swapped between the gate check and the query's embedding.**
   - Expected: lexical, with `embedding_space_mismatch`; neither dense leg runs.
   - Pinned: Task 4 `TestRetrieveRefusesADenseReadWhoseSpaceMovedDuringTheEmbedding`.
3. **A vector from another space written while the gate's 30 s "open" is still cached.**
   - Expected: it is never ranked.
   - Pinned: Task 4 `TestDenseDocumentLegsReadOnlyTheQuerysSpace` (the predicate is in both sub-pipelines of both statements), and Task 8 `TestDocumentSpaceLiveFusedLegNeverRanksAnotherSpace`. In the live test the other-space passage carries the query's exact vector and is still absent.
4. **The OpenRouter key rotated in the cockpit.**
   - Expected: `document_search` and the reasoning classifier send the new key on their next request, with no restart.
   - Pinned: Task 7 `TestDocumentQueryEmbedderReadsTheRotatedKey`.
5. **A hosted route with no key, or a sidecar that stops answering its attestation.**
   - Expected: documents answer lexically with `query_embedding_unavailable`. That is never an error, and never zero documents while a lexical match exists.
   - Pinned: Task 6 `TestLexicalModeAnswersEveryDenseFailure`, rows "no credential" and "attestation fails".

---

## File Structure

| file | change |
|---|---|
| `docs/superpowers/specs/2026-09-23-embedding-model-change-design.md` | §8 and §9 amended with the measurements; Files table; "does not prove" |
| `internal/arcadedb/relevance_floors.go` | **new**: the per-space floors table, `bindDenseFloors`, `FloorsCalibrated`, `ReasonUncalibratedFloors` |
| `internal/arcadedb/client.go` | the two dense floors lose their defaults (zero means "the table") |
| `internal/arcadedb/memory_vector.go` | `SearchFactsHybrid` binds floors by space; `FactSearchResult.FloorsReason` |
| `internal/arcadedb/memory_recall.go` | `recallSemantic` binds floors by space; `RecallResult.FloorsReason` |
| `internal/arcadedb/embedding_space.go` | gate generalized over `spaceCount`; `documentGate`; `DocumentsDenseOpen` |
| `internal/arcadedb/document_retrieval.go` | `FusedCandidateQuery.Space`; space predicate; floors by space; lexical leg in the decoder; `PassageCandidate.LexicalScore` and `Score()`; shared query validation |
| `internal/arcadedb/document_cards.go` | `DocumentCardsScoped` takes the space; `DocumentCards` deleted (test-only); fix-on-touch comment |
| `internal/arcadedb/document_schema.go` | the two unset floor fields and their defaults deleted (they move to the table) |
| `internal/arcadedb/document_lexical.go` | **new**: stopwords, `lexicalQuery`, `LexicalCandidates`, `LexicalDocumentCards` |
| `internal/arcadedb/stopwords/italian_stop.txt`, `english_stop.txt` | **new**: Lucene's lists, byte for byte |
| `internal/documents/retrieval.go` | `Retrieve` goes through the gate and chooses a mode; constants; `FloorsReason`; profile bump |
| `internal/documents/retrieval_space.go` | **new**: `QueryEmbedder`, `queryVector`, `denseQuery` |
| `internal/documents/retrieval_lexical.go` | **new**: `routeCards`, `passages` (the mode switch for each leg) |
| `internal/documents/retrieval_cards.go` | `CardQuery`; `DocumentsDenseOpen`; `LexicalDocumentCards`; shared card conversion |
| `internal/documents/retrieval_rank.go` | ranks by `PassageCandidate.Score()` |
| `cmd/aura/document_retrieval_wiring.go` | `newQueryEmbedder`; `wireDocumentQueryEmbedder` |
| `cmd/aura/memory_embedder.go` | `liveKey`; `logEmbeddingSpace` generalizes `logMemorySpace` |
| `cmd/aura/chat_boot.go`, `main.go`, `serve.go`, `docs.go` | live-key wiring; `runtimeToolHandles.Documents`; the documents space at boot; `documentQueryTimeout` |
| `cmd/aura/embedding_client.go` | **deleted** (no caller left) |
| `internal/runner/runner_deps.go` | the `Embedder` comment names the route |
| `internal/arcadedb/document_space_live_integration_test.go` | **new**: `arcadedb_integration`, external test package |

---

## Task 1: Baseline on the VM, pre-deploy checks, and the spec amendment

The spec is the measurement registry: it records first, then the code follows. Everything here is read-only on the VM.

**Files:**
- Modify: `docs/superpowers/specs/2026-09-23-embedding-model-change-design.md` (§8 at lines 440-460, §9 at 462-469, Files table, "What this design does not prove")
- Scratch (not committed): `$SCRATCH/q25.sh`, `$SCRATCH/q26.sh`

**Interfaces:**
- Consumes: nothing.
- Produces: the baseline table (in the spec) that plan 5's E2E compares against; the §8/§9 text every later task cites.

- [ ] **Step 1: Record today's `document_search` on the VM**

Write `$SCRATCH/q25.sh`:

```bash
# Read-only, on the VM: document_search as the running edge image answers it, for the queries
# plan 4's lexical measurements used, and how many document vectors already carry a stamp.
S() { echo aura | sudo -S -p '' "$@"; }
QUERIES=(
  "prompt engineering intelligenza artificiale"
  "fuso orario Europe/Rome"
  "come si scrive bene la domanda da porre al modello"
  "PID_Temp multi-zone temperature control"
  "approvals and durable grants"
  "safety light curtain"
  "heating and cooling actuator"
  "PidTemp MultiZone"
  "videoplayback"
  "ricetta della carbonara"
  "chi ha vinto il mondiale 1982"
  "orari dei traghetti per la Sardegna"
  "recipe for chocolate cake"
  "who won the 1982 world cup"
  "train timetable to Milan"
)
echo "image: $(S docker inspect -f '{{.Config.Image}}' aura)"
for q in "${QUERIES[@]}"; do
  if ! S docker exec aura aura docs search --limit 3 "$q" > /tmp/ds.json 2> /tmp/ds.err; then
    echo "$q -> ERROR $(head -c 300 /tmp/ds.err)"; continue
  fi
  python3 - "$q" <<'PY'
import json, sys
r = json.load(open("/tmp/ds.json"))
docs = [(d["title"][:30], round(d["score"], 4)) for d in r["documents"]]
print(f'{sys.argv[1][:50]:50} status={r["status"]} reason={r.get("degradation_reason", "")} '
      f'abstained={r["abstained"]} {docs}')
PY
done
rm -f /tmp/ds.json /tmp/ds.err
export PW=$(S docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' aura-arcadedb | grep -o 'rootPassword=[^ ]*' | head -1 | cut -d= -f2)
export IP=$(S docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}' aura-arcadedb | awk '{print $1}')
python3 - <<'PY'
import base64, json, os, urllib.request
auth = "Basic " + base64.b64encode(f"root:{os.environ['PW']}".encode()).decode()
db = "mem_448ddbe1_96ea_405d_8219_4a3d52a425c0"
for t in ("Passage", "IndexedDocument"):
    req = urllib.request.Request(f"http://{os.environ['IP']}:2480/api/v1/query/{db}", data=json.dumps(
        {"language": "sql", "command": f"SELECT embed_space, count(*) AS n FROM {t} GROUP BY embed_space"}).encode(),
        headers={"Authorization": auth, "Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=30) as resp:
        print(t, json.load(resp)["result"])
PY
```

Run: `cd $SCRATCH && MSYS_NO_PATHCONV=1 wsl -e bash r.sh q25.sh > plan4-baseline.txt 2>&1; cat plan4-baseline.txt`
Expected: an `image:` line and 15 query lines, none with `ERROR`, then one stamp-count line per type.
- If `aura docs search` fails inside the container, stop and report. This baseline is the "nothing broke" gate the operator asked for, so any substitute for it must be agreed first.

- [ ] **Step 2: Check the media routes that the deploy's re-extraction needs**

Plan 3's deploy re-extracts every document once (spec §6). A document whose re-extraction fails keeps its old, unstamped rows, and those keep the whole documents family lexical until someone fixes the file. The VM's 7 `videoplayback.mp4` need speech-to-text and its 3 screenshots need vision.

Write `$SCRATCH/q26.sh`:

```bash
# Read-only, on the VM: whether the media routes the re-extraction needs are configured, and
# how the ingest's last media extractions went. No extraction is run: a vision call may be billed.
S() { echo aura | sudo -S -p '' "$@"; }
echo "media fingerprint: $(S docker exec aura-ingest aura-media-index -fingerprint 2>&1 | head -c 200)"
S docker logs --since 168h aura-ingest 2>&1 | grep -E '\[extract\].*\.(png|jpg|jpeg|mp4|mp3|wav|m4a)|NoVisionRoute|media indexer exited' | tail -30
```

Run: `cd $SCRATCH && MSYS_NO_PATHCONV=1 wsl -e bash r.sh q26.sh`
Expected: a non-empty fingerprint, and no `NoVisionRoute` or `media indexer exited` line.
- **If either failure line appears, stop and report to the operator before Task 2.** The fix is the media route, not this plan, and shipping plan 3+4 onto a broken media route would leave documents lexical after the deploy.
- If the logs are empty, `docker logs` may have stopped at a truncated line (memory `reference_docker_logs_stops_at_truncated_json_line`). Read the container's `LogPath` raw with `sudo tail -c 200000`.

- [ ] **Step 3: Amend §8 of the spec**

Replace the whole §8 body (from `` `HostRetriever.Retrieve` serves documents lexically `` through `…and the response says so.`) with:

```markdown
`HostRetriever.Retrieve` serves documents lexically when the documents gate is closed, when
the gate cannot be read, and when the query cannot be embedded; until plan 4 the last one
returned zero documents (`retrieval.go:301-305`).

- Passages come from `SEARCH_INDEX('Passage[text]', :q)`, ranked by its score.
- Cards come from two separate queries, `IndexedDocument[card]` and
  `IndexedDocument[file_name_words]`, merged by the higher score. One `OR` query is not used:
  its `$score` depends on predicate order (measured: 1.3798 vs 1.2880 for the same document,
  audit F8).
- Results are grouped by `raw_sha256`, so identical files count once. Measured: five identical
  transcripts took the top five places for an unrelated question.
- A document ranks by the best score among its card and its passages.
- The status is `lexical_only`, with the reason `embedding_space_mismatch`,
  `embedding_space_check_failed` or `query_embedding_unavailable`.

**The floor, measured (amended 2026-09-24, plan 4).** On the lab VM's tenant (12
`IndexedDocument`, 50 `Passage`; ArcadeDB 26.9.1, whose new full-text indexes score BM25 with
k1 1.2 and b 0.75), raw BM25 does not separate: "orari dei traghetti per la Sardegna", which the
library cannot answer, scored 7.107 on the stopwords "dei", "la" and "per" alone, above the
correct top passage for "PidTemp MultiZone" (5.361). "chi ha vinto il mondiale 1982" scored
3.901. English out-of-corpus questions scored about 0.5 on "the", "for" and "to", which 39 of 50
passages contain.

ArcadeDB offers no query-side remedy. `SEARCH_INDEX` takes exactly the index and the query
(arcadedb-docs `how-to/data-modeling/full-text-index.adoc`; engine `SQLFunctionSearchIndex`,
"requires 2 parameters"): no per-query stopwords, no minimum-should-match. The analyzer that
could drop stopwords (`query_analyzer`) is fixed when the index is created, and no stock class
combines `StandardAnalyzer`'s tokenization with Italian and English stopwords and no stemming.
Changing it would move the ingest DDL, force a `REBUILD INDEX` of all three indexes and change
what the dense mode's own lexical sub-legs match. Rejected.

**Decision (operator, 2026-09-24).** Go removes Lucene's own Snowball Italian (279 words) and
English (174) stop lists from the query before sending it. The lists are copied byte for byte
from the `lucene-analysis-common-10.5.1.jar` ArcadeDB 26.9.1 runs
(`org/apache/lucene/analysis/snowball/{italian,english}_stop.txt`, sha256 `23e3d7c9…ac5f` and
`c8d811c2…24f8`). Memory's rule then applies: a floor of 2 on `$score`, and none for a query of
one remaining word. A query with no word left is not sent and abstains.

Measured with the stopwords removed, same tenant: all six out-of-corpus questions (three
Italian, three English) matched nothing at all; every known query kept its correct top
document; the weakest correct top match for a query of two or more words was a card at 2.68;
the strongest wrong match was 1.02.

Card and passage scores come from different indexes with different corpus statistics, so they
are not on one scale; the floor of 2 is one number measured against both, not a shared unit.

This deliberately amends `document_retrieval.go:20-24`. That measurement compared two fusions.
It says nothing about a single-leg lexical answer while the dense leg is unavailable. Lexical
results are candidates, and the status says so.
```

- [ ] **Step 4: Amend §9 of the spec**

Replace the §9 body with:

```markdown
The dense floors are measured values for EmbeddingGemma alone. They become a table keyed by
the **space** (amended 2026-09-24, plan 4: the space also names the width and the recipe, and
either changes the vectors a floor was measured on), holding one row today: `es1-e0aa6accf0b79c6b`,
the lab VM's local EmbeddingGemma-300M Q8_0 at 768 dimensions, recipe 1 (memory 0.72 / 0.28,
documents 0.72 / 0.32). A space with no row keeps those values, and every dense response carries
`uncalibrated_floors` in a field of its own, `floors_reason`. `Reason` and `degradation_reason`
name why a read left the dense path; an uncalibrated dense answer did not leave it. The memory
overrides `AURA_MEMORY_DENSE_MAX_DISTANCE_RATIO` and `AURA_MEMORY_MIN_RELEVANCE` still win over
the table when set. The cockpit and the preview state the same. Calibrating a new model is the
existing measurement procedure (the 2026-09-02 and 2026-09-09 calibrations behind
`relevance_floors.go`) adding a row. The E2E below records the first datum for the cloud model it
uses.
```

- [ ] **Step 5: Record the baseline and the Files rows**

Add at the end of §8 a sub-section with the Step 1 output as a table:

```markdown
**Baseline, 2026-09-24** (`aura docs search --limit 3` on the VM, image `<image from Step 1>`,
before plan 3+4 ship). After the deploy, plan 5's E2E runs the same queries: once both gates are
open, each must return the same top document.

| query | status | top documents (score) |
|---|---|---|
| <one row per Step 1 line> | | |
```

In the Files table, replace the row `` | `internal/arcadedb/client.go` | 420 | floors keyed by model | `` with these rows:

```markdown
| `internal/arcadedb/relevance_floors.go` | new | floors keyed by space (§9) |
| `internal/arcadedb/client.go` | 423 | floors lose their defaults |
| `internal/arcadedb/stopwords/*.txt` | new | Lucene's Snowball IT/EN lists (§8) |
| `cmd/aura/document_retrieval_wiring.go` | 42 | the document route; live key |
```

- [ ] **Step 6: Record what the §8 measurement does not prove**

Add to "What this design does not prove", after the "Lexical-mode quality" bullet:

```markdown
- **The lexical floor is measured on one small library.** One tenant, 12 documents, 15
  queries, top-1 only, one day. BM25's idf moves with corpus size, so a library of thousands can
  push correct matches below 2 or wrong ones above it. No stemming: "orari" does not match
  "orario". Indexes created before ArcadeDB's BM25 support keep CLASSIC scoring, where `$score`
  counts matched terms, until `REBUILD INDEX` (arcadedb-docs full-text-index): there the floor
  of 2 means "two terms", which was not measured.
```

- [ ] **Step 7: Commit**

```bash
git commit -m "docs(spec): record the lexical floor and per-space floors plan 4 measured

Raw BM25 cannot abstain on the VM's library: a question it cannot answer outscored
a correct match on its stopwords alone. ArcadeDB offers no query-side remedy, so the
Go side drops Lucene's own Snowball lists and applies memory's floor of 2, measured
to reject all six out-of-corpus questions and keep every known top document. The
floors table is keyed by space, and the baseline plan 5 compares against is recorded.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>" -- docs/superpowers/specs/2026-09-23-embedding-model-change-design.md
```

---

## Task 2: Relevance floors keyed by space (memory family)

**Files:**
- Create: `internal/arcadedb/relevance_floors.go`, `internal/arcadedb/relevance_floors_test.go`
- Modify: `internal/arcadedb/client.go:37-110` (limits), `internal/arcadedb/memory_vector.go:137-262`, `internal/arcadedb/memory_recall.go:105-115,227-275`, `internal/arcadedb/client_test.go:103-120`, `internal/arcadedb/memory_vector_test.go:17-26` (stub space)

**Interfaces:**
- Consumes: `embeddings.SpaceFor(kind config.EmbedKind, model, base string, dims int, artifact string) embeddings.Space`; `vectorDimensions` (768).
- Produces:
  - `type denseFloors struct{ maxDistance, minRelevance float64 }`;
  - `type calibration struct{ memory denseFloors }`; Task 4 adds `documents`;
  - `floorsFor(space string) (calibration, bool)`;
  - `(MemoryLimits) bindDenseFloors(params map[string]any, space string) string`;
  - `const ReasonUncalibratedFloors = "uncalibrated_floors"`;
  - `var embeddingGemmaSpace string`;
  - `FactSearchResult.FloorsReason`, `RecallResult.FloorsReason`.

- [ ] **Step 1: Let the stub embedder name any space**

In `internal/arcadedb/memory_vector_test.go`, add a `space string` field to `stubEmbedder`, and change its `Space` method to:

```go
func (s *stubEmbedder) Space(context.Context) (embeddings.Space, error) {
	id := s.space
	if id == "" {
		id = stubSpace
	}
	return embeddings.Space{ID: id}, s.spaceErr
}
```

- [ ] **Step 2: Write the failing tests**

Create `internal/arcadedb/relevance_floors_test.go`:

```go
package arcadedb

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The table's one row is keyed by the space the lab VM's sidecar attests (logged by the
// daemon and the ingest supervisor, 2026-09-24). A key that drifted from it would leave the
// production model uncalibrated with nothing to say so but a field.
func TestEmbeddingGemmaFloorsAreKeyedByTheSpaceTheVMAttests(t *testing.T) {
	if embeddingGemmaSpace != "es1-e0aa6accf0b79c6b" {
		t.Fatalf("embeddingGemmaSpace = %q, want es1-e0aa6accf0b79c6b", embeddingGemmaSpace)
	}
	floors, measured := floorsFor(embeddingGemmaSpace)
	if !measured || floors.memory != (denseFloors{maxDistance: 0.72, minRelevance: 0.28}) {
		t.Fatalf("floors = %+v measured = %v", floors, measured)
	}
}

func TestFloorsForAnUnmeasuredSpaceAreEmbeddingGemmasAndSaySo(t *testing.T) {
	floors, measured := floorsFor("es1-never-measured")
	if measured || floors != embeddingGemmaFloors {
		t.Fatalf("floors = %+v measured = %v, want EmbeddingGemma's, unmeasured", floors, measured)
	}
}

// AURA_MEMORY_DENSE_MAX_DISTANCE_RATIO and AURA_MEMORY_MIN_RELEVANCE are an operator's
// calibration, so a positive limit wins over the table; the reason still says whether the
// table had a row.
func TestBindDenseFloorsLetsAnOperatorOverrideWin(t *testing.T) {
	params := map[string]any{}
	reason := MemoryLimits{DenseMaxDistance: 0.5}.bindDenseFloors(params, embeddingGemmaSpace)
	if reason != "" || params["max_distance"] != 0.5 || params["min_relevance"] != 0.28 {
		t.Fatalf("measured space with an override: params = %v reason = %q", params, reason)
	}
	params = map[string]any{}
	reason = MemoryLimits{}.bindDenseFloors(params, "es1-never-measured")
	if reason != ReasonUncalibratedFloors || params["max_distance"] != 0.72 || params["min_relevance"] != 0.28 {
		t.Fatalf("unmeasured space: params = %v reason = %q", params, reason)
	}
}

func hybridFactsClient(t *testing.T, embedder DenseEmbedder) *Client {
	t.Helper()
	client, _ := routedClient(t, withOpenGate(func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		switch {
		case strings.Contains(statement, "vector.fuse"):
			return testResponse{Body: `{"result":[{"rid":"#3:1"}]}`}
		case strings.Contains(statement, "@rid IN"):
			return testResponse{Body: `{"result":[{"@rid":"#3:1","statement":"first","subject":"A","object":"B"}]}`}
		}
		return testResponse{Status: http.StatusBadRequest, Body: `{"detail":"unexpected query"}`}
	}))
	return client.WithEmbedder(embedder)
}

func TestSearchFactsHybridNamesFloorsNeverMeasuredForItsSpace(t *testing.T) {
	stub := &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}
	result, err := hybridFactsClient(t, stub).SearchFactsHybrid(context.Background(), "cliente torino", 1, time.Time{})
	if err != nil {
		t.Fatalf("SearchFactsHybrid: %v", err)
	}
	if result.RetrievalPath != retrievalPathHybrid || result.Reason != "" ||
		result.FloorsReason != ReasonUncalibratedFloors {
		t.Fatalf("result = %+v, want a hybrid answer naming its uncalibrated floors", result)
	}
	measured := &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}, space: embeddingGemmaSpace}
	result, err = hybridFactsClient(t, measured).SearchFactsHybrid(context.Background(), "cliente torino", 1, time.Time{})
	if err != nil {
		t.Fatalf("SearchFactsHybrid: %v", err)
	}
	if result.RetrievalPath != retrievalPathHybrid || result.FloorsReason != "" {
		t.Fatalf("result = %+v, want no floors reason in the measured space", result)
	}
}

func TestRecallNamesFloorsNeverMeasuredForItsSpace(t *testing.T) {
	recall := func(embedder DenseEmbedder) RecallResult {
		t.Helper()
		client, _ := routedClient(t, withOpenGate(func(request recordedRequest) testResponse {
			statement, _ := request.Payload["command"].(string)
			switch {
			case strings.Contains(statement, "vector.fuse"):
				return testResponse{Body: `{"result":[{"rid":"#10:1","score":0.03}]}`}
			case strings.Contains(statement, "FROM FACT") && strings.Contains(statement, "@rid IN"):
				return testResponse{Body: recallFactRow}
			}
			return testResponse{Body: `{"result":[]}`}
		}))
		result, err := client.WithEmbedder(embedder).RecallMemory(context.Background(), RecallRequest{
			IdentityID: "identity-a", Mode: RecallModeSemantic, Query: "blue notebook", Limit: 5,
		})
		if err != nil {
			t.Fatalf("RecallMemory: %v", err)
		}
		return result
	}
	if got := recall(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}); got.Retrieval.Path != retrievalPathHybrid ||
		got.FloorsReason != ReasonUncalibratedFloors {
		t.Fatalf("hybrid recall in an unmeasured space: path %q floors %q", got.Retrieval.Path, got.FloorsReason)
	}
	if got := recall(&stubEmbedder{err: context.DeadlineExceeded}); got.Retrieval.Path != retrievalPathLexical ||
		got.FloorsReason != "" {
		t.Fatalf("lexical recall named dense floors: path %q floors %q", got.Retrieval.Path, got.FloorsReason)
	}
}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `wsl -e bash -c 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go test ./internal/arcadedb/ -run "Floors" -count=1'`
Expected: build failure. `embeddingGemmaSpace`, `floorsFor`, `denseFloors`, `bindDenseFloors`, `ReasonUncalibratedFloors` and `FloorsReason` are undefined.

- [ ] **Step 4: Create `internal/arcadedb/relevance_floors.go`**

```go
package arcadedb

import (
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
)

// ReasonUncalibratedFloors marks a dense answer admitted by floors never measured for the
// space it ran in (spec §9). It is not a degradation -- the dense leg ran -- so it travels in a
// field of its own, apart from the reason a read left the dense path.
const ReasonUncalibratedFloors = "uncalibrated_floors"

// denseFloors is one family's dense admission: how far a neighbour may lie, and how low the
// reranked cosine of a kept candidate may fall. vector.neighbors returns its k nearest however
// far away they are, so without these nothing is ever "no qualified candidates".
type denseFloors struct {
	maxDistance  float64
	minRelevance float64
}

// calibration is one space's measured floors, per family.
type calibration struct {
	memory denseFloors
}

// embeddingGemmaFloors are the only floors ever measured, all on EmbeddingGemma-300M at 768
// dimensions.
//
// Memory, 2026-09-02, a live 102-fact memory: the nearest neighbour for a question the memory
// COULD answer sat at 0.514 and 0.667; for one it could not ("ricetta della pizza napoletana",
// "chi ha vinto il mondiale 1982") at 0.777 and 0.890, and 0.72 is the midpoint of that band.
// The previous 0.55 fell INSIDE the true-match band, so it discarded correct facts while
// admitting nothing useful -- the dense leg came back empty and "hybrid" retrieval ran on its
// lexical leg alone. Not established by that measure: conversation turns, other identities'
// corpora, or a memory much larger than 102 facts.
var embeddingGemmaFloors = calibration{
	memory: denseFloors{maxDistance: 0.72, minRelevance: 0.28},
}

// embeddingGemmaSpace is the space those measurements ran in: the local sidecar serving
// EmbeddingGemma-300M Q8_0 at 768 dimensions, recipe 1, as its /v1/models attests it (lab VM,
// 2026-09-24). The key is the space, not the model name, because the space also names the
// width and the recipe, and either changes the vectors a floor was measured on.
var embeddingGemmaSpace = embeddings.SpaceFor(config.EmbedLocal, "", "", vectorDimensions,
	"embeddinggemma-300M-Q8_0.gguf|size=327060480|params=307581696|embd=768|ftype=Q8_0").ID

// calibratedFloors holds one row per space whose floors were measured. Calibrating a new model
// is the measurement behind embeddingGemmaFloors, repeated, and a row added here.
var calibratedFloors = map[string]calibration{
	embeddingGemmaSpace: embeddingGemmaFloors,
}

// floorsFor is space's calibration and whether it was measured. A space without a row is
// served with EmbeddingGemma's floors, which may abstain too often or too rarely for another
// model -- which is what the reason says.
func floorsFor(space string) (calibration, bool) {
	floors, measured := calibratedFloors[space]
	if !measured {
		return embeddingGemmaFloors, false
	}
	return floors, true
}

// bindDenseFloors sets a dense memory read's floors for space, and returns
// ReasonUncalibratedFloors when the table has no row for it. A positive limit is an operator
// override (AURA_MEMORY_DENSE_MAX_DISTANCE_RATIO, AURA_MEMORY_MIN_RELEVANCE) and wins.
func (limits MemoryLimits) bindDenseFloors(params map[string]any, space string) string {
	calibrated, measured := floorsFor(space)
	floors := calibrated.memory
	if limits.DenseMaxDistance > 0 {
		floors.maxDistance = limits.DenseMaxDistance
	}
	if limits.MinRelevance > 0 {
		floors.minRelevance = limits.MinRelevance
	}
	params["max_distance"], params["min_relevance"] = floors.maxDistance, floors.minRelevance
	if measured {
		return ""
	}
	return ReasonUncalibratedFloors
}
```

- [ ] **Step 5: The two floors lose their defaults in `client.go`**

In `MemoryLimits`, the three consecutive fields `DenseMaxDistance`, `LexicalMinScore`, `MinRelevance` (lines 52-54) become:

```go
	// DenseMaxDistance and MinRelevance are operator overrides of the dense floors. Zero, the
	// default, means the floors measured for the reader's space (relevance_floors.go).
	DenseMaxDistance     float64
	LexicalMinScore      float64
	MinRelevance         float64
```

Let gofmt align them with the rest of the struct.

In `defaultMemoryLimits`, delete the comment block that starts `// 0.72 is the midpoint of a measured separation band` (it moved to `relevance_floors.go`). Replace `DenseMaxDistance: 0.72, LexicalMinScore: 2, MinRelevance: 0.28,` with `LexicalMinScore: 2,`.

In `normalized()`, delete the two lines that default `DenseMaxDistance` and `MinRelevance`. `validate()` stays: a negative override is still refused.

- [ ] **Step 6: Memory's dense reads bind floors by space**

In `memory_vector.go`, add to `FactSearchResult`, after `Reason string`:

```go
	// FloorsReason is ReasonUncalibratedFloors when a hybrid answer was admitted by floors
	// never measured for its space (spec §9). Reason names why a read left the dense path;
	// this names how far to trust one that did not.
	FloorsReason string
```

In `SearchFactsHybrid`, remove `"max_distance": limits.DenseMaxDistance, "min_relevance": limits.MinRelevance,` from the `params` literal, and replace `dense.bind(params)` with:

```go
	dense.bind(params)
	floorsReason := limits.bindDenseFloors(params, dense.space)
```

Set `FloorsReason: floorsReason` on both hybrid results: the abstaining `FactSearchResult{RetrievalPath: retrievalPathHybrid, Abstained: true, Reason: reasonNoQualifiedCandidates}`, and `result := FactSearchResult{Facts: hits, RetrievalPath: retrievalPathHybrid}`.

In `memory_recall.go`, add to `RecallResult`, after `Reason string`:

```go
	// FloorsReason is ReasonUncalibratedFloors on a hybrid recall admitted by floors never
	// measured for its space (spec §9).
	FloorsReason string
```

In `recallSemantic`:
- remove `"max_distance": limits.DenseMaxDistance, "min_relevance": limits.MinRelevance,` from the `params` literal;
- replace `path, reason := retrievalPathHybrid, ""` with `path, reason, floorsReason := retrievalPathHybrid, "", ""`;
- in the `else` branch, replace `dense.bind(params)` with:

```go
		dense.bind(params)
		floorsReason = limits.bindDenseFloors(params, dense.space)
```

After `result, err := c.hydrateRecallRanking(...)` and its error check, add:

```go
	if path == retrievalPathHybrid {
		result.FloorsReason = floorsReason
	}
```

- [ ] **Step 7: The defaults test follows the new contract**

In `client_test.go` `TestNewDefaultsMemoryLimits`, replace `DenseMaxDistance: 0.72, LexicalMinScore: 2, MinRelevance: 0.28,` with `LexicalMinScore: 2,`, and add above the `want` literal:

```go
	// The dense floors are not limits with a default any more: zero means the floors measured
	// for the reader's space, read at query time (relevance_floors.go, spec §9).
```

- [ ] **Step 8: Run the tests and the package**

Run: `wsl -e bash -c 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go test ./internal/arcadedb/ -count=1 && go vet ./internal/arcadedb/ ./cmd/arcadedb-mcp/ && go build ./...'`
Expected:
- `ok` for `./internal/arcadedb/`.
- `TestSearchFactsHybridRestoresFusionOrder` still sees `max_distance` 0.72: `es1-stub` has no row, so it falls back to EmbeddingGemma's floors.
- vet and build print nothing.

If `TestEmbeddingGemmaFloorsAreKeyedByTheSpaceTheVMAttests` fails on the ID, the artifact string differs from what `AttestLocal` builds. Read `internal/ingestsupervisor/route_test.go` `TestRouteResolverNamesTheLocalSidecarsSpace` (same string) and rule; never edit the expected ID.

Check line counts: `wc -l internal/arcadedb/memory_recall.go internal/arcadedb/memory_vector.go` both ≤ 600.

- [ ] **Step 9: Commit**

```bash
git add internal/arcadedb/relevance_floors.go internal/arcadedb/relevance_floors_test.go
git commit -m "feat(arcadedb): key memory's dense floors by the reader's space

The floors are measurements on EmbeddingGemma at 768 dimensions; under another
model they may abstain too often or too rarely, and nothing said so. They now
live in a table keyed by the space they were measured in, with one row, and a
hybrid answer in any other space carries uncalibrated_floors. Operator
overrides still win. TestNewDefaultsMemoryLimits changes because the two floors
no longer have a default: zero now means the table (spec §9).

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>" -- internal/arcadedb/relevance_floors.go internal/arcadedb/relevance_floors_test.go internal/arcadedb/client.go internal/arcadedb/client_test.go internal/arcadedb/memory_vector.go internal/arcadedb/memory_vector_test.go internal/arcadedb/memory_recall.go
```

---

## Task 3: The documents gate

**Files:**
- Modify: `internal/arcadedb/embedding_space.go:91-145`, `internal/arcadedb/client.go:190-192` (field)
- Test: `internal/arcadedb/embedding_space_test.go` (append)

**Interfaces:**
- Consumes: `missingIngestType(err, typeName)`, `documentPassageType`, `IndexedDocumentType`, `(*DocumentIndex).tenantClient`.
- Produces:
  - `func (d *DocumentIndex) DocumentsDenseOpen(ctx context.Context, identityID, space string) (bool, error)`;
  - `(*Client).documentsDenseOpen(ctx, space) (bool, error)`;
  - `Client.documentGate spaceGate`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/arcadedb/embedding_space_test.go`:

```go
func TestDocumentGateOpensOnlyWhenNoDocumentVectorIsInAnotherSpace(t *testing.T) {
	open, _ := gateClient(t, map[string]int{}, 0)
	if ok, err := open.documentsDenseOpen(context.Background(), "es1-a"); err != nil || !ok {
		t.Fatalf("no document vector outside the space: open=%v err=%v, want open", ok, err)
	}
	closed, requests := gateClient(t, map[string]int{IndexedDocumentType: 1}, 0)
	if ok, err := closed.documentsDenseOpen(context.Background(), "es1-a"); err != nil || ok {
		t.Fatalf("one card in another space: open=%v err=%v, want closed", ok, err)
	}
	counted := map[string]bool{}
	for _, request := range gateQueries(requests) {
		statement, _ := request.Payload["command"].(string)
		params, _ := request.Payload["params"].(map[string]any)
		if !strings.Contains(statement, "embedding IS NOT NULL") || !strings.Contains(statement, otherSpace) ||
			params["space"] != "es1-a" {
			t.Fatalf("gate count does not ask for vectors outside the space:\n%s params=%v", statement, params)
		}
		counted[strings.Fields(strings.TrimPrefix(statement, "SELECT count(*) AS n FROM "))[0]] = true
	}
	if !counted[documentPassageType] || !counted[IndexedDocumentType] || counted[factEdgeType] {
		t.Fatalf("documents gate counted %v, want Passage and IndexedDocument and no memory type", counted)
	}
}

// services/ingest declares both document types on its first run (arcade.py), so a tenant that
// never had a document has neither. That is an empty library, with no vector in any space.
func TestDocumentGateTreatsAnUningestedLibraryAsOpen(t *testing.T) {
	client, _ := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		if strings.Contains(statement, "FROM "+documentPassageType+" ") {
			return testResponse{Status: 500, Body: missingPassageBody}
		}
		return testResponse{Status: 500, Body: missingTypeBody}
	})
	if ok, err := client.documentsDenseOpen(context.Background(), "es1-a"); err != nil || !ok {
		t.Fatalf("empty library: open=%v err=%v, want open", ok, err)
	}
}

func TestDocumentGateFailsOnARealFault(t *testing.T) {
	client, _ := gateClient(t, nil, http.StatusInternalServerError)
	if _, err := client.documentsDenseOpen(context.Background(), "es1-a"); err == nil {
		t.Fatal("a refused count opened the gate")
	}
}

// The two families are gated apart (spec §3): a document that will not re-index must not turn
// dense memory off, and each family's answer is cached on its own.
func TestDocumentGateIsCachedApartFromMemory(t *testing.T) {
	client, requests := gateClient(t, map[string]int{documentPassageType: 3}, 0)
	ctx := context.Background()
	if ok, err := client.memoryDenseOpen(ctx, "es1-a"); err != nil || !ok {
		t.Fatalf("memory: open=%v err=%v, want open", ok, err)
	}
	if ok, err := client.documentsDenseOpen(ctx, "es1-a"); err != nil || ok {
		t.Fatalf("documents: open=%v err=%v, want closed", ok, err)
	}
	if _, err := client.documentsDenseOpen(ctx, "es1-a"); err != nil {
		t.Fatalf("documentsDenseOpen: %v", err)
	}
	// Memory counts its three types. Documents stops at Passage, whose 3 vectors already
	// close the gate, and the repeat is answered from the cache.
	if n := len(gateQueries(requests)); n != len(memorySpaceTypes)+1 {
		t.Fatalf("gate counts = %d, want %d", n, len(memorySpaceTypes)+1)
	}
}

func TestDocumentsDenseOpenAsksTheIdentitysOwnDatabase(t *testing.T) {
	index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
		return testResponse{Body: `{"result":[{"n":0}]}`}
	})
	if ok, err := index.DocumentsDenseOpen(t.Context(), documentTestIdentity, "es1-a"); err != nil || !ok {
		t.Fatalf("open=%v err=%v", ok, err)
	}
	if len(*requests) != 2 {
		t.Fatalf("requests = %d, want one count per document type", len(*requests))
	}
	if _, err := index.DocumentsDenseOpen(t.Context(), documentTestIdentity, " "); err == nil {
		t.Fatal("a gate without a space answered")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `wsl -e bash -c 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go test ./internal/arcadedb/ -run "DocumentGate|DocumentsDenseOpen" -count=1'`
Expected: build failure: `documentsDenseOpen` and `DocumentsDenseOpen` are undefined.

- [ ] **Step 3: Generalize the gate in `embedding_space.go`**

Replace everything from `// otherSpace matches a row` down to the end of `memoryDenseOpen` (lines 91-145) with:

```go
// otherSpace matches a row whose stamp is not :space, a missing stamp included. ArcadeDB
// happened to answer `NULL <> 'x'` as true (lab VM, 26.9.1, 2026-09-23), but its docs do not
// define it, and the explicit form does not depend on it.
const otherSpace = "(embed_space IS NULL OR embed_space <> :space)"

// spaceCount is one type's share of a family's gate.
type spaceCount struct {
	typeName  string
	statement string
	// ingested: services/ingest declares the type on its first run (arcade.py), so a tenant
	// with no document yet has none -- an empty library, with no vector in any space.
	ingested bool
}

// vectorsOutside counts typeName's vectors in another space. Rows without a vector are not
// counted: they cannot be ranked, so they cannot be ranked wrongly.
func vectorsOutside(typeName, live string) string {
	return "SELECT count(*) AS n FROM " + typeName + " WHERE embedding IS NOT NULL AND " + otherSpace + live
}

func (t memorySpaceType) gateCount() spaceCount {
	return spaceCount{typeName: t.name, statement: vectorsOutside(t.name, t.live)}
}

var (
	memoryGateCounts = []spaceCount{factSpace.gateCount(), turnSpace.gateCount(), traceSpace.gateCount()}
	// documentGateCounts is the documents family (spec §3), gated apart from memory.
	documentGateCounts = []spaceCount{
		{typeName: documentPassageType, statement: vectorsOutside(documentPassageType, ""), ingested: true},
		{typeName: IndexedDocumentType, statement: vectorsOutside(IndexedDocumentType, ""), ingested: true},
	}
)

// spaceGateTTL bounds how stale one tenant's gate answer can be: a pass that finishes
// opens the gate, and a stale writer closes it, within this.
const spaceGateTTL = 30 * time.Second

// spaceGate caches one tenant's answer for one family, keyed by the space it was asked for.
type spaceGate struct {
	mu      sync.Mutex
	space   string
	open    bool
	checked time.Time
}

// denseOpen reports whether no vector counted by counts is outside space. Dense retrieval
// ranks a query against the whole corpus, so one vector from another model makes every
// distance suspect: until all of them are re-embedded, the family is served lexically (spec
// §3, operator decision 2).
//
// The lock is held across the counts on purpose: readers of one tenant arriving together
// share one check instead of sending the counts each. The cost is that a waiter cannot give
// up before the holder's counts return, whatever its own deadline. Each count scans its type
// (the `<>` half of otherSpace uses no index): 25-34 ms at 5,000 rows, measured 2026-09-24
// (spec, "What this design does not prove").
func (c *Client) denseOpen(ctx context.Context, gate *spaceGate, counts []spaceCount, space string) (bool, error) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.space == space && time.Since(gate.checked) < spaceGateTTL {
		return gate.open, nil
	}
	open := true
	params := map[string]any{"space": space, "now": time.Now().UTC().Format(time.RFC3339Nano)}
	for _, count := range counts {
		rows, err := c.Query(ctx, count.statement, params)
		if err != nil {
			if count.ingested && missingIngestType(err, count.typeName) {
				continue
			}
			return false, fmt.Errorf("arcadedb: count %s vectors outside the space: %w", count.typeName, err)
		}
		if len(rows) > 0 && rowInt(rows[0], "n") > 0 {
			open = false
			break
		}
	}
	gate.space, gate.open, gate.checked = space, open, time.Now()
	return open, nil
}

// memoryDenseOpen reports whether every memory vector this tenant holds is in space.
func (c *Client) memoryDenseOpen(ctx context.Context, space string) (bool, error) {
	return c.denseOpen(ctx, &c.memoryGate, memoryGateCounts, space)
}

// documentsDenseOpen reports whether every document vector this tenant holds is in space.
func (c *Client) documentsDenseOpen(ctx context.Context, space string) (bool, error) {
	return c.denseOpen(ctx, &c.documentGate, documentGateCounts, space)
}

// DocumentsDenseOpen reports whether every Passage and IndexedDocument vector identityID holds
// is in space, so the dense legs may rank a query embedded there (spec §3). A tenant with
// nothing ingested is open.
func (d *DocumentIndex) DocumentsDenseOpen(ctx context.Context, identityID, space string) (bool, error) {
	if strings.TrimSpace(space) == "" {
		return false, fmt.Errorf("arcadedb: the document gate needs the reader's space")
	}
	client, err := d.tenantClient(ctx, identityID)
	if err != nil {
		return false, err
	}
	return client.documentsDenseOpen(ctx, space)
}
```

Add `"strings"` to the imports. The old method `memorySpaceType.mismatchCount()` is gone; `gateCount()` replaces it.

In `client.go`, after the `memoryGate spaceGate` field, add:

```go
	// documentGate is the same answer for this tenant's documents, gated apart from memory
	// (spec §3) so a document that will not re-index never turns dense memory off.
	documentGate spaceGate
```

- [ ] **Step 4: Run the tests and the package**

Run: `wsl -e bash -c 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go test ./internal/arcadedb/ -count=1 && go vet ./internal/arcadedb/'`
Expected: `ok`, and every existing memory gate test still passes.

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(arcadedb): gate the documents family on its own stamps

The dense document legs may rank a query only against a library wholly in the
query's space (spec §3). The memory gate becomes one gate over a list of counts,
and documents get their own: Passage and IndexedDocument, cached per tenant for
30 s apart from memory, with a library that was never ingested counted as open.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>" -- internal/arcadedb/embedding_space.go internal/arcadedb/embedding_space_test.go internal/arcadedb/client.go
```

---

## Task 4: Dense document legs bound to the query's space

After this task a dense read goes through the gate, and its two statements see only the query's space. Until Task 6, a closed gate or a moved space answers the way an embedding failure answers today: `degraded_card_only` with no documents. This intermediate state never ships (no push before plan 5).

**Files:**
- Modify:
  - `internal/arcadedb/relevance_floors.go` (documents row);
  - `internal/arcadedb/document_retrieval.go:57-143,145-187`;
  - `internal/arcadedb/document_cards.go:80-195`;
  - `internal/arcadedb/document_schema.go:14-106`;
  - `internal/documents/retrieval.go`;
  - `internal/documents/retrieval_cards.go`;
  - `cmd/aura/document_retrieval_wiring.go`.
- Create: `internal/documents/retrieval_space.go`, `internal/documents/retrieval_space_test.go`
- Test:
  - `internal/arcadedb/document_retrieval_floor_test.go`, `document_retrieval_test.go:236-242`, `document_cards_test.go`, `document_cards_missing_type_test.go`;
  - `internal/documents/retrieval_test.go` (fakes);
  - `internal/documents/retrieval_fusion_bench_test.go`, `retrieval_recall_bench_test.go` (tag `retrieval_eval`).

**Interfaces:**
- Consumes (Task 2): `floorsFor`, `embeddingGemmaFloors`, `ReasonUncalibratedFloors`, `denseSpaceFilter` (plan 2).
- Consumes (Task 3): `DocumentsDenseOpen`.
- Produces:
  - `FusedCandidateQuery.Space string`;
  - `(*DocumentIndex).DocumentCardsScoped(ctx, filter CandidateFilter, query string, embedding []float64, space string)`;
  - `func FloorsCalibrated(space string) bool`;
  - `(*DocumentIndex).validQuery(query string) (string, error)`;
  - `documents.QueryEmbedder`, `documents.CardQuery`, `(*HostRetriever).denseQuery(ctx, identityID, query) (queryVector, string, error)`, `RetrievalResponse.FloorsReason`, `DegradationSpaceMismatch`, `DegradationSpaceCheck`;
  - `RetrievalControlPlane.DocumentsDenseOpen`, `RetrievalControlPlane.RouteDocumentCards(ctx, CardQuery)`;
  - `cmd/aura newQueryEmbedder(cfg *config.Config, credential func() string) documents.QueryEmbedder`.

- [ ] **Step 1: Write the failing arcadedb tests**

In `document_retrieval_test.go`, give `fusedFixtureQuery` a space:

```go
func fusedFixtureQuery(query string) FusedCandidateQuery {
	return FusedCandidateQuery{
		IdentityID: documentTestIdentity,
		Query:      query,
		Embedding:  []float64{1, 0, 0},
		Space:      "es1-docs",
	}
}
```

In `document_retrieval_floor_test.go`, replace `TestDocumentConfigDefaultsTheRelevanceFloor` with:

```go
// A vector from another space is never ranked, even inside the 30 s a cached "open" gate
// lives (Review Focus 3). Both sub-pipelines carry the predicate: the neighbours' filter, and
// the lexical candidates vector.rerank re-scores against their stored vectors.
func TestDenseDocumentLegsReadOnlyTheQuerysSpace(t *testing.T) {
	index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
		return testResponse{Body: `{"result":[]}`}
	})
	if _, err := index.FusedCandidates(t.Context(), fusedFixtureQuery("clienti")); err != nil {
		t.Fatal(err)
	}
	if _, err := index.DocumentCardsScoped(t.Context(), CandidateFilter{IdentityID: documentTestIdentity, Limit: 2},
		"clienti", documentCardVector(), "es1-docs"); err != nil {
		t.Fatal(err)
	}
	if len(*requests) != 2 {
		t.Fatalf("requests = %d, want the fused and the card statement", len(*requests))
	}
	for _, request := range *requests {
		statement, _ := request.Payload["command"].(string)
		params, _ := request.Payload["params"].(map[string]any)
		if strings.Count(statement, denseSpaceFilter) != 2 || params["space"] != "es1-docs" {
			t.Fatalf("a dense document leg can rank another space's vector:\n%s\nparams=%v", statement, params)
		}
	}
}

// The floors are EmbeddingGemma's measured ones in its space and, lacking a row, in any
// other: the table changes nothing a healthy library returns today.
func TestDocumentLegsTakeTheirFloorsFromTheQuerysSpace(t *testing.T) {
	for _, space := range []string{embeddingGemmaSpace, "es1-never-measured"} {
		index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
			return testResponse{Body: `{"result":[]}`}
		})
		query := fusedFixtureQuery("clienti")
		query.Space = space
		if _, err := index.FusedCandidates(t.Context(), query); err != nil {
			t.Fatal(err)
		}
		params := (*requests)[0].Payload["params"].(map[string]any)
		if params["max_distance"] != 0.72 || params["min_relevance"] != 0.32 {
			t.Fatalf("space %s: floors = %v / %v, want 0.72 / 0.32", space, params["max_distance"], params["min_relevance"])
		}
	}
	if !FloorsCalibrated(embeddingGemmaSpace) || FloorsCalibrated("es1-never-measured") {
		t.Fatal("FloorsCalibrated disagrees with the table")
	}
}

func TestDenseDocumentLegsRefuseAQueryWithoutItsSpace(t *testing.T) {
	index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
		return testResponse{Body: `{"result":[]}`}
	})
	query := fusedFixtureQuery("clienti")
	query.Space = ""
	if _, err := index.FusedCandidates(t.Context(), query); err == nil {
		t.Fatal("a fused read without the query's space ran")
	}
	if _, err := index.DocumentCardsScoped(t.Context(), CandidateFilter{IdentityID: documentTestIdentity, Limit: 2},
		"clienti", documentCardVector(), " "); err == nil {
		t.Fatal("a card read without the query's space ran")
	}
	if len(*requests) != 0 {
		t.Fatalf("requests = %d, want none", len(*requests))
	}
}
```

Every existing call must carry the space, or it fails on the missing space instead of testing what it names:
- In `document_retrieval_test.go`, add `Space: "es1-docs",` to each `FusedCandidateQuery{…}` literal (lines 36, 121, 140, 164, 202, 225).
- In `document_cards_test.go`:
  - replace each `index.DocumentCards(t.Context(), documentTestIdentity, <query>, documentCardVector(), 3)` with `index.DocumentCardsScoped(t.Context(), CandidateFilter{IdentityID: documentTestIdentity, Limit: 3}, <query>, documentCardVector(), "es1-docs")`;
  - add the argument `"es1-docs"` after the vector in every `DocumentCardsScoped` call;
  - the calls are at lines 44, 69, 96, 118, 133, 141, 145, 149, 153, 270 and 292.
- In `document_cards_missing_type_test.go:38`, add `"es1-docs"` after `documentCardVector()`.

- [ ] **Step 2: Run them to verify they fail**

Run: `wsl -e bash -c 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go test ./internal/arcadedb/ -run "Document" -count=1'`
Expected: build failure. `FusedCandidateQuery.Space` and `FloorsCalibrated` are unknown, and `DocumentCardsScoped` has too many arguments.

- [ ] **Step 3: The documents row of the floors table**

In `relevance_floors.go`, add `documents denseFloors` to `calibration`, and `documents: denseFloors{maxDistance: 0.72, minRelevance: 0.32},` to `embeddingGemmaFloors`. Move into its doc comment the two measurement paragraphs from `document_schema.go`: the `DenseMaxDistance` one, "0.72 is the midpoint of a measured band…", and the `RelevanceFloor` one, from "RelevanceFloor is the abstention gate…" through "…not a shared constant.". Introduce them with `// Documents, 2026-09-09, the live 1079-passage corpus:`. Then add:

```go
// FloorsCalibrated reports whether the dense floors were measured for space.
func FloorsCalibrated(space string) bool {
	_, measured := calibratedFloors[space]
	return measured
}

// documentFloors are the dense document floors for space.
func documentFloors(space string) denseFloors {
	calibrated, _ := floorsFor(space)
	return calibrated.documents
}
```

In `document_schema.go`:
- delete the `DenseMaxDistance` and `RelevanceFloor` fields of `DocumentIndexConfig` with their comments. No caller ever set them: `document_index_wiring.go` sets `Dimensions` and `MaxRetrievalCandidates` only.
- delete the constants `defaultDocumentDenseMaxDistance` and `defaultDocumentRelevanceFloor` and the two `normalized()` lines that used them.

- [ ] **Step 4: Bind the space in both dense statements**

In `document_retrieval.go`, add to `FusedCandidateQuery`, after `Embedding []float64`:

```go
	// Space is the space Embedding is in. Both sub-pipelines read only rows stamped with it
	// (spec §3), and the floors are the ones measured for it (spec §9).
	Space string
```

Add a shared validation, used by all four document reads (the two dense ones now, the two lexical ones in Task 5):

```go
// validQuery trims a document query and bounds it.
func (d *DocumentIndex) validQuery(query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("arcadedb: document query must be non-empty")
	}
	if utf8.RuneCountInString(query) > d.config.MaxQueryRunes {
		return "", fmt.Errorf("arcadedb: document query exceeds %d characters", d.config.MaxQueryRunes)
	}
	return query, nil
}

// bindDense extends a candidate scope to the query's space and binds that space's floors.
func bindDense(where string, params map[string]any, space string) (string, error) {
	if strings.TrimSpace(space) == "" {
		return "", fmt.Errorf("arcadedb: a dense document read needs the query's embedding space")
	}
	floors := documentFloors(space)
	params["space"], params["max_distance"], params["min_relevance"] = space, floors.maxDistance, floors.minRelevance
	return where + denseSpaceFilter, nil
}
```

In `FusedCandidates`:
- replace the block from `query := strings.TrimSpace(request.Query)` through the rune-count check with:

```go
	query, err := d.validQuery(request.Query)
	if err != nil {
		return nil, err
	}
```

- replace `where, params := candidateWhere(filter)` and the two lines setting `max_distance` and `min_relevance` with:

```go
	where, params := candidateWhere(filter)
	if where, err = bindDense(where, params, request.Space); err != nil {
		return nil, err
	}
```

Keep the lines that set `embedding`, `query`, `fetch` and `candidates`. The space check must run before `tenantClient`, so `TestDenseDocumentLegsRefuseAQueryWithoutItsSpace` sees no request: move `client, err := d.tenantClient(...)` below it.

In the `PassageCandidate.FusedScore` comment, replace "bounded below by RelevanceFloor" with "bounded below by the documents' relevance floor (relevance_floors.go)". In the `fusedStatement` comment, replace "See RelevanceFloor." with "See embeddingGemmaFloors."

- [ ] **Step 5: The card leg takes the space; the test-only wrapper goes**

In `document_cards.go`:
- delete `DocumentCards` (it had no production caller);
- replace the doc comment block that sat above it (lines 113-120) with a comment on `DocumentCardsScoped`, and change its signature and the start of its body:

```go
// DocumentCardsScoped ranks documents by their own description, constrained by the same
// document ids and Garage source coordinates as the fused passage leg, and scored on the same
// reranked cosine against the query's vector. It needs that vector: an embedding failure takes
// it out together with the passage leg, and lexical mode (document_lexical.go) answers then.
// Both legs of the OR matter: the card matches what a document CONTAINS ("Fatturato",
// "Torino"), the split file name matches what it is CALLED. The name needs its own property
// because Lucene's StandardAnalyzer keeps "clienti_complesso.xlsx" as one token -- measured --
// so a search for "clienti" finds nothing against the raw name.
func (d *DocumentIndex) DocumentCardsScoped(
	ctx context.Context,
	filter CandidateFilter,
	query string,
	embedding []float64,
	space string,
) ([]DocumentCard, error) {
	filter, err := d.normalizeCandidateFilter(filter)
	if err != nil {
		return nil, err
	}
	query, err = d.validQuery(query)
	if err != nil {
		return nil, err
	}
	if err := validateDenseVector(embedding, d.config.Dimensions); err != nil {
		return nil, err
	}
	where, params := candidateWhere(filter)
	if where, err = bindDense(where, params, space); err != nil {
		return nil, err
	}
	client, err := d.tenantClient(ctx, filter.IdentityID)
	if err != nil {
		return nil, err
	}
	params["query"] = escapeLucene(query)
	params["embedding"] = append([]float64(nil), embedding...)
	params["fetch"] = fusedDenseNeighbours
	params["candidates"] = min(max(filter.Limit*4, 20), d.config.MaxRetrievalCandidates)
```

The rest of the body is unchanged from `rows, err := client.Query(ctx, documentCardStatement(where, filter.Limit), params)` on.

- [ ] **Step 6: Run the arcadedb suite**

Run: `wsl -e bash -c 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go test ./internal/arcadedb/ -count=1'`
Expected: `ok`. `go build ./...` still fails in `internal/documents`, which Steps 7-11 fix.

- [ ] **Step 7: Write the failing documents tests**

In `internal/documents/retrieval_test.go`, update the fakes (import `embeddings`):

```go
type fakeRetrievalControl struct {
	scopeRequest      []string
	scope             []string
	cardDocumentScope []string
	cardEmbedding     []float64
	cardSpace         string
	cardSourceScope   []SourceScope
	cards             []RetrievalCard
	names             map[string]string
	namesRequest      []string
	namesErr          error
	err               error
	// closed answers the documents gate "a vector is in another space"; gateErr fails it.
	closed    bool
	gateErr   error
	gateSpace string
}

func (f *fakeRetrievalControl) DocumentsDenseOpen(_ context.Context, _ string, space string) (bool, error) {
	f.gateSpace = space
	return !f.closed, f.gateErr
}

func (f *fakeRetrievalControl) RouteDocumentCards(_ context.Context, query CardQuery) ([]RetrievalCard, error) {
	f.cardEmbedding = append([]float64(nil), query.Vector...)
	f.cardSpace = query.Space
	f.cardDocumentScope = append([]string(nil), query.DocumentIDs...)
	f.cardSourceScope = append([]SourceScope(nil), query.SourceScopes...)
	if f.err != nil {
		return nil, f.err
	}
	return append([]RetrievalCard(nil), f.cards...), nil
}

type fakeRetrievalEmbedder struct {
	inputs []string
	vector []float64
	err    error
	// spaces are answered in order, the last one repeated; "es1-docs" when empty.
	spaces     []string
	spaceErr   error
	spaceCalls int
}

func (f *fakeRetrievalEmbedder) Space(context.Context) (embeddings.Space, error) {
	if f.spaceErr != nil {
		return embeddings.Space{}, f.spaceErr
	}
	space := "es1-docs"
	if len(f.spaces) > 0 {
		space = f.spaces[min(f.spaceCalls, len(f.spaces)-1)]
	}
	f.spaceCalls++
	return embeddings.Space{ID: space}, nil
}
```

The old positional `RouteDocumentCards` is replaced; `Embed` is unchanged.

Create `internal/documents/retrieval_space_test.go`:

```go
package documents

import (
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/embeddings"
)

func spaceRetriever(control *fakeRetrievalControl, embedder *fakeRetrievalEmbedder, index *fakePassageIndex) *HostRetriever {
	return &HostRetriever{
		ControlPlane: control, PassageIndex: index, Embedder: embedder,
		Config: RetrievalConfig{CandidateLimit: 20},
	}
}

func retrieveCodice(t *testing.T, retriever *HostRetriever) RetrievalResponse {
	t.Helper()
	response, err := retriever.Retrieve(t.Context(), RetrievalRequest{IdentityID: retrievalIdentity, Query: "codice cliente"})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	return response
}

// A closed gate is answered before the query is embedded, so it costs no embedding request.
func TestRetrieveAsksTheGateBeforeEmbedding(t *testing.T) {
	control := &fakeRetrievalControl{closed: true, cards: []RetrievalCard{retrievalCard()}}
	embedder := &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}}
	response := retrieveCodice(t, spaceRetriever(control, embedder, &fakePassageIndex{}))
	if response.DegradationReason != DegradationSpaceMismatch || embedder.inputs != nil ||
		control.gateSpace != "es1-docs" || control.cardEmbedding != nil {
		t.Fatalf("response = %#v inputs = %v gate = %q", response, embedder.inputs, control.gateSpace)
	}
}

func TestRetrieveBindsTheDenseLegsToTheQuerysSpace(t *testing.T) {
	control := &fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}}
	index := &fakePassageIndex{}
	embedder := &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}, spaces: []string{"es1-a"}}
	response := retrieveCodice(t, spaceRetriever(control, embedder, index))
	if control.gateSpace != "es1-a" || control.cardSpace != "es1-a" || index.fusedQuery.Space != "es1-a" {
		t.Fatalf("gate %q cards %q passages %q, want es1-a everywhere",
			control.gateSpace, control.cardSpace, index.fusedQuery.Space)
	}
	if response.DegradationReason != "" || response.FloorsReason != arcadedb.ReasonUncalibratedFloors {
		t.Fatalf("response = %#v, want a dense answer naming its uncalibrated floors", response)
	}
}

func TestRetrieveNamesNoFloorsReasonInTheMeasuredSpace(t *testing.T) {
	embedder := &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}, spaces: []string{"es1-e0aa6accf0b79c6b"}}
	response := retrieveCodice(t, spaceRetriever(&fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}},
		embedder, &fakePassageIndex{}))
	if response.FloorsReason != "" {
		t.Fatalf("floors reason = %q in the space the floors were measured in", response.FloorsReason)
	}
}

// Review Focus 2: a model swapped between the gate and the embedding would rank the new
// model's vector against a library checked in the old space.
func TestRetrieveRefusesADenseReadWhoseSpaceMovedDuringTheEmbedding(t *testing.T) {
	control := &fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}}
	index := &fakePassageIndex{}
	embedder := &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}, spaces: []string{"es1-a", "es1-b"}}
	response := retrieveCodice(t, spaceRetriever(control, embedder, index))
	if response.DegradationReason != DegradationSpaceMismatch || control.cardEmbedding != nil ||
		index.fusedQuery.Space != "" {
		t.Fatalf("response = %#v; a dense leg ran after the space moved", response)
	}
}

func TestRetrieveNamesAGateItCouldNotRead(t *testing.T) {
	control := &fakeRetrievalControl{gateErr: errors.New("count refused"), cards: []RetrievalCard{retrievalCard()}}
	embedder := &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}}
	response := retrieveCodice(t, spaceRetriever(control, embedder, &fakePassageIndex{}))
	if response.DegradationReason != DegradationSpaceCheck || embedder.inputs != nil {
		t.Fatalf("response = %#v", response)
	}
}

// Review Focus 5: a hosted route with no key names no space; that is the embedder being
// unavailable, not a failed call.
func TestRetrieveWithoutACredentialIsAnEmbeddingDegradation(t *testing.T) {
	control := &fakeRetrievalControl{cards: []RetrievalCard{retrievalCard()}}
	embedder := &fakeRetrievalEmbedder{spaceErr: embeddings.ErrNoCredential}
	response := retrieveCodice(t, spaceRetriever(control, embedder, &fakePassageIndex{}))
	if response.DegradationReason != DegradationEmbedding || control.gateSpace != "" {
		t.Fatalf("response = %#v gate = %q", response, control.gateSpace)
	}
}
```

- [ ] **Step 8: Run them to verify they fail**

Run: `wsl -e bash -c 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go test ./internal/documents/ -count=1'`
Expected: build failure: `CardQuery`, `DegradationSpaceMismatch`, `DegradationSpaceCheck` and `FloorsReason` are undefined.

- [ ] **Step 9: `internal/documents/retrieval_space.go`**

```go
package documents

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/embeddings"
)

// QueryEmbedder embeds a query and names the space its vector is in (embeddings.Route), so
// the dense legs run only over a library wholly in that space (spec §3).
type QueryEmbedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
	Space(ctx context.Context) (embeddings.Space, error)
}

// queryVector is the query embedded for the dense legs, and the space its vector is in.
type queryVector struct {
	vector []float64
	space  string
}

var errNoQueryEmbedder = errors.New("documents: retrieval embedder is not configured")

// denseQuery is the dense legs' entry: the embedded query, or the degradation that keeps the
// read off them and its cause. The gate is asked before the query is embedded, so a closed
// gate costs no request. The space is read again after, because a model swapped in between
// would rank a new model's vector against a library checked in the old space.
func (r *HostRetriever) denseQuery(ctx context.Context, identityID, query string) (queryVector, string, error) {
	if r.Embedder == nil {
		return queryVector{}, DegradationEmbedding, errNoQueryEmbedder
	}
	space, err := r.Embedder.Space(ctx)
	if err != nil {
		return queryVector{}, DegradationEmbedding, err
	}
	open, err := r.ControlPlane.DocumentsDenseOpen(ctx, identityID, space.ID)
	if err != nil {
		return queryVector{}, DegradationSpaceCheck, err
	}
	if !open {
		return queryVector{}, DegradationSpaceMismatch,
			fmt.Errorf("the library holds vectors outside space %s", space.ID)
	}
	vectors, err := r.Embedder.Embed(ctx, embeddings.RetrievalQueries([]string{query}))
	if err != nil {
		return queryVector{}, DegradationEmbedding, err
	}
	if len(vectors) != 1 {
		return queryVector{}, DegradationEmbedding,
			fmt.Errorf("documents: embedder returned %d vectors, want 1", len(vectors))
	}
	after, err := r.Embedder.Space(ctx)
	if err != nil {
		return queryVector{}, DegradationEmbedding, err
	}
	if after.ID != space.ID {
		return queryVector{}, DegradationSpaceMismatch,
			fmt.Errorf("the embedding space moved from %s to %s during the query", space.ID, after.ID)
	}
	return queryVector{vector: vectors[0], space: space.ID}, "", nil
}
```

- [ ] **Step 10: `retrieval.go` and `retrieval_cards.go`**

In `retrieval.go`:
- Change `HostRetriever.Embedder` to `Embedder QueryEmbedder`, and delete `embedQuery` (its job is in `denseQuery`).
- Add to the `const` block, after `DegradationUnconfigured`:

```go
	// DegradationSpaceMismatch: the library holds vectors from another embedding space, or
	// the space moved while the query was embedded; ranking across two spaces is meaningless
	// (spec §3). DegradationSpaceCheck: the gate that decides it could not be read.
	DegradationSpaceMismatch = "embedding_space_mismatch"
	DegradationSpaceCheck    = "embedding_space_check_failed"
```

- Add to `RetrievalResponse`, after `DegradationReason`:

```go
	// FloorsReason is "uncalibrated_floors" when a dense answer was admitted by relevance
	// floors never measured for its embedding space (spec §9). It is not a degradation.
	FloorsReason string `json:"floors_reason,omitempty"`
```

- In `RetrievalControlPlane`, replace the `RouteDocumentCards(...)` line with:

```go
	// DocumentsDenseOpen: every document vector the identity holds is in the space.
	DocumentsDenseOpen(ctx context.Context, identityID, space string) (bool, error)
	RouteDocumentCards(context.Context, CardQuery) ([]RetrievalCard, error)
```

- In `Retrieve`, replace the comment block starting `// The embedding now comes before BOTH legs` and the `vectors, embedErr := r.embedQuery(...)` block with:

```go
	// The dense legs run only over a library wholly in the query's space (spec §3). Both are
	// scored against the query vector, so without one neither can run.
	dense, reason, cause := r.denseQuery(ctx, request.IdentityID, request.Query)
	if reason != "" {
		response.Status, response.DegradationReason = RetrievalCardOnly, reason
		r.degradations.warn(reason, cause.Error(), request.IdentityID)
		return response, nil
	}
	if !arcadedb.FloorsCalibrated(dense.space) {
		response.FloorsReason = arcadedb.ReasonUncalibratedFloors
	}
	cards, err := r.ControlPlane.RouteDocumentCards(ctx, CardQuery{
		IdentityID: request.IdentityID, Query: request.Query, Vector: dense.vector, Space: dense.space,
		DocumentIDs: scope, SourceScopes: request.SourceScopes, Limit: cfg.CandidateLimit,
	})
```

- In the `FusedCandidates` call, replace `Query: request.Query, Embedding: vectors, Strategy: cfg.FusionStrategy,` with `Query: request.Query, Embedding: dense.vector, Space: dense.space, Strategy: cfg.FusionStrategy,`.

In `retrieval_cards.go`, replace `RouteDocumentCards` with:

```go
// CardQuery is one card-leg request: the query and its scope, and for the dense leg the
// query's vector and the space it is in.
type CardQuery struct {
	IdentityID   string
	Query        string
	Vector       []float64
	Space        string
	DocumentIDs  []string
	SourceScopes []SourceScope
	Limit        int
}

func (q CardQuery) filter() arcadedb.CandidateFilter {
	sourceKeys, sourcePrefixes := ArcadeSourceFilters(q.SourceScopes)
	return arcadedb.CandidateFilter{
		IdentityID: q.IdentityID, Limit: q.Limit, DocumentIDs: q.DocumentIDs,
		SourceKeys: sourceKeys, SourcePrefixes: sourcePrefixes,
	}
}

// DocumentsDenseOpen asks the identity's documents gate (arcadedb embedding_space.go).
func (c *ArcadeRetrievalControlPlane) DocumentsDenseOpen(
	ctx context.Context, identityID, space string,
) (bool, error) {
	if c == nil || c.Index == nil {
		return false, errRetrievalControlPlaneUnset
	}
	return c.Index.DocumentsDenseOpen(ctx, identityID, space)
}

// RouteDocumentCards ranks documents by their own description inside the exact same scope
// as the passage leg. An ignored filter here would turn a scoped search into an unscoped one.
func (c *ArcadeRetrievalControlPlane) RouteDocumentCards(ctx context.Context, q CardQuery) ([]RetrievalCard, error) {
	if c == nil || c.Index == nil {
		return nil, errRetrievalControlPlaneUnset
	}
	found, err := c.Index.DocumentCardsScoped(ctx, q.filter(), q.Query, q.Vector, q.Space)
	if err != nil {
		return nil, err
	}
	return retrievalCards(found), nil
}

// retrievalCards is ArcadeDB's card record as the ranking reads it.
func retrievalCards(found []arcadedb.DocumentCard) []RetrievalCard {
	cards := make([]RetrievalCard, 0, len(found))
	for _, card := range found {
		cards = append(cards, RetrievalCard{
			DocumentID: card.SearchDocumentID,
			// The file name IS the title. There is no catalog row to hold a nicer one, and
			// a name is what a person uses to ask for a file.
			Title:            card.FileName,
			SourceKind:       card.SourceKind,
			SourceKey:        card.SourceKey,
			Card:             card.Card,
			Rank:             card.Score,
			OriginalSHA256:   card.RawSHA256,
			NormalizedSHA256: card.NormalizedSHA256,
			SizeBytes:        card.SizeBytes,
			PassageCount:     card.PassageCount,
			IndexedAt:        card.IndexedAt,
		})
	}
	return cards
}
```

- [ ] **Step 11: Callers the compiler names**

- `cmd/aura/document_retrieval_wiring.go` (add the `internal/embeddings` import):
  - replace `Embedder: embeddingClient(cfg, documentHTTPClient(cfg)),` with `Embedder: newQueryEmbedder(cfg, func() string { return cfg.LLM.APIKey }),`. The boot key is what `embeddingClient` sent; Task 7 makes it live.
  - add:

```go
// newQueryEmbedder is the daemon's route for embedding a document query, at the documents'
// width (AURA_EMBED_DIMENSIONS, spec §1), or nil when dense embedding is switched off. It is an
// interface on purpose: a nil *embeddings.Route stored in one is non-nil. The timeout is the
// one the query embedder always had.
func newQueryEmbedder(cfg *config.Config, credential func() string) documents.QueryEmbedder {
	route := embeddings.NewRoute(cfg.Embed, credential, cfg.Embed.Dimensions, documentHTTPClient(cfg).Timeout)
	if route == nil {
		return nil
	}
	return route
}
```

- `internal/documents/retrieval_fusion_bench_test.go` and `retrieval_recall_bench_test.go` (tag `retrieval_eval`):
  - after `embedder := arcadedb.NewMemoryEmbedder(...)`, read `space, err := embedder.Space(ctx)` (`t.Fatalf` on error);
  - pass `Space: space.ID` in every `FusedCandidateQuery`;
  - replace `index.DocumentCards(ctx, identity, question.Query, vectors[0], cfg.CandidateLimit)` with `index.DocumentCardsScoped(ctx, arcadedb.CandidateFilter{IdentityID: identity, Limit: cfg.CandidateLimit}, question.Query, vectors[0], space.ID)`;
  - add `space.ID` to the recall bench's `DocumentCardsScoped` call.

- Any other test the compiler names (`retrieval_neighbours_test.go`, `retrieval_degradation_test.go`) builds unchanged: the fake embedder now has `Space`.

- [ ] **Step 12: Run everything this task touched**

Run: `wsl -e bash -c 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go build ./... && go vet ./internal/arcadedb/ ./internal/documents/ ./cmd/aura/ && go vet -tags retrieval_eval ./internal/documents/ && go test ./internal/arcadedb/ ./internal/documents/ -count=1 && go test ./internal/agent/tools/ -run "Document" -count=1 && go test ./cmd/aura/ -run "Document|Retriev|Docs" -count=1'`
Expected:
- everything `ok`;
- `TestHostRetrieverDegradationIsExplicit` still passes, because an embedding failure still answers card-only with no documents until Task 6;
- the retrieval_eval vet prints nothing.

Check: `wc -l internal/documents/retrieval.go internal/arcadedb/document_retrieval.go internal/arcadedb/document_cards.go` all ≤ 600.

- [ ] **Step 13: Commit**

```bash
git add internal/documents/retrieval_space.go internal/documents/retrieval_space_test.go
git commit -m "feat(documents): bind the dense document legs to the query's space

A document query is ranked only against a library wholly in its vector's space:
the retriever asks the documents gate before embedding, reads the space again
after, and both dense statements filter every candidate by it, so a stale vector
is never ranked even inside the gate's cache. Floors come from the per-space
table (unchanged 0.72/0.32 for EmbeddingGemma) and an unmeasured space says so.
The card-leg comment that claimed it survives an embedding failure is corrected,
and the test-only DocumentCards wrapper and two floor fields no caller set are
removed. TestDocumentConfigDefaultsTheRelevanceFloor is replaced: the floor is no
longer a config default but a row of the table.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>" -- internal/arcadedb/relevance_floors.go internal/arcadedb/document_retrieval.go internal/arcadedb/document_cards.go internal/arcadedb/document_schema.go internal/arcadedb/document_retrieval_floor_test.go internal/arcadedb/document_retrieval_test.go internal/arcadedb/document_cards_test.go internal/arcadedb/document_cards_missing_type_test.go internal/documents/retrieval.go internal/documents/retrieval_cards.go internal/documents/retrieval_space.go internal/documents/retrieval_space_test.go internal/documents/retrieval_test.go internal/documents/retrieval_fusion_bench_test.go internal/documents/retrieval_recall_bench_test.go cmd/aura/document_retrieval_wiring.go
```

---

## Task 5: Lexical document statements

**Files:**
- Create:
  - `internal/arcadedb/stopwords/italian_stop.txt`, `internal/arcadedb/stopwords/english_stop.txt`;
  - `internal/arcadedb/document_lexical.go`, `internal/arcadedb/document_lexical_test.go`.
- Modify: `internal/arcadedb/document_retrieval.go` (leg constant, `LexicalScore`, `Score()`, decoder)

**Interfaces:**
- Consumes: `candidateWhere`, `escapeLucene`, `lexicalScoreFloor`, `validQuery` (Task 4), `decodeCandidates`, `decodeDocumentCard`, `missingIngestType`, `candidateFixture` (tests).
- Produces:
  - `RetrievalLegLexical RetrievalLeg = "lexical"`;
  - `PassageCandidate.LexicalScore *float64`;
  - `func (c PassageCandidate) Score() *float64`;
  - `func (d *DocumentIndex) LexicalCandidates(ctx context.Context, filter CandidateFilter, query string) ([]PassageCandidate, error)`;
  - `func (d *DocumentIndex) LexicalDocumentCards(ctx context.Context, filter CandidateFilter, query string) ([]DocumentCard, error)`.

- [ ] **Step 1: Copy Lucene's stop lists byte for byte**

The measurement run already saved both files, verified by sha256, in `$SCRATCH/stop/`. Copy them:

```bash
mkdir -p internal/arcadedb/stopwords
cp "$SCRATCH/stop/italian_stop.txt" "$SCRATCH/stop/english_stop.txt" internal/arcadedb/stopwords/
sha256sum internal/arcadedb/stopwords/*.txt
```

Expected: `23e3d7c9d977756e5dd8ae9a120b51a468f904e79ea12bd084dcccfbba70ac5f` for italian and `c8d811c265112dc3e6c2f12bbc59af49f34c874be16436ca9ac7c56dd62a24f8` for english.
- If `$SCRATCH/stop` is gone, re-extract read-only with `$SCRATCH/q24.sh` (it prints base64) and decode.
- The files must stay LF. Check with `tr -dc '\r' < file | wc -c` → `0`: a CRLF checkout would change the hash.

- [ ] **Step 2: Write the failing tests**

Create `internal/arcadedb/document_lexical_test.go`:

```go
package arcadedb

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// The lists are Lucene's own, byte for byte, from the jar ArcadeDB 26.9.1 runs: a list edited
// here would silently disagree with the one the measurement in spec §8 used.
func TestStopwordListsAreLucenesOwn(t *testing.T) {
	for _, list := range []struct {
		name, text, sha string
		words           int
	}{
		{"italian", italianStopwords, "23e3d7c9d977756e5dd8ae9a120b51a468f904e79ea12bd084dcccfbba70ac5f", 279},
		{"english", englishStopwords, "c8d811c265112dc3e6c2f12bbc59af49f34c874be16436ca9ac7c56dd62a24f8", 174},
	} {
		sum := sha256.Sum256([]byte(list.text))
		if hex.EncodeToString(sum[:]) != list.sha {
			t.Fatalf("%s list is not Lucene's: sha256 %x", list.name, sum)
		}
		if n := len(stopwordSet(list.text)); n != list.words {
			t.Fatalf("%s list reads %d words, want %d", list.name, n, list.words)
		}
	}
	if len(documentStopwords) != 449 {
		t.Fatalf("union = %d words, want 449", len(documentStopwords))
	}
}

func TestLexicalQueryDropsStopwordsAndEarnsItsFloor(t *testing.T) {
	for _, test := range []struct {
		query, terms string
		floor        float64
	}{
		{"orari dei traghetti per la Sardegna", "orari traghetti Sardegna", 2},
		{"chi ha vinto il mondiale 1982", "vinto mondiale 1982", 2},
		{"approvals and durable grants", "approvals durable grants", 2},
		{"PID_Temp (multi-zone)?", "PID_Temp (multi-zone)?", 2},
		{"videoplayback", "videoplayback", 0},
		{"the PidTemp?", "PidTemp?", 0},
		{"chi è il?", "", 0},
		{"what is it", "", 0},
		{"?", "", 0},
	} {
		terms, floor := lexicalQuery(test.query)
		if terms != test.terms || floor != test.floor {
			t.Fatalf("lexicalQuery(%q) = %q, %v; want %q, %v", test.query, terms, floor, test.terms, test.floor)
		}
	}
}

// Review Focus 1: nothing left to search for is an empty answer, never a statement ArcadeDB
// would read as "match everything" or refuse to parse.
func TestLexicalReadsSendNothingForAStopwordOnlyQuery(t *testing.T) {
	index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
		return testResponse{Status: 500, Body: `{"detail":"no statement was expected"}`}
	})
	filter := CandidateFilter{IdentityID: documentTestIdentity, Limit: 3}
	passages, err := index.LexicalCandidates(t.Context(), filter, "chi è il?")
	if err != nil || passages != nil {
		t.Fatalf("passages = %v err = %v", passages, err)
	}
	cards, err := index.LexicalDocumentCards(t.Context(), filter, "what is it")
	if err != nil || cards != nil {
		t.Fatalf("cards = %v err = %v", cards, err)
	}
	if len(*requests) != 0 {
		t.Fatalf("requests = %d, want none", len(*requests))
	}
}

func TestLexicalCandidatesRankByTheFullTextScoreAlone(t *testing.T) {
	var index *DocumentIndex
	index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
		return testResponse{Body: resultBody([]any{
			candidateFixture(index, "doc_a:0", "doc_a", 0, "lexical_score", 7.5),
			candidateFixture(index, "doc_b:3", "doc_b", 3, "lexical_score", 2.2),
		})}
	})
	passages, err := index.LexicalCandidates(t.Context(), CandidateFilter{
		IdentityID: documentTestIdentity, Limit: 3, DocumentIDs: []string{"doc_a", "doc_b"},
	}, "orari dei traghetti per la Sardegna")
	if err != nil {
		t.Fatal(err)
	}
	if len(passages) != 2 || passages[0].Leg != RetrievalLegLexical || passages[0].FusedScore != nil ||
		passages[0].LexicalScore == nil || *passages[0].Score() != 7.5 {
		t.Fatalf("passages = %+v", passages)
	}
	statement, _ := (*requests)[0].Payload["command"].(string)
	params, _ := (*requests)[0].Payload["params"].(map[string]any)
	for _, want := range []string{
		"SEARCH_INDEX('Passage[text]', :query) = true", "search_document_id IN :document_ids",
		"$score >= :min_lexical_score", "ORDER BY lexical_score DESC",
	} {
		if !strings.Contains(statement, want) {
			t.Fatalf("statement lacks %q:\n%s", want, statement)
		}
	}
	if strings.Contains(statement, "vector.") || params["query"] != "orari traghetti Sardegna" ||
		params["min_lexical_score"] != float64(2) {
		t.Fatalf("statement or params wrong:\n%s\n%v", statement, params)
	}
}

// One OR query's $score depends on predicate order (audit F8), so each index is asked apart
// and a document keeps the higher of its two scores.
func TestLexicalDocumentCardsMergeTheTwoIndexesOnTheHigherScore(t *testing.T) {
	index, requests := testDocumentIndex(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		card := func(id string, score float64) map[string]any {
			row := documentCardFixture(id, id+".pdf", "card of "+id)
			row["card_score"] = score
			return row
		}
		if strings.Contains(statement, "IndexedDocument[card]") {
			return testResponse{Body: resultBody([]any{card("doc_a", 3.1), card("doc_b", 2.5)})}
		}
		return testResponse{Body: resultBody([]any{card("doc_b", 4.0), card("doc_c", 2.2)})}
	})
	cards, err := index.LexicalDocumentCards(t.Context(),
		CandidateFilter{IdentityID: documentTestIdentity, Limit: 3}, "fatturato clienti torino")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, card := range cards {
		got = append(got, card.SearchDocumentID)
	}
	if strings.Join(got, ",") != "doc_b,doc_a,doc_c" || cards[0].Score != 4.0 {
		t.Fatalf("cards = %v (first score %v), want doc_b 4.0 first, then doc_a, doc_c", got, cards[0].Score)
	}
	if len(*requests) != 2 {
		t.Fatalf("requests = %d, want one per index", len(*requests))
	}
	for _, request := range *requests {
		statement, _ := request.Payload["command"].(string)
		if strings.Count(statement, "SEARCH_INDEX(") != 1 || strings.Contains(statement, "vector.") {
			t.Fatalf("a card statement asks more than one index:\n%s", statement)
		}
	}
}

func TestLexicalReadsTreatAMissingTypeAsAnEmptyLibrary(t *testing.T) {
	filter := CandidateFilter{IdentityID: documentTestIdentity, Limit: 3}
	passages, err := func() ([]PassageCandidate, error) {
		index, _ := testDocumentIndex(t, func(recordedRequest) testResponse {
			return testResponse{Status: 500, Body: missingPassageBody}
		})
		return index.LexicalCandidates(t.Context(), filter, "fatturato clienti")
	}()
	if err != nil || len(passages) != 0 {
		t.Fatalf("passages = %v err = %v", passages, err)
	}
	cards, err := missingTypeIndex(t).LexicalDocumentCards(t.Context(), filter, "fatturato clienti")
	if err != nil || len(cards) != 0 {
		t.Fatalf("cards = %v err = %v", cards, err)
	}
}
```

`resultBody` and `candidateFixture` are the existing helpers in `document_retrieval_test.go`. The `var index` declared before `testDocumentIndex` is the pattern `TestCandidateLocatorRoundTripsStrictly` uses: the closure reads `index` only when a request arrives, after it is assigned.

- [ ] **Step 3: Run them to verify they fail**

Run: `wsl -e bash -c 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go test ./internal/arcadedb/ -run "Stopword|LexicalQuery|LexicalReads|LexicalCandidates|LexicalDocumentCards" -count=1'`
Expected: build failure: `italianStopwords`, `lexicalQuery`, `LexicalCandidates`, `RetrievalLegLexical` and the rest are undefined.

- [ ] **Step 4: The lexical leg in `document_retrieval.go`**

Replace the `RetrievalLegFused` constant block with:

```go
const (
	// RetrievalLegFused: ArcadeDB fuses the full-text and the vector index itself and
	// returns one ranking, because reconciling two rankings in Go is what measured 0.300
	// recall@1 against the engine's 0.850.
	RetrievalLegFused RetrievalLeg = "fused"
	// RetrievalLegLexical is the full-text index alone, for when the dense legs cannot run
	// (spec §8). It does not fuse with the fused leg: a response is one or the other.
	RetrievalLegLexical RetrievalLeg = "lexical"
)
```

Add to `PassageCandidate`, after `FusedScore`:

```go
	// LexicalScore is the passage's BM25 score on the lexical leg. It is not on the cosine's
	// scale; a response carries one leg or the other, never both.
	LexicalScore *float64
}

// Score is the score the candidate's own leg ranked it by.
func (c PassageCandidate) Score() *float64 {
	if c.Leg == RetrievalLegLexical {
		return c.LexicalScore
	}
	return c.FusedScore
```

(The original closing `}` of the struct now closes `Score`.)

In `decodeCandidate`, replace everything from `if leg != RetrievalLegFused {` through `candidate.FusedScore = &score` with:

```go
	field, target := "fused_score", &candidate.FusedScore
	switch leg {
	case RetrievalLegFused:
	case RetrievalLegLexical:
		field, target = "lexical_score", &candidate.LexicalScore
	default:
		return PassageCandidate{}, "", fmt.Errorf("unknown retrieval leg %q", leg)
	}
	candidate.Leg = leg
	score, err := requiredNonNegativeFloat(row, field)
	if err != nil {
		return PassageCandidate{}, "", err
	}
	*target = &score
```

- [ ] **Step 5: `internal/arcadedb/document_lexical.go`**

```go
package arcadedb

import (
	"context"
	_ "embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Documents in lexical mode (spec §8): the full-text indexes alone, when the dense legs cannot
// run -- the documents gate is closed, it could not be read, or the query could not be
// embedded.
//
// BM25 cannot abstain on raw scores. Measured 2026-09-24 on the lab VM's library (12
// documents, 50 passages): "orari dei traghetti per la Sardegna", which it cannot answer,
// scored 7.107 on "dei", "la" and "per" alone, above the correct top match for "PidTemp
// MultiZone" (5.361). SEARCH_INDEX takes the index and the query and nothing else
// (arcadedb-docs how-to/data-modeling/full-text-index.adoc; SQLFunctionSearchIndex requires 2
// parameters), and the analyzer that could drop stopwords is fixed when the index is created.
// So they are dropped here, before the query is sent, from the lists Lucene itself ships.

// italianStopwords and englishStopwords are Lucene's Snowball stop lists, byte for byte from
// lucene-analysis-common-10.5.1.jar (org/apache/lucene/analysis/snowball/), the jar ArcadeDB
// 26.9.1 runs. BSD-licensed; the notice is inside each file.
//
//go:embed stopwords/italian_stop.txt
var italianStopwords string

//go:embed stopwords/english_stop.txt
var englishStopwords string

var documentStopwords = stopwordSet(italianStopwords, englishStopwords)

// stopwordSet reads Snowball stop lists: words separated by white space, "|" starting a
// comment (Lucene WordlistLoader.getSnowballWordSet).
func stopwordSet(lists ...string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, list := range lists {
		for _, line := range strings.Split(list, "\n") {
			line, _, _ = strings.Cut(line, "|")
			for _, word := range strings.Fields(line) {
				set[word] = struct{}{}
			}
		}
	}
	return set
}

// documentLexicalMinScore is the documents twin of memory's LexicalMinScore, applied the same
// way (lexicalScoreFloor: a query of one remaining word needs no floor). Measured 2026-09-24
// with the stopwords dropped: all six out-of-corpus questions, three Italian and three English,
// matched nothing at all; the weakest correct top match for a query of two or more words was
// a card at 2.68, and the strongest wrong match 1.02. Card and passage scores come from
// different indexes, so this is one number measured against both, not a shared scale.
const documentLexicalMinScore = 2

// lexicalQuery is what the lexical legs search for -- the query's words less its stopwords --
// and the floor those words earn. Empty terms mean there is nothing left to search for.
func lexicalQuery(query string) (string, float64) {
	kept := make([]string, 0, 8)
	for _, field := range strings.Fields(query) {
		word := strings.ToLower(strings.TrimFunc(field, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		}))
		if word == "" {
			continue
		}
		if _, stop := documentStopwords[word]; stop {
			continue
		}
		kept = append(kept, field)
	}
	terms := strings.Join(kept, " ")
	return terms, lexicalScoreFloor(terms, documentLexicalMinScore)
}

// lexicalRequest validates a lexical read as the dense reads are validated, and binds its
// terms and floor. Empty terms return no parameters: the caller sends nothing.
func (d *DocumentIndex) lexicalRequest(filter CandidateFilter, query string) (CandidateFilter, string, map[string]any, error) {
	filter, err := d.normalizeCandidateFilter(filter)
	if err != nil {
		return CandidateFilter{}, "", nil, err
	}
	query, err = d.validQuery(query)
	if err != nil {
		return CandidateFilter{}, "", nil, err
	}
	terms, floor := lexicalQuery(query)
	if terms == "" {
		return filter, "", nil, nil
	}
	where, params := candidateWhere(filter)
	params["query"], params["min_lexical_score"] = escapeLucene(terms), floor
	return filter, where, params, nil
}

// LexicalCandidates ranks passages by the full-text index alone, best first. A query that is
// only stopwords returns nothing without asking ArcadeDB.
func (d *DocumentIndex) LexicalCandidates(ctx context.Context, filter CandidateFilter, query string) ([]PassageCandidate, error) {
	filter, where, params, err := d.lexicalRequest(filter, query)
	if err != nil || params == nil {
		return nil, err
	}
	client, err := d.tenantClient(ctx, filter.IdentityID)
	if err != nil {
		return nil, err
	}
	rows, err := client.Query(ctx, lexicalPassageStatement(where, filter.Limit), params)
	if err != nil {
		if missingIngestType(err, documentPassageType) {
			return nil, nil // nothing ingested yet — an empty library, not a failure
		}
		return nil, fmt.Errorf("arcadedb: lexical document candidates: %w", err)
	}
	return d.decodeCandidates(rows, RetrievalLegLexical, filter.Limit)
}

// LexicalDocumentCards ranks documents by their card and by their split file name, one query
// per index, a document keeping the higher of its two scores. One OR query is not used: its
// $score depends on predicate order (measured 1.3798 against 1.2880 for one document, audit F8).
func (d *DocumentIndex) LexicalDocumentCards(ctx context.Context, filter CandidateFilter, query string) ([]DocumentCard, error) {
	filter, where, params, err := d.lexicalRequest(filter, query)
	if err != nil || params == nil {
		return nil, err
	}
	client, err := d.tenantClient(ctx, filter.IdentityID)
	if err != nil {
		return nil, err
	}
	best := make(map[string]DocumentCard)
	for _, field := range []string{"card", "file_name_words"} {
		rows, err := client.Query(ctx, lexicalCardStatement(field, where, filter.Limit), params)
		if err != nil {
			if missingIndexedDocumentType(err) {
				return nil, nil // nothing ingested yet — an empty library, not a failure
			}
			return nil, fmt.Errorf("arcadedb: lexical document cards (%s): %w", field, err)
		}
		for index, row := range rows {
			card, err := decodeDocumentCard(row)
			if err != nil {
				return nil, fmt.Errorf("arcadedb: lexical document card %d: %w", index, err)
			}
			if kept, seen := best[card.SearchDocumentID]; !seen || card.Score > kept.Score {
				best[card.SearchDocumentID] = card
			}
		}
	}
	cards := make([]DocumentCard, 0, len(best))
	for _, card := range best {
		cards = append(cards, card)
	}
	sort.Slice(cards, func(i, j int) bool {
		if cards[i].Score != cards[j].Score {
			return cards[i].Score > cards[j].Score
		}
		return cards[i].SearchDocumentID < cards[j].SearchDocumentID
	})
	return cards[:min(len(cards), filter.Limit)], nil
}

// lexicalPassageStatement ranks passages by BM25. The floor sits in the WHERE, as memory's
// lexical leg has it (searchFactsStatement), and the scope follows the full-text match so the
// caller's document filter always applies.
func lexicalPassageStatement(where string, limit int) string {
	return "SELECT " + passageCandidateFields + ", $score AS lexical_score FROM " + documentPassageType +
		" WHERE SEARCH_INDEX('" + documentPassageType + "[text]', :query) = true AND " + where +
		" AND $score >= :min_lexical_score ORDER BY lexical_score DESC, passage_key ASC LIMIT " +
		strconv.Itoa(limit)
}

// lexicalCardStatement ranks cards by BM25 over one of their two full-text indexes.
func lexicalCardStatement(field, where string, limit int) string {
	return "SELECT " + documentCardFields + ", $score AS card_score FROM " + IndexedDocumentType +
		" WHERE SEARCH_INDEX('" + IndexedDocumentType + "[" + field + "]', :query) = true AND " + where +
		" AND $score >= :min_lexical_score ORDER BY card_score DESC, search_document_id ASC LIMIT " +
		strconv.Itoa(limit)
}
```

- [ ] **Step 6: Run the tests and the package**

Run: `wsl -e bash -c 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go test ./internal/arcadedb/ -count=1 && go vet ./internal/arcadedb/ && go build ./...'`
Expected: `ok`. Whether ArcadeDB accepts the two-key `ORDER BY` on a `$score` alias is proved live in Task 8. The earlier VM measurement used the one-key form.

- [ ] **Step 7: Commit**

```bash
git add internal/arcadedb/stopwords/italian_stop.txt internal/arcadedb/stopwords/english_stop.txt internal/arcadedb/document_lexical.go internal/arcadedb/document_lexical_test.go
git commit -m "feat(arcadedb): read documents from the full-text indexes alone

Lexical mode's statements (spec §8): passages from Passage[text], cards from
card and file_name_words asked apart and merged on the higher score, both with
Lucene's own Snowball Italian and English stopwords removed from the query and a
BM25 floor of 2 -- measured on the VM to reject every out-of-corpus question and
keep every known top document. A query of stopwords alone sends nothing. The
lists are Lucene's files byte for byte, pinned by sha256.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>" -- internal/arcadedb/stopwords/italian_stop.txt internal/arcadedb/stopwords/english_stop.txt internal/arcadedb/document_lexical.go internal/arcadedb/document_lexical_test.go internal/arcadedb/document_retrieval.go
```

---

## Task 6: Lexical mode in `HostRetriever`

**Files:**
- Create: `internal/documents/retrieval_lexical.go`, `internal/documents/retrieval_lexical_test.go`
- Modify: `internal/documents/retrieval.go`, `retrieval_cards.go`, `retrieval_rank.go:55-63,298-302`
- Test: `internal/documents/retrieval_test.go` (fakes; `TestHostRetrieverDegradationIsExplicit`)

**Interfaces:**
- Consumes (Task 5): `LexicalCandidates`, `LexicalDocumentCards`, `RetrievalLegLexical`, `Score()`.
- Consumes (Task 4): `denseQuery`, `queryVector`, `CardQuery`, `retrievalCards`.
- Produces:
  - `RetrievalLexicalOnly RetrievalStatus = "lexical_only"`;
  - `RetrievalControlPlane.LexicalDocumentCards(context.Context, CardQuery) ([]RetrievalCard, error)`;
  - `PassageIndex.LexicalCandidates(context.Context, arcadedb.CandidateFilter, string) ([]arcadedb.PassageCandidate, error)`;
  - `ProductionRetrievalProfile = "arcadedb-fused-card-v3"`.

- [ ] **Step 1: Extend the fakes**

In `retrieval_test.go`, add `lexicalQuery string` to `fakeRetrievalControl`, and:

```go
func (f *fakeRetrievalControl) LexicalDocumentCards(_ context.Context, query CardQuery) ([]RetrievalCard, error) {
	f.lexicalQuery = query.Query
	f.cardDocumentScope = append([]string(nil), query.DocumentIDs...)
	f.cardSourceScope = append([]SourceScope(nil), query.SourceScopes...)
	if f.err != nil {
		return nil, f.err
	}
	return append([]RetrievalCard(nil), f.cards...), nil
}
```

Add to `fakePassageIndex`, with this method:

```go
	lexical       []arcadedb.PassageCandidate
	lexicalErr    error
	lexicalQuery  string
	lexicalFilter arcadedb.CandidateFilter
```

```go
func (f *fakePassageIndex) LexicalCandidates(
	_ context.Context, filter arcadedb.CandidateFilter, query string,
) ([]arcadedb.PassageCandidate, error) {
	f.lexicalFilter, f.lexicalQuery = filter, query
	return append([]arcadedb.PassageCandidate(nil), f.lexical...), f.lexicalErr
}
```

- [ ] **Step 2: Write the failing tests**

Create `internal/documents/retrieval_lexical_test.go`:

```go
package documents

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/embeddings"
)

func lexicalCandidate(documentID, raw string, score float64) arcadedb.PassageCandidate {
	candidate := retrievalCandidate(arcadedb.RetrievalLegLexical)
	candidate.SearchDocumentID, candidate.PassageID, candidate.RawSHA256 = documentID, documentID+":0", raw
	candidate.FusedScore, candidate.LexicalScore = nil, new(score)
	return candidate
}

// Every reason the dense legs cannot run is answered from the full-text indexes, never with
// zero documents or an error (spec §8; Review Focus 5 for the credential and attestation rows).
func TestLexicalModeAnswersEveryDenseFailure(t *testing.T) {
	vector := []float64{0.1, 0.2}
	tests := []struct {
		name     string
		control  *fakeRetrievalControl
		embedder QueryEmbedder
		reason   string
	}{
		{"library in another space", &fakeRetrievalControl{closed: true}, &fakeRetrievalEmbedder{vector: vector}, DegradationSpaceMismatch},
		{"gate unreadable", &fakeRetrievalControl{gateErr: errors.New("count refused")}, &fakeRetrievalEmbedder{vector: vector}, DegradationSpaceCheck},
		{"embedding refused", &fakeRetrievalControl{}, &fakeRetrievalEmbedder{err: errors.New("offline")}, DegradationEmbedding},
		{"no credential", &fakeRetrievalControl{}, &fakeRetrievalEmbedder{spaceErr: embeddings.ErrNoCredential}, DegradationEmbedding},
		{"attestation fails", &fakeRetrievalControl{},
			&fakeRetrievalEmbedder{spaceErr: errors.New("attest local embedder: /v1/models returned HTTP 503")}, DegradationEmbedding},
		{"space moved", &fakeRetrievalControl{}, &fakeRetrievalEmbedder{vector: vector, spaces: []string{"es1-a", "es1-b"}}, DegradationSpaceMismatch},
		{"no embedder", &fakeRetrievalControl{}, nil, DegradationEmbedding},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.control.cards = []RetrievalCard{retrievalCard()}
			index := &fakePassageIndex{lexical: []arcadedb.PassageCandidate{
				lexicalCandidate("doc_9f2c", strings.Repeat("a", 64), 4.2),
			}}
			response := retrieveCodice(t, &HostRetriever{
				ControlPlane: test.control, PassageIndex: index, Embedder: test.embedder,
			})
			if response.Status != RetrievalLexicalOnly || response.DegradationReason != test.reason ||
				response.Abstained || response.FloorsReason != "" {
				t.Fatalf("response = %#v", response)
			}
			if len(response.Documents) != 1 || len(response.Documents[0].Passages) != 1 ||
				response.Documents[0].Passages[0].Evidence[0].Leg != string(arcadedb.RetrievalLegLexical) ||
				response.Documents[0].Score != 4.2 {
				t.Fatalf("documents = %#v", response.Documents)
			}
			if test.control.lexicalQuery != "codice cliente" || index.lexicalQuery != "codice cliente" ||
				index.fusedQuery.Query != "" || test.control.cardEmbedding != nil {
				t.Fatalf("a dense leg ran, or a lexical one did not: cards %q passages %q fused %q",
					test.control.lexicalQuery, index.lexicalQuery, index.fusedQuery.Query)
			}
		})
	}
}

func TestLexicalModeAbstainsWhenNothingMatches(t *testing.T) {
	response := retrieveCodice(t, &HostRetriever{
		ControlPlane: &fakeRetrievalControl{closed: true}, PassageIndex: &fakePassageIndex{},
		Embedder: &fakeRetrievalEmbedder{},
	})
	if !response.Abstained || response.AbstentionReason != AbstainedNoQualifiedPassage ||
		response.Status != RetrievalLexicalOnly || len(response.Documents) != 0 {
		t.Fatalf("response = %#v", response)
	}
}

// Measured: five identical transcripts took the top five places for an unrelated question.
func TestLexicalModeCountsIdenticalFilesOnce(t *testing.T) {
	raw := strings.Repeat("c", 64)
	response := retrieveCodice(t, &HostRetriever{
		ControlPlane: &fakeRetrievalControl{closed: true},
		PassageIndex: &fakePassageIndex{lexical: []arcadedb.PassageCandidate{
			lexicalCandidate("doc_video1", raw, 7.5), lexicalCandidate("doc_video2", raw, 7.5),
			lexicalCandidate("doc_video3", raw, 7.5),
		}},
		Embedder: &fakeRetrievalEmbedder{},
	})
	if len(response.Documents) != 1 {
		t.Fatalf("documents = %d, want the three copies as one", len(response.Documents))
	}
}

func TestLexicalModeKeepsTheCallersScope(t *testing.T) {
	control := &fakeRetrievalControl{closed: true, scope: []string{retrievalDocument}}
	index := &fakePassageIndex{}
	scopes := []SourceScope{{Kind: SourceScopeFolder, Path: "/finance/2026/"}}
	_, err := (&HostRetriever{ControlPlane: control, PassageIndex: index, Embedder: &fakeRetrievalEmbedder{}}).
		Retrieve(t.Context(), RetrievalRequest{
			IdentityID: retrievalIdentity, Query: "fatturato", DocumentIDs: []string{retrievalDocument}, SourceScopes: scopes,
		})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(control.cardDocumentScope, []string{retrievalDocument}) ||
		!reflect.DeepEqual(index.lexicalFilter.DocumentIDs, []string{retrievalDocument}) ||
		!reflect.DeepEqual(index.lexicalFilter.SourcePrefixes, []string{"finance/2026/"}) ||
		len(control.cardSourceScope) != 1 {
		t.Fatalf("scope lost: cards %v %v passages %#v", control.cardDocumentScope, control.cardSourceScope, index.lexicalFilter)
	}
}

// A passage index that refuses in lexical mode still leaves the cards, as it does in dense mode.
func TestLexicalModeServesItsCardsWhenThePassageIndexRefuses(t *testing.T) {
	response := retrieveCodice(t, &HostRetriever{
		ControlPlane: &fakeRetrievalControl{closed: true, cards: []RetrievalCard{retrievalCard()}},
		PassageIndex: &fakePassageIndex{lexicalErr: errors.New("engine refused")},
		Embedder:     &fakeRetrievalEmbedder{},
	})
	if response.Status != RetrievalCardOnly || response.DegradationReason != DegradationArcade ||
		len(response.Documents) != 1 || !response.Documents[0].RequiresOpen {
		t.Fatalf("response = %#v", response)
	}
}
```

In `TestHostRetrieverDegradationIsExplicit`:
- delete the `{"embedding", …, DegradationEmbedding}` row and the `if test.reason == DegradationEmbedding { … }` block;
- rewrite the leading comment to say that a refused passage index leaves only the cards. The embedding case is now lexical mode, pinned by `TestLexicalModeAnswersEveryDenseFailure`.

- [ ] **Step 3: Run them to verify they fail**

Run: `wsl -e bash -c 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go test ./internal/documents/ -run "LexicalMode|DegradationIsExplicit" -count=1'`
Expected: build failure: `RetrievalLexicalOnly` is undefined. After adding only that constant, the table fails with `Status degraded_card_only`.

- [ ] **Step 4: Rank by the leg's own score**

In `retrieval_rank.go`, replace

```go
		if passage.FusedScore != nil && *passage.FusedScore > doc.document.Score {
			doc.document.Score = *passage.FusedScore
		}
```

with

```go
		if score := passage.Score(); score != nil && *score > doc.document.Score {
			doc.document.Score = *score
		}
```

and in `mergePassage` replace `if candidate.FusedScore != nil { evidence.Score = new(*candidate.FusedScore) }` with:

```go
	if score := candidate.Score(); score != nil {
		evidence.Score = new(*score)
	}
```

- [ ] **Step 5: `internal/documents/retrieval_lexical.go`**

```go
package documents

import (
	"context"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// Lexical mode (spec §8). When the dense legs cannot run -- the library holds vectors from
// another embedding space, the gate that says so could not be read, or the query could not be
// embedded -- both legs read the full-text indexes instead, and the status says the answer is
// lexical: candidates matched by their words. This deliberately amends the measured "no
// lexical-only path" (arcadedb RetrievalLegFused): that measurement compared two fusions, not
// one leg answering while the other is unavailable.

// routeCards runs the card leg the query can have: ranked against its vector, or lexically.
func (r *HostRetriever) routeCards(ctx context.Context, card CardQuery, dense queryVector) ([]RetrievalCard, error) {
	if dense.vector == nil {
		return r.ControlPlane.LexicalDocumentCards(ctx, card)
	}
	card.Vector, card.Space = dense.vector, dense.space
	return r.ControlPlane.RouteDocumentCards(ctx, card)
}

// passages runs the passage leg the query can have: fused against its vector, or lexically.
func (r *HostRetriever) passages(
	ctx context.Context,
	filter arcadedb.CandidateFilter,
	query string,
	dense queryVector,
	strategy arcadedb.FusionStrategy,
) ([]arcadedb.PassageCandidate, error) {
	if dense.vector == nil {
		return r.PassageIndex.LexicalCandidates(ctx, filter, query)
	}
	return r.PassageIndex.FusedCandidates(ctx, arcadedb.FusedCandidateQuery{
		CandidateFilter: filter, Query: query, Embedding: dense.vector, Space: dense.space, Strategy: strategy,
	})
}
```

In `retrieval_cards.go`, add:

```go
// LexicalDocumentCards ranks documents by their card and file name alone, inside the same
// scope, for lexical mode (spec §8).
func (c *ArcadeRetrievalControlPlane) LexicalDocumentCards(ctx context.Context, q CardQuery) ([]RetrievalCard, error) {
	if c == nil || c.Index == nil {
		return nil, errRetrievalControlPlaneUnset
	}
	found, err := c.Index.LexicalDocumentCards(ctx, q.filter(), q.Query)
	if err != nil {
		return nil, err
	}
	return retrievalCards(found), nil
}
```

- [ ] **Step 6: `Retrieve` chooses its mode once**

In `retrieval.go`:
- Set `ProductionRetrievalProfile = "arcadedb-fused-card-v3"`: the leg set changed.
- Add `RetrievalLexicalOnly RetrievalStatus = "lexical_only"` after `RetrievalCardOnly`, with the comment: `// RetrievalLexicalOnly: both legs read the full-text indexes alone (spec §8), for the reason in DegradationReason.`
- Rewrite the comment above the constants so it no longer says an embedding failure answers card-only.
- In `RetrievalControlPlane`, add `LexicalDocumentCards(context.Context, CardQuery) ([]RetrievalCard, error)`.
- In `PassageIndex`, add:

```go
	// LexicalCandidates ranks passages by the full-text index alone, for lexical mode.
	LexicalCandidates(context.Context, arcadedb.CandidateFilter, string) ([]arcadedb.PassageCandidate, error)
```

- Replace the body of `Retrieve` from `dense, reason, cause := r.denseQuery(...)` through the end of the `FusedCandidates` error branch with:

```go
	// The dense legs run only over a library wholly in the query's space (spec §3); otherwise,
	// and when the query cannot be embedded, both legs are lexical (spec §8).
	dense, reason, cause := r.denseQuery(ctx, request.IdentityID, request.Query)
	switch {
	case reason != "":
		response.Status, response.DegradationReason = RetrievalLexicalOnly, reason
		r.degradations.warn(reason, cause.Error(), request.IdentityID)
	case !arcadedb.FloorsCalibrated(dense.space):
		response.FloorsReason = arcadedb.ReasonUncalibratedFloors
	}
	cards, err := r.routeCards(ctx, CardQuery{
		IdentityID: request.IdentityID, Query: request.Query, DocumentIDs: scope,
		SourceScopes: request.SourceScopes, Limit: cfg.CandidateLimit,
	}, dense)
	if err != nil {
		return RetrievalResponse{}, fmt.Errorf("documents: route document cards: %w", err)
	}
	if r.PassageIndex == nil {
		response.Status, response.DegradationReason = RetrievalCardOnly, DegradationUnconfigured
		r.degradations.warn(DegradationUnconfigured, "no passage index is wired", request.IdentityID)
		response.Documents = rankCardsOnly(cards, request.Limit, cfg.TopPassages)
		return response, nil
	}
	response.Indexing = r.ingestState(ctx, request.IdentityID)
	sourceKeys, sourcePrefixes := ArcadeSourceFilters(request.SourceScopes)
	fused, err := r.passages(ctx, arcadedb.CandidateFilter{
		IdentityID: request.IdentityID, Limit: cfg.CandidateLimit, DocumentIDs: scope,
		SourceKeys: sourceKeys, SourcePrefixes: sourcePrefixes,
	}, request.Query, dense, cfg.FusionStrategy)
	if err != nil {
		response.Status, response.DegradationReason = RetrievalCardOnly, DegradationArcade
		r.degradations.warn(DegradationArcade, err.Error(), request.IdentityID)
		response.Documents = rankCardsOnly(cards, request.Limit, cfg.TopPassages)
		return response, nil
	}
```

The abstention and ranking tail that follows is unchanged. It reads `fused`, which is now either leg's passages. In its long comment, replace "Both legs now score the same reranked cosine and BOTH are cut by the same RelevanceFloor" with "In dense mode both legs score the same reranked cosine and both are cut by the same relevance floor; in lexical mode both are cut by the same BM25 floor".

- [ ] **Step 7: Run everything this task touched**

Run: `wsl -e bash -c 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go build ./... && go vet ./internal/documents/ ./cmd/aura/ && go vet -tags retrieval_eval ./internal/documents/ && go test ./internal/documents/ ./internal/arcadedb/ -count=1 && go test ./internal/agent/tools/ -run "Document" -count=1 && go test ./cmd/aura/ -run "Document|Retriev|Docs" -count=1'`
Expected:
- all `ok`;
- the whole `./internal/documents/` suite stays green, so every dense test (citations, scope, neighbours, card lane, duplicates) is unchanged;
- `wc -l internal/documents/retrieval.go` ≤ 600.

- [ ] **Step 8: Commit**

```bash
git add internal/documents/retrieval_lexical.go internal/documents/retrieval_lexical_test.go
git commit -m "feat(documents): answer from the full-text indexes when dense cannot run

A library in another embedding space, a gate that cannot be read, a query that
cannot be embedded, a hosted route with no key: each used to leave document_search
with zero documents, and now answers lexically with status lexical_only and the
reason, grouped by content and floored, abstaining when nothing matches (spec §8).
A healthy library takes the dense path exactly as before. The profile becomes
arcadedb-fused-card-v3. TestHostRetrieverDegradationIsExplicit loses its embedding
row: that case is now lexical mode, pinned by TestLexicalModeAnswersEveryDenseFailure.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>" -- internal/documents/retrieval.go internal/documents/retrieval_cards.go internal/documents/retrieval_rank.go internal/documents/retrieval_lexical.go internal/documents/retrieval_lexical_test.go internal/documents/retrieval_test.go
```

---

## Task 7: The daemon's document embedder reads its key live

**Files:**
- Modify:
  - `cmd/aura/memory_embedder.go`, `cmd/aura/document_retrieval_wiring.go`;
  - `cmd/aura/main.go:163-190,246-247` (handles);
  - `cmd/aura/chat_boot.go:419-490,513` (wiring, chatEnv);
  - `cmd/aura/serve.go:345`;
  - `cmd/aura/docs.go:367-373`;
  - `internal/runner/runner_deps.go:120-124`.
- Delete: `cmd/aura/embedding_client.go`
- Test: `cmd/aura/memory_embedder_test.go` (append), `cmd/aura/document_retrieval_wiring_test.go` (append)

**Interfaces:**
- Consumes (Task 4): `newQueryEmbedder`, `documents.QueryEmbedder`.
- Produces:
  - `liveKey(runtime *llm.Runtime) func() string`;
  - `wireDocumentQueryEmbedder(handles *runtimeToolHandles, embedder documents.QueryEmbedder)`;
  - `logEmbeddingSpace(logger *slog.Logger, family string, embedder arcadedb.DenseEmbedder, timeout time.Duration)`;
  - `runtimeToolHandles.Documents *documentLibrary`;
  - `chatEnv.documentEmbedder`;
  - `documentQueryTimeout(cfg *config.Config) time.Duration`.

- [ ] **Step 1: Write the failing tests**

Append to `cmd/aura/document_retrieval_wiring_test.go`:

```go
// Review Focus 4: a key rotated in the cockpit replaces the LLM profile in place, and the
// retriever the registry built before that runtime existed must send the new key on its next
// request (spec §5).
func TestDocumentQueryEmbedderReadsTheRotatedKey(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A hosted route reads the model's input limit from the catalogue first (fit.go),
		// as TestMemoryEmbedderReadsTheRotatedKey's server answers it.
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = io.WriteString(w, `{"data":[{"id":"vendor/embed","context_length":2048}]}`)
			return
		}
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		vector := make([]float64, 768)
		vector[0] = 1
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"index": 0, "embedding": vector}}})
	}))
	t.Cleanup(server.Close)
	cfg := &config.Config{Embed: config.EmbedConfig{CloudModel: "vendor/embed", CloudBaseURL: server.URL, Dimensions: 768}}
	handles := runtimeToolHandles{Documents: newDocumentLibrary(&pgxpool.Pool{}, cfg)}
	runtime := llm.NewRuntime(nil, llm.Config{APIKey: "boot-key"})
	wireDocumentQueryEmbedder(&handles, newQueryEmbedder(cfg, liveKey(runtime)))

	embedder := handles.Documents.retriever.Embedder
	if _, err := embedder.Embed(t.Context(), []string{"a"}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	runtime.Replace(nil, llm.Config{APIKey: "rotated-key"})
	if _, err := embedder.Embed(t.Context(), []string{"b"}); err != nil {
		t.Fatalf("Embed after rotation: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0] != "Bearer boot-key" || seen[1] != "Bearer rotated-key" {
		t.Fatalf("authorization = %q, want the key read live at each request", seen)
	}
}

// The daemon must read documents in the space the ingest supervisor stamps them with, or the
// documents gate never opens. Both sides meet at one literal: the supervisor's
// TestRouteResolverNamesTheLocalSidecarsSpace and arcadedb's
// TestEmbeddingGemmaFloorsAreKeyedByTheSpaceTheVMAttests pin the same attestation to it.
func TestDocumentQuerySpaceIsTheSpaceIngestStamps(t *testing.T) {
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"/models/embeddinggemma-300M-Q8_0.gguf",`+
			`"meta":{"n_embd":768,"n_params":307581696,"size":327060480,"ftype":"Q8_0"}}]}`)
	}))
	t.Cleanup(sidecar.Close)
	space, err := newQueryEmbedder(&config.Config{Embed: config.EmbedConfig{BaseURL: sidecar.URL, Dimensions: 768}}, nil).
		Space(t.Context())
	if err != nil || space.ID != "es1-e0aa6accf0b79c6b" {
		t.Fatalf("daemon reads documents in %q (err %v), want es1-e0aa6accf0b79c6b", space.ID, err)
	}
}
```

Imports: `encoding/json`, `io`, `net/http`, `net/http/httptest`, `strings`, `sync`, `internal/llm`.

Append to `cmd/aura/memory_embedder_test.go`:

```go
func TestLogEmbeddingSpaceNamesTheFamily(t *testing.T) {
	var out bytes.Buffer
	logEmbeddingSpace(slog.New(slog.NewTextHandler(&out, nil)), "documents", namedSpace("es1-docs"), time.Second)
	if !strings.Contains(out.String(), "family=documents") || !strings.Contains(out.String(), "space=es1-docs") {
		t.Fatalf("boot log = %q", out.String())
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `wsl -e bash -c 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go test ./cmd/aura/ -run "DocumentQuery|LogEmbeddingSpace|MemoryEmbedder|LogMemorySpace" -count=1'`
Expected: build failure: `liveKey`, `wireDocumentQueryEmbedder`, `runtimeToolHandles.Documents` and `logEmbeddingSpace` are undefined.

- [ ] **Step 3: One live key, one boot log**

In `memory_embedder.go`:

```go
// liveKey reads the running LLM profile's key on every request: a key rotated in the cockpit
// replaces that profile in place (serve_settings.go primaryLLMRouteReloader) without a restart,
// and a boot copy would keep embedding with the revoked key (spec §5).
func liveKey(runtime *llm.Runtime) func() string {
	return func() string { return runtime.Snapshot().Config.APIKey }
}

// memoryEmbedder is the daemon's one memory route.
func memoryEmbedder(cfg *config.Config, runtime *llm.Runtime) arcadedb.DenseEmbedder {
	return arcadedb.NewMemoryEmbedder(cfg.Embed, liveKey(runtime))
}
```

Replace `logMemorySpace` with a generalization whose memory call keeps its current text:

```go
// logEmbeddingSpace names a family's embedding space at boot. The daemon's memory space must
// match arcadedb-mcp's, and its documents space the ingest supervisor's stamp; otherwise that
// family stays lexical, and this line is where an operator sees why.
func logEmbeddingSpace(logger *slog.Logger, family string, embedder arcadedb.DenseEmbedder, timeout time.Duration) {
	if embedder == nil {
		logger.Info("aura serve: dense retrieval disabled: no embedding route", "family", family)
		return
	}
	space, err := arcadedb.SpaceWithin(embedder, timeout)
	logger.Info("aura serve: embedding space", "family", family, "space", space.ID,
		"space_label", space.Label, "space_error", err)
}
```

Rename `memorySpaceBootTimeout` to `spaceBootTimeout`. Update `TestLogMemorySpaceNamesTheDaemonsSpace` to call `logEmbeddingSpace(…, "memory", …)`: the function was renamed, and the assertion `space=es1-daemon` is unchanged.

- [ ] **Step 4: Wire the live key after the runtime exists**

In `main.go`, add to `runtimeToolHandles`:

```go
	// Documents is retained so chat boot can route its query embedder through the live LLM key
	// once the runtime exists (wireDocumentQueryEmbedder).
	Documents *documentLibrary
```

In `buildBaseRegistryWithHandles`, replace

```go
	library := newDocumentLibrary(taskStorePool(ts), cfg)
	reg.Register(&tools.DocumentSearch{Library: library})
```

with

```go
	handles.Documents = newDocumentLibrary(taskStorePool(ts), cfg)
	reg.Register(&tools.DocumentSearch{Library: handles.Documents})
```

In `document_retrieval_wiring.go`, add:

```go
// wireDocumentQueryEmbedder puts embedder behind document_search. The retriever is built with
// the registry, before the LLM runtime exists, so it starts on the boot key; chat boot calls
// this before any turn, once the runtime can give the key live (spec §5).
func wireDocumentQueryEmbedder(handles *runtimeToolHandles, embedder documents.QueryEmbedder) {
	if handles == nil || handles.Documents == nil || handles.Documents.retriever == nil {
		return
	}
	handles.Documents.retriever.Embedder = embedder
}
```

Also change `newQueryEmbedder`'s timeout argument from `documentHTTPClient(cfg).Timeout` to `documentQueryTimeout(cfg)`. In `docs.go`, replace `documentHTTPClient` with:

```go
// documentQueryTimeout bounds one query embedding, as the document embedder always has.
func documentQueryTimeout(cfg *config.Config) time.Duration {
	if cfg != nil && cfg.MultimodalTimeoutSec > 0 {
		return time.Duration(cfg.MultimodalTimeoutSec) * time.Second
	}
	return 120 * time.Second
}
```

Drop `"net/http"` from `docs.go` if nothing else there uses it.

In `chat_boot.go`, after `memoryDense := memoryEmbedder(cfg, llmRuntime)`:

```go
	// The document query route and the reasoning classifier embed with the live key too.
	documentQuery := newQueryEmbedder(cfg, liveKey(llmRuntime))
	wireDocumentQueryEmbedder(&toolHandles, documentQuery)
```

In `deps`, replace `Embedder: embeddingClient(cfg, documentHTTPClient(cfg)),` with `Embedder: documentQuery,`. A nil `documents.QueryEmbedder` converts to a nil `prompt.Embedder`, which is the agent's documented fall back to the LLM router. Add `documentEmbedder documents.QueryEmbedder` to `chatEnv` beside `memoryEmbedder`, and set `documentEmbedder: documentQuery` in the `return &chatEnv{…}` literal.

In `serve.go:345`, replace the `logMemorySpace` call with:

```go
	logEmbeddingSpace(slog.Default(), "memory", chat.memoryEmbedder, spaceBootTimeout)
	logEmbeddingSpace(slog.Default(), "documents", chat.documentEmbedder, spaceBootTimeout)
```

Delete `cmd/aura/embedding_client.go`: its two callers are gone. In `internal/runner/runner_deps.go`, replace the comment line `documents.EmbeddingClient built over the resolved config.EmbedRoute` and the one after it with `the daemon's document query route (embeddings.Route over config.EmbedRoute, key read live)`.

- [ ] **Step 5: Run everything this task touched**

Run: `wsl -e bash -c 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go build ./... && go vet ./cmd/aura/ ./internal/runner/ && go test ./cmd/aura/ -run "DocumentQuery|LogEmbeddingSpace|MemoryEmbedder|NewHostDocumentRetriever|Registry|Docs" -count=1 && go test ./internal/documents/ ./internal/runner/ -count=1'`
Expected:
- all `ok`;
- `wc -l cmd/aura/main.go` ≤ 600 (one field added, one line saved);
- `git grep -n "embeddingClient(\|documentHTTPClient(" -- cmd` prints nothing.

- [ ] **Step 6: Commit**

```bash
git rm -q cmd/aura/embedding_client.go
git commit -m "feat(aura): read the document query key live, and name the documents space

The document retriever and the reasoning classifier embedded with the key read
at boot, so a key rotated in the cockpit broke document_search until a restart.
Both now share one route that reads the running LLM profile's key per request,
wired into the registry's retriever once the runtime exists (spec §5). The boot
log names the documents space beside the memory one, and a test pins that the
daemon reads documents in exactly the space the ingest supervisor stamps.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>" -- cmd/aura/memory_embedder.go cmd/aura/memory_embedder_test.go cmd/aura/document_retrieval_wiring.go cmd/aura/document_retrieval_wiring_test.go cmd/aura/main.go cmd/aura/chat_boot.go cmd/aura/serve.go cmd/aura/docs.go cmd/aura/embedding_client.go internal/runner/runner_deps.go
```

---

## Task 8: Live proof on ArcadeDB

**Files:**
- Create: `internal/arcadedb/document_space_live_integration_test.go` (tag `arcadedb_integration`, package `arcadedb_test`)
- Scratch (not committed): `$SCRATCH/arcade-vm.sh`

**Interfaces:**
- Consumes: everything above, through exported names only (`arcadedb.New`, `NewDocumentIndex`, `DocumentsDenseOpen`, `FusedCandidates`, `documents.HostRetriever`, `ArcadeRetrievalControlPlane`, `QueryEmbedder`).
- Produces: nothing new in production code.

- [ ] **Step 1: The tunnel runner**

Write `$SCRATCH/arcade-vm.sh`:

```bash
#!/bin/bash
# The arcadedb_integration tier against the lab VM's ArcadeDB 26.9.1 through an SSH tunnel.
# Each test creates and drops its own database; nothing else on the VM is written.
# $1 = go test -run regex; $2 = package (default ./internal/arcadedb/); $3 = tags (default arcadedb_integration)
set -euo pipefail
cd /mnt/d/Aura
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
VM=aura@192.168.101.158
ssh_vm() { sshpass -p aura ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=8 "$VM" "$@"; }
ip=$(ssh_vm "echo aura | sudo -S -p '' docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}' aura-arcadedb" | awk '{print $1}')
ARCADEDB_PASSWORD=$(ssh_vm "echo aura | sudo -S -p '' docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' aura-arcadedb" | grep -o 'rootPassword=[^ ]*' | head -1 | cut -d= -f2)
export ARCADEDB_PASSWORD ARCADEDB_URL=http://127.0.0.1:12480
sshpass -p aura ssh -o ExitOnForwardFailure=yes -o ConnectTimeout=8 -N -L 12480:"$ip":2480 "$VM" &
tunnel=$!
trap 'kill $tunnel 2>/dev/null' EXIT
for _ in $(seq 1 20); do curl -s -m 2 -o /dev/null http://127.0.0.1:12480/api/v1/ready && break; sleep 0.5; done
go test -tags "${3:-arcadedb_integration}" "${2:-./internal/arcadedb/}" -run "$1" -count=1 -v 2>&1 | tail -80
```

- [ ] **Step 2: Write the live test**

Create `internal/arcadedb/document_space_live_integration_test.go`:

```go
//go:build arcadedb_integration

// Documents over a live ArcadeDB (spec §3, §8): a library with one vector in another space is
// served lexically -- grouped, floored, abstaining where it cannot answer -- and the dense
// legs never rank a vector from another space. An external test package, so the retriever in
// internal/documents can drive the real index without an import cycle.
//
// Run: arcade-vm.sh DocumentSpaceLive
package arcadedb_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/embeddings"
)

const (
	liveIdentity   = "20000000-0000-0000-0000-000000000009"
	liveSpace      = "es1-docs-live-a"
	liveOtherSpace = "es1-docs-live-b"
)

// liveQuery is the query vector: in-space transcripts sit near it, the other-space passage on it.
var liveQuery = []float64{1, 0, 0}

type oneTenant struct{ client *arcadedb.Client }

func (o oneTenant) For(context.Context, string) (*arcadedb.Client, error) { return o.client, nil }

type liveEmbedder struct{ space string }

func (e liveEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i := range texts {
		out[i] = append([]float64(nil), liveQuery...)
	}
	return out, nil
}

func (e liveEmbedder) Space(context.Context) (embeddings.Space, error) {
	return embeddings.Space{ID: e.space}, nil
}

func sha(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func liveDocumentDatabase(t *testing.T) (*arcadedb.Client, func() *arcadedb.Client) {
	t.Helper()
	base := os.Getenv("ARCADEDB_URL")
	if base == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("ARCADEDB_URL must be set in CI: a skipped integration tier is a falsely-green job")
		}
		t.Skip("ARCADEDB_URL not set")
	}
	user := os.Getenv("ARCADEDB_USER")
	if user == "" {
		user = "root"
	}
	password := os.Getenv("ARCADEDB_PASSWORD")
	ctx := context.Background()
	admin, err := arcadedb.New(arcadedb.Config{BaseURL: base, Database: "unused", User: user, Password: password})
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	database := fmt.Sprintf("aura_documents_space_%d", time.Now().UnixNano())
	if _, err := admin.CreateDatabase(ctx, database); err != nil {
		t.Fatalf("create disposable database: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.DropDatabase(context.Background(), database); err != nil {
			t.Errorf("drop disposable database %s: %v", database, err)
		}
	})
	// A fresh client is a fresh gate cache: the test asks for one after changing a stamp.
	fresh := func() *arcadedb.Client {
		client, err := arcadedb.New(arcadedb.Config{BaseURL: base, Database: database, User: user, Password: password})
		if err != nil {
			t.Fatalf("disposable client: %v", err)
		}
		return client
	}
	client := fresh()
	for _, statement := range documentDDL(len(liveQuery)) {
		if _, err := client.Command(ctx, statement, nil); err != nil {
			t.Fatalf("DDL %q: %v", statement, err)
		}
	}
	return client, fresh
}

// documentDDL mirrors services/ingest/arcade.py _document_ddl, which owns the real schema: the
// properties the reads decode, the three full-text indexes with the analyzer pinned, both
// vector indexes, and the stamp with the null strategy plan 2 measured.
func documentDDL(dims int) []string {
	fullText := "FULL_TEXT METADATA {analyzer:'org.apache.lucene.analysis.standard.StandardAnalyzer'}"
	vector := fmt.Sprintf(`LSM_VECTOR METADATA { "dimensions": %d, "similarity": "COSINE", "quantization": "NONE" }`, dims)
	statements := []string{}
	for _, t := range []struct {
		name       string
		properties []string
		indexes    []string
	}{
		{"Passage", []string{
			"passage_key STRING", "search_document_id STRING", "source_kind STRING", "source_key STRING",
			"raw_sha256 STRING", "schema_version STRING", "ordinal LONG", "text STRING",
			"normalized_text_sha256 STRING", "heading_path LIST OF STRING", "char_start LONG", "char_end LONG",
			"embedding ARRAY_OF_FLOATS", "embed_space STRING",
		}, []string{"(passage_key) UNIQUE", "(search_document_id) NOTUNIQUE", "(text) " + fullText,
			"(embedding) " + vector, "(embed_space) NOTUNIQUE NULL_STRATEGY INDEX"}},
		{"IndexedDocument", []string{
			"search_document_id STRING", "source_kind STRING", "source_key STRING", "file_name STRING",
			"file_name_words STRING", "raw_sha256 STRING", "normalized_text_sha256 STRING", "size_bytes LONG",
			"passage_count LONG", "card STRING", "indexed_at DATETIME", "embedding ARRAY_OF_FLOATS", "embed_space STRING",
		}, []string{"(search_document_id) UNIQUE", "(card) " + fullText, "(file_name_words) " + fullText,
			"(embedding) " + vector, "(embed_space) NOTUNIQUE NULL_STRATEGY INDEX"}},
	} {
		statements = append(statements, "CREATE VERTEX TYPE "+t.name+" IF NOT EXISTS")
		for _, property := range t.properties {
			name, kind, _ := strings.Cut(property, " ")
			statements = append(statements, "CREATE PROPERTY "+t.name+"."+name+" IF NOT EXISTS "+kind)
		}
		for _, index := range t.indexes {
			statements = append(statements, "CREATE INDEX IF NOT EXISTS ON "+t.name+" "+index)
		}
	}
	return statements
}

type liveDocument struct {
	id, fileName, words, card, content string
	passages                           []livePassage
}

type livePassage struct {
	text   string
	vector []float64
	space  string
}

// liveLibrary is the VM library's shape in miniature: an English manual, an Italian
// transcript stored three times, an Italian screenshot, and the PRD, one of whose passages sits
// in another space and on the query's own vector.
func liveLibrary() []liveDocument {
	far := []float64{0, 1, 0}
	transcript := "In questo video parliamo di prompt engineering e di intelligenza artificiale: come si " +
		"scrive bene la domanda da porre al modello, con esempi e contesto."
	video := func(n int) liveDocument {
		return liveDocument{
			id: fmt.Sprintf("doc_video%d", n), fileName: "videoplayback.mp4", words: "videoplayback mp4",
			card: "videoplayback.mp4 — video, transcript in Italian about prompt engineering.", content: transcript,
			passages: []livePassage{{text: transcript, vector: []float64{0.9, 0.1, 0}, space: liveSpace}},
		}
	}
	return []liveDocument{
		{id: "doc_pid", fileName: "PidTemp_MultiZone_DOC_V11_en.pdf", words: "PidTemp MultiZone DOC V11 en pdf",
			card: "PidTemp_MultiZone_DOC_V11_en.pdf — PDF manual. Titled: PID_Temp multi-zone temperature control.",
			content: "pid", passages: []livePassage{
				{text: "PID_Temp controls the temperature of several zones at once. Each zone has its own heating " +
					"and cooling actuator, and the controller keeps the zones coupled while it tunes them.", vector: far, space: liveSpace},
				{text: "The safety light curtain stops the actuator when a hand enters the protected area; restarting " +
					"requires an explicit acknowledgement on the panel.", vector: far, space: liveSpace},
			}},
		video(1), video(2), video(3),
		{id: "doc_clock", fileName: "orologio_impostazioni.png", words: "orologio impostazioni png",
			card: "Screenshot delle impostazioni dell'orologio: fuso orario Europe/Rome, formato 24 ore.",
			content: "clock", passages: []livePassage{{text: "Impostazioni orologio. Fuso orario: Europe/Rome. " +
				"Formato 24 ore. Sincronizzazione automatica attiva.", vector: far, space: liveSpace}}},
		{id: "doc_prd", fileName: "prd.md", words: "prd md",
			card: "prd.md — Markdown. Titled: Aura product requirements. Headings: Approvals and durable grants.",
			content: "prd", passages: []livePassage{
				{text: "Approvals and durable grants: an operator approves a tool once and the grant is durable " +
					"until revoked.", vector: far, space: liveSpace},
				{text: "Durable grants survive a restart; a revoked grant stops the next call.", vector: liveQuery, space: liveOtherSpace},
			}},
	}
}

func seedLiveLibrary(t *testing.T, client *arcadedb.Client, schema string) {
	t.Helper()
	ctx := context.Background()
	for _, doc := range liveLibrary() {
		if _, err := client.Command(ctx, "INSERT INTO IndexedDocument SET search_document_id = :id, "+
			"source_kind = 's3', source_key = :key, file_name = :name, file_name_words = :words, "+
			"raw_sha256 = :raw, normalized_text_sha256 = :normalized, size_bytes = 1000, passage_count = :count, "+
			"card = :card, embedding = :embedding, embed_space = :space", map[string]any{
			"id": doc.id, "key": "library/" + doc.id + "/" + doc.fileName, "name": doc.fileName, "words": doc.words,
			"raw": sha(doc.content), "normalized": sha(doc.content), "count": len(doc.passages), "card": doc.card,
			"embedding": []float64{0, 0, 1}, "space": liveSpace,
		}); err != nil {
			t.Fatalf("insert card %s: %v", doc.id, err)
		}
		for ordinal, passage := range doc.passages {
			if _, err := client.Command(ctx, "INSERT INTO Passage SET passage_key = :key, search_document_id = :id, "+
				"source_kind = 's3', source_key = :source, raw_sha256 = :raw, schema_version = :schema, "+
				"ordinal = :ordinal, `text` = :text, normalized_text_sha256 = :normalized, "+
				"embedding = :embedding, embed_space = :space", map[string]any{
				"key": fmt.Sprintf("%s:%d", doc.id, ordinal), "id": doc.id, "source": "library/" + doc.id + "/" + doc.fileName,
				"raw": sha(doc.content), "schema": schema, "ordinal": ordinal, "text": passage.text,
				"normalized": sha(passage.text), "embedding": passage.vector, "space": passage.space,
			}); err != nil {
				t.Fatalf("insert passage %s:%d: %v", doc.id, ordinal, err)
			}
		}
	}
}

func liveRetriever(t *testing.T, client *arcadedb.Client) (*documents.HostRetriever, *arcadedb.DocumentIndex) {
	t.Helper()
	index, err := arcadedb.NewDocumentIndex(oneTenant{client}, arcadedb.DocumentIndexConfig{Dimensions: len(liveQuery)})
	if err != nil {
		t.Fatalf("NewDocumentIndex: %v", err)
	}
	return &documents.HostRetriever{
		ControlPlane: &documents.ArcadeRetrievalControlPlane{Index: index},
		PassageIndex: index, Embedder: liveEmbedder{space: liveSpace},
	}, index
}

// The schema version the reads accept is document-v1:standard-analyzer:cosine:none:<dims>
// (document_schema.go schemaVersion); the seed must write exactly that.
const liveSchema = "document-v1:standard-analyzer:cosine:none:3"

func TestDocumentSpaceLiveLexicalModeAnswersAndAbstains(t *testing.T) {
	client, _ := liveDocumentDatabase(t)
	seedLiveLibrary(t, client, liveSchema)
	retriever, index := liveRetriever(t, client)
	ctx := context.Background()
	if open, err := index.DocumentsDenseOpen(ctx, liveIdentity, liveSpace); err != nil || open {
		t.Fatalf("gate with a passage in another space: open=%v err=%v, want closed", open, err)
	}
	for _, test := range []struct {
		query, top string
	}{
		{"prompt engineering intelligenza artificiale", "videoplayback.mp4"},
		{"approvals and durable grants", "prd.md"},
		{"fuso orario Europe/Rome", "orologio_impostazioni.png"},
		{"heating and cooling actuator", "PidTemp_MultiZone_DOC_V11_en.pdf"},
		{"orari dei traghetti per la Sardegna", ""},
		{"recipe for chocolate cake", ""},
		{"chi è il?", ""},
	} {
		response, err := retriever.Retrieve(ctx, documents.RetrievalRequest{IdentityID: liveIdentity, Query: test.query})
		if err != nil {
			t.Fatalf("%q: %v", test.query, err)
		}
		if response.Status != documents.RetrievalLexicalOnly || response.DegradationReason != documents.DegradationSpaceMismatch {
			t.Fatalf("%q: status %q reason %q, want lexical_only for the space mismatch", test.query,
				response.Status, response.DegradationReason)
		}
		if test.top == "" {
			if !response.Abstained || len(response.Documents) != 0 {
				t.Fatalf("%q: %d documents, want abstention", test.query, len(response.Documents))
			}
			continue
		}
		if len(response.Documents) == 0 || response.Documents[0].Title != test.top {
			t.Fatalf("%q: documents %+v, want %s first", test.query, response.Documents, test.top)
		}
		videos := 0
		for _, doc := range response.Documents {
			if doc.Title == "videoplayback.mp4" {
				videos++
			}
		}
		if videos > 1 {
			t.Fatalf("%q: the three identical transcripts took %d places", test.query, videos)
		}
	}
}

// Review Focus 3: inside a cached "open" the other-space passage -- sitting on the query's own
// vector, the nearest possible neighbour -- must still never be ranked.
func TestDocumentSpaceLiveFusedLegNeverRanksAnotherSpace(t *testing.T) {
	client, _ := liveDocumentDatabase(t)
	seedLiveLibrary(t, client, liveSchema)
	_, index := liveRetriever(t, client)
	candidates, err := index.FusedCandidates(context.Background(), arcadedb.FusedCandidateQuery{
		CandidateFilter: arcadedb.CandidateFilter{IdentityID: liveIdentity, Limit: 10},
		Query:           "durable grants prompt engineering", Embedding: liveQuery, Space: liveSpace,
	})
	if err != nil {
		t.Fatalf("FusedCandidates: %v", err)
	}
	for _, candidate := range candidates {
		if candidate.PassageID == "doc_prd:1" {
			t.Fatalf("the passage stamped %s was ranked for a query in %s", liveOtherSpace, liveSpace)
		}
	}
	if len(candidates) == 0 || !strings.HasPrefix(candidates[0].SearchDocumentID, "doc_video") {
		t.Fatalf("candidates = %+v, want the in-space transcript first", candidates)
	}
}

// Once every vector is in the reader's space the gate opens, and the dense path the engine
// runs with the space predicate is the complete one.
func TestDocumentSpaceLiveGateOpensOnceTheLibraryIsWhole(t *testing.T) {
	client, fresh := liveDocumentDatabase(t)
	seedLiveLibrary(t, client, liveSchema)
	if _, err := client.Command(context.Background(),
		"UPDATE Passage SET embed_space = :space WHERE passage_key = 'doc_prd:1'", map[string]any{"space": liveSpace}); err != nil {
		t.Fatalf("restamp: %v", err)
	}
	retriever, _ := liveRetriever(t, fresh())
	response, err := retriever.Retrieve(context.Background(), documents.RetrievalRequest{
		IdentityID: liveIdentity, Query: "prompt engineering intelligenza artificiale",
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if response.Status != documents.RetrievalComplete || response.FloorsReason != arcadedb.ReasonUncalibratedFloors ||
		len(response.Documents) == 0 {
		t.Fatalf("response = %+v, want a complete dense answer naming its uncalibrated floors", response)
	}
}
```

- [ ] **Step 3: Run it on the VM's ArcadeDB**

Run: `cd $SCRATCH && MSYS_NO_PATHCONV=1 wsl -e bash arcade-vm.sh DocumentSpaceLive`
Expected: three `--- PASS` lines, and runtimes above a second (a sub-second tier is a skip tell).
- If ArcadeDB rejects `ORDER BY lexical_score DESC, passage_key ASC` (or the card twin), drop the second key from both statements and ledger the ruling. Re-run Task 5's tests as well: they assert only `ORDER BY lexical_score DESC`.
- If a known query ranks below the floor on this tiny corpus, the corpus is at fault, not the floor. Lengthen the passage text with more on-topic words, and ledger the ruling. Never lower the floor to pass.

Then check that nothing was left behind. Write `$SCRATCH/q27.sh`:

```bash
# Read-only, on the VM: disposable test databases the live tier left behind (want 0).
S() { echo aura | sudo -S -p '' "$@"; }
S docker exec aura-arcadedb ls /home/arcadedb/databases | grep -c aura_documents_space_ || true
```

Run: `cd $SCRATCH && MSYS_NO_PATHCONV=1 wsl -e bash r.sh q27.sh`
Expected: `0`.

- [ ] **Step 4: Commit**

```bash
git add internal/arcadedb/document_space_live_integration_test.go
git commit -m "test(documents): prove the space gate and lexical mode on a live ArcadeDB

On a disposable database mirroring the ingest DDL: a library with one passage in
another space answers known Italian and English questions lexically with the right
file first, counts identical transcripts once, and abstains on questions it cannot
answer; the fused leg never ranks the other-space passage even when it sits on the
query's own vector; and the gate opens once the library is whole.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>" -- internal/arcadedb/document_space_live_integration_test.go
```

---

## After this plan (carried to plan 5 and the VM E2E)

- **Plan 5:**
  - expose `floors_reason` through the memory MCP tools (`FactSearchResult.FloorsReason`, `RecallResult.FloorsReason`);
  - add the cockpit's per-family state and `aura doctor`'s documents gate report;
  - include the documents family in the release note: after the deploy, documents answer `lexical_only` until the re-embed completes.
- **VM E2E, after the deploy through the updater:**
  - rerun Task 1's `q25.sh`. While any row is unstamped, every line must read `lexical_only`, with the top documents spec §8's stopword measurement recorded. Once both gates open, every line must return Task 1's top document, with status `complete`.
  - Check that the boot log's `family=documents space=…` equals the stamps' `embed_space`.
  - If the documents gate never opens, list the files still in another space: that is spec §8's "a document that keeps failing", and the operator's to fix.
