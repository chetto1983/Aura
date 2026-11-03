# The urgency centroid in shadow: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or superpowers:executing-plans, task by task, on `master`. Steps use checkbox syntax.

**Goal:** every mail the watch decides also gets the tier a per-identity centroid would give, written beside the verdict with what the scoring needs, shown in the decisions view where it differs from the ladder, and counted by a read-only report. Nothing it says reaches a verdict, an alert or the digest.

**Architecture:** a new domain-agnostic package `internal/examplebank` keeps what a person confirmed as vectors in the identity's ArcadeDB (`arcadedb.ConfirmedExample`), embeds each domain's shipped seeds once per space, and builds the identity's classifier on `semindex.Classifier`. `internal/pimwatch` owns the mail-tier domain: the seeds file, the text and the four facts, the `Centroid` port the tick and the correction call, five new verdict columns and the report. `cmd/aura` wires the bank on the memory tenants and the documents' embedding route.

**Tech Stack:** Go, Postgres (migration + sqlc), ArcadeDB 26.11.x, `internal/semindex`, React (decisions view).

**Spec:** `docs/superpowers/specs/2026-11-03-urgency-centroid-design.md` (approved 2026-11-03).

## Global Constraints

- No sender, subject or mail text in Postgres, ArcadeDB, a log line or the ledger summary. ArcadeDB keeps vectors, labels, ids and the space; the ledger summary keeps counts.
- The shadow never changes a verdict's tier, label, `decided_by`, alert or the digest.
- A shadow failure (embedder, ArcadeDB, PIM, Postgres read) is silent to the person and never fails a tick or a correction.
- The vector: EmbeddingGemma over `"<from>\n<subject>"` (`from` is the bare address of `Change.From`), no task prefix, no body, normalised; then four facts in this order, each `0.3` when true: `WrittenTo` (true only when the provider says so), `ProviderImportant`, `AuthFailed`, `Bulk`.
- Weights: a correction counts 6 times, a seed once.
- Seeds: the pilot's 200 mails, 23 `now`, 72 `today`, 105 `later`, every sender under `.example`, embedded in memory and never written to ArcadeDB.
- Migration number: `ls internal/db/migrations/ | tail -1` at landing. Today the last is 0151, so this is 0152.
- No new env var and no switch: going live is a later spec.
- ≤600 LOC a file. Measured today: `watcher.go` 541, `memory.go` 577, `service.go` 446, `store_verdicts.go` 440, `DecisionRow.tsx` 259.
- Go runs in WSL: `go vet ./...`, `go build ./...`, then test and `-race` on each package touched. `db_integration` tests need `AURA_DB_URL` and `AURA_DB_MIGRATE_URL`, and `arcadedb_integration` tests need `ARCADEDB_URL` and `ARCADEDB_PASSWORD`, all with the stack up.
- Commit by pathspec on `master`. The 22 sign-in files of the stopped peer session stay out of every commit.
- The repository is public: no real mail, address or person's name in tests, fixtures, docs or commit messages.

## Review Focus

1. **The embedding sidecar down or hung at tick time.** The tick's notification goes out at most `shadowTimeout` (5 s) later, with the shadow empty and counted. Pinned by `TestASlowEmbedderCostsAtMostTheTimeout` (Task 4).
2. **The embedding route swapped while the daemon runs (local ↔ OpenRouter).** Seeds and examples of the old space never mix into a classifier of the new one. Pinned by `TestClassifierReadsOnlyTheLiveSpace` (Task 3).
3. **A mail with an unparsable `From` or no subject.** The centroid still answers, and the per-sender memory falls back to the ladder's tier. Pinned by `TestAMailWithNoSenderStillGetsAShadow` (Task 5).
4. **An account removed while ArcadeDB is unreachable, or re-linked.** Its examples leave the bank at the next build and are forgotten then. Pinned by `TestClassifierLeavesOutAndForgetsOrphanOwners` (Task 3) and `TestRemovingAnAccountForgetsItsExamples` (Task 5).
5. **Two accounts of one identity ticking at once on a cold start.** The seeds are embedded once. Pinned by `TestTheSeedsWarmOnceInTheBackground` (Task 3).

## Decisions taken while writing this plan

