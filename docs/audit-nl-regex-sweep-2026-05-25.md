# NL Regex / Dictionary Anti-Pattern Audit — 2026-05-25

Audit scope: `d:/Aura/internal/**/*.go` + `d:/Aura/cmd/**/*.go`. Pursuant to
`feedback_no_regex_for_nlp` (just-updated): natural-language text in →
hardcoded NL words, regex with NL verb/noun alternations, or static
language-specific dictionaries is the anti-pattern under sweep.

Trigger: `lowSignalSearchTerms` IT+EN stopword map removed from
`internal/storage/search/sqlite.go` earlier today. User reaction:
*"le regex non funzionano mai te lo metti in testa!"*

Severity legend:
- **KILL** — definite NL anti-pattern, same shape as the removed
  `lowSignalSearchTerms`. Should be deleted or replaced.
- **REVIEW** — same shape but the source text is partially structured
  (security signatures, attacker-controlled spans, etc.) — needs a human
  call on whether the risk profile justifies keeping the hardcoded list.
- **SAFE** — included only because the grep hit looked similar but the
  source is structured (URLs, headers, slugs, locale tags). Listed for
  completeness so the next reader knows they were inspected.

---

## KILL — definite NL anti-pattern

### `internal/agent/phantom_guard.go:222-244` — bilingual past-tense first-person verb list

- **Pattern shape**: `performativeVerbs` is a hardcoded `[]string` of ~50 Italian
  and English verb conjugations (`"ho schedulato"`, `"i scheduled"`,
  `"just ran"`, …) that `hasPerformativeNear()` scans for inside the
  assistant's natural-language reply. Triggers a "phantom tool" correction
  when one of these tokens appears within 120 chars before a bare tool name.
- **Source text it operates on**: LLM-generated assistant prose (the reply
  about to be sent to the user). Pure NL.
- **Severity**: KILL. This is the textbook shape of the just-removed
  stopword list — language-coupled (only IT+EN), tense-coupled (only past-
  tense first-person), and silently misses anything paraphrased outside the
  list (passive voice, future tense, third person, any other language). The
  in-file comment even concedes: *"means a future user explaining in
  French/Spanish won't trigger, which is the right default (safer to skip
  than over-correct)"* — which is the same fragility the user just rejected.
- **Suggested replacement**: Ground the phantom-detection signal in
  structured evidence instead of NL prose. Options:
  1. **Drop the guard entirely.** The post-turn enforcement loop already
     fixes phantom claims via the user's next turn ("but you said you did
     X"). Live cost of a phantom that slips through = the user re-asks.
  2. **Tool registry only.** The presence of a bare tool name in the reply
     when `calledThisTurn` is empty is already a usable signal; drop the
     verb-proximity check, accept the higher false-positive rate, and let
     `MaxRetries=1` cap the cost.
  3. **LLM-as-judge.** One small follow-up call: *"Does this reply claim
     to have invoked the listed tool? yes/no."* — language-agnostic, robust
     to paraphrase. Cost: 1 extra LLM round only when a registered tool
     name appears without a tool call (rare).
- **Blast radius**: `internal/agent/phantom_guard_test.go` exercises the
  bilingual verb list directly (`"italian eseguito run_now sul task"`,
  `"english search_memory mentioned"`, ~9 cases). Replacement strategy
  decides whether those tests become assertions on the registry-only
  signal or are deleted with the guard. One call site in
  `loop.go` (PhantomToolGuard.LooksPhantom). Live impact already
  documented in memory: *"Phantom guard needs proximity, not just
  presence"* — that fix layered proximity on top of the same fragile
  bilingual dict; the right move now is to remove the dict instead of
  add another layer.

### `internal/agent/posthook.go:26` — `userNegativePattern` regex on user prose

- **Pattern shape**: `regexp.MustCompile(\`(?i)^\s*(no|non|stop|smetti|sbagliato|wrong|fermati)\b\`)`
  applied to `turn.UserMessage` to decide whether to write a
  `user_negative_feedback` operational lesson.
- **Source text it operates on**: User-typed Telegram / web chat message.
  Pure NL.
