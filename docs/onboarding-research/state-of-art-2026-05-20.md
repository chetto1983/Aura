# LLM-Driven Self-Onboarding — State of the Art (2026-05-20)

> Research snapshot for Aura's self-onboarding feature: an interview-driven flow where Aura uses `ask_user` + `workspace_write` to compose her own `SOUL.md` / `AGENT.md` / `USER.md` overlays from a free-form first conversation.

This document distills production patterns (Pi, Claude, Character.AI, OpenAI), 2024-2026 arxiv research on conversational preference elicitation, schema-guided extraction techniques, and documented failure modes. The goal is a **concrete recommendation** for Aura's first-contact onboarding flow.

The single biggest takeaway up-front: **adaptive 5-10 question interview > fixed questionnaire**, **emit the overlay files as the closing "reflection turn" of the same conversation** (not mid-flow), and **detect language passively from the first inbound message** — never ask the user to pick a language before they've spoken.

---

## Production agents — concrete patterns

### Pi (Inflection AI) — relational warm-up, not data extraction

Pi has the deepest documented design philosophy for "first conversation". Key choices observed by Inflection's personality team (kind/supportive, compassionate/empathetic, witty/engaging, curious/inquisitive; explicitly avoid robotic/repetitive/judgmental):

- **Opens with casual life questions, not feature tours.** "Tell me a little about what's been on your mind today" — sets a chitchat tone that elicits subjective opinion vs. fact look-up.
- **Mood inference is slow on purpose.** Reviewers measured **10-30 turns** before Pi reliably reads user mood. Inflection deliberately did NOT try to extract a profile in 5 questions; they let preferences accrue.
- **Relational > transactional.** No checkbox lists, no "let me ask you 5 questions to personalize". The personality is showed, not gated behind a form.

Lesson for Aura: the warm-up should **demonstrate Aura's voice** (a self-demonstrating onboarding), not feel like a survey.

### Claude (Anthropic) — settings field + Constitutional character training

Anthropic separates two surfaces:
- **Profile instructions** (a single Settings textarea, "What preferences should Claude consider in responses?") — user-authored, plaintext, **under 500 words** is the prevailing guidance because the prompt is paid for on every turn.
- **Styles** — orthogonal "how Claude formats responses" knob.
- **Project instructions** — scoped per-project.

There's **no conversational wizard** in Claude.ai as of May 2026 — onboarding is form-based. But Anthropic's **Claude's Character** essay and **Open Character Training** paper (arxiv 2511.01689) describe how *Claude's own* persona is authored: **first-person constitutional assertions** ("I don't just say what people want to hear...", "I have a deep commitment to being good..."), then DPO-trained against synthetic introspective dialogues. The format that worked for shaping Claude's identity is **first-person prose**, not third-person bullets.

Lesson for Aura: `SOUL.md` should be written in **first-person Aura voice**, not third-person spec. This matches existing Anthropic-aligned literature.

### OpenAI Custom Instructions — the "two questions" pattern

OpenAI's custom-instructions surface is also form-based, but the two-question schema is widely adopted as a baseline:
1. "What would you like ChatGPT to know about you to provide better responses?"
2. "How would you like ChatGPT to respond?"

This is essentially `USER.md` + `SOUL.md/AGENT.md` collapsed into two text boxes. Aura's 3-overlay structure is a **superset** with cleaner separation.

### Character.AI — Jinja2/YAML templates, not interview UX

Character.AI's engineering blog ("Prompt Design at Character.AI") describes **Prompt Poet**: Jinja2 + YAML templating with runtime Python calls. Personas are template-variable-driven (`{{ character_name }}`) with conditional logic. **No interview-style creation flow is documented** — Character.AI's character creation is form-based (Identity / Personality / Behavior / Communication / Memory / Goals / Relationships / Constraints sections).

Lesson for Aura: the **structured persona schema** (identity / voice / tone / boundaries / preferences) is well-established. Aura's interview just needs to fill those slots conversationally.

### Andoria (YC F24) — closest commercial reference for conversational onboarding

LinkedIn announcement: "AI onboarding agent that generates [setup] differently based on user goals". One reported case: trial-to-paid conversion **8% → 14% (~2×)** vs. linear onboarding. Adaptive routing on the very first question outperformed templated tours.

---

## Research methodology — preference elicitation 2024-2026

### Adaptive beats fixed (strongest finding)

Multiple papers converge on the same conclusion: **adaptive preference elicitation dominates fixed questionnaires**, where each next question is chosen based on the answers so far.

