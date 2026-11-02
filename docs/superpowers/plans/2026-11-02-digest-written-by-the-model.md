# The digest the model writes: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans, task by task, on `master`. Steps use checkbox syntax.

**Goal:** the daily digest stops being a list of everything and becomes what the person needs to read, written once a day by the identity's model and checked by Aura, with the fixed list as its fallback.

**Architecture:** `internal/pimwatch` asks through a new `DigestModel` port and keeps the judgment that must not be the model's: which numbers exist, who a line comes from, what cannot be lost, what goes to the record. `cmd/aura` binds the port to the identity's LLM snapshot and frames the attacker text. No migration: `notified` is already on the verdict.

**Tech Stack:** Go, `internal/llm`, existing `pim_digest` handler and dispatcher.

**Spec:** `docs/superpowers/specs/2026-11-02-digest-written-by-the-model-design.md`

## Global Constraints

- Prompts in English, the digest in the identity's language.
- No tool on the call: `ToolChoice: "none"`, `Reasoning.Effort: none`.
- At most 100 items read (`maxDigestRead`), at most 20 lines shown (`maxDigestLines`).
- Sender and subject stay out of logs and of the ledger's summary, which holds counts only.
- ≤600 LOC a file; `text.go` is at 403.
- Go runs in WSL. `go vet`, build, test and race after every Go edit.
- Commit by pathspec on `master`. The peer's dirty files stay out.

## Review Focus

1. A subject telling the model to hide a mail or to name a sender: the sender comes from the PIM, and an untold `now` stands anyway.
2. A reply in a code fence, with a number that does not exist, or with no JSON: the list goes out, the window moves.
3. The model down or out of credit at the digest hour: the list goes out, on time.
4. A day made only of what was already told, or of mail since deleted: no call, nothing sent, the window moves.
5. More than 100 lines in the window, most of them told: told lines must not eat the budget of what is new.

## Decisions taken while writing this plan

- `sortDigest` ranks told verdicts last, so a day of alerts cannot push new mail past `maxDigestRead`. Only new verdicts beyond the budget are counted as unread.
- The model's text is cut at 160 runes (`writtenRunes`), the sender at `nameRunes`.
- A sender in a written line is its name alone, with the address kept in the record. The fixed list keeps name and address. The mark of a failed authentication is what warns about a forged name.
- `llm.Collect` extracts the stream loop that `GenerateTitle` and `Summarize` each carry: this would be the third copy.
- The model gets 3 minutes (`digestModelTimeout`), inside the handler's 5.

## File structure

| File | Change |
|---|---|
| `internal/pimwatch/digest.go` | window budget, told and gone leave, the model's turn |
| `internal/pimwatch/digest_written.go` | new: the port, the prompt, the items, reading the reply |
| `internal/pimwatch/text_written.go` | new: rendering a written digest and its record |
| `internal/pimwatch/text.go` | `Line.Told`, the new phrases |
| `internal/pimwatch/watcher.go` | `lineOf` carries `Told` |
| `internal/llm/collect.go` | new: `Collect` |
| `internal/conversations/title.go`, `compaction.go` | fold onto `llm.Collect` |
| `cmd/aura/serve_pimwatch.go` | new adapter `pimDigestModel`, wiring |
| `docs/superpowers/specs/2026-10-31-mail-calendar-watch-design.md` | §5.2, §5.3, §5.7 point here |

---

### Task 1: What was told and what is gone leave the digest

**Files:** `digest.go`, `text.go`, `watcher.go`, `digest_integration_test.go`, `digest_test.go`

- [ ] Test `TestWhatThePhoneAlreadySaidLeavesTheDigest`: one `now` marked notified and one `today` give a digest with the `today` alone, and `"2 in the window, 1 shown"`.
- [ ] Test `TestSortDigestPutsWhatWasToldLast` in `digest_test.go`.
- [ ] Test `TestToldLinesDoNotEatTheReadBudget`: 150 told and 10 new read every new one.
- [ ] Rewrite `TestADigestShowsAGoneMailAsGone` as `TestAGoneMailLeavesTheDigest`, and `TestADigestIsCappedAtTwentyLinesAndReadsNoMore` as `TestADigestShowsTwentyLinesAndReadsTheWindow`: the operator changed both behaviours; say so in the commit.
- [ ] Run them, red.
- [ ] Implement: `Line.Told`, set by `lineOf`; `maxDigestRead = 100`; `rank` puts `v.Notified` after everything; `Run` reads up to the budget, keeps `fresh := lines not Told and not Gone`, shows the first 20, and counts the rest plus the new verdicts past the budget.
- [ ] Run `go vet`, test, race and `t-db.sh` on `./internal/pimwatch/`, green. Commit.