1. **Which owners are alive.** An owner is alive while the account has a correction row in Postgres (`CorrectedAccounts`). The bank's build forgets orphans, not the retention sweep: `pimwatch.Sweep` only expires verdicts and has no ArcadeDB client, and a re-link already deletes the correction rows.
2. **Digest coverage.** Whether a digest covered a verdict is derived from `pim_watch_settings.last_digest_at` and the tier, not kept in a column: every `now` or `today` verdict decided before the last window closed was in a window. Spec decision 7 named a column, but the column would hold the same fact.
3. **The rung is kept too.** The row also keeps the ladder's rung (`ladder_by`), because a correction overwrites `decided_by` and decision 8's counts per rung need it.
4. **Seeds embed in the background, once per space.** A cold start therefore never holds an alert, and until the seeds are ready the shadow is empty and counted. Spec decision 4 said a failed seed leaves the bank smaller, but the embedding API fails a batch whole, so the build retries whole on the next tick.
5. **What a correction teaches.** A correction teaches the centroid only when four things hold: it changes a mail's tier, the row has the facts (it was written after landing), the sender was read, and the mail passed authentication. These are the mails that already write a correction row.
6. **One embedding call per page, not per tick.** A tick that reads two pages makes two calls.
7. **The ArcadeDB type.** It is a vertex type, `ConfirmedExample`, with no vector index (banks hold tens of rows and `semindex` is brute force), unique on `(domain, owner_ref, item_ref)`. It stays out of `memorySpaceTypes`: the space pass re-embeds from text, and an example has none.
8. **The shadow runs on the documents' embedding route, whatever it is.** On a cloud route it costs that route's tokens: about 4,000 for the seeds, once per process and per space, then one short text per mail.
9. **The report's three truths.** It prints strong truth, where the ladder loses by construction (a correction is almost always the ladder's mistake), weak truth, where the ladder wins by construction, and their union. The union is the fair comparison. How the promotion gate reads these is the promotion spec's question.
10. **Timeouts.** The page's embedding and the bank read get `shadowTimeout = 5 * time.Second`, the background seed build `seedTimeout = 2 * time.Minute`, and a correction's embedding `shadowTimeout`.

## File structure

| File | Change |
|---|---|
| `internal/db/migrations/0152_pim_watch_shadow.{up,down}.sql` | new: eight columns on `pim_watch_verdicts` |
| `internal/db/queries/pim_watch.sql`, `internal/db/sqlc/` | the insert takes them, three new reads |
| `internal/db/db_unit_test.go`, `internal/db/migrate_0152_integration_test.go` | head 152, the migration's test |
| `internal/pimwatch/store_verdicts.go`, new `store_shadow.go` | `Verdict` and `VerdictDraft` carry the shadow, `SenderMemory`, `CorrectedAccounts`, `ShadowRows` |
| `internal/arcadedb/examples.go` (new), `memory.go`, `memory_batch_store.go` | the example type, put, list, forget; `rowFloats` extracted |
| `internal/examplebank/` (new) | `bank.go`, `seeds.go`, `tenant.go` and tests |
| `scripts/coverage_package_policy.json` | `internal/examplebank` at target |
| `internal/pimwatch/seeds/mail_tier.json` (new), `centroid.go` (new) | the mail-tier domain and the `Centroid` port |
| `internal/pimwatch/watcher.go`, new `watcher_shadow.go` | the tick writes the shadow and counts it |
| `internal/pimwatch/service.go`, `service_rules.go` | a correction teaches, a removed account forgets, the decisions line marks a disagreement |
| `cmd/aura/chat_boot.go`, `serve_pimwatch.go` | wiring |
| `internal/agui/pim_watch_api.go`, `web/src/governance/…` | `centroid_tier` and its mark |
| `internal/pimwatch/shadow_report.go` (new), `cmd/aura/mail_watch_cli.go` (new), `main.go` | `aura mail-watch shadow` |

---

### Task 1: The verdict row keeps the ladder's answer, the facts and the shadow

**Files:** new `internal/db/migrations/0152_pim_watch_shadow.up.sql` and `.down.sql`, new `internal/db/migrate_0152_integration_test.go`, `internal/db/db_unit_test.go:397-399`, `internal/db/queries/pim_watch.sql`, `internal/db/sqlc/` (regenerated), `internal/pimwatch/store_verdicts.go`, new `internal/pimwatch/store_shadow.go`, new `internal/pimwatch/store_shadow_integration_test.go`

**Produces:**
```go
type MailFacts struct{ WrittenTo, ProviderImportant, AuthFailed, Bulk bool }

// Shadow is what a mail's row keeps beside the ladder for the scoring. Nothing in it changes a verdict.
type Shadow struct {
    Facts        MailFacts
    SenderMemory Tier    // "" when it could not be read
    Centroid     Tier    // "" when the centroid did not answer
    Margin       float64 // meaningful only with Centroid
}
// VerdictDraft gains:  Shadow *Shadow  (nil for events)
// Verdict gains:       LadderTier Tier; LadderBy DecidedBy; Facts MailFacts; Centroid Tier; SenderMemory Tier

type ShadowRow struct {
    Tier, LadderTier, Centroid, SenderMemory Tier
    By, LadderBy                             DecidedBy
    Margin                                   float64
    DecidedAt                                time.Time
    Notified                                 bool
}
func (s *Store) SenderMemory(ctx context.Context, identityID string, senderKeys []string) (map[string]Tier, error)
func (s *Store) CorrectedAccounts(ctx context.Context, identityID string) ([]string, error)
func (s *Store) ShadowRows(ctx context.Context, identityID string, since time.Time) ([]ShadowRow, error)
```