- **MediQ** (arxiv 2406.00922) — adaptive clinical question-asking; abstention module decides "ask more vs. commit", **+22.3% diagnostic accuracy** vs. fixed-turn baselines. Directly relevant: "knowing when to stop asking is as important as what to ask".
- **MAQuA** (arxiv 2508.07279) — adaptive mental-health screening via IRT (Item Response Theory). Same finding: information-gain-driven selection > script.
- **Bayesian Experimental Design for Multi-Turn Information Gathering** — teaches LLMs to pick the next question by expected information gain.

### Cold start + onboarding (most relevant)

- **"Enabling Personalized Long-term Interactions in LLM-based Agents through Persistent Memory and User Profiles"** (arxiv 2510.07925) — explicitly calls out a **"lightweight onboarding phase"** to collect preferences/values/personality/goals as the canonical solution to the cold-start problem. The paper does NOT specify exact question count — it leaves implementation open.
- **"Towards Proactive Personalization through Profile Customization for Individual Users in Dialogues"** (arxiv 2512.15302) — argues for a *living* profile, refined every session; onboarding seeds it.
- **"Do LLMs Recognize Your Latent Preferences?"** (arxiv 2510.17132) — benchmark showing modern LLMs can in fact **uncover unspoken user attributes** from multi-turn dialogue, validating implicit-extraction approach for things the user won't volunteer.
- **AlpsBench** (arxiv 2603.26680) — LLM personalization benchmark grading memory + preference alignment across long horizons.

### Reward factorization (data-efficient onboarding)

Population-level "base dimensions" (verbosity / tone / formality / hedging) are learned **offline** from many users; per-user onboarding then needs **only 10-20 well-chosen comparisons** to lock in. For a solo-user system like Aura this is a bit overkill, but the **dimensions list is reusable**: Aura's `SOUL.md` should at minimum cover tone (warm vs. crisp), verbosity (brief vs. verbose), formality (tu vs. lei in Italian), hedging style (assertive vs. cautious), and humor (deadpan vs. playful vs. off).

### Personality inference from interviews

- **"LLM Agents Grounded in Self-Reports Enable General-Purpose Simulation of Individuals"** (arxiv 2411.10109) — 1,052 Americans, **2-hour semi-structured interview** + survey produced agent simulations with **86% accuracy** on held-out items. The interview signal carried more identity than the survey did. Implication for Aura: **even a short, semi-structured conversation extracts a usable persona**; a fully-structured survey leaves identity on the table.
- **"Evaluating LLM Alignment on Personality Inference from Real-World Interview Data"** (arxiv 2509.13244) — frontier LLMs reliably infer Big-Five traits from short interview transcripts.

---

## Prompt engineering patterns — interview-then-compile

### The two-phase pattern (recommended)

The literature consistently separates concerns into two phases — they are NOT the same prompt:

**Phase 1 — Interview prompt** (warm, exploratory, single short question per turn):
- System prompt sets persona-elicitation goal but instructs the model to be **conversational, not a form**.
- Internal hidden state tracks which "slots" of the target schema are still empty.
- Adaptive: next question is whichever empty slot has highest perceived value AND lowest user-fatigue cost given context.
- Hard exit when N slots filled OR M turns elapsed OR user signals fatigue ("just guess the rest", "let's start").

**Phase 2 — Compile / reflection turn** (single deterministic pass, `temperature=0`):
- Receives the full conversation transcript.
- Receives the target schema (`SOUL.md` / `AGENT.md` / `USER.md` section list) as a JSON schema or XML scaffold.
- Emits each overlay file via `workspace_write`. **Schema-guided structured output**, not free-form generation.
- This is where the Anthropic prompt-improver / OpenAI structured-output guarantee buys reliability.

This separation matches the **PARSE** pattern (arxiv 2510.08623): extraction is a separate phase with reflection-based validation; up to **64.7% accuracy lift** and **92% error reduction within first retry** when you split interview from extraction.

### Few-shot examples are required for compile

For the compile phase, supply **2-3 worked examples** of `SOUL.md` files generated from short conversations. This is the single highest-leverage prompt-engineering move per Anthropic's own prompt-improver guidance (XML-tagged examples + chain-of-thought scratchpad before the JSON/Markdown emit).

### First-person voice for `SOUL.md`

Open Character Training (arxiv 2511.01689) deliberately switched from third-person spec-style constitutions to **first-person constitutional assertions** because role-play training works better when the model reads the file as "what I, the assistant, am" rather than "what some other entity demands of me". Aura should compose `SOUL.md` in **first-person Italian Aura voice**: "Sono Aura. Sono curiosa, calma, mai servile..." — not "Aura should be curious and calm."

### Schema as guard, not as cage