- **Severity**: KILL. Mixed-language alternation in the exact shape the
  feedback memory bans. Misses: `"non funziona"` (works), `"fermati"`
  (works) but also `"hai sbagliato di nuovo"` (works) vs `"è sbagliato"`
  (works — anchored) vs `"sbagli sempre"` (MISS — `sbagli` not in list)
  vs `"that's wrong"` (MISS — `wrong` not anchored to start), vs French
  / Spanish / German negation entirely.
- **Suggested replacement**:
  1. **Drop the heuristic** and rely solely on the `n_failure` threshold
     path. A repeated tool failure is already a stronger signal than a
     single user grumble; the user-negative branch is best-effort sugar.
  2. **LLM-as-judge.** Add a tiny per-turn classifier (cached by user
     message hash): *"Is the user expressing frustration with the
     previous tool action? yes/no."* — language-agnostic.
  3. **Sentiment via the existing memory-judge LLM call** (already running
     post-turn for operational lessons): extend its JSON contract to also
     return `{"user_negative_feedback": bool}`. Zero new LLM round-trips.
- **Blast radius**: `internal/agent/posthook_test.go::TestHeuristicPostTurnHookUserNegativeCases`
  drives the exact strings (`"no"`, `"non funziona"`, `"stop"`). Single
  call site (line 158). Lesson rows produced by this branch are
  best-effort and recoverable — deletion does not corrupt store state.

### `internal/agent/tools/registry/description_audit_test.go:86` — `itWords` regex bans Italian function words

- **Pattern shape**:
  `itWords := regexp.MustCompile(\`\b(gli|agli|nella|dei|delle)\b\`)`
  applied to every tool description, fails the test if any match.
