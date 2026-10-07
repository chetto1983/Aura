# Turn recall — frozen evaluation protocol

What it measures: whether reusing remembered effort labels (plan 2 of the turn recall spec,
`docs/superpowers/specs/2026-10-06-turn-recall-design.md`) changes the effort decision for the
worse, and what it saves. It measures the effort only; the tool preload is measured end to
end on the lab VM (plan 2, Task 8).

## The set

`internal/agent/testdata/turn_recall_frozen_2026-10-07.json`, frozen on 2026-10-07: 51 user
turns in 46 conversations, Italian and English, split into calibration (22 turns) and final
(29 turns, 13 hard). Each turn lists the efforts the rubric accepts; a hard turn accepts only
`high`. Each family sits entirely in one split, so no calibration label can answer a final
turn. No turn copies the prompt package (`TestFrozenEvalSetDoesNotCopyTheSeedBank`).

Dataset SHA-256: `3080ea85af6ec6e6af1cbdf6ec345bd45fc624a72997c9c3e77071757579f6ee`

`TestFrozenEvalSetMatchesItsProtocol` fails if the set changes without this line. Changing the
set means a new file with a new date and a new entry below, never an edit in place.

## Arms

Every turn is read three ways, in conversation order, on the production path (`readTurn`):

1. **seeds** — the seed bank's verdict alone, whatever its margin.
2. **seeds + teacher** — the production decision with memory empty.
3. **recall** — the production decision with the identity's memory: a fresh ArcadeDB database
   per trial, into which every earlier turn of the trial was projected with the decision the
   recall arm made, exactly as the runner persists it.

Since the amendment of 2026-10-07 the teacher answers in the background and never decides the
turn it is asked about. A turn's reading asks for it (`AskTeacher`) when its seed margin is below
`teacherMargin`. The replay then asks `AskTeacher` once, on the eval's client and model,
bounded by the runner's default teacher timeout (30 s), whenever either arm asked. One answer
serves both arms, which read the same text with the same classifier.

- In time order, a successful answer becomes the recall arm's label for that turn before the
  next turn is read. Its source is `teacher`, its requested effort is the tier's effort before
  the clamp (as migration 0137 defines the column; a reuse clamps it again), and its applied
  effort is the one the turn sent. A failed answer leaves the seeds decision as it was, as on a
  real row.
- What this does not show: the replay writes a teacher label before the next turn is read, but
  production reaches memory only at the next reconcile tick (about a minute). Recall-arm memory
  hits are therefore an upper bound for follow-ups sent sooner than that.
- The **seeds + teacher** arm therefore measures two things: the turn's own seeds decision, and
  how often the background teacher would have labelled it.

The context key of a turn is `TurnContextKey` of its conversation's earlier turns (each
answered by a placeholder assistant message); a turn with an attachment has none, and a turn
with no earlier turn is standalone.

## Metrics

Per split and arm: accuracy (decided effort in the accepted set) with its 95% Wilson interval;
hard→none count; the decided-effort × accepted-set table. For the recall arm also: decision
sources, memory precision (memory decisions in the accepted set), recall latency p50/p95, and
whole-reading latency p50/p95. For the background teacher: the readings on which each arm asked,
how many the seeds + teacher arm would have had labelled, how many labels the recall arm
learned, the outcomes, and the teacher's latency p50/p95. Each reading's row records its
teacher outcome.

## Release criterion

On the final split, in every trial: no hard turn that the recall arm decided from memory as
`none` unless the seeds + teacher arm also decided it `none`. Memory may not add a hard→none.
Everything else is reported, not gated.

## Running

Paid: every uncertain turn asks the teacher once. Run only with the operator's OK.
Calibration may be run as often as needed. The final split runs once per frozen set and
constants; a changed constant (`teacherMargin`, `recallRadius`) needs a fresh calibration run
before the final.

    W 'source scripts/lib/disposable_stack.sh && export ARCADEDB_URL=http://127.0.0.1:2480 \
       ARCADEDB_PASSWORD="$(read_secret ARCADEDB_PASSWORD)" AURA_EMBED_BASE_URL=http://127.0.0.1:8081 \
       TURN_EVAL_LLM_PROVIDER=openrouter TURN_EVAL_LLM_BASE_URL=https://openrouter.ai/api/v1 \
       TURN_EVAL_LLM_MODEL=<the production primary model> TURN_EVAL_LLM_API_KEY="$(read_secret OPENROUTER_API_KEY)" \
       TURN_EVAL_SPLIT=calibration TURN_EVAL_TRIALS=3 TURN_EVAL_REPORT=/mnt/d/Aura/docs/verification/turn-recall-eval-<date>-<split>.md && \
       go test -tags turn_recall_eval -count=1 -timeout 60m -run TestTurnRecallFrozenEval ./internal/agent/'

## Results

| Date | Split | Model | Trials | Report | Criterion |
|---|---|---|---|---|---|
| 2026-10-07 | calibration | gemma4:31b-cloud (Ollama on the operator's workstation, bridged to WSL through a throwaway socat container) | 3 | `turn-recall-eval-2026-10-07-calibration.md` | not gated (calibration). All three arms scored 60/66 (Wilson 0.816–0.958) with 0 hard→none. Memory decided 3 of 66 readings, all in the translate family, and all 3 were correct. The teacher answered 24/24 (p50 0.40 s, p95 2.2 s). Recall latency was p50 30 ms, p95 56 ms. A 1-trial probe run earlier the same day gave the same picture: 20/22, memory 1/22. |
| 2026-10-07 | final | — | — | not run | **Postponed.** In calibration, memory fires once per trial. The final split has fewer same-language paraphrase pairs with uncertain seeds, so the memory-never-ran gate would most likely fail for lack of opportunity rather than a defect, and that would spend the set's single final run. Fixing this needs a revised, newly dated set with more same-language uncertain pairs. The set is never edited in place. |
