# paper/ — Fast Lookup Pattern Study (2026-05-21)

Source: `D:/tmp/paper.md` (no `paper/` directory exists; only the rendered PDF→Markdown of the paper plus its JSON request/response, ~113 KB).

## TL;DR

`D:/tmp/paper.md` is the **Kimi K2.5 Technical Report** (Moonshot AI, Feb 2026) — a model paper, not a runnable system or codebase. Its "Agent Swarm" contribution is a **horizontal latency reducer** (3-4.5× via parallel sub-agent decomposition of *complex wide-search* tasks), measured against ~100-step orchestrator/sub-agent budgets — i.e. the opposite end of the latency curve from Aura's "11-42s single-word lookup" problem. **For Aura's ≤4s fast-lookup target, this paper is largely irrelevant** — it has no fast-path / no-tool-needed / classifier-before-tools pattern. The one transferable idea is the **Toggle / budget-control reward** (paper §4.4, lines 683-727) which trains the model to *answer within a problem-dependent token budget* and cut 25-30% of redundant CoT.

## What the paper actually optimizes (latency-wise)

1. **Critical Steps as Resource Constraint** (paper §3, lines 381-417). An RL reward that defines an episode's "duration" as the longest path through the parallel-execution DAG — not total work. The orchestrator is trained to minimize end-to-end critical-step count, with anti-reward-hacking guards against (a) "serial collapse" (always single-agent) and (b) "fake parallelism" (spawning useless subagents). **All assumes ≥10-step problems** — wide-search benchmarks with 15-100 step orchestrator budgets (lines 2641-2648).
2. **Agent Swarm** (lines 97-118, 1456-1505). Dynamic decomposition: main agent either invokes a tool directly OR instantiates a parallel cohort of sub-agents with isolated contexts; only summaries return to the orchestrator. Yields **3-4.5× wall-clock reduction** on WideSearch — *but* baseline is single-agent at ~100 sequential tool calls. Aura's 4-12 tool-call simple lookups are below the floor where this matters.
3. **Toggle / budget-control reward** (§4.4, lines 683-727). The transferable bit. Two alternating RL phases:
   - **Phase0:** model penalized for exceeding `budget(x) = ρ-th percentile of token lengths among correct rollouts` — *but only when mean accuracy already exceeds threshold λ* (so it doesn't trade quality for speed prematurely).
   - **Phase1:** standard unconstrained scaling.
   - Result: 25-30% fewer output tokens with negligible accuracy loss; "redundant patterns in the chain-of-thought, such as repeated verifications and mechanical calculations, decrease substantially" (line 723-725).
4. **Step caps per benchmark** (lines 2641-2648): BrowseComp orchestrator=15 + sub-agents=100, WideSearch=100+100, In-house=100+50. These are *upper* bounds — there's no lower-side classifier ("if simple, answer without tools").
5. **System prompt** (lines 2650-2671) is a stock "you are professional, use tools efficiently" — explicitly *encourages* parallel search calls ("supporting multiple queries in parallel") but contains zero "skip tools if you know the answer" or "answer directly when query is trivial" guidance.

## What the paper does NOT have

- No query classifier / intent router.
- No KV/result cache architecture (only training-time KV optimizations — irrelevant to runtime latency).
- No streaming/early-exit pattern.
- No "direct answer if confident" mode.
- No tool-dispatch-budget at runtime (only RL-training-time critical-step rewards).
- No discussion of single-word / sub-second lookup latency anywhere in 2700+ lines.

## Patterns Aura could (partially) adopt

The paper is wrong-shaped for "kill the 11-42s simple-lookup tail", but two ideas are useful at the margins:

1. **Toggle-style "budget(x)" as a runtime prompt-overlay rule**, not RL training. Inject into AGENT.md: *"If the most likely answer is shorter than 200 tokens AND requires fewer than 2 distinct facts, answer directly without calling any tool. Tool-call budget for definitional/lookup queries: 1."* This mirrors paper §4.4's "task-dependent budget" but at prompt level, not weight level. Cheaper than fine-tuning, addresses the *exact* failure mode (4-12 calls for a word lookup).
2. **Critical-step framing for retries.** If Aura *does* call tools on a simple lookup, kill after the 1st result is sufficient (paper's "critical step = longest path through DAG", lines 388-411). Aura's loop today seems to keep iterating beyond information sufficiency — instrument a stop condition keyed on "answer is grounded by one citation, no contradiction in retrieval".
3. **Parallel tool dispatch only above complexity threshold.** Paper's `r_parallel` (line 362) penalizes "serial collapse" *and* "fake parallelism" symmetrically. Aura should resist the temptation to fan out for simple queries — paper explicitly observes (lines 1465-1466) "Agent Swarm... maintains near-constant low latency in the range of 0.6×∼1.6×" when problems are below the parallelization threshold. Translation: parallel sub-agents add overhead on small tasks, don't always-parallelize.

## Hard truth

**This paper is the wrong reference for Aura's fast-lookup problem.**

- Paper's frame: "how do we make 100-step research tasks finish in 30 critical steps instead of 100".
- Aura's frame: "how do we make a 1-fact lookup that needs 0-1 tool calls actually stop at 0-1 tool calls and stream the answer in <4s".

They overlap only in the abstract observation that "token/step budgets, conditionally enforced, reduce waste without hurting quality" (Toggle, §4.4). The Agent Swarm / Parallel-Agent RL / Critical Steps machinery — which is 90% of the paper's latency content — is *anti-useful* for Aura's current bottleneck because it assumes complex multi-hop tasks where parallelism dominates the constant overhead per LLM call.

**For Aura's ≤4s target, look elsewhere:** local-cache hit path (lookup → vector-DB single shot → stream), prompt-overlay rule "no tools needed for definitional/proper-noun/conversational queries", model warm-up + KV cache reuse across turns, and reducing per-tool-call overhead (which the in-tree `cli-printing-press-output-discipline` and `picobot-output-discipline` studies in this same folder address more directly). The Kimi K2.5 paper is a useful *training* reference for a different problem class — building it into Aura's runtime architecture would be a category error.

---

Word count: ~890.
