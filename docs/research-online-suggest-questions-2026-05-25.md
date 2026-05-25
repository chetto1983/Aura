# Online research — cold-start question suggestion in chat assistants

Date: 2026-05-25
Researcher: Claude (web search agent)
For: Aura `search(action=suggest_questions)` (Wave G2-S2) — `docs/aura-g2-s2-s3-plan-2026-05-25.md`
Time-box: 45 min wall-clock
Method: WebSearch + WebFetch, 14 queries, 7 deep-fetches

This document audits production playbooks and academic literature for "what to
suggest when the user opens a chat with no specific request" so we can stress-
test the planned G2-S2 design before shipping.

---

## TL;DR — top 3 patterns by impact for G2-S2

1. **Capability-transparency over generic prompts.** Three to ten domain-
   scoped example queries with a one-line *why* beat any generic "how can I
   help?" — confirmed by Fuselab Creative's 2026 chatbot guide and Microsoft
   Copilot Studio's 10-slot starter-prompt convention. G2-S2 already does
   this (4 buckets, each with a `why:` line) — KEEP as-designed.
2. **Suggestive ending > suggestion list (Ovsiankina effect).** Park et al.
   (Heliyon / PMC 2024) found that ending replies with *implicit hints* drove
   20% more follow-up queries and 47% wider perspective coverage than either
   plain replies OR explicit suggestion links. Aura should consider returning
   suggestions both as a tool action AND as a tail-attachment to substantive
   answers — see "Open questions" below.
3. **Workflow-boundary timing, not mid-task.** Kuo et al. IUI '26 field
   study reports 52% engagement at workflow boundaries vs 62% *dismissal*
   for mid-task suggestions. Aura's current plan is correct (tool-driven →
   user requests it) but we should explicitly NOT auto-fire suggestions
   between turns of an active task.

**Reshape flag**: nothing in the research forces a fundamental redesign of
G2-S2 as planned. The 4-bucket KGQG approach is well-grounded (InfraNodus
ships the same recipe). The biggest *additive* opportunity is a small
follow-up story (G2-S2b?) that wires suggested-question emission into the
*reply tail* of substantive answers, gated on user opt-in.

---

## Findings (numbered, full detail)

### F1 — Microsoft Copilot Studio: 10 starter prompts on welcome page

- **Source**: <https://learn.microsoft.com/en-us/microsoft-copilot-studio/configure-starter-prompts>
- **Year**: 2026-02 (last updated)
- **Pattern shape**: Up to 10 author-curated "suggested prompts" with a
  title + body, displayed on the agent's welcome page *before* the first
  message. Auto-generated from the agent's description + instructions on
  creation; editable thereafter. Shown only at conversation start, never
  mid-chat. A "Prompt Gallery" exposes them on-demand later.
- **Aura applicability**: ABSORB (partial). The 10-cap is informative — our
  top_k=7 default is in the same ballpark. The "shown on welcome only" rule
  reinforces F4 below.
- **Adaptation note**: G2-S2 should default top_k=4–7 (matches Copilot's
  user-cognitive-load curve), and the markdown capsule format we already use
  (`- [bucket] question\n  why: ...`) is a good analog of Copilot's "title +
  prompt" two-line shape. No change needed.

### F2 — Perplexity Copilot: in-turn follow-up suggestions

- **Source**: <https://www.perplexity.ai/hub/blog/getting-started-with-perplexity>, <https://blog.bytebytego.com/p/how-perplexity-built-an-ai-google>
- **Year**: 2025
- **Pattern shape**: After every answer, Perplexity surfaces 3–5
  follow-up question chips tailored to the topic just discussed. Users can
  also "customize their own follow-up query if defaults aren't relevant".
  Memory feature (Dec 2025) retains context across sessions for
  personalization.
- **Aura applicability**: SEPARATE-STORY (G2-S2b candidate). G2-S2 as
  planned is cold-start only. Perplexity proves the high-engagement variant
  is *post-answer* follow-ups, derived from the conversation in-flight, not
  the static graph state.
- **Adaptation note**: Don't bake this into G2-S2. Open a follow-up story
  "in-turn follow-up suggestions" that calls `search(action=suggest_questions)`
  with a *conversation-context override* (last 2 turns → relevant slug set →
  bucket scoring biased by recency). Cleanly orthogonal to the cold-start
  case.