### Task 2: Reading the model's answer

**Files:** new `digest_written.go`, `text_written.go`, their `_test.go`; `text.go`

**Produces:**
```go
type DigestModel interface {
    Write(ctx context.Context, identityID string, ask ModelAsk) (ModelReply, error)
}
type ModelAsk struct{ System, Items string }
type ModelReply struct {
    Text                           string
    PromptTokens, CompletionTokens int
}
func digestSystem(lang Lang) string
func digestItems(lines []Line, book Book, p phrases, loc *time.Location) string
func parseWritten(reply string, items int) ([]writtenLine, error)
func renderWritten(lang Lang, lines []Line, written []writtenLine, unseen int, missing unread) Outcome
```

- [ ] Test the items: numbered from 1, one per line, told marked, control characters gone, nothing longer than the bound.
- [ ] Test `parseWritten`: a plain reply, a reply in a code fence, and refusal on a number out of range, a section outside `do|look|know`, empty text, no items, no JSON. A number cited twice stands on its first line.
- [ ] Test `renderWritten`: the mock of the spec, in Italian and in English; sender from the line and never from the text; two senders; `+N` past two; an event with no sender; the mark of a failed authentication; an untold `now` the model left out stands in "Da guardare"; a told item it left out appears nowhere and is not counted; a new item it left out is counted once in "Altre N voci non chiedono nulla."; `unseen` in its own sentence; more than 20 lines cut, and what was cut counted; `Refs` holds every address of a merged line under its one number; `Head` holds no attacker text.
- [ ] Red, then implement. New phrases: `digestDo`, `digestLook`, `digestKnow`, `digestQuiet`.
- [ ] Green. Commit.

### Task 3: The digest asks the model

**Files:** `digest.go`, `digest_integration_test.go`

- [ ] Tests: `TestTheModelWritesTheDigest`; `TestAFailingModelSendsTheList`; `TestARefusedAnswerSendsTheList`; `TestNothingNewAsksNoModel`; the last three also prove the window moved. The summary reads `written by the model, N+M tokens` or `the list, the model failed`.
- [ ] Red, then add `Model DigestModel` to `Digester`. A nil one sends the list. A failure logs a fixed line, with no provider text in it.
- [ ] Green, with race. Commit.

### Task 4: The identity's model behind the port

**Files:** new `internal/llm/collect.go` and test; `title.go`, `compaction.go`; `cmd/aura/serve_pimwatch.go` and test

**Produces:** `func Collect(ctx context.Context, client Client, req Request) (text string, usage Usage, err error)`: drains the stream, wants a clean stop, keeps the last usage.

- [ ] Test `Collect`: text, usage, an error mid-stream, an incomplete stop. Fold `GenerateTitle` and `Summarize` onto it, their tests unchanged and green.
- [ ] Test `pimDigestModel`: identity resolved, no tools, no reasoning, items inside the untrusted frame, system outside it; a refused snapshot is an error.
- [ ] Implement and wire: `runtime.digester = &pimwatch.Digester{..., Model: pimDigestModel{...}}`, resolver from `identityLLMResolver(chat)`.
- [ ] `go vet ./...`, `go build ./...`, tests and race on the packages touched. Commit.

### Task 5: Live, on the VM

- [ ] Correct the watch spec §5.2, §5.3, §5.7, and commit with the plan and the spec.
- [ ] Push with the recipe, every workflow green on the SHA, the operator updates from the cockpit.
- [ ] Read the first real digest with the operator. Write `docs/superpowers/verification/2026-11-03-digest-written-live.md`: tokens from the ledger, lines in, lines out, what it hid, what it does not prove.
- [ ] Only then the PRD amendment, citing that file.
