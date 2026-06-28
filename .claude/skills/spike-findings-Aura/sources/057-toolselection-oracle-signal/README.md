---
spike: 057
name: toolselection-oracle-signal
type: standard
validates: "Given real/synthetic turn traces (incl. the bitcoin skill->fs_glob->shell_exec chain), when candidate 'should-have-used tool' oracles are tested (shell-fallback heuristic, used-tool != ranker-top-1, low margin) and the step-2 semantic ranker is used as the teacher, then we learn whether a cheap reliable teacher exists for the step-3 active-learning loop"
verdict: PARTIAL
related: [053-reasoning-classifier-active-learning, 054-semantic-toolsearch-vs-bm25, 056-hybrid-fusion-vs-pure]
tags: [tool-selection, active-learning, oracle, self-improvement, slice-tooling]
---

# Spike 057: The teacher for the step-3 tool-selection loop

## What This Validates

Step 3 of the roadmap = a tool-selection self-improvement loop, mirroring the reasoning-tier
learner. Spike 053 already validated the **learner** (async centroid-refresh of oracle-labeled
examples, +7pts, content-hash dedup, seeds authoritative). The only unproven piece of step 3 is
the **oracle/teacher**: given a turn, (a) can we cheaply DETECT it was mis-routed, and (b) can we
cheaply LABEL the tool that should have been used? The elegant candidate: the step-2 semantic
ranker (spikes 054/056) applied to the original request — free, local, deterministic. idea-2
feeds idea-3.

## How to Run

```bash
export OPENROUTER_API_KEY=dummy-for-config-load
go run ./.planning/spikes/057-toolselection-oracle-signal
```

12 synthetic-but-grounded traces (7 mis-routes incl. the documented bitcoin chain + 5 efficient
turns), each `{request, tool-actually-used, gold-tool}`. The free oracle is the live granite
ranker over the 53-tool corpus. No paid LLM call (the stronger-oracle tier is designed, not run
— `feedback_no_unsolicited_paid_runs_batch_calls`).

## Results

**PARTIAL — detection is solved cheaply and reliably; the free semantic-ranker teacher is
self-limited (it can only confirm what it already knows), so a robust step-3 loop needs the
two-tier oracle spike 053 already validated: free ranker for the confident majority + an LLM
escalation for the low-confidence tail.**

### Detection — cheap and reliable

| Signal | Precision | Recall | notes |
|---|---|---|---|
| used-tool ≠ ranker top-1 | 0.88 | 1.00 | catches all mis-routes |
| **shell/fs fallback used** | **0.88** | **1.00** | best single signal — no embedding needed |
| low margin (<0.02) | 0.60 | 0.86 | noisy alone (narrow cosine band) — use as the *gate*, not the flag |
| **UNION (fallback OR disagree)** | **0.88** | **1.00** | R=1.0 with one benign FP |

Recall 1.00: every mis-route is caught. The single false positive is the *efficient* "esegui
script python" → shell_exec turn (flagged because shell_exec is a fallback tool and the ranker
disagreed). That FP is harmless — a flagged-but-efficient turn just gets an oracle check that
confirms it. **The shell/fs-fallback heuristic alone (P=0.88, R=1.0) is a zero-cost detector
that needs no embedding** — it directly encodes "the model improvised instead of using a
dedicated tool", the exact bitcoin failure mode.

### Labeling — the free oracle is a self-limited teacher (the ceiling)

The free semantic-ranker oracle labels **4/7 mis-routes correctly** (meteo, news, preference,
web_fetch — where the ranker is already right). For the other **3/7 the ranker is itself wrong**
and would teach the WRONG label:

- "quanto costa il bitcoin adesso?" → oracle says `mail__delete_mailbox` (the gravity well)
- "cerca nei miei documenti…" → oracle says `web_fetch` (not document_search)
- "manda una mail a Marco…" → oracle says `mail__mark_email` (intra-cluster confusion)

These are **exactly** the hard misses spike 054 catalogued (verbose-conversational queries +
dense-namespace clusters). The implication is sharp: **the free ranker cannot self-correct its
own systematic errors** — using it as the sole teacher would reinforce them (a feedback loop
that amplifies the gravity well). It is a good teacher only for the cases it already gets right,
which is precisely where teaching adds least.

### The design conclusion: two-tier oracle (= spike 053's validated shape)

The robust step-3 teacher is therefore:

1. **Detect** mis-routes cheaply on every turn: shell/fs-fallback used, or used-tool ≠ ranker
   top-1. Zero/near-zero cost, R=1.0.
2. **Label the confident cases for free** with the ranker top-1 when its top-2 margin is high
   (the ranker is sure and usually right).
3. **Escalate the low-margin / disagreement tail to a stronger oracle** — the local Gemma-E2B
   or DeepSeek router that spike 053 already proved as a labeler (~3% noise, tolerated by
   centroid averaging). This is the ONLY tier that can fix the ranker's systematic errors,
   because it has independent signal. Async, post-turn, margin-gated, bounded — never on the hot
   path.
4. **Fold** confirmed `(request-embedding → correct-tool)` pairs into the per-tool bank;
   refresh centroids (spike 053's mechanism, not kNN); content-hash dedup; curated anchors stay
   authoritative.

## Investigation Trail

- First run logged the efficient whatsapp turn as a false positive: the disagreement check
  compared the bare `send_message` against the namespaced `whatsapp__send_message` with an
  order-sensitive suffix match → spurious mismatch. Made tool identity symmetric (`sameTool`);
  detection precision rose 0.78→0.88 and the artifact disappeared. (No-skip-as-green: a wrong
  metric is worse than no metric.)
- low-margin as a standalone flag is noisy (P=0.60) because the cosine band is narrow
  everywhere (spike 054); it is the right *gate* for "should we spend an oracle call" but the
  wrong *flag* for "was this a mis-route".

## Signal for the Build

- **Build the step-3 loop, but with a two-tier oracle, not a free-only self-bootstrap.** The
  free ranker bootstraps the easy majority; the LLM tier (spike 053) is mandatory for the hard
  ~40% where the ranker is itself wrong — otherwise the loop amplifies its own gravity-well bias.
- **Ship the shell/fs-fallback detector first** — it is a zero-cost, R=1.0 inefficiency flag
  that needs no embeddings and directly targets the documented failure (improvise-instead-of-
  dedicated-tool). It is also useful on its own as an eval signal.
- **Gate the (paid) LLM oracle on low margin** (spike 052/053): only the uncertain turns cost a
  call; as the bank grows, fewer turns are uncertain → self-limiting cost (spike 053 economics).
- This closes the roadmap loop: step-2's ranker is step-3's free teacher AND step-3's escalation
  improves step-2 — but the escalation tier is what makes it converge instead of echo.