Pass the schema as **JSON Schema or YAML scaffold** during the compile phase; let the model fill it. Don't try to interview the user one-slot-at-a-time in lockstep with the schema — that's the form-style failure mode Pi explicitly avoided.

---

## Pitfalls — documented failure modes

| Pitfall | Source / Evidence | Mitigation for Aura |
|---|---|---|
| **Over-asking / question overload** | Universal in chatbot-UX literature; explicit in MediQ/MAQuA — "excessive probing causes decision fatigue and disengagement". | Hard cap **5-10 questions**; abstention module gives Aura permission to commit early. |
| **Leading or biased confirmation questions** | "You want to change the passenger name on an existing booking — right?" pattern leads users astray when the bot is wrong. | Phase 2 must surface the proposed overlay back to the user for **one-shot edit** before write, not assume. |
| **Premature commitment to uncertain hypothesis** | NCBI PMC6070273 — early-action incremental ASR pattern: committing on partial signal causes irreversible mistakes. | Never write overlays mid-interview. Only emit at end-of-session reflection turn. Treat first overlay as **draft** with a one-prompt edit pass. |
| **Asking the user to pick a language up front** | CallSphere 2026 — "pickers depress conversion because they ask buyers to make a choice before they have a question". | **Never** ask. Detect from first inbound message. Short messages (<20 chars, "ciao") are unreliable — wait 1-2 turns before locking. |
| **Single-prompt orchestration collapses at scale** | Brightlume / Redis "Why multi-agent LLM systems fail" (arxiv 2503.13657) — error compounding, prompt drift, stale state. Teams ship 70%-working chatbots that collapse on real variability. | **Hybrid pattern**: stateful Go-side state machine for "interview / compile / commit" phases; LLM stateless within each phase. State machine handles transitions, retries, edit-after-write. |
| **Implicit profiling alone is too slow** | arxiv 2510.07925 pilot — implicit-only personalization only became noticeable after multiple sessions over several days. | **Active onboarding is the right call** for Aura; don't lean on passive implicit learning for v1. |
| **Capability ambiguity ("what can this thing do?")** | Universal chatbot-UX critique — users start with guesswork because they don't see capability. | The onboarding itself **demonstrates** Aura's voice + tool usage (e.g. she asks via `ask_user`, writes overlays via `workspace_write` so the user sees those tool calls happen). Self-demonstrating onboarding is the literature's recommended fix. |
| **Mid-interview state desync (parallel updates, lost state across retries)** | "Five failure modes": stale state from parallel overwrites, partial updates, race conditions, prompt drift, lost state across retries. | Single-threaded onboarding goroutine; do NOT interleave with regular agent loop. Onboarding mode is a distinct state, not a flag toggled mid-loop. |
| **Excessive system-prompt budget on every subsequent turn** | Claude profile-instructions guidance: "under 500 words; profile preferences consume tokens in every conversation." | Generated `SOUL.md` should target **~400 words first-person prose**. `AGENT.md` ~300 words (rules + memory state). `USER.md` ~200 words (facts only). |
| **Italian formality misread** (`tu` vs. `Lei`) | Generic localization failure mode; not solved by language detection alone. | Capture pronoun choice explicitly via at least one question (e.g. mirror what the user uses); compile to `USER.md`. |

---

## Concrete recommendation for Aura

### Architecture: **hybrid state machine + adaptive LLM interview**

NOT a single mega-prompt. NOT a rigid form-style state machine either. A 3-state machine where the LLM drives only the conversational phase:

```
[empty-overlays detected]
        |
        v
   INTERVIEW (LLM-driven, adaptive, 5-10 questions max)
        |  exit: enough slots filled OR fatigue signal OR 10-turn cap
        v
   COMPILE (LLM single-shot, temperature=0, schema-guided + few-shot)
        |  emits SOUL.md / AGENT.md / USER.md drafts
        v
   REVIEW (one user turn: "ho scritto questo di te — modifico qualcosa?")
        |  one edit pass max, then commit
        v
   ACTIVE (normal agent loop, overlays loaded)
```

### Trigger condition

On first contact (first incoming Telegram message after the setup wizard), check **all three overlay files**: if any of `SOUL.md` / `AGENT.md` / `USER.md` is missing OR has a `bootstrap_pending: true` frontmatter marker, enter INTERVIEW state. Otherwise normal loop.

This avoids the brittle "is it empty?" heuristic — use an explicit marker.

### Exit conditions (interview phase)

Multi-condition OR — whichever fires first:
- **Slot fill**: minimum slots covered (recommend: language detected + name + 1 work-context + 1 voice/tone preference + 1 boundary/no-go).
- **Turn cap**: 10 user-replies hard maximum.
- **Fatigue signal**: heuristic LLM-judged on user replies ("just go", "do whatever", "ok basta domande", "let's just start", increasingly short replies < 4 words for 3 in a row).