The up migration, verbatim:
```sql
-- The urgency centroid's shadow (docs/superpowers/specs/2026-11-03-urgency-centroid-design.md, decision 7).
-- A correction overwrites tier and decided_by, so the ladder's own answer is kept apart and never
-- changed. The four facts are the centroid's; sender_memory_tier is the per-sender baseline's. No
-- sender and no text.
ALTER TABLE aura.pim_watch_verdicts
    ADD COLUMN ladder_tier        text CHECK (ladder_tier IN ('now', 'today', 'later')),
    ADD COLUMN ladder_by          text CHECK (ladder_by IN ('rule', 'auth_failed', 'people', 'category', 'default')),
    ADD COLUMN centroid_tier      text CHECK (centroid_tier IN ('now', 'today', 'later')),
    ADD COLUMN centroid_margin    double precision,
    ADD COLUMN sender_memory_tier text CHECK (sender_memory_tier IN ('now', 'today', 'later')),
    ADD COLUMN written_to         boolean NOT NULL DEFAULT false,
    ADD COLUMN provider_important boolean NOT NULL DEFAULT false,
    ADD COLUMN bulk               boolean NOT NULL DEFAULT false;
```
The down migration drops the eight columns.

The queries: `InsertPIMWatchVerdict` takes the eight columns after `decided_at`. `UpsertPIMWatchEventVerdict` and `CorrectPIMWatchVerdict` stay as they are, so a correction never touches `ladder_*`. Three new reads:
```sql
-- name: ListPIMWatchSenderMemory :many
-- The per-sender memory, §7 G2b's baseline: each sender's last corrected tier.
SELECT DISTINCT ON (sender_key) sender_key, tier FROM aura.pim_watch_corrections
WHERE identity_id = $1 AND sender_key = ANY(@sender_keys::text[]) AND tier IS NOT NULL
ORDER BY sender_key, created_at DESC;

-- name: ListPIMWatchCorrectedAccounts :many
SELECT DISTINCT account_id FROM aura.pim_watch_corrections WHERE identity_id = $1;

-- name: ListPIMWatchShadowRows :many
SELECT tier, decided_by, ladder_tier, ladder_by, centroid_tier, centroid_margin, sender_memory_tier, decided_at, notified
FROM aura.pim_watch_verdicts
WHERE identity_id = $1 AND resource = 'mail' AND ladder_tier IS NOT NULL AND decided_at >= @since;
```

- [ ] `TestMigrate0152AddsTheShadowColumns` (copy the shape of `migrate_0149_integration_test.go`). At 151, insert a mail verdict and step up one. Expected: the row reads `ladder_tier IS NULL`, `written_to = false`, `bulk = false`. An update to `ladder_tier = 'soon'` and one to `ladder_by = 'person'` both fail on their CHECK. Stepping down drops the columns.
- [ ] Set the head pin in `db_unit_test.go` to 152.
- [ ] `TestAMailRowKeepsTheLaddersAnswerAfterACorrection`. `CommitPage` takes one mail draft with `Decision{Tier: today, By: people}` and `Shadow{Facts{WrittenTo: true, Bulk: true}, SenderMemory: today, Centroid: now, Margin: 0.04}`. Expected from `AccountVerdicts`: `LadderTier today`, `LadderBy people`, `Centroid now`, `SenderMemory today`, `Facts.WrittenTo` and `Facts.Bulk` true. After `ApplyCorrection` to `later`: `Tier later`, `By person`, `LadderTier today`, `LadderBy people`.
- [ ] `TestAnEventRowHasNoLadderAnswer`. An event draft leaves `ladder_tier`, `centroid_tier` and `sender_memory_tier` NULL.
- [ ] `TestSenderMemoryIsTheLastCorrectedTier`. Sender `a@one.example` is corrected to `today` and later to `now`, and `b@one.example` gets a label-only correction. Expected: `SenderMemory(…, ["a@one.example", "b@one.example", "c@one.example"])` returns `{"a@one.example": now}`.
- [ ] `TestCorrectedAccountsListsTheAccountsWithCorrections`. Two accounts each with a correction are listed. After `DeleteAccount` on one, only the other is listed.
- [ ] `TestShadowRowsReadOnlyMailWithALadderAnswer`. A pre-landing mail row (`ladder_tier` NULL), an event row and a row older than `since` are absent. The landed mail row comes back with every field.
- [ ] Run them and see them fail. Write the migration, run `make sqlc` in WSL, then implement `writeVerdict` (a mail draft writes `ladder_tier`/`ladder_by` from its `Decision` and the rest from `Shadow`), `verdictFromRow` and the three reads in `store_shadow.go`.
- [ ] Green, with race, on `./internal/db/` and `./internal/pimwatch/` under `db_integration`. Commit: `feat(pimwatch): the verdict row keeps the ladder's answer and the centroid's shadow`.