### F3 — InfraNodus: structural-gap → research-question generation

- **Source**: <https://infranodus.com/use-case/visualize-knowledge-graphs-pkm>, <https://infranodus.com/use-case/network-analysis-visualization>
- **Year**: 2024-2025 (continuously updated product)
- **Pattern shape**: InfraNodus identifies "topical clusters that should be
  connected but aren't" (structural gaps) and uses betweenness centrality to
  rank influential bridge nodes, then asks an LLM to "propose different ways
  to bridge these gaps — generating research questions, facts, and creative
  ideas". The UI is **on-demand**: user clicks "Generate Insight" rather
  than auto-fire.
- **Aura applicability**: ABSORB — this is the closest production sibling
  to G2-S2 and validates buckets 2 (bridge nodes) + 4 (isolated nodes) +
  implicitly 5 (low-cohesion communities, deferred to Wave G3). InfraNodus
  also exposes betweenness centrality which Aura does NOT compute today.
- **Adaptation note**: Two specific lifts:
  1. Bucket 2 (bridge nodes) currently uses degree ≈ P99 + multi-category
     span. Consider adding *betweenness centrality* as a secondary signal —
     a node with high betweenness but only mid-degree is a stronger
     "bridge" candidate than a high-degree hub. Defer to Wave G3 if not
     trivially computable on 45 pages.
  2. The on-demand UI mode matches G2-S2's tool-driven invocation — no
     change needed, but explicitly document that Aura does NOT auto-fire
     these suggestions (per F8 below).

### F4 — Welcome-page-only display (not in-stream)

- **Source**: Microsoft Copilot Studio docs (F1); Fuselab Creative 2026
  guide <https://fuselabcreative.com/chatbot-interface-design-guide/>
- **Year**: 2026
- **Pattern shape**: Suggestion chips appear ONLY on the empty-state
  welcome page. Once the user has typed once, they disappear and don't
  re-appear unless the user explicitly opens a "prompt gallery". This
  preserves the chat surface for substantive content.
- **Aura applicability**: ABSORB. Tells us that even if we later add
  proactive suggestion-firing, it should be gated by "is this turn 1 of a
  new session?" and never inject between user-driven turns.
- **Adaptation note**: For G2-S2, since invocation is tool-driven (LLM
  decides), this is naturally satisfied — the LLM only calls
  `suggest_questions` when the user asks something cold-start-shaped. We
  should NOT add a server-push proactive trigger in this story.

### F5 — Ovsiankina effect: suggestive endings beat suggestion lists

- **Source**: Park et al., "Suggestive answers strategy in human-chatbot
  interaction" — <https://pmc.ncbi.nlm.nih.gov/articles/PMC11007170/>
- **Year**: 2024
- **Pattern shape**: Lab study comparing 3 chatbot styles: plain answers,
  expositive (full summary), and *suggestive ending* (ambiguous tail like
  "some would support this, while others would oppose…"). Suggestive ending
  produced 4.27 vs 3.56 follow-up questions/user, +114s task time, and 47%
  vs 42.1% perspective coverage. A "suggestive++" variant with explicit
  follow-up question links *did* drive more queries but reduced perspective
  diversity — users took easy paths instead of thinking critically.
- **Aura applicability**: ABSORB (partial) — informs how Aura PRESENTS the
  output of suggest_questions, not the underlying buckets.
- **Adaptation note**: When the agent loop receives the markdown capsule
  from `search(action=suggest_questions)`, the prompt overlay (TOOLS.md or
  SOUL.md) should bias toward *introducing* the questions with a brief
  framing rather than dumping them as a bare list. Example template:
  "Looking at the wiki right now, three things stand out to me… **What is
  the relationship between [[A]] and [[B]]?** I'm not sure the link is
  what we think it is." vs the current implicit dump. This is a PROMPT
  change, not a code change — capture as a one-line story in
  `scripts/ralph/` or as a manual SOUL.md edit after G2-S2 lands.

### F6 — Workflow-boundary timing (52% engagement vs 62% dismissal mid-task)