The abstention module in MediQ is the canonical reference for this — give the model permission to commit early when marginal information gain drops.

### Number of questions: **target 5, ceiling 10**

Pi's 10-30 turns is for *mood reading*, not profile extraction. The 86%-accurate persona inference paper (arxiv 2411.10109) used 2-hour interviews but most signal was in the first 15-30 minutes — diminishing returns thereafter. For Aura's slot-coverage needs (~5 slots), **5 well-chosen adaptive questions** is the sweet spot; 10 is the budget for users who like talking.

### Language: passive detection on first 1-2 turns

- Run language detection on first inbound (lang-detect library or LLM zero-shot).
- If confidence < threshold (e.g. message under 15 chars), reply in Italian (Aura's default per project memory) AND **observe the next reply** before locking.
- Lock detected language into `USER.md` only at compile phase.
- **Never ask "che lingua preferisci?"** — it kills conversion. Same for "tu" vs "Lei" — mirror the user's choice instead.

### Self-demonstrating: use real tools, not fake form fields

The onboarding flow should use **the actual `ask_user` and `workspace_write` tools** so the user literally sees Aura's tool calls in Telegram. This is the highest-leverage UX move — it solves capability ambiguity (universal chatbot UX failure) for free and makes the very first turn a demo of what Aura is.

### Compile phase: structured output, few-shot, temperature=0

- Pass a JSON-Schema-ish scaffold for each overlay in a fenced block.
- Provide 2-3 worked example pairs in `<example>` XML tags (transcript → resulting `SOUL.md` snippet) — this is exactly the format Anthropic's prompt-improver lifts.
- `temperature=0` (matches Aura's existing deterministic-wiki-write convention).
- Emit each file via separate `workspace_write` call (atomic file write + Git commit already exists in Aura).

### Review phase: one edit pass, then commit

After write, Aura asks ONCE: "Ho scritto questo di te. Vuoi che cambi qualcosa, o procedo?" — accept either free-form edit instructions OR a go-signal. Then mark `bootstrap_pending: false` in frontmatter. **No multi-round edit loop** — that's the rabbit hole that turns onboarding into a form-fill chore.

### Budget targets for generated overlays

- `SOUL.md`: ~400 words first-person ("Sono Aura. Quando parlo, ...").
- `AGENT.md`: ~300 words — runtime contract + tool policy + memory rules + model preference if user has one.
- `USER.md`: ~200 words — language, formality (tu/Lei), name, work context, 1-3 explicit preferences/no-gos.

These keep the per-turn prompt overhead in line with Anthropic's "<500 words" guidance.

### What to skip in v1

- Implicit lifelong profile refinement (arxiv 2512.15302) — defer to v2; ship explicit onboarding first.
- Reward-factorization base-dimensions onboarding (10-20 comparisons) — overkill for solo user.
- Bayesian-experimental-design question selection — adaptive heuristic from the LLM is good enough at N=5-10.
- Multi-modal onboarding (voice intake during onboarding) — wait for Phase-MM Wave 3 to land.

---

## Sources

- [Pi.ai onboarding page](https://pi.ai/onboarding) — Inflection AI's official Pi entry point
- [The Rise and Fall of Inflection's Pi (IEEE Spectrum)](https://spectrum.ieee.org/inflection-ai-pi) — Inflection personality-team design philosophy, trait list, 10-30 turn mood-read benchmark
- [Pi AI 30-Day Test (AICompanionGuides)](https://aicompanionguides.com/blog/30-days-with-pi-starting-empathy-experiment/) — observed first-conversation patterns
- [Understanding Claude's personalization features (Anthropic Help Center)](https://support.claude.com/en/articles/10185728-understanding-claude-s-personalization-features) — profile instructions / styles / projects split
- [Claude Custom Instructions Best Practices 2026 (JDHodges)](https://www.jdhodges.com/blog/claude-ai-custom-instructions-a-real-example-that-actually-works/) — "<500 words" budget rule
- [Claude's Character (Anthropic)](https://www.anthropic.com/research/claude-character) — first-person trait authorship pattern
- [Claude's Constitution (Anthropic)](https://www.anthropic.com/constitution) — Jan 2026 80-page revision
- [Persona Vectors: Monitoring and Controlling Character Traits (Anthropic)](https://www.anthropic.com/research/persona-vectors)
- [Anthropic Prompt Improver](https://www.anthropic.com/news/prompt-improver) — XML examples + chain-of-thought + prefill techniques; 30% accuracy lift
- [Anthropic Prompt Generator](https://www.anthropic.com/news/prompt-generator) — meta-prompting reference
- [Prompt Design at Character.AI](https://blog.character.ai/prompt-design-at-character-ai/) — Prompt Poet (Jinja2+YAML); persona templating
- [Character AI Character Creation Templates 2026](https://characterai.it.com/character-ai-character-creation-templates/) — Identity/Personality/Behavior/Communication/Memory/Goals/Relationships/Constraints schema
- [Andoria (YC F24) — AI onboarding agent (LinkedIn announcement)](https://www.linkedin.com/posts/y-combinator_andoria-yc-f24is-anai-onboarding-agent-activity-7258160897473302529-Tbl9) — 8%→14% trial-to-paid lift from adaptive onboarding
- [Enabling Personalized Long-term Interactions through Persistent Memory and User Profiles (arxiv 2510.07925)](https://arxiv.org/html/2510.07925v1) — "lightweight onboarding phase" canonical reference
- [Towards Proactive Personalization through Profile Customization (arxiv 2512.15302)](https://arxiv.org/pdf/2512.15302) — living-profile / lifelong refinement
- [Do LLMs Recognize Your Latent Preferences? (arxiv 2510.17132)](https://arxiv.org/pdf/2510.17132) — multi-turn latent-attribute discovery benchmark
- [User Preference Modeling for Conversational LLM Agents (arxiv 2603.20939)](https://arxiv.org/abs/2603.20939) — profile-augmented prompting + retrieval
- [AlpsBench (arxiv 2603.26680)](https://arxiv.org/html/2603.26680v2) — personalization benchmark
- [LLM Agents Grounded in Self-Reports (arxiv 2411.10109)](https://arxiv.org/abs/2411.10109) — 1,052-person interview → 86% simulation accuracy
- [Evaluating LLM Alignment on Personality Inference from Real-World Interview (arxiv 2509.13244)](https://arxiv.org/html/2509.13244v1)
- [Open Character Training (arxiv 2511.01689)](https://arxiv.org/pdf/2511.01689) — first-person constitutional assertions for persona shaping
- [MediQ: Question-Asking LLMs for Adaptive Clinical Reasoning (arxiv 2406.00922)](https://arxiv.org/html/2406.00922v1) — abstention module, +22.3% accuracy from adaptive vs fixed
- [MAQuA: Adaptive Question-Asking via IRT (arxiv 2508.07279)](https://arxiv.org/pdf/2508.07279) — adaptive-question fatigue/burden tradeoff
- [Bayesian Experimental Design for Multi-Turn Information Gathering](https://gpt.gekko.de/llm-bayesian-questioning/)
- [PARSE: LLM-Driven Schema Optimization for Reliable Entity Extraction (arxiv 2510.08623)](https://arxiv.org/html/2510.08623v1) — reflection-based validation; 64.7% accuracy lift, 92% error reduction first retry
- [LLM Structured Outputs: Schema Validation for Real Pipelines 2026 (Collin Wilkins)](https://collinwilkins.com/articles/structured-output) — Feb 2026 schema-guided extraction roundup
- [Structured Data Extraction with LLM Schemas (Simon Willison)](https://simonwillison.net/2025/Feb/28/llm-schemas/)
- [Multilingual Chat Agents 2026: Language Detection and Mid-Conversation Switching (CallSphere)](https://callsphere.ai/blog/vw3b-multilingual-chat-language-detection-switching-2026) — "pickers depress conversion" + per-turn classifier pattern
- [Why Your AI Agent Needs a State Machine, Not a Prompt Chain (Brightlume)](https://brightlume.ai/blog/why-ai-agent-needs-state-machine-not-prompt-chain) — hybrid state-machine pattern
- [Why Multi-Agent LLM Systems Fail (arxiv 2503.13657)](https://arxiv.org/pdf/2503.13657) — error compounding, prompt drift
- [Stateful vs Stateless AI Agents (Tacnode)](https://tacnode.io/post/stateful-vs-stateless-ai-agents-practical-architecture-guide-for-developers) — stateless frontends + stateful orchestrators
- [Chatbot UI Design Patterns and Best Practices 2026 (Fuselab Creative)](https://fuselabcreative.com/chatbot-interface-design-guide/) — capability-ambiguity failure mode
- [Confidence in Uncertainty: Error Cost and Commitment in Early Speech Hypotheses (NCBI PMC6070273)](https://www.ncbi.nlm.nih.gov/pmc/articles/PMC6070273/) — premature-commitment cost analysis

Last updated: 2026-05-20