### Task 2: ArcadeDB keeps confirmed examples

**Files:** new `internal/arcadedb/examples.go`, new `internal/arcadedb/examples_test.go`, new `internal/arcadedb/examples_live_integration_test.go` (`arcadedb_integration`), `internal/arcadedb/memory.go` (one loop in `EnsureMemorySchema`), `internal/arcadedb/memory_batch_store.go`

**Produces:**
```go
const ExampleType = "ConfirmedExample"

// Example is one thing a person confirmed: which domain, whose (Owner), which item (Ref), the
// label, and the vector in its space. No text.
type Example struct {
    Domain, Owner, Ref, Label string
    Vector                    []float64
    Space                     string
    CreatedAt                 time.Time
}
func (c *Client) PutExample(ctx context.Context, e Example) error
func (c *Client) Examples(ctx context.Context, domain, space string) ([]Example, error) // Owner, Ref, Label, Vector, Space filled
func (c *Client) ForgetExamples(ctx context.Context, domain, owner string) (int, error)
```

The schema, verbatim, plus `spaceStampStatements(ExampleType)`:
```
CREATE VERTEX TYPE ConfirmedExample IF NOT EXISTS
CREATE PROPERTY ConfirmedExample.domain IF NOT EXISTS STRING
CREATE PROPERTY ConfirmedExample.owner_ref IF NOT EXISTS STRING
CREATE PROPERTY ConfirmedExample.item_ref IF NOT EXISTS STRING
CREATE PROPERTY ConfirmedExample.label IF NOT EXISTS STRING
CREATE PROPERTY ConfirmedExample.embedding IF NOT EXISTS ARRAY_OF_FLOATS
CREATE PROPERTY ConfirmedExample.created_at IF NOT EXISTS DATETIME
CREATE INDEX IF NOT EXISTS ON ConfirmedExample (domain, owner_ref, item_ref) UNIQUE
```
`PutExample` is one `UPDATE ConfirmedExample SET … UPSERT WHERE domain = :domain AND owner_ref = :owner_ref AND item_ref = :item_ref`, the idiom of `upsertConversationProjectionStatement`. `created_at` is UTC, written the way this package writes its other datetimes. `rowVector`'s float loop moves into `rowFloats(row map[string]any, key string) []float64`, with no width check; `rowVector` keeps its own check and calls it.

- [ ] Unit tests in `examples_test.go`:
  - `TestExampleSchemaIsStampedAndHasNoVectorIndex`: the statements include `spaceStampStatements(ExampleType)` and none contains `LSM_VECTOR`.
  - `TestPutExampleRefusesAnIncompleteExample`: an empty domain, owner, ref, label, vector or space is an error before any request.
  - `TestExamplesAreNotInTheSpacePass`: no entry of `memorySpaceTypes` is named `ExampleType`.
- [ ] Live test `TestConfirmedExamplesPutReplaceListForget`:
  1. `EnsureMemorySchema` runs twice.
  2. Put owner `a` refs `1` and `2` in space `s1`, and owner `b` ref `1` in `s2`, all at 772 dimensions.
  3. Put `a`/`1` again with a new label.
  4. Expected: `Examples("mail_tier", "s1")` holds two rows, `a`/`1` carries the new label, and its vector round-trips within `1e-6` per component (`ARRAY_OF_FLOATS` is 32-bit).
  5. `ForgetExamples("mail_tier", "a")` returns 2; `s1` is then empty and `s2` still holds one row.
- [ ] Red, implement, green: unit tests and race on `./internal/arcadedb/`, the live test with `-tags arcadedb_integration -run ConfirmedExamples`. `memory.go` must stay under 600 lines. Commit: `feat(arcadedb): a type for what a person confirmed, as vectors`.

### Task 3: The example bank, shared by every domain

**Files:** new `internal/examplebank/bank.go`, `seeds.go`, `tenant.go`, `bank_test.go`, `main_test.go` (goleak, as `internal/agui/main_test.go`), `scripts/coverage_package_policy.json`

**Consumes:** `arcadedb.Example`, `arcadedb.DenseEmbedder`, `arcadedb.TenantClients`, `semindex.NewClassifier`, `semindex.Classifier.AddVecs`.