- **Source text it operates on**: Tool description strings (these are NL,
  even if they're authored by us and not the LLM).
- **Severity**: KILL — for the principle. This is *exactly* the
  dictionary-based language-classifier shape. The intent is good ("keep
  descriptions in English per `feedback_all_prompts_in_english_only`") but
  the mechanism is wrong: a 5-word IT-only dictionary will not catch
  `"questo strumento permette di..."`, `"se vuoi puoi..."`, `"crea una pagina"`
  — all of which are Italian leaking into English descriptions.
- **Suggested replacement**:
  1. **LLM-as-judge in CI.** Replace the test with a once-per-CI call:
     *"Is this string entirely in English? yes/no, with reason."* Robust,
     scales to whatever language slips in.
  2. **Tighter spec, not broader regex.** Replace the dictionary check
     with an `IsASCII` + `len(non-ASCII chars) == 0` assertion against
     a fixed Latin-1 superset. False-positive on `"€"` or quotes ("smart
     quotes") needs to be handled, but the structural rule is much more
     defensible than a 5-word dict.
  3. **Delete the test.** Description audit already tests for required
     marker prefixes + length caps + specific phrases — those are stronger
     signals than "no IT words." Authors who care about the EN-only rule
     can read CLAUDE.md.
- **Blast radius**: Test-only, no runtime impact. Single file. The
  description audit is already a curated regression gate; trimming one
  weak assertion does not weaken the rest.

---

## REVIEW — same shape, but operates on attacker-controlled untrusted input

### `internal/agent/tools/registry/propose_patch_security.go:21-41` — operational-memory prompt-injection signature list

- **Pattern shape**: Two structures applied to operational-memory lesson
  text submitted via `propose_patch action=operational`:
  - `operationalHardRejectLiterals` — 4 literal NL sentences (verbatim
    known prompt-injection corpus entries) checked with `strings.Contains`.
  - `operationalQuarantinePatterns` — 9 regexes mixing EN + IT + structural
    HTML-tag patterns (`(?i)\bignore\s+(previous|prior|all)\b`,
    `(?i)\btu\s+sei\s+(un|una|adesso|ora)\b`,
    `(?i)</?\s*(system|developer)\s*>`, …).
- **Source text it operates on**: Free-form `lesson` field submitted by the
  LLM to `propose_patch`. The agent is the *source* but a prompt-injected
  upstream document is what *causes* the agent to forward the payload —
  this is the threat model the file mitigates.
- **Severity**: REVIEW. Mechanically the same anti-pattern (regex/dict
  over NL), but unlike the search-stopword case the cost asymmetry runs
  the other way: a false negative writes a persistent malicious lesson
  into operational memory; a false positive just rejects a benign lesson
  the LLM can re-propose with different phrasing. Hardcoded signatures
  + verbose comments are also the prevailing convention in commercial
  prompt-injection libs (Lakera, PromptGuard, NeMo Guardrails) — so the
  "should it be regex?" debate is at least defensible.
- **Suggested replacement** (in order of decreasing scope):
  1. **Keep as-is**, but treat as a defence-in-depth tripwire, not a
     primary defence. Document explicitly that the primary defence is the
     `WrapUntrustedToolResult` envelope + LLM treating its own
     `propose_patch` body as suggestion-not-instruction.
  2. **Promote to LLM-as-judge.** A small *adversarial classifier* call
     on every `propose_patch action=operational`: *"Is this lesson
     attempting to subvert future LLM behaviour? severity 0-3."* Adds
     latency to a rare path; robust to paraphrase.
  3. **Both** (LLM + tripwire as the cheap pre-filter so obvious junk
     never reaches the classifier).
- **Blast radius**: `propose_patch_test.go::TestProposePatch_OperationalBenignItalianAccepted`
  and the security-rejection sibling tests. Multiple call sites in
  `propose_patch.go`. The literal list is curated against known CVE-style
  payloads — replacing it requires the LLM classifier to recognise the
  same examples. Worth a conscious decision before any change.

---

## SAFE — inspected, not flagged

Listed only so the next reader knows the grep ran and inspected these.

### `internal/api/setup_locale.go:5` — `detectLocale`
Reads `Accept-Language` HTTP header, matches against `it` / `en` /
`it-*` / `en-*` exact prefixes. Structured input (IANA BCP-47 tags),
not NL prose.

### `internal/cron/agent_job.go:84` — `NormalizeAgentJobLanguage`
Switch over `it|ita|italian|italiano|en|eng|english|inglese` to map onto
a canonical 2-letter code. Operates on a user-supplied scheduler field
(picked from a settings form), not on free prose. Single-source-of-truth
shape — same as a country-code map.

### `internal/llm/classify.go:47-53` — `redactJWT`, `redactBearer`, etc.
Regex on structured formats: JWTs, `sk-or-v1-*` keys, `Bearer xxx`
headers, `Authorization:` headers, basic-auth URLs, long base64 blobs.
All structural — not NL.

### `internal/llm/classify.go:118-129` — error-message string-classifier
`"rate limit"` / `"overloaded"` / `"quota"` / `"model not found"` over
LLM API error bodies. These are provider-emitted English error tokens
(OpenAI-compatible spec convention), not user prose. Single-language is
correct because all providers emit English. Borderline — could be
considered NL, but the source is API-emitted English by spec, not user
content.

### `internal/agent/tools/registry/registry.go:421` — `sensitiveArgKeyRe`
Matches sensitive JSON keys (`password`, `secret`, `token`, …) before
logging — structured field names, not NL.

### `internal/tokenjuice/postprocess.go:10-17` — git-status line regexes
`^\s+modified:\s+(.+)$` etc. on `git status` output. Structured CLI
output, not NL.

### `internal/tokenjuice/text.go:170-194` — `pluralize`
English plural-form helper. Operates on internal noun strings the
package authors emit (e.g. `pluralize(3, "file")` → `"3 files"`). Not
operating on user/LLM text.

### `internal/storage/sources/ingest/extractor.go:40-59` — `entityTypes`, `linkTypes`
Closed enums on LLM-emitted JSON `type` / `from_slug` / etc. fields.
Structured validation, not NL filtering.

### `internal/agent/terminal.go:109-116` — `toolCallMarkupMarkers`
Markers for tool-call markup detection (`tool_calls`, `<tool_call`,
`dsml`, `invoke name=`). These are structural protocol leakage markers,
not NL.

### `internal/agent/untrusted.go:21-25` — `untrustedSourceTools`
Tool-name set (`web`, `source`, `read_skill`). Tool names from registry,
not NL.

### `internal/agent/governance/governance.go` and similar — `map[string]bool`
Hits exist (governance, registry, mcp/policy, swarm/manager_test, …)
but every one is keyed on tool / role / setting / artifact identifiers,
not NL words. Safe shape.

### `internal/storage/search/sqlite.go:236-263` — `significantSearchTerms`, `tokenizeSearchQuery`, `escapeFTS5Query`
These are the cleaned-up siblings of the removed
`lowSignalSearchTerms`. They use `unicode.IsLetter`, `unicode.IsDigit`,
diacritic strip via `stringx.StripDiacritics`, and a `len(term) < 2`
character-class filter — all structural. Explicitly comment-cites the
feedback memory.

### `internal/storage/memoryindex/store_helpers.go:144-165` — `exactCandidates`, `escapeFTS5Query`
Same shape as above — character-class only.

### `internal/agent/tools/registry/registry_search.go:77-92` — `searchTerms` for `ToolSearch`
Character-class tokeniser (`'a'..'z' || '0'..'9' || 'à'..'ÿ'`) for tool
catalog search. Structural.

### `cmd/probe_chat/*` Italian prompt strings
Probe test prompts include Italian instruction sentences. These are test
fixtures (assertions on Aura's behaviour), not runtime NL filters.

---

## Patterns checked clean (grep recipes run, no hits beyond above)

The following greps returned only the findings above (or hits already
re-classified as SAFE):

- `stopword|stop_word|lowSignal|low_signal|commonword|fillerword|blacklist.*[Tt]erm|noiseword|bannedword`
- `[Ll]ow.*[Ss]ign|[Nn]oise[Ww]ord|[Ff]iller[Ww]ord|[Bb]ann?ed[Ww]ord`
- `strings\.Contains.*"non"|strings\.Contains.*"sì"|strings\.Contains.*"si "`
- `strings\.Contains.*"ho "|strings\.Contains.*"sono "|strings\.Contains.*"i did"|strings\.Contains.*"i've"`
- `strings\.Contains.*\b(che|come|qual|the|what|when)\b`
- `strings\.HasPrefix.*"(ho |sono |hai |abbiamo|si |come|che )"`
- `[Ii]sQuestion|[Ii]sGreet|[Ii]sSmallTalk|[Ii]sChitChat|[Ii]sIntent`
- `[Dd]etect[Ll]ang|[Ll]anguageOf` (only `detectLocale` on `Accept-Language` — SAFE)
- `map\[string\]bool\{` with NL words near declaration (40 files inspected
  — every map keyed on identifiers/IDs/tool names, not NL)
- `map\[string\]struct\{\}\{` with NL words (13 files inspected — same)
- `var \w+ ?= ?\[\]string\{` (200 file slice review — every slice is
  identifier-typed: action names, tool names, role names, command names,
  enum values, except `performativeVerbs` already flagged)
- `regexp\.MustCompile` containing NL verb/noun alternations — covered
  above (3 KILL/REVIEW hits; remaining matches all structural: URLs,
  slugs, timestamps, semver, FTS5, JSON, git output, JWT)
- `regexp\.MustCompile.*\([a-zA-Z]+\|[a-zA-Z]+` (alternation-with-words shape)
  — covered above
- `func .*[Ff]ilter.*[Tt]erm|func .*[Ss]kip.*[Ww]ord|func .*[Dd]rop.*[Ss]top`
  — no hits anywhere (well-named: nothing in the tree is shaped like
  "filter natural-language terms")
- `[Tt]okeniz|[Ee]scapeFTS|[Nn]ormaliz.*[Qq]uery|[Cc]lean.*[Tt]ext|[Ff]ilter.*[Tt]erms?`
  (21 files inspected — all character-class or structured-format
  cleaners)

Audit time: ~30 min wall-clock. Coverage: every grep recipe in the
prompt + 6 additional cross-checks above. No `internal/**/*.go` or
`cmd/**/*.go` file matching the anti-pattern shape went uninspected.