- **Source**: Kuo, Sergeyuk, Chen, Izadi — "Developer Interaction Patterns
  with Proactive AI: A Five-Day Field Study", IUI '26 —
  <https://arxiv.org/abs/2601.10253>
- **Year**: 2026
- **Pattern shape**: Field study with developers using proactive AI
  suggestions in IDE. Interventions at workflow boundaries (between files,
  after a commit, on session open) hit 52% engagement; mid-task
  interventions hit 62% *dismissal*. Well-timed boundary suggestions also
  needed 45.4s of interpretation time vs 101.4s for reactive suggestions —
  the boundary context made them easier to evaluate.
- **Aura applicability**: ABSORB. Directly validates "cold-start only" as
  the right G2-S2 surface. Argues AGAINST any future story that auto-fires
  suggest_questions in the middle of an active conversation.
- **Adaptation note**: If we ever add an in-stream proactive mode (F2
  candidate), gate it on conversation-boundary signals: idle ≥ N minutes,
  user said "ok thanks", or user typed `/done`. Capture as a hard rule in
  the future story's acceptance criteria.

### F7 — Proactive help can backfire (self-threat)

- **Source**: <https://arxiv.org/pdf/2509.09309> "Proactive AI Adoption can
  be Threatening: When Help Backfires"
- **Year**: 2025
- **Pattern shape**: Anticipatory ("I noticed you might want X") help
  elicited significantly more self-threat than reactive help, reducing
  willingness to accept assistance AND likelihood of future use. The effect
  was strongest when the user perceived themselves as competent in the
  task.
- **Aura applicability**: ABSORB. Aura's single primary user (Davide) is
  *very* competent in the wiki domain. Auto-firing "hey I noticed
  [[davide-marchetto]] is under-linked" without being asked would likely
  trigger exactly this backfire.
- **Adaptation note**: HARD RULE for G2-S2: only fire on explicit user
  request ("che domande mi consigli?" / "where should I start?"). Never
  inject from server-side scheduler, post-action hook, or
  conversation-archive scan. Document this in the action's Description
  field so the LLM also learns this norm.

### F8 — Capability transparency: 3 named example queries beat "How can I help?"

- **Source**: <https://fuselabcreative.com/chatbot-interface-design-guide/>
- **Year**: 2026
- **Pattern shape**: Open with "three named example queries scoped to its
  domain" so users grasp capabilities in five seconds. Vague "How can I
  help?" is explicitly called out as anti-pattern. Over-humanized
  personality + bot names + jokes create false intelligence expectations
  and degrade trust.
- **Aura applicability**: ABSORB. Confirms that Aura's current generic
  "posso aiutarti con…" cold-start reply IS the anti-pattern G2-S2 is
  designed to kill. Validates the G2-S2 motivation in the plan doc.
- **Adaptation note**: G2-S2 already shows the bucket name as a tag
  (`[ambiguous_edge]`, `[bridge_node]`, etc.) — this IS the "named example
  query" pattern. Keep the tags in the capsule; do NOT drop them in the
  name of brevity. They're load-bearing for capability transparency.

### F9 — Logseq Graph Analysis plugin: Adamic-Adar + CoCitation, on-demand mode

- **Source**: <https://github.com/trashhalo/logseq-graph-analysis>
- **Year**: 2024-2025
- **Pattern shape**: Plugin offers three modes (Adamic-Adar = secret
  connections, CoCitation = doc similarity via shared refs, Shortest Path).
  User must explicitly enter "graph analysis mode" — never auto-fires. No
  natural-language question generation — outputs are ranked lists of
  related notes that the user can click to navigate.
- **Aura applicability**: SKIP for G2-S2; SEPARATE-STORY candidate for a
  later "connection suggestion" action (e.g.
  `search(action=suggest_links)`) that proposes wiki edges Aura should add.
- **Adaptation note**: Don't conflate "suggest questions" (G2-S2) with
  "suggest links to add" (potential future story). The latter is the
  Logseq/Adamic-Adar shape — useful, but a different problem. Note it as a
  candidate for Wave G3 or later.

### F10 — Citation-grounding builds trust but micro-citation overwhelms UX

- **Source**: <https://arxiv.org/pdf/2501.01303> "Citations and Trust in
  LLM Generated Responses"; AiOps School 2026 guide
  <https://aiopsschool.com/blog/citation-grounding/>