**Produces:**
```go
type Seed struct {
    Text  string
    Extra []float64
    Label string
}
// Domain is what one consumer brings. Owners lists the owners whose examples still count.
type Domain struct {
    Name          string
    Seeds         []Seed
    ExampleWeight int
    Owners        func(ctx context.Context, identityID string) (map[string]bool, error)
}
type Store interface {
    PutExample(ctx context.Context, identityID string, e arcadedb.Example) error
    Examples(ctx context.Context, identityID, domain, space string) ([]arcadedb.Example, error)
    ForgetExamples(ctx context.Context, identityID, domain, owner string) (int, error)
}
// TenantStore is Store on the identities' memory databases: a write opens the database (For), a
// read or a forget on a database that does not exist finds nothing (Existing).
type TenantStore struct{ Clients *arcadedb.TenantClients }
type Bank struct {
    Store    Store
    Embedder arcadedb.DenseEmbedder
    // unexported: the mutex, seed rows per (domain, space), seed builds in flight, classifiers per (identity, domain, space)
}
var ErrSeedsWarming = errors.New("examplebank: the seeds are still being embedded")
func Row(embedding, extra []float64) []float64
func (b *Bank) Embed(ctx context.Context, texts []string) (vectors [][]float64, space string, err error)
func (b *Bank) Classifier(ctx context.Context, identityID string, d Domain, space string) (*semindex.Classifier, error)
func (b *Bank) Put(ctx context.Context, identityID string, d Domain, e arcadedb.Example) error
func (b *Bank) ForgetOwner(ctx context.Context, identityID string, d Domain, owner string) error
```

The behaviour the tests pin:
- `Embed` reads the space before and after embedding, and refuses when the two differ. This is `documents.HostRetriever.denseQuery`'s rule.
- `Classifier` returns the cached classifier for `(identity, domain, space)` if there is one. Otherwise it needs the domain's seed rows for that space.
- If the seed rows are missing, `Classifier` starts at most one build per `(domain, space)` and returns `ErrSeedsWarming`. The build runs on `context.WithoutCancel(ctx)` under `seedTimeout`, embeds every seed text in one call, keeps the rows only if the space read after the call equals `space`, and logs `slog.Info("example bank seeds embedded", "domain", …, "seeds", …, "space", …, "took", …)`. A failed build caches nothing.
- With the seeds ready, `Classifier` reads `Store.Examples(identity, domain, space)` and `d.Owners`. An example whose owner is not alive is left out, and `ForgetExamples` runs once per such owner. Every seed is added once, every example `d.ExampleWeight` times, and the classifier is cached. If a forget failed, the classifier is returned but not cached.
- `Put` refuses an example of another domain. `Put` and `ForgetOwner` drop the identity's cached classifiers for the domain before writing.

- [ ] `TestRowNormalisesTheEmbeddingAndAppendsTheFacts`: `Row([]float64{3, 4}, []float64{0.3, 0})` equals `{0.6, 0.8, 0.3, 0}`.
- [ ] `TestAnExampleWeighsAsMuchAsSixSeeds`. Build seeds `a`→`[1,0,0]` and `b`→`[0,1,0]` plus one `b` example at `[0.6,0,0.8]` with weight 6. A `semindex.Classifier` built by hand with `AddVecs("b", seed, v, v, v, v, v, v)` gives the same `Verdict` (label, score and margin to `1e-12`) for `[1,0,0]`, `[0,1,0]` and `[0.7,0,0.7]`.
- [ ] `TestTheSeedsWarmOnceInTheBackground`. Five concurrent `Classifier` calls return `ErrSeedsWarming`, the fake embedder is called once with all seed texts, and a bounded poll then gets a classifier.
- [ ] `TestAFailedSeedBuildIsRetried`. The first build errors and the second succeeds, so the embedder is called twice.
- [ ] `TestClassifierReadsOnlyTheLiveSpace`. `Store.Examples` gets the `space` argument, and a second space builds its own seeds (two embedder calls), never reusing the first.
- [ ] `TestClassifierLeavesOutAndForgetsOrphanOwners`. Examples come from owners `a` and `gone`, and `Owners` returns `{a}`. Expected: `ForgetExamples(…, "gone")` is called once, and for three queries the classifier gives the same `Verdict` as one built by hand from the seeds and `a`'s examples only.
- [ ] `TestClassifierIsCachedUntilAnExampleChanges`. Two calls read the store once; `Put` makes the next call read again, and so does `ForgetOwner`.
- [ ] `TestEmbedRefusesASpaceThatChangedMidRequest`.
- [ ] `TestPutRefusesAnExampleOfAnotherDomain`.
- [ ] Add `"github.com/chetto1983/aura/internal/examplebank": {"mode": "target"}` to the coverage policy, in order.
- [ ] Red, implement, green, with race and goleak. Measure coverage of the package with `go test -cover`: it must be ≥85%. Commit: `feat(examplebank): one bank of confirmed examples for every domain`.

### Task 4: The mail-tier domain

**Files:** new `internal/pimwatch/seeds/mail_tier.json`, new `internal/pimwatch/centroid.go`, new `internal/pimwatch/centroid_test.go`

**Consumes:** Task 3's `Bank`, `Domain`, `Seed`, `Row`; Task 1's `MailFacts`.