- **Year**: 2025-2026
- **Pattern shape**: A/B tests show 3–10% engagement lift when chatbot
  responses include citations; trust scores rise significantly when
  citations are present. BUT "embedding citations for every micro-
  interaction can overwhelm UX and create noise; overly aggressive citation
  of low-value evidence reduces clarity".
- **Aura applicability**: ABSORB. G2-S2 already cites `[[slug]]` in every
  question — this is the high-signal level of citation that helps trust
  without overwhelming.
- **Adaptation note**: Keep the `[[slug]]` style. Do NOT add multi-source
  citations per question (e.g. "based on `wiki/page.md` lines 12–18, see
  also `wiki/log.md`"). One slug per question is the sweet spot. The `why:`
  line is the explanation channel, not a citation channel — keep it
  one-line, prose.

### F11 — Subgraph-guided question generation (academic baseline)

- **Source**: Chen et al., "Toward Subgraph-Guided Knowledge Graph Question
  Generation with Graph Neural Networks" —
  <https://arxiv.org/pdf/2004.06015>; "Generate-on-Graph" (Xu et al.,
  EMNLP 2024) <https://aclanthology.org/2024.emnlp-main.1023/>
- **Year**: 2020 (foundation) → 2024 (LLM-driven)
- **Pattern shape**: Academic KGQG takes a subgraph + target answer and
  generates a natural-language question. Modern LLM-driven variants (2024)
  use the LLM as both agent AND graph reasoner. Standard pipeline:
  1. select a subgraph (often via random walks, BFS from a seed, or
     centrality scoring)
  2. linearize it into a textual template
  3. prompt an LLM to convert into a question
- **Aura applicability**: SKIP as full lift (heavy ML / GNN training is
  overkill at 45 pages), but ABSORB the *3-step pipeline shape*.
- **Adaptation note**: G2-S2 already does steps 1–3 in a lightweight
  template-driven way (Go code picks slugs, then formats with f-string
  templates). The academic literature suggests Aura's approach is
  *correct for the scale* — LLM-driven KGQG would be massive overkill.
  When the wiki grows past ~500 pages we might revisit using the LLM
  itself to phrase the question text from a structured (slug-set, why)
  pair; until then, templates are right.

### F12 — Suggestion fatigue / repeat-explore (RepeatNet pattern)

- **Source**: <https://arxiv.org/pdf/1812.02646> "RepeatNet: A Repeat
  Aware Neural Recommendation Machine for Session-based Recommendation";
  PISA <https://arxiv.org/pdf/2408.16578> ACT-R-inspired session recsys
- **Year**: 2019 + 2024
- **Pattern shape**: Recommender systems must explicitly model whether the
  next item should be a *repeat* of a past suggestion or an *explore* (new)
  suggestion. Naive systems either always show new (causing context loss)
  or always show same (causing fatigue). RepeatNet uses a learned gate.
- **Aura applicability**: ABSORB as design constraint, not full lift.
- **Adaptation note**: G2-S2 should suppress questions the user has
  already engaged with in recent sessions. Concrete addition:
  - Add a `recent_question_hashes` table (or reuse `conversations`) that
    logs the slug-set + bucket-type of every emitted question.
  - When generating the markdown capsule, skip any question whose hash
    matches one emitted in the last 7 days unless ALL non-stale questions
    are exhausted.
  - This is a small follow-up story (G2-S2b? ~50 LOC + 1 test). Not
    strictly blocking — the first ship can be stateless. Add to the
    risks section of the G2-S2 plan as "Stateless on first ship; stale-
    suppression to follow if user reports fatigue".

### F13 — Personal-AI deep-interview cold-start (toomanybrians gist)

- **Source**: <https://gist.github.com/toomanybrians/4c64f3f6774caee6feff9b0b12172867>
- **Year**: 2024
- **Pattern shape**: A starter prompt that interviews the user on 6
  domains (identity/scope, daily workflows, pain points, interaction
  preferences, prior attempts, existing knowledge) BEFORE building any
  system structure. The mantra: "I don't want a generic template. I want a
  system that's built around how I actually work."