**Produces:**
```go
// Centroid is the shadow's port. Nil switches the shadow off.
type Centroid interface {
    Judge(ctx context.Context, identityID string, mails []ShadowMail) ([]CentroidAnswer, error)
    Learn(ctx context.Context, identityID string, lesson Lesson) error
    Forget(ctx context.Context, identityID, accountID string) error
}
type ShadowMail struct {
    From, Subject string
    Facts         MailFacts
}
type CentroidAnswer struct {
    Tier   Tier // "" when it could not answer
    Margin float64
}
type Lesson struct {
    AccountID, ItemID, From, Subject string
    Facts                            MailFacts
    Tier                             Tier
}
type OwnerLister interface {
    CorrectedAccounts(ctx context.Context, identityID string) ([]string, error)
}
type CentroidShadow struct{ /* bank, domain, timeout */ }
func NewCentroidShadow(bank *examplebank.Bank, owners OwnerLister) (*CentroidShadow, error)
func factsOf(s Signals) MailFacts
func (f MailFacts) extra() []float64
func mailText(from, subject string) string
```
The constants are `mailTierDomain = "mail_tier"`, `correctionWeight = 6`, `factWeight = 0.3` and `shadowTimeout = 5 * time.Second`. The timeout lives on the struct (default `shadowTimeout`) so a test can shorten it. The seeds file is read with `//go:embed seeds/mail_tier.json`.

**The seeds file.** It is generated once by a script kept outside the repository (`D:\tmp\mail-class\export_seeds.py`) from `D:\tmp\mail-pilot`:
- personas in `personas.json` order, mails in file order;
- one JSON object per line inside a JSON array, keys `from`, `subject`, `written_to`, `provider_important`, `auth_failed`, `bulk`, `tier`;
- `from` is `run7.py`'s `anonymous()`: the local part, then the domain's labels without the TLD and without `www`, `mail` and `pec`, then `.example`;
- `written_to` ← `known_correspondent`, `provider_important` ← `importance_high`, `bulk` ← `list_id or unsubscribe`, `tier` from `labels-<persona>.jsonl`.

Before committing, read the 200 subjects once. A subject that names a real company or a real person is rewritten to a neutral one in its language, and the commit message gives the count.

- [ ] `TestMailTierSeedsAreTheAnonymisedPilot`. Expected: 200 seeds; tiers 23 `now`, 72 `today`, 105 `later`; true facts 154 `written_to`, 25 `provider_important`, 12 `auth_failed`, 78 `bulk`; every `from` has one `@` and ends in `.example`; no subject is empty.
- [ ] `TestFactsOfReadsWrittenToOnlyWhenTheProviderSaysSo`: a nil `WrittenTo` gives false.
- [ ] `TestExtraIsTheFourFactsInOrder`: `MailFacts{WrittenTo: true, AuthFailed: true}.extra()` equals `{0.3, 0, 0.3, 0}`.
- [ ] `TestMailTextIsSenderThenSubject`: `mailText("a@b.example", "Oggetto")` equals `"a@b.example\nOggetto"`.
- [ ] `TestJudgeFailsWhileTheSeedsWarm`: `errors.Is(err, examplebank.ErrSeedsWarming)`.
- [ ] `TestJudgeAnswersEveryMailOfAPage`. Use a deterministic fake embedder and a fake store, and poll until warm. Judging three mails gives three answers with valid tiers and margins ≥ 0, and the embedder received the three `mailText`s in order.
- [ ] `TestLearnStoresTheCorrectionAsAnExample`. Expected `PutExample` fields: `Domain "mail_tier"`, `Owner` the account, `Ref` the item, `Label "later"`, `len(Vector)` = dimensions + 4, the last four equal to `extra()`, and a non-empty `Space`.
- [ ] `TestASlowEmbedderCostsAtMostTheTimeout`. With a 50 ms timeout and an embedder that blocks until its context ends, `Judge` returns an error within 500 ms.
- [ ] Red, generate the seeds, implement, green with race. Commit: `feat(pimwatch): the mail-tier domain of the example bank`.

### Task 5: The tick writes the shadow, a correction teaches, a removed account forgets

**Files:** new `internal/pimwatch/watcher_shadow.go`, `watcher.go`, `service.go`, `service_rules.go`, new `internal/pimwatch/watcher_shadow_integration_test.go`, `service_rules_integration_test.go`

**Consumes:** Task 1's store reads and `Shadow`, Task 4's `Centroid`, `factsOf`.

**Produces:** `Watcher.Centroid Centroid` and `Service.Centroid Centroid` (nil is off); `WatchStore` gains `SenderMemory`; three counters on `tick`.

`watcher_shadow.go` holds `func (w *Watcher) shadow(ctx context.Context, t *tick, mails []Change, drafts []VerdictDraft)`, called from `draft` after the loop, for the added mails only. For each mail it does four things:
1. It sets `drafts[i].Shadow = &Shadow{Facts: factsOf(change.Signals)}`.
2. It reads `Store.SenderMemory` once for the page's sender keys. Each mail's `SenderMemory` is that tier, or else the draft's own ladder tier. A sender key that is empty or unparsable takes the ladder tier. A failed read leaves every `SenderMemory` empty.
3. With a non-nil `Centroid`, it calls `Judge` once for the page and copies the answers.
4. It counts each mail as answered, agreed (same tier as the ladder) or empty. A `Judge` error counts every mail empty.