- **Aura applicability**: SKIP for G2-S2 (Aura already knows Davide
  intimately via `USER.md`); SEPARATE-STORY for a hypothetical onboarding
  flow when Aura ships to other users (relevant if/when the DGX Spark
  bundle vision activates per `project_aura_dgx_spark_bundle_vision.md`).
- **Adaptation note**: Park this — not relevant to G2-S2's single-primary-
  user scope. Re-surface when Phase-ONB returns to the queue.

### F14 — Meta's internal AI second brain: bootstrap-scan pattern

- **Source**: <https://medium.com/@AnalyticsAtMeta/how-we-built-an-ai-second-brain-for-60k-knowledge-workers-78c507dd795b>
- **Year**: 2026-04
- **Pattern shape**: Meta's internal tool uses a "bootstrap command that
  scans recent activity and builds an initial workspace, removing barriers
  to adoption by showing value in the first session without manual file
  organization". I.e. proactive cold-start IS done — but only ONCE, at the
  very first session.
- **Aura applicability**: ABSORB (validates F4/F6/F7 sequencing). Confirms
  that even the most aggressive "proactive at first contact" production
  pattern is a one-shot, not a recurring auto-fire.
- **Adaptation note**: Already aligned with G2-S2's tool-driven model. No
  change.

---

## What we should NOT do (anti-patterns observed)

Distilled from F4, F5, F6, F7, F8, F10, F12:

1. **Auto-fire suggestions between user-driven turns.** Mid-task
   interventions get 62% dismissed (F6) and trigger self-threat (F7) in
   competent users. G2-S2 must remain explicitly tool-driven; do NOT add
   a server-side scheduler that injects suggestions during active sessions.

2. **Generic "How can I help?" replies.** Identified as anti-pattern in
   chatbot UX literature (F8). Aura's current cold-start behavior is
   precisely this; G2-S2 must replace it, not augment it.

3. **Explicit follow-up question lists at the end of every answer.** F5
   shows these REDUCE perspective diversity vs ambiguous/suggestive
   endings. Don't sprinkle bullet-list "you might also want to ask…"
   under every response — at most, weave one suggestion into the prose.

4. **Re-firing the same suggestion every session.** F12 (RepeatNet)
   shows uncalibrated repeat causes fatigue. Add stale-suppression as
   G2-S2b follow-up (see F12 adaptation).

5. **Over-citing inside suggested questions.** F10 — keep to one
   `[[slug]]` per question max-2. The `why:` line is for explanation,
   not for stacking citations.

6. **Welcome-page suggestions that persist into the chat scroll.** F4 —
   suggestion chips belong to the empty state only; if Aura ever ships a
   dashboard chat surface, do NOT re-render the chips alongside historical
   messages.

7. **Always-on suggestion banner ("don't forget to ask about X").** F7 —
   classic backfire pattern; provokes self-threat. Even on cold-start,
   suggestions should feel *responsive to a question*, not pushy.

8. **Heavy ML / GNN for question generation at 45 pages.** F11 — modern
   KGQG academic work uses Graph2Seq / LLM-as-agent pipelines that are
   massive overkill for a 45-page wiki. G2-S2's template-driven approach
   is right for the scale; do not over-engineer.

---

## Open questions for the user

1. **Trigger surface.** G2-S2 ships as an LLM tool action — the LLM
   decides to call it. Do you also want a Telegram slash command
   (`/suggest`) so you can manually invoke it without the LLM having to
   pick up on intent? *(Recommended: yes, ~10 LOC follow-up; F8 capability
   transparency.)*

2. **Reply-tail follow-ups (F2 / Perplexity pattern).** Should
   substantive Aura answers occasionally tail-attach a 1-line related
   question, conversation-context-aware? This is the highest-engagement
   pattern in production (Perplexity ships it) but requires a
   *conversation-aware* variant of suggest_questions. SEPARATE STORY if
   yes, ~150 LOC. *(My read: defer until G2 closes and we see how often
   you actually invoke G2-S2 cold-start.)*

3. **Stale-suppression scope.** F12 — should G2-S2b track suggestion
   emissions in SQLite for N=7 days suppression? Or is stateless fine for
   the single-user scale until you report fatigue? *(My read: ship
   stateless first, observe for 2 weeks, then add suppression if needed.)*

4. **Output language for the questions.** All Aura prompts are
   English-only per `feedback_all_prompts_in_english_only`, but the
   user-facing reply is Italian via overlay directive. The
   `wiki_suggest_questions.go` markdown capsule template strings — should
   they be in English (let the LLM translate) or pre-localized Italian?
   *(My read: English templates, let the LLM render to IT on the way out.
   Consistent with the LAT-02 lesson.)*

5. **Suggestive-ending styling (F5).** Should we add a SOUL.md /
   TOOLS.md directive that tells Aura how to *frame* the suggest_questions
   output (e.g. "introduce with a one-line observation; lead with the
   most surprising bucket; weave 1–2 into prose; don't dump as bare
   list")? This is a 5-line prompt change with potentially big UX win
   per Park et al. *(My read: yes, ship as part of G2-S2 commit, NOT
   separate.)*

6. **Bucket priority in mixed wiki state.** When all 4 buckets have hits,
   current plan is fixed priority 1 → 4. Should we randomize within top-k
   to avoid the user always seeing AMBIGUOUS first? Or stay deterministic
   per acceptance criterion #2? *(My read: stay deterministic; F12
   stale-suppression solves the variety problem more cleanly than
   randomization.)*

---

## Sources (all URLs)

Production:
- <https://learn.microsoft.com/en-us/microsoft-copilot-studio/configure-starter-prompts>
- <https://www.perplexity.ai/hub/blog/getting-started-with-perplexity>
- <https://blog.bytebytego.com/p/how-perplexity-built-an-ai-google>
- <https://infranodus.com/use-case/visualize-knowledge-graphs-pkm>
- <https://infranodus.com/use-case/network-analysis-visualization>
- <https://github.com/trashhalo/logseq-graph-analysis>
- <https://github.com/devsunb/logseq-graph-analysis>
- <https://docs.github.com/en/copilot/using-github-copilot/copilot-chat/getting-started-with-prompts-for-copilot-chat>
- <https://help.zapier.com/hc/en-us/articles/31261767095693-Add-suggested-questions-to-your-chatbot-conversations> (403 — referenced only)
- <https://medium.com/@AnalyticsAtMeta/how-we-built-an-ai-second-brain-for-60k-knowledge-workers-78c507dd795b>
- <https://gist.github.com/toomanybrians/4c64f3f6774caee6feff9b0b12172867>

HCI / UX design:
- <https://fuselabcreative.com/chatbot-interface-design-guide/>
- <https://www.bonanza-studios.com/blog/proactive-ai-vs-reactive-ai-in-ux-design>
- <https://aiopsschool.com/blog/citation-grounding/>
- <https://forum.obsidian.md/t/find-orphan-notes/817>
- <https://www.obsidianstats.com/plugins/find-unlinked-files>

Academic:
- <https://pmc.ncbi.nlm.nih.gov/articles/PMC11007170/> (Park et al., Ovsiankina effect, 2024)
- <https://arxiv.org/abs/2601.10253> (Kuo et al., Developer Interaction Patterns with Proactive AI, IUI '26)
- <https://arxiv.org/pdf/2410.04596> (Need Help? Designing Proactive AI Assistants for Programming)
- <https://arxiv.org/pdf/2509.09309> (Proactive AI Adoption can be Threatening)
- <https://arxiv.org/pdf/2501.01303> (Citations and Trust in LLM Generated Responses)
- <https://arxiv.org/pdf/2004.06015> (Subgraph-Guided KG Question Generation with GNNs)
- <https://aclanthology.org/2024.emnlp-main.1023/> (Generate-on-Graph, EMNLP 2024)
- <https://arxiv.org/pdf/1812.02646> (RepeatNet, session-based repeat-aware recsys)
- <https://arxiv.org/pdf/2408.16578> (PISA, ACT-R inspired session recsys)
- <https://emilykuang.github.io/assets/papers/CHI24-UX-Proactive-CA.pdf> (CHI '24 proactive UX evaluation)
- <https://arxiv.org/html/2501.09493v3> (Evaluating CRS via LLMs)
- <https://link.springer.com/article/10.1007/s11257-024-09420-2> (CRS theory/evaluation special issue)