`tick.summary()` appends `shadow: %d answered, %d agreed, %d empty` only when `w.Centroid != nil`.

`Correct` reads the item through `itemOf`, which replaces `senderOf` and returns the `Item` and whether it was read. After `ApplyCorrection` it calls `Centroid.Learn` only when four things hold: the resource is mail, `c.Tier` is set, the item was read, and the returned verdict has `!AuthFailed && LadderTier != ""`. A `Learn` error is logged as `slog.Warn("mail watch: a correction taught the centroid nothing", "timeout", errors.Is(err, context.DeadlineExceeded))`, and nothing else. `RemoveAccount` calls `Centroid.Forget` after `DeleteAccount`, and its error is joined with `stopDigestIfIdle`'s so neither skips the other.

- [ ] `TestTheTickWritesTheShadowBesideTheVerdict`. Earlier, sender `a@one.example` was corrected to `now`. The page holds two mails: one from `a@one.example` that the ladder files `today` by `people`, and one that a domain rule files `today`. A fake centroid answers `{now, 0.05}` and `{later, 0.01}`. Expected rows: `ladder_*` from the decisions; `sender_memory_tier` `now` for the first and `today` for the second; the centroid's tiers and margins; the facts from the signals. The summary ends `shadow: 2 answered, 0 agreed, 0 empty`.
- [ ] `TestAFailingCentroidLeavesTheVerdictAsToday`. Expected: the same tiers and alerts as a run with no centroid, NULL `centroid_tier`, `shadow: 0 answered, 0 agreed, 2 empty`, and no error.
- [ ] `TestNoCentroidStillWritesTheFactsAndTheMemory`. Expected: the facts and `sender_memory_tier` are written, `centroid_tier` is NULL, and the summary has no `shadow:`.
- [ ] `TestAMailWithNoSenderStillGetsAShadow`. With `From` `"not an address"` and an empty subject: `sender_memory_tier` is the ladder's tier and the centroid was asked for that mail.
- [ ] `TestACorrectionTeachesTheCentroid`. A tier correction calls `Learn` once with the sender and subject read live, the row's facts and the new tier. No `Learn` call follows a label-only correction, a calendar item, a mail that failed authentication, or a pre-landing row.
- [ ] `TestAFailedLessonKeepsTheCorrection`. When `Learn` errors, `Correct` returns the verdict with no error and the correction row exists.
- [ ] `TestRemovingAnAccountForgetsItsExamples`. Expected: `Forget` is called with the account. When `Forget` errors, `RemoveAccount` returns the error and the idle digest is still cancelled.
- [ ] Red, implement, green with race under `db_integration`. Keep `watcher.go` under 600. Commit: `feat(pimwatch): the centroid answers in shadow and learns from corrections`.

### Task 6: Wiring

**Files:** `cmd/aura/chat_boot.go` (`chatEnv.memoryClients`), `cmd/aura/serve_pimwatch.go`, `cmd/aura/serve_pimwatch_test.go`

**Produces:** `func newPIMWatchCentroid(chat *chatEnv, store *pimwatch.Store) pimwatch.Centroid`. It returns a true nil without `chat.memoryClients` or `chat.documentEmbedder`. Otherwise it returns `pimwatch.NewCentroidShadow(&examplebank.Bank{Store: examplebank.TenantStore{Clients: chat.memoryClients}, Embedder: chat.documentEmbedder}, store)`. A seeds-file error is logged and gives nil. `newPIMWatchRuntime` sets the result on both `service.Centroid` and `watcher.Centroid`.

- [ ] `TestThePIMWatchShadowNeedsMemoryAndAnEmbedder`. Without memory clients, `runtime.service.Centroid == nil` (an untyped nil interface). With both pieces, the service and the watcher hold the same non-nil value.
- [ ] Red, implement, green. `go vet ./... && go build ./...`. Commit: `feat(serve): wire the urgency centroid's shadow`.

### Task 7: The decisions view marks where the centroid disagrees

**Files:** `internal/pimwatch/service.go` (`DecisionLine.Centroid Tier`), `store_verdicts.go` (`func (v Verdict) centroidDisagrees() Tier`), `internal/agui/pim_watch_api.go` (`CentroidTier string \`json:"centroid_tier,omitempty"\``), `internal/agui/pim_watch_api_test.go`, `service_integration_test.go`, `web/src/governance/mailWatchApi.ts`, `web/src/governance/mailWatch/DecisionRow.tsx`, `web/src/i18n/resources.governance.mailWatch.ts`, `web/src/test/mailWatchServer.tsx`, `web/src/governance/mailWatch/__tests__/DecisionsView.test.tsx`

`centroidDisagrees` returns the centroid's tier when it is set and differs from `LadderTier`, else `""`. The comparison is with the ladder, never the current tier. Copy: en `row.centroid: 'From similar mail it would have said {{tier}}'`, it `row.centroid: 'Da mail simili avrebbe detto {{tier}}'`, where `{{tier}}` is `t('governance.mailWatch.tier.<tier>')`. `RowReason` renders it as a muted line under the reason, with lucide `FlaskConical` marked `aria-hidden`.

- [ ] `TestDecisionsMarkWhereTheCentroidDisagrees`. Three rows: ladder `today` with centroid `now` gives `now`; ladder `today` with centroid `today` gives `""`; a row corrected to `later` with ladder `today` and centroid `today` gives `""`.
- [ ] The API test sees `centroid_tier` when set and no key when empty.
- [ ] Vitest: a row with `centroid_tier: 'now'` shows the line with the localized tier, and a row without it shows none.
- [ ] Red, implement, green: Go tests with race; `npm run lint`, `npm test` and the type check in `web/` on Windows. Commit: `feat(cockpit): mark the mails where the centroid would have decided otherwise`.

### Task 8: The report

**Files:** new `internal/pimwatch/shadow_report.go` and `shadow_report_test.go`, new `cmd/aura/mail_watch_cli.go` and `mail_watch_cli_test.go`, `cmd/aura/main.go` (`case "mail-watch"`, `topLevelUsage`, and any test pinning it)

**Produces:**
```go
type ArmScore struct {
    Right, NowFound, NowAll int
    MacroF1                 float64
}
type Truth struct {
    Count                    int
    Ladder, Memory, Centroid ArmScore
}
type ShadowReport struct {
    Rows, Answered        int
    Strong, Weak, All     Truth
    Overrules             map[DecidedBy]int // centroid ≠ ladder, by the ladder's rung
    MarginP10, MarginP50, MarginP90 float64
}
func BuildShadowReport(rows []ShadowRow, lastDigest, now time.Time) ShadowReport
func (r ShadowReport) WriteText(w io.Writer)
```

The definitions:
- **Strong truth:** a row with `By == ByPerson`, true tier `Tier`.
- **Weak truth:** a row with `By != ByPerson` and `DecidedAt` before `now − 48h`, and either `Notified` or both a `Tier` of `now`/`today` and `DecidedAt ≤ lastDigest` (with `lastDigest` set).
- **All:** the union of the two.
- **The arms:** the ladder answers `LadderTier`, the memory `SenderMemory`, the centroid `Centroid`. An empty answer counts as wrong.
- **Macro F1:** the mean F1 over `now`, `today` and `later`, leaving out a tier absent from both the truth and that arm's answers.
- **Margins:** percentiles by nearest rank over the answered rows, 0 when there are none.

`WriteText` prints counts only, and says that strong truth favours the memory and weak truth favours the ladder by construction.

`aura mail-watch shadow --identity=<uuid> [--days=30]` opens the pool as `cache_stats.go` does, reads `Store.Settings` and `Store.ShadowRows(since = now − days)`, and writes the report. A missing identity or bad days exits 64.

- [ ] `TestBuildShadowReportOnAHandTable`. Ten hand-made rows give pinned exact values: counts per truth, `Right`, `NowFound`/`NowAll`, macro F1 to `1e-9`, the overrules per rung, and p10/p50/p90.
- [ ] `TestWeakTruthNeedsTheMailToHaveReachedThePerson`. A `later` row before the digest is not weak truth, a `today` row before it is, and an unnotified `now` row after it is not.
- [ ] `TestTheReportPrintsNoSenderAndNoId`. The text holds no `@` and no row id.
- [ ] `TestMailWatchShadowUsage`. No `--identity` exits 64, and `--days=0` exits 64.
- [ ] Red, implement, green. Commit: `feat(cli): aura mail-watch shadow, the centroid's counts`.

### Task 9: Live, on the lab VM

- [ ] Correct the watch spec `2026-10-31-mail-calendar-watch-design.md` §5.4: a correction's vector lives in the identity's ArcadeDB as a confirmed example. Point it at this plan and at the spec. Commit both docs.
- [ ] Run `go vet`, the build, lint and the full tag matrix. Push with the WSL recipe, wait until every workflow is green on the SHA, then the operator updates the VM from the cockpit.
- [ ] Measure, as aggregates only:
  - how long the seed build took, from its log line;
  - the tick's duration before and after, from the run ledger;
  - the first ticks' shadow counts in the ledger summary;
  - that one correction made in the cockpit leaves one `ConfirmedExample` row (count only);
  - the decisions view mark, which the operator confirms on screen;
  - `aura mail-watch shadow` printing.
- [ ] Write `docs/superpowers/verification/2026-11-XX-urgency-centroid-shadow-live.md`: what was measured, what it does not prove (the spike's number on real mail, past 15 corrections, a cloud embedding route), and that the seven-day promotion gate is still ahead.
- [ ] Only then the PRD amendment, citing that file.
