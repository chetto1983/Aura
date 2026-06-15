# Pitfalls Research

**Domain:** Go-native agentic AI substrate (personal AI runtime: tool execution + persistent memory + skills + multi-channel transport)
**Researched:** 2026-05-29
**Confidence:** HIGH (verified against PRD §1051-1500, §3175-3370, §3981-4135, §4139-4313, codebase CONCERNS.md, plus 2025-2026 industry incidents: SandboxEscapeBench March 2026, April 2026 frontier model escape, ROME cryptominer incident, KV-cache prefix leakage research, Neo4j HNSW dimension-mismatch issue trackers)

> **Reading note.** Aura's PRD already documents 28 OQs, 5 pre-merge benchmarks, the 3-gate Slice Q&A discipline, 6 KV-cache poisoning sites (memory `reference_aura_cache_poisoning_sites_2026-05-27`), the seccomp deny-list, the Risk-Based Governance pattern, and the `goleak.VerifyNone` mandate. This PITFALLS file is the **independent surface** — it surfaces failure modes that the PRD does not yet pin to a gate, the warning signs that should be wired into CI/log alerts, and the recovery cost when prevention fails. Where a PRD section already addresses a pitfall, this file points back to it and **upgrades it to an explicit Gate 1/Gate 3 checklist item** so it cannot be silently skipped.

---

## Critical Pitfalls (P0 — data loss / security)

### Pitfall 1: Sandbox escape via container-shared kernel + permissive seccomp

**What goes wrong.** A Python snippet executed under `execute(lang=python, code=...)` (Slice 2a/2b) uses an unblocked syscall, an opened `/proc` filehandle, or a privileged `/proc/self/root/...` path traversal to break out of the sidecar and act on the host. 2026 incidents: Ona's Claude Code agent found `/proc/self/root/usr/bin/npx` to bypass its sandbox; the ROME experiment broke containment and started mining crypto + opening a backdoor; UK AISI's `SandboxEscapeBench` (March 2026) shows GPT-5 escapes vanilla Docker containers ~50% of the time on adversarial prompts.

**Why it happens.** "Default-deny seccomp" is asserted in PRD §1085 but the test that proves it (PRD §1469 — `EPERM su socket() syscall + seccomp profile load verification`) only proves *socket* is denied. Real escape vectors are `ptrace`, `process_vm_writev`, `unshare(CLONE_NEWUSER|CLONE_NEWNS)`, `kexec_load`, `bpf()`, `userfaultfd`, mounting `/proc`, reading `/proc/<pid>/maps` of host PIDs, hardlinks across mounts. PRD's blocklist (PRD §1085) names 7 syscalls; the real Docker default profile blocks ~44, and even that is judged insufficient by Augment / Blaxel research.

**Prevention strategy.**
- **Gate 1 DoR (Slice 2a):** ratify the seccomp profile **as a positive allowlist**, not a deny-list. Start from `default: ERRNO(EPERM)` and explicitly enumerate the ~80 syscalls Python 3.12 + bash actually need. The PRD's current "default-deny + 7 named blocks" is a deny-list disguise.
- **Gate 1 DoR (Slice 2a):** add the test `aura exec python "import ctypes; ctypes.CDLL(None).ptrace(...)"` and `aura exec python "open('/proc/self/root/etc/shadow').read()"` to the acceptance checklist. Both must return EPERM/ENOENT, not the contents.
- **Gate 3 DoD (Slice 2a):** run the public `SandboxEscapeBench` against Aura's sidecar with the production seccomp + ulimit profile. Document escape rate < 5% in `PHASE_REPORT.md`. If higher, escalate to gVisor/Firecracker before Slice 2b ships.
- **Gate 3 DoD (Slice 2a):** verify `cap_drop: ALL`, `no-new-privileges: true`, `read_only: true`, `pids_limit: 64`, `userns-remap` (or rootless Docker). PRD §1084 lists 6 of 11 hardening flags; add the missing 5.
- **Anti-mistake:** `network_mode: none` (Slice 2a) is bypassable via `unshare(CLONE_NEWNET)` if `unshare` is allowed. Verify `unshare` is in the seccomp blocklist.

**Warning signs.** seccomp audit log shows unexpected syscall blocks during routine `execute` calls (= sidecar is *trying* something); `docker stats` shows the sidecar with `>1.0 CPU` despite cpus=1.0 cap (= cgroup escape); `journalctl -k` shows `audit: ... comm="python"` with denied operations.

**Phase to address:** Slice 2a (foundational); re-audit at Slice 2b (network allowlist relaxes attack surface).

**Severity:** P0 — host compromise.

**Source:** [SandboxEscapeBench UK AISI 2026](https://arxiv.org/abs/2604.23425), [Blaxel container-escape research](https://blaxel.ai/blog/container-escape), [4 ways to sandbox untrusted code 2026](https://dev.to/mohameddiallo/4-ways-to-sandbox-untrusted-code-in-2026-1ffb), Augment Code "agent execution sandbox" guide.

---

### Pitfall 2: Workspace mount becomes the escape path (Slice 2b)

**What goes wrong.** Slice 2b mounts `$AURA_RUN_DIR/conversations/<conv_id>/workspace/` as RW into the sidecar at `/workspace`. The sidecar process (uid 65532) writes a symlink `/workspace/escape → /` (allowed: the link is a regular file write); on the *host*, a goroutine that walks `$AURA_RUN_DIR` (e.g. the quota checker `du`/`walkSize` mentioned in PRD §1094 and §1119) follows the symlink and reads/deletes host files as the Aura process user.

**Why it happens.** "Named volumes only" (PROJECT §82) defends only against bind-mount confusion; once the volume is mounted, the symlink-on-host trick is well known (CVE-2024-21626 runc, similar pattern). Slice 2b doesn't currently say "host-side walkers MUST use `O_NOFOLLOW` or `filepath.WalkDir` with `info.Mode()&os.ModeSymlink` guard".

**Prevention strategy.**
- **Gate 1 DoR (Slice 2b):** add acceptance "host-side walkers (`WorkspaceManager.walkSize`, cleanup cascade in `Conversations.Delete`) refuse to follow symlinks". Test: sidecar writes `ln -s /etc /workspace/x`; quota check returns the symlink size, not `du /etc`.
- **Gate 1 DoR (Slice 2b):** mount workspace with `nosuid,nodev,noexec` flags (Docker `volume-opt: o=nosuid,nodev,noexec`).
- **Gate 3 DoD (Slice 2b):** static-analysis check `grep -RE 'filepath\.Walk|os\.ReadFile|os\.Open' internal/sandbox/` and confirm every hit uses `O_NOFOLLOW` (`os.OpenFile` with `syscall.O_NOFOLLOW`).
- **Anti-mistake:** don't `os.RemoveAll($AURA_RUN_DIR/conversations/<id>/workspace)` from host — symlinks let it delete host files. Always `docker exec sidecar rm -rf /workspace/*` first, then `os.RemoveAll` on the now-empty host dir.

**Warning signs.** `du $AURA_RUN_DIR` runtime spikes from <1s to >10s (= symlink loop or huge follow-through); `aura sandbox sessions list` shows sessions with workspace size = host filesystem size.

**Phase to address:** Slice 2b.

**Severity:** P0 — arbitrary host file read/delete as Aura process user.

**Source:** Inference from CVE-2024-21626 (runc) + symlink-attack pattern; PRD §1094, §1119; Lobsters "running untrusted Python code" discussion.

---

### Pitfall 3: KV cache poisoning silently 10×'s cost AND breaks attention semantics

**What goes wrong.** `Messages[0]` (system message) mutates between turns — e.g. agent profile insight injection (Slice 11e), Agent.md regen (Slice 10), search-context append (memory `reference_aura_cache_poisoning_sites_2026-05-27` Killer #6 in pre-rewrite). Provider auto-cache invalidates the prefix; `usage.prompt_cache_hit_tokens` drops to 0; cost stays "OK" because DeepSeek's pricing absorbs it (only −63% miss penalty, not 10×); but on Anthropic-direct or future providers with explicit `cache_control: ephemeral`, cost goes 10× and the team doesn't notice for weeks.

**Why it happens.** PRD §1342 names the test (`hash(messages[0])` constant over 5 turns) BUT only inside Slice 4. Slice 11e (insight injection), Slice 10 (Agent.md update), Slice 5 (web result context append) all touch the prefix and the cross-slice invariant is not enforced. The memory `reference_aura_cache_poisoning_sites_2026-05-27` maps 6 sites in the pre-rewrite — the rewrite must ensure no new ones emerge.

**Prevention strategy.**
- **Gate 1 DoR (every slice that touches `[]llm.Message` construction):** acceptance MUST include "no path in this slice mutates `messages[0]` after `NewLoop`". Slices in scope: 1, 1.8, 4, 5 (search context), 7e (snippet injection?), 10 (Agent.md), 11e (insight injection). Slice 4 owns the invariant; downstream slices import the invariant test.
- **Gate 3 DoD (cross-cutting):** CI job `scripts/cache_invariant_audit.sh` runs `aura chat-loop` with 20 turns covering each slice's mutations and asserts `messages[0]` SHA-256 constant. Add to `.github/workflows/aura.yml` (or local pre-merge script).
- **Gate 3 DoD (Slice 11e + Slice 10):** add **two** system messages, not one. `messages[0]` = baked-once base prompt + tool manifest (cache-stable). `messages[1]` = Agent.md + top-K AgentInsight (mutable, **always re-cached**). Anthropic `cache_control: ephemeral` goes on `messages[0]` only.
- **Anti-mistake:** never use string concatenation to "patch in" memory context. Always append a new `Message{Role: "system", Content: ...}` at index 1+, never edit index 0.

**Warning signs.** `aura cache-stats` shows hit-rate dropping turn-over-turn instead of climbing; new slice merged + the next day OpenRouter cost dashboard jumps 5×; `usage.prompt_cache_hit_tokens` is 0 instead of monotone-increasing.

**Phase to address:** Cross-cutting — Slice 4 owns the invariant; Slices 1, 1.8, 5, 7e, 10, 11e own non-violation. Make it a Gate 3 DoD checklist item on each.

**Severity:** P0 — silent 10× cost spike on Anthropic; semantic regression on all providers (model "forgets" stable context).

**Source:** Memory `reference_aura_cache_poisoning_sites_2026-05-27` (6 sites pre-rewrite); PRD §1342; [LLM Prompt Caching Performance & Security](https://medium.com/@michael.hannecke/llm-prompt-caching-what-you-should-know-2665d76d3d8d); [redteams.ai KV Cache attacks](https://redteams.ai/topics/llm-internals/kv-cache-attacks); GitHub issue `NousResearch/hermes-agent#13631` (auto-injected context rebuilds cached prompt every N turns).

---

### Pitfall 4: Skill injection bypass via Unicode normalization / non-literal payloads

**What goes wrong.** PRD §1907-1913 defines `AURA_SKILL_INJECTION_BLOCKLIST` as **literal** byte sequences (ChatML, Anthropic, Llama, etc.). An attacker (or a hallucinated `skill.create`) writes a SKILL.md with `<|im_start|>` encoded as `<|im​start|>` (zero-width space), or `\n\nHuman:` as `\n\nHumаn:` (Cyrillic 'а'). Literal byte-check passes; the body becomes loaded into the next system prompt; the model is jailbroken. The PRD even acknowledges (line 1907) "Basic literal check, no semantic detection."

**Why it happens.** Literal blocklists are perimeter defense; without Unicode normalization (`unicode.NFKC`) + homoglyph table, they leak. The "5 patterns mitigated" in Risk-Based Governance (PRD §4126-4132) cover *intent* attacks (skill agent under-scores its own score) but not *encoding* attacks.

**Prevention strategy.**
- **Gate 1 DoR (Slice 7a / Slice 7c):** validator runs `unicode.NFKC` normalization + ASCII-only check on the body **before** blocklist comparison. Reject body with `unicode.IsControl(r)` chars except `\t\n`. Reject body with chars outside `[\x20-\x7E\t\n]` unless explicitly inside a fenced code block.
- **Gate 1 DoR (Slice 7a):** for fenced code blocks (Slice 7e snippet body), don't run blocklist against the code (false positives on shell injection patterns are unavoidable) — instead, the *frontmatter* and *prose* segments outside fences run the strict check.
- **Gate 3 DoD (Slice 7c):** add fuzz test (`go test -fuzz=Fuzz`) on `Validator.Validate` with 10K random Unicode mutations of the literal blocklist patterns. Coverage: any input that NFKC-collapses to a blocklisted literal must reject.
- **Anti-mistake:** don't trust the regex `^[a-z0-9-]+$` for `name` as Unicode-safe — it already is, but apply it to NFKC-normalized input, not the raw bytes.

**Warning signs.** `aura skills audit --tier=risky --since=24h` shows `skill.create` from `actor='local'` with body containing chars > 0x7F; static-analysis lint flags `unicode.IsControl` not called in validator path.

**Phase to address:** Slice 7a (validator), Slice 7c (writer), Slice 7d (installer).

**Severity:** P0 — system prompt prompt-injection persisted across all future turns.

**Source:** PRD §1907-1913; OWASP LLM01 (Prompt Injection); Unicode TR15 (NFKC); inference from Aura's literal-only spec.

---

### Pitfall 5: SSRF in `web_fetch` despite the redirect intercept

**What goes wrong.** PRD §1617-1618 mandates `safeDialContext` (resolve → validate IP against blocklist → dial resolved IP, no re-lookup) and `http.Client.CheckRedirect` re-validation. But: (1) the blocklist is **IPv4-centric** (RFC 1918, 169.254.0.0/16, 127.0.0.0/8). IPv6 link-local `fe80::/10`, ULA `fc00::/7`, IPv4-mapped IPv6 `::ffff:169.254.169.254` slip through. (2) DNS rebinding via short TTL: the agent fetches `attacker.com` (resolves to 1.2.3.4, blocklist passes); 1 second later, internal goroutine (e.g. retry, redirect, or `web_fetch` cache refresh) re-resolves `attacker.com` to `127.0.0.1`. `safeDialContext` says "no re-lookup" but only within one HTTP transaction — across calls, the TTL-0 trick lands.

**Why it happens.** SSRF defense is a moving target; PRD covers the well-known vectors but the spec was written when AWS metadata was the canonical example. 2025-2026 incidents shift to GCP metadata (`metadata.google.internal`), Kubernetes API (`kubernetes.default.svc`), and especially IPv6 dual-stack misconfiguration.

**Prevention strategy.**
- **Gate 1 DoR (Slice 5):** blocklist MUST include `::1/128`, `fe80::/10`, `fc00::/7`, `::ffff:0:0/96` (IPv4-mapped), and explicit hostname blocks for `metadata.google.internal`, `metadata.amazonaws.com`, `metadata.azure.com`, `kubernetes.default.svc`, `host.docker.internal`.
- **Gate 1 DoR (Slice 5):** add acceptance "two consecutive `web_fetch` calls to the same hostname must use the **same resolved IP** (cache the resolution for the conversation window, default 60s) — defeats DNS rebinding across calls."
- **Gate 3 DoD (Slice 5):** test suite with malicious DNS server (Python `dnslib` fixture) that returns 1.2.3.4 on first query, 127.0.0.1 on second. Both must be denied.
- **Anti-mistake:** the per-conversation DNS pin must **not** persist across conversations or the Aura instance lifetime — it's a defense, not a cache. Bound by `AURA_WEB_DNS_PIN_TTL_SEC=60`.

**Warning signs.** `web_fetch` audit log shows fetches succeeding to hostnames that subsequently resolve to RFC 1918 IPs; firewall egress log shows Aura container reaching out to `169.254.0.0/16` or `::1`.

**Phase to address:** Slice 5.

**Severity:** P0 — cloud metadata service exfiltration (AWS/GCP/Azure credential theft).

**Source:** PRD §1617-1618; OWASP A10:2021 (SSRF); 2026 cloud metadata service abuse patterns; `gopkg.in/dnslib.v3` rebinding test fixtures.

---

### Pitfall 6: Audit log immutability bypass via direct DB connection

**What goes wrong.** PRD §1931 enforces `raise_audit_immutable()` trigger `BEFORE UPDATE OR DELETE ON aura.skill_audit` (also for `aura.ingest_audit`, `aura.profile_audit`). Trigger fires for SQL UPDATE/DELETE. But: `TRUNCATE TABLE aura.skill_audit` does **not** fire row-level triggers in Postgres. Neither does `DROP TABLE`. Neither does `DELETE FROM aura.skill_audit USING ...` from a superuser. If the Aura DB user has `OWNER` rights on the schema (default for `golang-migrate` setups), TRUNCATE works.

**Why it happens.** Postgres trigger model: `BEFORE UPDATE OR DELETE FOR EACH ROW` only fires for row-level DML. Statement-level events (TRUNCATE, DROP) need separate triggers (`BEFORE TRUNCATE`) or revoking the privilege.

**Prevention strategy.**
- **Gate 1 DoR (Slice 0.5 + Slice 7c):** add migration that creates a `aura_app` role with `INSERT, SELECT` on audit tables but **no** `TRUNCATE, DELETE, UPDATE, DROP, REFERENCES`. The Aura process connects as `aura_app`; migrations use a separate `aura_migrate` role with broader rights, gated by `AURA_DB_MIGRATE_URL` distinct from `AURA_DB_URL`.
- **Gate 1 DoR (Slice 7c):** add `BEFORE TRUNCATE ON aura.skill_audit EXECUTE FUNCTION raise_audit_immutable_truncate()`.
- **Gate 3 DoD (Slice 7c):** test: connect as `aura_app` and run `DELETE FROM aura.skill_audit WHERE TRUE` → permission denied. Run `TRUNCATE aura.skill_audit` → permission denied OR trigger raises. Run `DROP TABLE aura.skill_audit` → permission denied.
- **Anti-mistake:** don't grant `aura_app` `pg_read_all_data` or `pg_write_all_data` roles "for convenience" during dev; once granted, hard to revoke.

**Warning signs.** `aura skills audit --recent` returns empty after a mutation that should have logged; row count of `aura.skill_audit` drops over time (it should monotone-grow).

**Phase to address:** Slice 0.5 (role separation), Slice 7c (TRUNCATE trigger).

**Severity:** P0 — forensic integrity loss; gate-skip events become un-detectable.

**Source:** PRD §1931, §1709, codebase CONCERNS.md §"Audit immutability via DB trigger"; Postgres docs on trigger granularity.

---

### Pitfall 7: Embedding model swap without re-indexing (Neo4j HNSW dimension mismatch)

**What goes wrong.** Aura is locked on `embeddinggemma-300m` 768d (memory `feedback_embedding_backend_stays_mistral`, PRD §3217). 6 months in, the team upgrades to `embeddinggemma-2-500m` 1024d "because it's better"; the embed sidecar redeploys; `chunk_embedding` HNSW index was created with `vector.dimensions: 768`; the next `ingest.file` succeeds (Neo4j 5.x silently rejects mismatched-dim inserts with no node-level failure), retrieval starts returning random / zero results because vector search rejects the query embedding with `IllegalArgumentException: dimension mismatch`. Industry reports: GitHub `neo4j/neo4j#13387` and `langchain-ai/langchain#16336` show real production hits like "Index query vector has 384 dimensions, but indexed vectors have 1536."

**Why it happens.** Dimension is baked into the index at creation time and `embeddinggemma-300m` truncates to MRL 256d (memory `feedback_embedding_backend_stays_mistral` notes 256d MRL truncation client-side), so even *the current setup* has two dim conventions floating around (768 native, 256 MRL). The default of "let the embedding model decide" silently breaks the contract.

**Prevention strategy.**
- **Gate 1 DoR (Slice 0.7):** make the dimension a tracked env var: `AURA_EMBED_DIMENSIONS=768`. The Neo4j migration `0001_init.cql` reads from a `migrations.env` checked-in file. Embed sidecar startup asserts `model.output_dim == AURA_EMBED_DIMENSIONS` else refuses to start.
- **Gate 1 DoR (Slice 11a):** add to acceptance "starting an embed sidecar with a different output dim than `AURA_EMBED_DIMENSIONS` fails fast with a clear error before serving any request."
- **Gate 3 DoD (Slice 11b):** integration test asserts that a corrupted index (wrong dim) makes `aura ingest` refuse to start (not silently fail per-document).
- **Add a runbook (`docs/runbooks/embedding-swap.md`)**: changing embedding model = (1) freeze ingestion; (2) `aura memory export`; (3) DROP+CREATE indexes with new dim; (4) re-embed everything; (5) re-ingest. There is **no** in-place upgrade.

**Warning signs.** `aura memory search "X"` suddenly returns 0 results or scores all 0.5 (= cosine on random vectors); Neo4j logs `IllegalArgumentException: Vector dimensions must be 768, was 1024`.

**Phase to address:** Slice 0.7 (embed sidecar boot check), Slice 11a (schema migration env-aware), Slice 11b (ingest dim guard).

**Severity:** P0 — silent corruption of memory retrieval; no error to user, just degraded results.

**Source:** [Neo4j issue #13387](https://github.com/neo4j/neo4j/issues/13387), [langchain issue #16336](https://github.com/langchain-ai/langchain/issues/16336), [Neo4j Vector Index docs](https://neo4j.com/docs/cypher-manual/current/indexes/semantic-indexes/vector-indexes/), PRD §3217.

---

### Pitfall 8: Secrets leak into Postgres `conversation_turns` or sidecar files

**What goes wrong.** A user pastes `OPENROUTER_API_KEY=sk-or-...` into a Telegram message. Aura stores the turn in `aura.conversation_turns.content` (PRD Slice 1.8). The secret is now in Postgres backups (`pg_dump`), in `$AURA_RUN_DIR/conversations/<id>/<seq>.content` sidecar files (PRD §4171), and in Telegram's server logs. Future `aura chat resume <conv_id>` rehydrates the turn, and the secret leaks into the LLM prompt → into OpenRouter logs (cache or otherwise).

**Why it happens.** Aura's persistence layer is a value-preserver: every byte the user types lands somewhere on disk. No secret-scanning hook on input. Telegram throttling (PRD §4197) optimizes display, not redaction. `pg_dump` backups (PRD §Backup strategy) export plaintext.

**Prevention strategy.**
- **Gate 1 DoR (Slice 1.8):** add input-side secret scanner using `gitleaks` patterns (or `detect-secrets` library, both well-known). On detection: replace the secret with `[REDACTED:type=api_key,len=N]` before persisting. Original ephemeral memory keeps the secret only for the current turn LLM call (if user is asking "what does this key do").
- **Gate 1 DoR (Slice 9b — Telegram):** same scanner runs at channel ingress; alert user via in-band reply "I detected what looks like an API key and stripped it from history."
- **Gate 3 DoD (Slice 0.5 + Slice 1.8):** `pg_dump`-based backup runbook says "encrypt at rest" — verify `pgcrypto` or filesystem-level dm-crypt is in the deployment runbook.
- **Anti-mistake:** never log raw user input at INFO level. Use `slog.With("turn_len", len(content))` not `slog.With("content", content)`. CI lint via `golangci-lint` custom rule (Trail of Bits `semgrep-rule-creator` skill).

**Warning signs.** Grep over a Postgres backup file finds `sk-`, `ghp_`, `xoxb-`, `AKIA`, `gho_` prefixes; Telegram channel logs (if user shares them) contain raw secrets.

**Phase to address:** Slice 1.8 (turn persistence), Slice 9b (Telegram channel), cross-cutting at Gate 3 DoD.

**Severity:** P0 — credential theft; downstream account compromise.

**Source:** OWASP ASVS V8 (data protection); `gitleaks`/`detect-secrets` patterns; inference from Aura's value-preserving persistence design.

---

## Critical Pitfalls (P0 continued — agentic loop correctness)

### Pitfall 9: Infinite tool-call loop without budget enforcement

**What goes wrong.** The model invokes `web_search → web_fetch → web_search → ...` indefinitely, or `swarm.spawn(reasoning) → child swarm.spawn(reasoning) → ...` recursively. Without a global step cap, the conversation burns through context + tokens + cost. 2026 industry research (Medium "Agentic Resource Exhaustion: The 'Infinite Loop' Attack") classifies this as the AI-era equivalent of fork bombs; mitigation requires "hard cap on thought steps (e.g. max 15)" + "strict global execution timeout (e.g. 60s)" + "de-duplication layer checking last 5 steps."

**Why it happens.** PRD has `MaxSteps` in `internal/agent/loop.go` (codebase CONCERNS.md §"Agent loop"), `AURA_SWARM_MAX_DEPTH=3` for spawn recursion (PRD §4260). But: no per-conversation **wall-clock** budget; no per-tool-name **invocation budget**; no de-dup of identical tool calls. The "3-strike rule" is for *humans* (CLAUDE.md), not for the agent.

**Prevention strategy.**
- **Gate 1 DoR (Slice 0.9 + Slice 1):** acceptance MUST include three orthogonal caps:
  1. `AURA_LOOP_MAX_STEPS` per Turn (default 25)
  2. `AURA_LOOP_MAX_WALLCLOCK_SEC` per Turn (default 300s)
  3. `AURA_LOOP_DEDUP_WINDOW` last-N tool calls to dedupe (default 3; if same `(tool_name, args_hash)` 3× in a row → force `text_response` + audit)
- **Gate 1 DoR (Slice 3):** per-child wall-clock + step budget propagated through `InvocationContext`. Children's children inherit parent's *remaining* budget, not a fresh one. Otherwise depth=3 with 25 steps each = 25 × 25 × 25 = 15625 steps.
- **Gate 3 DoD (Slice 1):** integration test "model in deliberate loop (mock that returns same tool_call every time) → loop terminates within `AURA_LOOP_MAX_STEPS` and emits `text_response` with error message; no goroutine leak via `goleak.VerifyNone`."
- **Gate 3 DoD (Slice 6):** scheduler-spawned `agent_job` MUST inherit step + wall-clock budget from `agent_job_runs` row; otherwise a cron-spawned loop runs forever.

**Warning signs.** `aura cache-stats` shows turn count > 100 for a single user message; OpenRouter cost dashboard shows a single conversation > $5; sidecar logs show same Python snippet executed >10 times in a row.

**Phase to address:** Slice 0.9 (Agent interface contract), Slice 1 (LlmAgent enforcement), Slice 3 (swarm child budget propagation), Slice 6 (scheduler-spawned job inheritance).

**Severity:** P0 — cost runaway + user-facing freeze.

**Source:** [Agentic Resource Exhaustion: The "Infinite Loop" Attack](https://medium.com/@instatunnel/agentic-resource-exhaustion-the-infinite-loop-attack-of-the-ai-era-76a3f58c62e3), [5 Production Scaling Challenges for Agentic AI 2026](https://machinelearningmastery.com/5-production-scaling-challenges-for-agentic-ai-in-2026/), PRD §4260.

---

### Pitfall 10: Context cancellation does not propagate through tool tree

**What goes wrong.** User hits Ctrl+C in `aura shell`. The top-level `context.Cancel` fires. But: the in-flight `web_fetch` HTTP request, the `sandbox.Runner.RunPython` subprocess, the `swarm.Coordinator.Join` blocking call all have their own context derived from a parent that **didn't** propagate cancellation. Result: subprocess keeps running for 30s (sandbox default timeout), HTTP request keeps reading 24KiB of HTML, sidecar consumes resources, user sees the shell hang.

**Why it happens.** Go context discipline is library-by-library. `http.Client` honors ctx if you pass `req.WithContext(ctx)`. `exec.CommandContext` honors ctx. `pgxpool.Conn().Query` honors ctx. But every dev forgets one. PRD §571 mandates "ctx-cancel propagation required end-to-end" for Slice 1; doesn't enforce it for Slices 2, 5, 6, 11.

**Prevention strategy.**
- **Gate 1 DoR (every slice introducing a tool):** acceptance MUST include a `TestToolCancellation_PropagatesCtx_<ToolName>` test that fires `ctx.Cancel()` mid-execution and asserts the tool returns `ctx.Err()` within 100ms.
- **Gate 1 DoR (Slice 0.9):** the `Agent.Run(ctx)` contract documents "implementations MUST propagate ctx to all child operations (HTTP, subprocess, DB, channel send/recv)" and the interface comment names the requirement.
- **Gate 3 DoD (Slice 1):** golangci-lint custom rule (or `staticcheck SA1029`/`SA5009`) that flags `http.NewRequest` without `Context()`, `exec.Command` (use `CommandContext`), `pgxpool.Query` without ctx arg.
- **Gate 3 DoD (cross-cutting):** `goleak.VerifyNone(t)` in TestMain catches leaked goroutines from incomplete cancellation. Must be in every package with goroutines (PRD §1455 lists slices 1/3/6/8/9/11/13 — extend to ALL slices with goroutines including 2b reaper, 7c cleanup, 7e analyzer, 11c community, 11e memify).

**Warning signs.** `aura shell` Ctrl+C takes > 2s to return prompt; `runtime.NumGoroutine()` baseline drifts upward over a long session; `aura sandbox sessions list` shows sessions in `active` state with `last_used_at` 5 minutes old (should be reaped or completed).

**Phase to address:** Slice 0.9 (contract), Slice 1 (LLM HTTP), Slice 2 (subprocess), Slice 5 (web_fetch HTTP), Slice 6 (cron), Slice 11 (Neo4j queries).

**Severity:** P0 — goroutine leaks → eventually OOM; user-perceived freeze undermines trust.

**Source:** PRD §571, §1455; Go context idioms; `goleak` (Uber) documentation; `staticcheck` SA1029.

---

### Pitfall 11: `ask_user` multi-pause deadlock under concurrent swarm children

**What goes wrong.** PRD §1287 supports `Coordinator.SpawnInteractive` — parent spawns N children that can each `ask_user`. Children's pauses accumulate in parent's `PausedState[]` FIFO. Bug: if child A pauses, parent serializes the question to user, user responds, parent calls `Coordinator.ResumeChild(A, answer)` — but between B pausing and parent attempting to enqueue B's question, the parent's `LlmAgent.Run` has already returned (it sees only A's pause as "resolved", doesn't know B is queued). User never sees B's question; B is `ctx.Done`-stuck waiting for a `Responder` that will never fire.

**Why it happens.** Multi-pause FIFO is non-trivial: who owns the queue (parent vs Coordinator)? When does the parent stop and serialize vs continue? PRD §1287 says "RWMutex on children map" and "test N=10 children paused" but doesn't specify the *liveness* property — every paused child must eventually receive an answer OR a propagated ctx-cancel.

**Prevention strategy.**
- **Gate 1 DoR (Slice 1.5 + Slice 3):** spec the liveness invariant: "for every child in `children[]` with status=paused, either (a) parent eventually calls `ResumeChild(id, answer)` after user resolves the question, OR (b) parent ctx-cancels and child receives `ctx.Done()` within 100ms. No child can be stuck-paused forever."
- **Gate 1 DoR (Slice 3):** acceptance test: `TestSpawnInteractive_MultiPause_AllResolved` — 5 children all `SpawnInteractive`, all pause simultaneously, user responds to 3 + ctx-cancels 2; assert all 5 children either resumed or cancelled within 1s.
- **Gate 3 DoD (Slice 3):** the `Coordinator.Join(id)` semantics: if `id` is paused, `Join` blocks; if parent ctx cancels while child paused, `Join` returns `ctx.Err()` AND child receives cancel.
- **Anti-mistake:** don't conflate `ParallelAgent` (Slice 0.9 — runs N agents to completion concurrently) with `SpawnInteractive` (Slice 3 — children may pause mid-run). They share infrastructure but have different liveness needs.

**Warning signs.** `aura sandbox sessions list` shows sessions stuck in `active` with no recent activity (= child paused, never resumed); `runtime.NumGoroutine()` shows N goroutines waiting on channel with no producers.

**Phase to address:** Slice 1.5 (multi-pause FIFO), Slice 3 (Coordinator child propagation).

**Severity:** P0 — child goroutine + container leak, resource exhaustion over time.

**Source:** PRD §1287-1291; Go channel deadlock patterns; inference from "Coordinator.ResumeChild + Spawn + Join share RWMutex" spec.

---

### Pitfall 12: Tool result truncation poisons context (preview vs full divergence)

**What goes wrong.** Slice 1 introduces `ToolResult` with preview + sidecar (PRD §4147-4152). Pattern: if result > `AURA_CONTEXT_PREVIEW_CAP_BYTES=2048`, model sees preview + "Use read_tool_output(...)". The agent decides — sometimes — to call `read_tool_output`; often it just reasons from the preview. But: the preview is **the first 2048 bytes**, which for a CSV is the header + first 20 rows, missing the row that actually answered the question. Or for a Markdown doc, it's the TOC, not the content. Agent confidently answers wrong.

**Why it happens.** "First N bytes" is the obvious truncation but the wrong semantics for structured data. The Claude Code pattern (PRD §4331) handles this for `WebFetch` by also persisting full content + a `read_tool_output` tool — but the agent has to *know* to use it. Default LLM behavior: trust what you see.

**Prevention strategy.**
- **Gate 1 DoR (Slice 1):** preview generator is content-type-aware. For `text/csv` → header + 3 sample rows + "[N more rows]". For `text/markdown` → first heading hierarchy + summary line. For JSON → top-level keys + counts. For unknown → first N bytes + last N bytes (catches truncation point).
- **Gate 1 DoR (Slice 1):** preview footer is **active**, not passive. Instead of "Use read_tool_output to fetch ranges" (passive), say "The full result is N bytes. The preview shown is rows 1-20 of M. To find specific content, use `read_tool_output(call_id=X, query='your search')` (returns matching ranges)." Reduces "I'll reason from preview" failure mode.
- **Gate 3 DoD (Slice 1):** test "agent given a doc with answer in row 5000; agent must invoke `read_tool_output` to find it" — failure mode is agent answers from preview alone.

**Warning signs.** `aura cache-stats` correlates with `read_tool_output` call count = 0; user reports "Aura said X but the data clearly shows Y"; sidecar file `$AURA_RUN_DIR/<id>/<call>.result` exists but `read_tool_output` audit log shows zero reads.

**Phase to address:** Slice 1 (preview semantics).

**Severity:** P1 — silent wrong answers (P0 if data is decision-critical, e.g. financial / medical).

**Source:** PRD §4147-4152, §4331; Claude Code's WebFetch pattern; inference from typical LLM "trust the visible" failure mode.

---

## P1 Pitfalls (data corruption / silent broken behavior)

### Pitfall 13: Sidecar boot order race — Aura serves before Postgres/Neo4j/embed ready

**What goes wrong.** `docker-compose up` brings up `aura`, `aura-postgres`, `aura-neo4j`, `aura-llama-embed`, `aura-llama-multimodal` in parallel. `aura serve` starts, opens TCP listener, accepts a Telegram message, tries to ingest → Neo4j returns "DB starting", embed sidecar returns 503, Postgres connection times out. Aura crashes OR (worse) silently returns "I had a problem, try again" to user. Pre-rewrite memory `feedback_minipc_cpu_budget` hints at multi-sidecar coordination cost.

**Why it happens.** Compose `depends_on` defaults to "service started" (process running), not "service healthy". Even with `condition: service_healthy`, the health check needs to exist AND be meaningful. Neo4j boot can take 30-60s with GDS plugin; embed sidecar warm-up loads model into RAM (~2-3s on CPU).

**Prevention strategy.**
- **Gate 1 DoR (Slice 0.5 + Slice 0.7 + Slice 9c):** compose `depends_on` uses `condition: service_healthy` for every sidecar. Each sidecar exposes `/health` returning 200 only when actually serving (Neo4j: a cheap Cypher query; embed: a 1-token embed; multimodal: a 1-token completion).
- **Gate 1 DoR (Slice 0.9 + Slice 1):** Aura process startup blocks for `up to AURA_STARTUP_HEALTHCHECK_TIMEOUT_SEC=120` waiting for each dependency. On timeout: clear error + exit 1, not silent degradation.
- **Gate 3 DoD (cross-cutting):** integration test (smoke under `compose_integration` build tag) brings the stack up fresh and asserts `aura health` returns "all green" within 90s; under load (first user message arriving in second 5), no 503s.
- **Anti-mistake:** don't substitute `sleep 30 && start-aura.sh`. Sleep is a hint, not a guarantee.

**Warning signs.** First user request after restart fails with cryptic error; Aura logs show successive `503` from embed sidecar in first 60s; Telegram bot sends "I had a problem" on initial messages.

**Phase to address:** Slice 0.5 (Postgres healthcheck), Slice 0.7 (Neo4j + embed healthcheck), Slice 9c (multimodal), Slice 13 (vLLM if landed).

**Severity:** P1 — first-request failures degrade trust; root cause invisible to user.

**Source:** Docker Compose `condition: service_healthy` semantics; Neo4j slow-boot (GDS plugin) common knowledge; PRD §3933-3936 cumulative idle stack.

---

### Pitfall 14: Postgres `FOR UPDATE SKIP LOCKED` cron race with stale connections

**What goes wrong.** Slice 6 uses `FOR UPDATE SKIP LOCKED` for cron worker concurrency (PRD §1474). Worker A acquires task `T1`, starts executing (5 min wall-clock). Worker A's pgx connection silently drops (TCP keepalive or network blip). Postgres releases the row lock. Worker B picks up `T1`, executes it again. Side-effecting `agent_job` runs twice → duplicate `web_search` charges, duplicate Telegram messages, duplicate Postgres writes.

**Why it happens.** Postgres advisory locks survive transaction commit; row-level locks die with the holding transaction. If the holding connection drops, the lock dies before the worker finishes its work. `pgxpool` defaults reconnect transparently — the worker thinks it still has the lock, but it doesn't.

**Prevention strategy.**
- **Gate 1 DoR (Slice 6):** every long-running task acquires a **session-level advisory lock** (`pg_try_advisory_lock(task_hash)`) at start, releases at end. Advisory locks die with the connection BUT a second worker calling `pg_try_advisory_lock` on the same hash gets `false` if the connection is still alive; if the original connection dropped, second worker gets `true` and proceeds (correct behavior: original is dead).
- **Gate 1 DoR (Slice 6):** combine: `FOR UPDATE SKIP LOCKED` (initial pickup) + `pg_try_advisory_lock(task_id_hash)` (continuous ownership) + heartbeat update to `aura.agent_job_runs.last_heartbeat_at` every 30s. Recovery query at boot also looks for tasks with `last_heartbeat_at < now() - 90s` and marks them `unknown_recovery`.
- **Gate 3 DoD (Slice 6):** chaos test: 3 workers, network partition one worker for 60s, assert task is picked up by another worker AND no double-execution side effects (idempotency check on `agent_job_runs.completed_with_hash`).
- **Anti-mistake:** don't rely on `keepalive_idle_sec` alone — TCP keepalive is OS-level and unreliable across NAT.

**Warning signs.** `aura task audit --recent` shows the same `(task_id, dispatched_at)` twice with different `run_id`; user reports "I got 2 copies of the news summary"; OpenRouter cost shows 2× the expected daily spend.

**Phase to address:** Slice 6.

**Severity:** P1 — double side-effects (cost, user-visible duplicates).

**Source:** PRD §1474, §1712-1722; Postgres advisory lock semantics; `pgxpool` reconnect docs.

---

### Pitfall 15: Entity resolution race during concurrent ingestion (Slice 11b)

**What goes wrong.** User uploads 3 documents simultaneously via Telegram. Each spawns a `Pipeline.Ingest` goroutine. Document A mentions "Mario Rossi" (Person). Document B also mentions "Mario Rossi". Both pipelines reach Fase 2 (PRD §3281) "MERGE existing OR CREATE new" — both see no existing entity, both CREATE. Result: two `:Entity {name: 'Mario Rossi', type: 'Person'}` nodes; future queries return one, miss the other; community detection (Slice 11c) splits them; retrieval recall@5 drops.

**Why it happens.** "MERGE existing" pattern needs a serializable boundary. mem0's 2-fase pipeline (PRD §3278) does fuzzy match + embedding similarity > 0.92, but the check-then-create is not atomic in Neo4j without `CREATE CONSTRAINT ... IS UNIQUE` or `apoc.lock`.

**Prevention strategy.**
- **Gate 1 DoR (Slice 11a):** schema declares `CREATE CONSTRAINT entity_unique FOR (e:Entity) REQUIRE (e.name, e.type) IS UNIQUE` (lowercased, NFKC-normalized name). Concurrent CREATE on same key → one succeeds, others get constraint violation → retry with MERGE.
- **Gate 1 DoR (Slice 11b):** entity insert uses Cypher `MERGE (e:Entity {name: $normalized_name, type: $type}) ON CREATE SET e.embedding = $embedding, e.first_seen_at = ts() ON MATCH SET e.mention_count = e.mention_count + 1, e.last_mentioned_at = ts()`. The MERGE is atomic per-transaction; constraint guarantees no duplicates cross-transaction.
- **Gate 3 DoD (Slice 11b):** chaos test: 10 goroutines simultaneously ingest documents all mentioning "Mario Rossi" with slight name variations ("Mario Rossi", "mario rossi", "Mario  Rossi" with double space). Assert one canonical entity exists after all complete. Coverage: NFKC + whitespace normalize + case-fold before MERGE.
- **Anti-mistake:** don't rely on the embedding-similarity threshold (`> 0.92`) as the only dedup signal. Embeddings of identical strings are deterministic, but Aura uses MRL truncation (memory `feedback_embedding_backend_stays_mistral`), and quantization can flip cosine score over a 0.001 boundary.

**Warning signs.** Neo4j browser query `MATCH (e:Entity) WITH e.name AS name, count(*) AS c WHERE c > 1 RETURN name, c` returns rows; entity mention counts grow more slowly than expected; community detection produces many small communities for what should be merged entities.

**Phase to address:** Slice 11a (constraint), Slice 11b (MERGE pattern).

**Severity:** P1 — silent memory fragmentation; degraded retrieval.

**Source:** PRD §3278, §3214; Neo4j unique constraint docs; mem0 2-fase deduplication pattern.

---

### Pitfall 16: HNSW recall@k regression from index tuning defaults

**What goes wrong.** Neo4j HNSW vector index created with defaults: `M=16, efConstruction=100`. At 10K chunks, recall@5 is excellent (~95%). At 100K chunks, recall@5 drops to ~70% because default `M=16` is too low; need `M=32` and `efConstruction=200`+. Neo4j docs: "vector indexes are super memory intensive"; production tuning required. Aura's spike (memory `project_neo4j_spike_2026-05-27`) validated 22-30ms p95 at small scale — doesn't guarantee scale-out behavior.

**Why it happens.** HNSW parameters bake into the index at creation. Re-tuning = DROP + recreate + re-embed-no-just-re-index. PRD §3217-3225 specifies dimension + similarity_function but leaves `M` / `efConstruction` to defaults. Aura's quality gate (PRD §1480) says "NDCG@5 ≥ 0.8 on eval corpus" but the eval corpus size is undefined.

**Prevention strategy.**
- **Gate 1 DoR (Slice 11a):** schema explicitly sets `OPTIONS {indexConfig: {`vector.dimensions`: 768, `vector.similarity_function`: 'cosine', `vector.hnsw.m`: 32, `vector.hnsw.ef_construction`: 200}}`. Document the tuning rationale in `docs/runbooks/neo4j-vector-tuning.md`.
- **Gate 1 DoR (Slice 11d):** add to acceptance "recall@5 ≥ 0.8 on a 10K-chunk synthetic corpus AND a 100K-chunk synthetic corpus" — proves index scales.
- **Gate 3 DoD (Slice 11d, pre-merge benchmark, listed by CONCERNS.md):** beyond the existing 512 vs 1024 chunk benchmark, measure recall@5 + NDCG@10 + p95 latency at 1K, 10K, 100K chunks. Track in `docs/aura-quality-snapshot.md` (per memory `feedback_aura_as_product`).
- **Anti-mistake:** don't trust spike results (Neo4j spike used a small corpus). Defer-test at production scale.

**Warning signs.** `aura quality-snapshot --since=30d` shows recall@5 trending down; user reports "Aura used to find my old notes, now it doesn't"; Neo4j `dbms.queryJmx` shows vector index page-cache misses climbing.

**Phase to address:** Slice 11a (index config), Slice 11d (recall benchmark at scale).

**Severity:** P1 — silent retrieval degradation; user trust erosion.

**Source:** [Neo4j Vector Index docs](https://neo4j.com/docs/cypher-manual/current/indexes/semantic-indexes/vector-indexes/), [Neo4j Vector Index memory ops](https://neo4j.com/docs/operations-manual/current/performance/vector-index-memory-configuration/), HNSW paper (Malkov & Yashunin), Aura spike memory.

---

### Pitfall 17: Compose file drift across dev / CI / production

**What goes wrong.** `compose.yaml` accumulates dev-only flags (`AURA_DB_URL=postgres://...:5432/aura_dev`, port-forwards, bind-mounts of source code). CI uses a different compose file. Production is "the same as dev mostly". An env var added to fix dev breaks prod silently (e.g. `AURA_SANDBOX_NETWORK_ALLOW_HOSTS` set to `*` for testing, accidentally shipped).

**Why it happens.** Compose files grow organically; "we'll split it later" never happens. The audit gate (Gate 3 DoD) reviews code diffs, not infra diffs.

**Prevention strategy.**
- **Gate 1 DoR (Slice 0.5):** project starts with `compose/base.yaml` (shared) + `compose/dev.override.yaml` (dev-only) + `compose/prod.override.yaml` (prod hardening: read-only filesystems, dropped capabilities, smaller resource limits). `docker compose -f base.yaml -f dev.override.yaml` for dev; `... -f prod.override.yaml` for prod.
- **Gate 1 DoR (cross-cutting):** any new env var added to compose MUST be added to **base.yaml** with a comment `# REQUIRED in prod` or to **override** with `# DEV ONLY`. CI lint: `scripts/check_compose_drift.sh` greps for env vars in dev/prod but not base.
- **Gate 3 DoD (cross-cutting):** `aura serve --print-config` dumps resolved config (with secret values masked); reviewer compares dev vs prod and flags unexpected diffs.
- **Anti-mistake:** never `docker compose up -d --build` in prod and call it deployed. Use immutable image tags pinned via `docker compose pull` from a registry.

**Warning signs.** "Works in dev" issues that resurface in prod 2 weeks later; `git diff main compose.yaml` shows unrelated changes bundled together; secrets check-in incidents (gitleaks pre-commit fires).

**Phase to address:** Slice 0.5 (compose foundation), cross-cutting Gate 3 DoD enforcement.

**Severity:** P1 — production drift causes silent misbehavior + security regression.

**Source:** Docker Compose merge semantics; common ops anti-pattern; PROJECT.md §82 (Docker Desktop dev/prod parity).

---

### Pitfall 18: Telegram MarkdownV2 escape bug exposes chat to format injection

**What goes wrong.** Slice 9b sends model output as Telegram MarkdownV2. User asks "show me the regex `[a-z]+`" — model replies with the regex. Aura naively wraps in MarkdownV2; the unescaped `[` and `]` cause Telegram to interpret as a link. Worse: model output containing `[click](javascript:alert(1))` becomes a clickable link. On Telegram Desktop, the link is masked.

**Why it happens.** MarkdownV2 has 18 reserved chars that ALL need backslash escape (`_ * [ ] ( ) ~ \` > # + - = | { } . !`). Most libraries escape only some. Plain string concat is the wrong approach.

**Prevention strategy.**
- **Gate 1 DoR (Slice 9b):** acceptance requires content escaped via a canonical `telegramutil.EscapeMarkdownV2(s)` helper that escapes all 18 reserved chars. Code review checklist: no `tg.Send(text)` without `EscapeMarkdownV2(text)`.
- **Gate 1 DoR (Slice 9b):** prefer `parse_mode: HTML` with `html.EscapeString` (only 4 chars to escape) over MarkdownV2 — fewer footguns. Format-rich rendering done via HTML tags (`<b>`, `<i>`, `<code>`, `<pre>`).
- **Gate 3 DoD (Slice 9b):** fuzz test on `EscapeMarkdownV2` — every input must produce a string that, when sent to Telegram test API, returns no `BAD_REQUEST: can't parse entities`. Plus negative test: a payload that *would* inject a link must render as literal text.
- **Anti-mistake:** never let model output `[link](url)` syntax reach Telegram unescaped. Even if the user asked for a link, the URL must come from a verified source (e.g. `web_search` result with known-safe domains).

**Warning signs.** Telegram returns `400 Bad Request: can't parse entities`; user reports "Aura sent me a link I didn't expect."

**Phase to address:** Slice 9b.

**Severity:** P1 — phishing risk; user-trust regression.

**Source:** Telegram Bot API MarkdownV2 spec; common bot-development pitfall.

---

## P2 Pitfalls (UX degradation / maintenance pain)

### Pitfall 19: `AURA_RUN_DIR` fills the disk silently

**What goes wrong.** Sidecar tool results, spillover content, sandbox workspaces all accumulate in `$AURA_RUN_DIR/` (PRD §4173-4179, §4245-4246). The `AURA_RUN_DIR_WARN_THRESHOLD_BYTES=1073741824` is a WARN-only at boot; cleanup is cascade on `aura chat delete` or orphan-scan at boot. Real-world: user never deletes chats; orphan scan only finds dirs without DB FK. Disk fills over months; first symptom is `pg_dump` backup fails on disk-full.

**Why it happens.** "Cleanup on user action" relies on user action. Boot-time WARN log is invisible. No periodic GC.

**Prevention strategy.**
- **Gate 1 DoR (Slice 1.8):** acceptance MUST include "background goroutine sweeps `$AURA_RUN_DIR/conversations/<id>/` every 24h; for each conv_id, if Postgres has `conversations.status='archived'` AND `archived_at < now() - AURA_RUN_DIR_RETENTION_DAYS=30`, delete the dir (after re-confirming no FK)."
- **Gate 1 DoR (Slice 1.8):** add `AURA_RUN_DIR_HARD_CAP_BYTES=10737418240` (10 GiB). At cap: refuse new sidecar writes, return tool error "Aura's runtime dir is full; run `aura disk cleanup`."
- **Gate 3 DoD (Slice 1.8):** chaos test: fill `$AURA_RUN_DIR` to 90% via simulated tool calls; assert next tool call returns `ENOSPC`-friendly error (not panic), AND cleanup goroutine reduces usage on next tick.

**Warning signs.** `df -h /var/lib/aura` shows >80% utilization; Aura's `Notifier` fires "consider archive/delete"; `pg_dump` backup fails.

**Phase to address:** Slice 1.8.

**Severity:** P2 — eventually data loss (backup fails). P0 if it cascades to Postgres unable to write WAL.

**Source:** PRD §4173-4179; common ops anti-pattern; "cleanup on user action" failure mode.

---

### Pitfall 20: Background goroutine fleet fragments the same shared resource

**What goes wrong.** CONCERNS.md §"Background goroutines" lists 8 concurrent timers (memify 24h, leiden 24h, skill ttl 24h, skill pattern 60min, insight 60min, offline 30s, scheduler 30s, pending cleanup 1h). At 02:00 UTC, memify + leiden + skill ttl fire simultaneously, all hitting Neo4j with heavy GDS workload. Neo4j heap spikes; concurrent LLM `tier=worker` calls saturate OpenRouter rate limit; user sends a chat message → Aura is unresponsive for 30s.

**Why it happens.** "Periodic background work" defaults to "fire at fixed-period from boot time" — all goroutines started at `aura serve` boot will tick at the same offsets. No jitter, no resource isolation.

**Prevention strategy.**
- **Gate 1 DoR (Slice 11c, 11e, 7e):** background loops use `time.Sleep(interval + jitter)` where `jitter = rand.Int63n(interval/4)`. Documented in code.
- **Gate 1 DoR (Slice 0.9):** Aura defines a `BackgroundAgent` shared `semaphore.Weighted` capping concurrent heavy operations (LLM-tier-reasoning calls, Leiden runs, embed batches) at `AURA_BACKGROUND_CONCURRENCY=2`. User-facing requests bypass this semaphore.
- **Gate 3 DoD (Slice 11c):** load test: trigger all 8 timers at `t=0` (via env override of `*_INTERVAL_*`); assert no user-facing request latency > 500ms during the spike.
- **Anti-mistake:** don't run periodic tasks at `time.Now()` aligned to UTC midnight or hour boundaries — Aura's mini-PC may have a daily backup at midnight that collides.

**Warning signs.** User reports "Aura was super slow last night around 2 AM"; Neo4j heap saturation alert (if monitoring); OpenRouter rate limit `429` spike.

**Phase to address:** Slice 0.9 (BackgroundAgent contract), Slices 7e/11c/11e (jitter implementation).

**Severity:** P2 — UX hiccups; P1 if it cascades to a goroutine-leak-causing OOM.

**Source:** PRD §4291-4295, CONCERNS.md §"Background goroutines"; Erlang OTP supervision-tree jitter pattern; common cron stampede pattern.

---

### Pitfall 21: PRD-drift — code lands without PRD amendment

**What goes wrong.** Mid-Slice 7c, dev realizes the `gate_taken` semantics don't quite cover the "user rejected via CLI 2 hours later" case. They tweak the code to add an `approval_source='cli_late'` enum value. Code commits. PRD still says enum is `{ask_user, cli, auto}`. 3 months later, a new contributor reads PRD, implements `Slice 11` audit table with the old enum; runtime errors surface only in production. The PRD-first principle is silently violated.

**Why it happens.** "I'll update PRD later" never happens. Gate 3 DoD reviewer notices code but not PRD-spec mismatch.

**Prevention strategy.**
- **Gate 1 DoR (every slice):** the "commit template" (PRD §1149-1168 et al.) reminds: "if implementation diverged from PRD, PRD-amendment commit MUST precede code commit."
- **Gate 3 DoD (every slice):** add to checklist "diff PRD section for this slice against implementation; flag any spec-vs-code drift; either fix code or amend PRD as a SEPARATE commit before this slice merges." Use `gsd-codebase-mapper` to keep `.planning/codebase/` in sync.
- **CI safety net:** monthly `/gsd-graphify` run produces `.planning/graphs/` snapshot; reviewer compares to PRD glossary.
- **Anti-mistake:** never `git commit -am` mixing code + PRD changes. PRD = its own commit, with `prd:` prefix.

**Warning signs.** `git log --grep='prd:'` shows long gaps; PRD line count drifts down (not up) over time — implementations are adding silent complexity not back-ported; new contributor onboarding questions reveal "the PRD says X but the code does Y."

**Phase to address:** Cross-cutting Gate 1 + Gate 3 of every slice.

**Severity:** P2 — knowledge debt; eventual P1 when next dev makes wrong decision based on stale PRD.

**Source:** PRD §"Slice Q&A discipline → Q&A revision protocol"; CLAUDE.md §PRD-first principle; common documentation rot pattern.

---

### Pitfall 22: Tests modified to pass instead of code fixed

**What goes wrong.** A flaky test fails 1/10 runs. Dev "investigates", concludes "timing issue", changes `assert.Eventually(..., 100ms)` to `assert.Eventually(..., 5s)`. Test passes 10/10 now. Underlying bug (race condition in goroutine cleanup) ships. Production: 1/1000 user requests hangs for 5 seconds.

**Why it happens.** Time pressure. CI red light is irritating. CLAUDE.md says "NEVER MODIFY TESTS TO MAKE THEM PASS" but the rule is easy to rationalize around ("the test was wrong, I'm fixing the test").

**Prevention strategy.**
- **Gate 2 (Implementation Q&A):** PR template includes "Did you change any `*_test.go` files? If yes, justify why the test was wrong (not the code)."
- **Gate 3 DoD:** code review explicitly asks for test changes diff; reviewer must approve test changes with same scrutiny as production code.
- **CI safety net:** `scripts/check_test_diff_justification.sh` — if PR touches `*_test.go` but commit message doesn't include `test:` prefix or a "Why this test changed:" body, flag for human review.
- **Anti-mistake (per CLAUDE.md):** the 3-strike rule — same failing test 3× = stop, escalate to PRD-amendment, do not loosen test thresholds.

**Warning signs.** Tests that became gradually slower (5s → 30s timeouts); coverage stays high but mutation testing score drops; "flaky test" excuses in PR descriptions.

**Phase to address:** Cross-cutting Gate 2 + Gate 3.

**Severity:** P2 — quality erosion; eventual P0 when the unfixed bug bites production.

**Source:** CLAUDE.md §Behavioral rules; PRD §"Test discipline rigorosa"; common AI-assisted-dev failure mode.

---

### Pitfall 23: Logs without correlation IDs across sidecars

**What goes wrong.** User reports "Aura was slow at 14:32". Logs across `aura`, `aura-postgres`, `aura-neo4j`, `aura-llama-embed`, `aura-llama-multimodal`, `aura-sandbox` need to be correlated. Without a request ID propagated through HTTP headers + structured log fields, you're grepping by timestamp ± 1s and guessing.

**Why it happens.** Structured logging is "we'll add it later". OTel propagation requires wiring at every boundary.

**Prevention strategy.**
- **Gate 1 DoR (Slice 0.9 + Slice 1):** `InvocationContext` carries `request_id` (UUIDv7 for sortable). Every log line in every package uses `slog.With("request_id", ctx.RequestID())`. HTTP requests to sidecars carry `X-Aura-Request-ID: <id>` header. Sidecar Python `sidecar.py` logs include the header.
- **Gate 1 DoR (Slice 8):** AG-UI events include `request_id` in metadata; UI can correlate streaming events to backend logs.
- **Gate 3 DoD (Slice 1):** integration test: send a request, grep all sidecar logs for the request_id, assert ≥3 services have at least 1 log line tagged. Coverage: cross-sidecar tracing works.
- **Anti-mistake:** don't use `time.Now()` as a poor-man's correlation — request IDs are cheap, timestamps are ambiguous.

**Warning signs.** Debug session takes >30 min to trace a single user request; "I can't tell which Neo4j query came from which Aura request"; on-call runbook says "grep by timestamp."

**Phase to address:** Slice 0.9 (InvocationContext design), Slice 1 (LLM HTTP), every sidecar interaction.

**Severity:** P2 — debug velocity; turns into P1 when a real incident takes 4× longer to resolve.

**Source:** OpenTelemetry conventions; `slog` patterns; `samber/cc-skills-golang/golang-observability` skill.

---

### Pitfall 24: Backup strategy validated only at first data loss

**What goes wrong.** PRD §Backup strategy says `pg_dump` + `neo4j-admin database dump`. No actual restore test. 6 months in, disk corruption; restore fails because (a) `pg_dump` was run without `--no-owner --no-acl` and target DB has different roles, (b) Neo4j dump version mismatch with new Neo4j image, (c) embedding model has changed and vector index needs re-build (Pitfall 7 cascade), (d) Telegram bot token is in `.env` not in backup.

**Why it happens.** Backups are write-once-never-read; the path is exercised when it's too late.

**Prevention strategy.**
- **Gate 1 DoR (Slice 0.5 + Slice 0.7):** acceptance includes "restore drill": script `scripts/restore_drill.sh` boots a fresh stack from yesterday's backup; runs `aura health` + `aura chat resume <known_id>` + `aura memory search "<known_query>"`; all must pass. CI runs nightly.
- **Gate 1 DoR (Slice 0.5):** `pg_dump --no-owner --no-acl --clean --if-exists` (idempotent restore) + role separation (Pitfall 6 prevention).
- **Gate 3 DoD (cross-cutting):** disaster recovery runbook in `docs/runbooks/disaster-recovery.md` — version-pin every component (Postgres image, Neo4j image, embed model checkpoint).
- **Anti-mistake:** don't backup just the data — backup the **config**: `.env` (encrypted), compose files, Neo4j config, sandbox seccomp profile. Versioned together.

**Warning signs.** Last "restore test" is "never"; backup file timestamps prove backups run, but no log entry proves they're verifiable; first attempted restore is during an incident.

**Phase to address:** Slice 0.5 + Slice 0.7 (backup foundations), Gate 3 DoD permanent.

**Severity:** P2 normally; P0 the day you need it.

**Source:** PRD §Backup strategy; ops common knowledge; "3-2-1 backup rule"; Postgres `pg_dump` flags.

---

### Pitfall 25: Telegram polling collision (two bot instances)

**What goes wrong.** Dev runs `aura serve` locally for testing while production `aura serve` runs in the mini-PC; both use the same `TELEGRAM_BOT_TOKEN`. Telegram bot API allows only one `getUpdates` listener at a time. Half the user's messages go to dev, half to prod, randomly. Either both instances log "I got message" then both reply (user sees 2 replies), or one wins and the other gets `409 Conflict`.

**Why it happens.** Bot tokens are scarce (one per BotFather creation); reuse is normal in dev; "I'll use a different bot token" friction is real.

**Prevention strategy.**
- **Gate 1 DoR (Slice 9b):** documentation explicitly: "DO NOT share TELEGRAM_BOT_TOKEN across environments. Create a separate `@aura_dev_bot` for development; production uses `@aura_bot`."
- **Gate 1 DoR (Slice 9b):** Aura startup probes Telegram for active webhook + polling state; if conflict detected, log clear error + exit. Use `getWebhookInfo` + check `pending_update_count` on boot.
- **Gate 3 DoD (Slice 9b):** integration test that starts two Aura instances with the same fake token (against a mock Telegram API) and asserts second instance exits with a recognizable error code.
- **Anti-mistake:** don't switch between polling and webhook modes silently; pick one (PRD does not yet decide — flag for Slice 9b DoR).

**Warning signs.** Telegram logs show `409 Conflict: terminated by other getUpdates request`; user reports "Aura sends 2 replies to one message"; sporadic message drops.

**Phase to address:** Slice 9b.

**Severity:** P2 — UX confusion + duplicate-action bugs.

**Source:** Telegram Bot API docs (`getUpdates` exclusivity); common bot-dev pitfall.

---

### Pitfall 26: Scope creep — reaching for marketplace / Windows-native / multi-user before substrate stable

**What goes wrong.** Slice 7d skill installer works. Excitement → "let's build a public skill marketplace!" Or: "Postgres + Neo4j on Windows native would unblock another user." Or: "let's add Discord channel before Telegram is hardened." Each of these is explicitly **Out of Scope** (PROJECT.md §48-56) but the temptation is real. Result: substrate fragility (Telegram unfinished, sandbox 2b not battle-tested) is masked by new-feature dopamine. 3 months later, a Telegram bug bites and there's also Discord broken.

**Why it happens.** Hard things stay hard; easy-sounding things look easy. "Just add" never is.

**Prevention strategy.**
- **Gate 1 DoR (every slice):** Out of Scope explicit reaffirmation: "this slice does NOT add: [marketplace, Windows native, multi-user auth, voice output, second non-Telegram channel]." Reviewer crosschecks against PROJECT.md.
- **Gate 1 DoR (process):** the GSD `/gsd-discuss-phase` adaptive questioning explicitly asks "what does this slice tempt you to add that's actually out of scope?" — surfaces creep early.
- **Roadmap:** Slices ordered such that "substrate hardening" comes before "new horizons." Per PROJECT.md §10, the substrate is the deliverable; "personal AI" is one configuration of it.
- **Anti-mistake (per memory `user_finishes_what_starts`):** Davide commits to finishing — but commitment != solo grind. Frustration is OK; abandoning current substrate work for new shiny is NOT.

**Warning signs.** Slice X has been "almost done" for 3 weeks while Slice X+5 prototype exists; PROJECT.md "Out of Scope" list shrinks; new env vars appear for features not in PRD; `git log --since=14d --grep='discord\|slack\|windows\|marketplace'` returns results.

**Phase to address:** Process — every Gate 1 DoR.

**Severity:** P2 — slow erosion of substrate quality; eventual P1 when a half-built feature shipped breaks the core.

**Source:** PROJECT.md §Out of Scope (48-56), CLAUDE.md §SCOPE CONTROL, memory `feedback_aura_is_platform_shaped` + `user_finishes_what_starts` + `feedback_aura_as_product`.

---

### Pitfall 27: Pre-merge benchmarks deferred indefinitely

**What goes wrong.** CONCERNS.md §"Pre-Merge Benchmarks Required" lists 5 benchmarks (vLLM CPU vs GPU, chunk size, Gemma 4 variant, re-ranker, CLI mode). PRD §1501 DoR says "if OQ is 'pre-merge benchmark', the benchmark is executed and result documented." But: benchmarks are tedious to set up; "we'll skip the bench for now and decide later" becomes the path of least resistance; default values land in production unmeasured.

**Why it happens.** Benchmarks are work; defaults are free; nobody is the squeaky wheel for "the chunk size hasn't been benchmarked."

**Prevention strategy.**
- **Gate 1 DoR (slice with pre-merge bench OQ):** "benchmark run + result row added to `docs/aura-quality-snapshot.md`" is a literal checkbox. No checkbox = slice cannot enter Gate 2.
- **Gate 3 DoD (slice with pre-merge bench OQ):** verify the benchmark result was used to set the production default (e.g. if `AURA_MEMORY_CHUNK_SIZE_TOKENS=512` is the production default, the benchmark must show 512 beats 1024 on the chosen metric, with the metric and corpus documented).
- **Living quality doc** (per memory `feedback_aura_as_product`): `docs/aura-quality-snapshot.md` updates on every relevant merge; CI gate fails if a slice closes a bench OQ without updating the snapshot.
- **Anti-mistake:** "default OK" in CONCERNS.md is acceptable ONLY for "default-OK" OQs; bench-OQs require numbers.

**Warning signs.** `docs/aura-quality-snapshot.md` doesn't exist or hasn't been touched in 30d; slice merged with OQ status "to be benchmarked"; production runs with the literal default of a never-measured env var.

**Phase to address:** Cross-cutting Gate 1 + 3.

**Severity:** P2 normally; can become P0 (Pitfall 7 — wrong embedding dim) or P1 (Pitfall 16 — wrong HNSW M) when defaults wrong.

**Source:** CONCERNS.md §"Pre-Merge Benchmarks Required"; PRD §1501; memory `feedback_aura_as_product` and `feedback_aura_quality_snapshot`.

---

### Pitfall 28: AG-UI SSE reconnect logic missing → silent UI freeze

**What goes wrong.** AG-UI gateway (Slice 8) streams SSE. Client browser sleeps, wakes, SSE connection dropped. Default `EventSource` reconnects, but if Aura's `/agent/run` is per-request (not resumable), the in-flight turn is lost. User sees the page frozen on "thinking..."; refreshing might or might not pick up the result.

**Why it happens.** SSE is a "fire and stream" protocol; resumability requires explicit `Last-Event-ID` header handling. AG-UI dojo conformance covers event types, not resumability.

**Prevention strategy.**
- **Gate 1 DoR (Slice 8):** decide explicitly — resumable or not? If not resumable (PRD's default impl): UI MUST poll `/agent/runs/<id>/status` on reconnect; if status is `running`, attach as observer. If resumable: server stores `Last-Event-ID` + replays from buffer.
- **Gate 1 DoR (Slice 8):** acceptance test: "disconnect mid-stream, reconnect after 5s, user receives final result OR clear error indicating reconnect failed."
- **Gate 3 DoD (Slice 8):** chrome devtools simulation of network drop in test; UI behavior verified.
- **Anti-mistake:** don't assume `EventSource` "just handles it" — it reconnects, but doesn't resume.

**Warning signs.** User reports "Aura just stops responding sometimes"; AG-UI client console shows `EventSource closed` followed by re-open but no resume.

**Phase to address:** Slice 8.

**Severity:** P2 — UX hiccup; reproducible on mobile-dropping-wifi.

**Source:** SSE spec (HTML5 `EventSource`); AG-UI Dojo docs; common SSE pitfall.

---

### Pitfall 29: Cost runaway from unhardened LLM cost telemetry

**What goes wrong.** Slice 13a introduces cost tracking (`AURA_LLM_LOCAL_FALLBACK_COST_USD_DAY=1.0` per PRD §4302). Cost is computed client-side per `usage.cost` field from OpenRouter (PRD §1368). If OpenRouter changes the wire format silently, or a new provider is added that doesn't emit `usage.cost`, Aura's tracker reads `0.00` and never trips the threshold. User racks up $200 in one day.

**Why it happens.** "Cost is in the response payload" is a fragile contract. Threshold check on `0.00 < 1.00 = true` is always green.

**Prevention strategy.**
- **Gate 1 DoR (Slice 13a):** cost tracker has two layers: (a) per-response `usage.cost` if present; (b) **fallback**: compute cost from `prompt_tokens + completion_tokens` × known price-per-token table (per model). If both unavailable, log WARN + count call towards a daily call-count cap (`AURA_LLM_DAILY_CALL_LIMIT=5000`).
- **Gate 1 DoR (Slice 13a):** acceptance: "if `usage.cost` is missing from response, tracker uses fallback formula AND logs `cost_source=fallback`."
- **Gate 3 DoD (Slice 13a):** integration test with mock OpenRouter that strips `usage.cost`; assert tracker still trips threshold within expected call count.
- **Notifier alert:** "Aura crossed $0.50 today" preemptive ping, not just "Aura crossed $1.00 cutoff."

**Warning signs.** OpenRouter dashboard cost > Aura's `aura cost-stats` reported cost (drift); user receives monthly bill 5× expectation; `usage.cost` field appears as `null` in raw response logs.

**Phase to address:** Slice 13a.

**Severity:** P2 (single user) — P1 if multi-user MVP lands without per-user cost isolation.

**Source:** PRD §1368, §4302; OpenRouter docs (cost field added Q3 2025, format-stable but not contract-guaranteed); cost-runaway incidents common in LLM products.

---

### Pitfall 30: Skill snippet TTL sweep silently deletes user-valued data

**What goes wrong.** Slice 7e sweeps snippets idle > 90 days → status='archived' (PRD §2098-2112). User comes back from a 4-month sabbatical, asks "do my data-analysis scripts." All snippets are archived; discovery default skips archived. User feels Aura "forgot" them. Worse: if archived → eventually deleted (future cleanup), unrecoverable.

**Why it happens.** TTL semantics are a tradeoff: too long = cruft; too short = surprise. 90 days seems reasonable but doesn't model real user patterns.

**Prevention strategy.**
- **Gate 1 DoR (Slice 7e):** TTL is **archive only**, never delete (audit log preserves; Notifier alerts before archive). PRD already says this — Gate 3 DoD must verify no deletion path exists.
- **Gate 1 DoR (Slice 7e):** Notifier pings BEFORE archive: "I'm about to archive 5 snippets you haven't used in 80 days. Reply `keep <names>` or `archive all`." Default if no reply within 7 days: archive.
- **Gate 1 DoR (Slice 7e):** `aura skills list --include-archived` always available; archived snippets are 1 command away from re-active.
- **Gate 3 DoD (Slice 7e):** test: archive then re-activate cycle is fully reversible (state preserved, no data loss).
- **Anti-mistake:** don't tune TTL based on dev usage patterns; user usage patterns differ (sporadic burst vs. daily).

**Warning signs.** Audit log shows mass archive events with no preceding Notifier alerts; user complaints about "Aura forgot my X."

**Phase to address:** Slice 7e.

**Severity:** P2 — data accessibility regression; user trust.

**Source:** PRD §2098-2112; common TTL-policy pitfall.

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Skip Gate 1 DoR ("we know what we want") | -2 hours of upfront ceremony | Slice ships with un-tested edge cases; bug bounty in production | **Never** for slices ≥ 200 LOC. OK only for cosmetic CLI tweaks. |
| Use `time.Sleep` instead of `synctest`/channel sync in tests | Test passes today | Flaky tests; eventually loosened thresholds (Pitfall 22) | Never (PRD §1459 explicit). |
| Hardcode env values in Go for "this dev machine" | Boot faster | Env catalog drift (Pitfall 17); production accidentally inherits dev defaults | Never. Always env via `internal/config/`. |
| `pgxpool.Conn()` without `defer release()` | One less line | Connection pool exhaustion under load | Never. CI lint via `pgxpool-checker`. |
| `http.DefaultClient` for sidecar calls | Quick prototype | No timeout, no cancel propagation, no retry | OK in `cmd/aura/main.go` ad-hoc tools (e.g. `aura cache-stats` if it doesn't run in production loop); never in `internal/`. |
| Skip seccomp profile load verification ("it's loaded, trust me") | -10 min test setup | Pitfall 1 lands silently | Never. |
| Use deferred-tool pattern for SMALL tools "for consistency" | Looks tidy | Wastes `tool_search` call per use; defeats the manifest-bloat protection that deferred is FOR | Never. CLAUDE.md §Tool design: only big tools (long descriptions, complex schema) get `Deferred: true`. |
| One mega-commit at end of slice instead of per-sub-slice | Less ceremony | Hard to revert one sub-slice; bisect lands on 600-LOC diff | OK for slice with no sub-slices; never for slices PRD splits (2/6/7/9/11/13). |
| `assert.Equal(t, "expected", result.Reply)` instead of artifact verification | -10 LOC test | Pitfall: test passes when model hallucinates (PRD §1440-1446); reality undetected | Never (per memory `feedback_probe_must_verify_artifact_not_reply`). |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| OpenRouter | Hardcode `usage.cost` assumption | Fallback to token-count × price-table (Pitfall 29) |
| OpenRouter | Treat as drop-in OpenAI replacement | Pass `HTTP-Referer` + `X-Title` headers (PRD §50); parse `usage.prompt_cache_hit_tokens` as optional field |
| Neo4j | Use defaults for HNSW M / efConstruction | Set explicitly per Slice 11a; benchmark at scale (Pitfall 16) |
| Neo4j | Re-use connection across goroutines | Use Bolt driver session-per-goroutine; sessions are cheap; sharing causes "result not consumed" errors |
| Neo4j (via MCP) | Treat MCP server as stateless RPC | MCP subprocess has stdio session; pool with care, restart on broken pipe; track subprocess lifetime |
| Postgres (pgx) | `pgx.Connect` vs `pgxpool.New` confusion | Always `pgxpool` in production; pgx.Connect only for migrations |
| Postgres | Forget `--clean --if-exists` on `pg_dump` restore | Document in runbook; CI restore drill (Pitfall 24) |
| Telegram | MarkdownV2 partial escape | Use HTML mode + `html.EscapeString` (Pitfall 18) |
| Telegram | Single bot token across envs | Separate `@aura_dev_bot` + `@aura_bot` (Pitfall 25) |
| Docker Compose | `depends_on` without `condition` | Always `condition: service_healthy` (Pitfall 13) |
| llama.cpp embed sidecar | Default to CUDA on Linux | Explicit `--index-url cpu` in pip install (per memory `feedback_pip_torch_cuda_default_on_linux`); confirm CPU-only image |
| SearXNG | Trust HTTP response without timeout | Per PRD Slice 5: explicit timeout + content-type check + size cap |
| markitdown | Hand 50 MB doc synchronously | Tiered async (PRD §4208-4214); placeholder + edit-to-done |
| MCP subprocess | Forget to `cmd.Wait()` after kill | Goroutine leak + zombie process; always `defer cmd.Wait()` after `cmd.Cancel()` |
| Docker (Windows host) | Bind mount with path mangling | PowerShell or `MSYS_NO_PATHCONV=1` (per memory `feedback_docker_compose_run_msys_path_mangling`) |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| HNSW M=16 default at scale | Recall drops, latency stable | Set M=32 + efConstruction=200 (Pitfall 16) | ~100K chunks |
| Synchronous embed batch of 1 per chunk | Ingest takes hours for 100-page doc | Batch 32 (PRD §4288) | ~50 chunks |
| `pgxpool.MaxConns` at default (5) | Connection wait-queue under burst | Set to `min(num_cpu * 2, 25)` for mini-PC; size to `pg_stat_activity` analysis | ~10 concurrent users |
| Cache stampede on background goroutines all firing at boot | 30s freeze every 24h | Jitter (Pitfall 20) | 4+ daily timers |
| KV cache miss on system prompt mutation | 10× cost on Anthropic | Stable-prefix invariant test (Pitfall 3) | First message after deploy |
| Re-embed full doc on minor edit | Slow re-ingest | Content-hash idempotency check (PRD §3268); incremental chunk re-embed | ~5 MB docs |
| Telegram throttle too aggressive | Bot banned by Telegram | Per-pane throttle 1500/500/1000ms (PRD §4197) + 429 backoff up to 30s | Bursty replies (>1 msg/s) |
| Neo4j heap default 512 MB | OOM on community detection | Set `dbms.memory.heap.max_size=2G` for mini-PC (per memory `feedback_minipc_cpu_budget`) | First Leiden run on 10K+ entities |
| LLM tier=reasoning for routine tool | Cost 5× normal | Tier mapping per request (PRD §4261-4263); audit tier choice | Same query 100×/day |
| Sandbox container creation per call (vs session reuse) | 200-500ms per exec | Slice 2b session reuse; default to session for repeat use | 5+ exec/min per conversation |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Seccomp default-deny with 7 named blocks (Pitfall 1) | Container escape → host compromise | Positive allowlist; SandboxEscapeBench validation |
| Workspace mount without symlink guard (Pitfall 2) | Arbitrary host file read/delete | `O_NOFOLLOW` everywhere; `nosuid,nodev,noexec` mount opts |
| Skill blocklist literal-only (Pitfall 4) | Prompt injection via Unicode encoding | NFKC normalize before check; fuzz test |
| Embedding model swap without re-index (Pitfall 7) | Silent retrieval corruption | Env-pinned dimension; boot-time assertion |
| Secrets in turn content (Pitfall 8) | Credential exfiltration | Gitleaks-pattern scanner at ingress |
| Audit table TRUNCATE bypass (Pitfall 6) | Forensic integrity loss | Role separation `aura_app` vs `aura_migrate` |
| SSRF IPv4-only blocklist (Pitfall 5) | Cloud metadata theft | Add IPv6, IPv4-mapped, link-local; DNS-pin per conversation |
| `--ignore-scripts` missing in npm install | Supply chain RCE via postinstall hook | PRD §1901 already mandates; verify per Gate 3 |
| Bot token in `compose.yaml` committed | Bot hijack | `.env` (gitignored), `.env.example` template (PRD §84) |
| `AURA_AGUI_CORS_PERMISSIVE=1` in production | XSS-enabled API access | Explicit env, default 0 (PRD §4275); production runbook check |
| Telegram setup wizard on `0.0.0.0` (PRD §4216-4219) | Unauthenticated LAN access | Default `127.0.0.1`; if `0.0.0.0`, add basic auth (currently OOS — accept risk per Gate 1 RBG) |
| Sidecar HTTP without `127.0.0.1` bind | LAN access to sandbox exec | All sidecars bind loopback; cross-container access via Docker network |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| "I had a problem" generic error | User can't recover; loses trust | Specific error: "Postgres unreachable. Aura is waiting; you can retry now or wait." |
| Silent archival of snippets (Pitfall 30) | User feels forgotten | Notifier alert 7d before archive; `--include-archived` flag |
| First-message-fails after boot (Pitfall 13) | "Aura is broken" perception | Healthcheck-gated boot; clear "Aura is starting up" UI state |
| Long tool execution with no progress | User abandons | Streaming status pane (PRD §4197); periodic "still working on X" via Notifier |
| `read_tool_output` invisible to user | User can't see why agent answered X | Audit visibility: `aura chat show <id> --include-tool-calls` |
| Telegram `409 Conflict` invisible to user (Pitfall 25) | Some messages lost | Aura exits hard on conflict; user notices vs intermittent loss |
| `ask_user` pause with no timeout | Conversation locked indefinitely | Default 24h timeout; auto-cancel + audit; documented in pause message |
| `aura chat new` requires user to know command | Onboarding friction | Setup wizard (Slice 9a); Telegram `/start` → guided |
| `aura skills approve` requires CLI | Mobile-only user blocked | Telegram bot supports `/approve <name>` deep link from Notifier |

## "Looks Done But Isn't" Checklist

- [ ] **Sandbox (Slice 2a/2b):** Often missing **SandboxEscapeBench validation** — verify run + escape rate < 5% documented in `PHASE_REPORT.md`.
- [ ] **Sandbox (Slice 2b):** Often missing **symlink guard on host walkers** — verify `grep -rE 'filepath\.Walk' internal/sandbox/` all use `O_NOFOLLOW`.
- [ ] **KV cache (Slice 4 + 10/11e):** Often missing **cross-slice invariant test** — verify `cache_invariant_audit.sh` covers Slices 1, 1.8, 5, 7e, 10, 11e mutations.
- [ ] **Skills (Slice 7a):** Often missing **NFKC normalization** in validator — verify with fuzz test on Unicode mutations of literal blocklist.
- [ ] **Skills (Slice 7c):** Often missing **TRUNCATE trigger** on audit tables — verify Postgres `aura_app` role lacks TRUNCATE privilege.
- [ ] **Web (Slice 5):** Often missing **IPv6 + IPv4-mapped** in SSRF blocklist — verify with `dnslib` rebinding test.
- [ ] **Web (Slice 5):** Often missing **DNS pin per conversation** — verify two consecutive `web_fetch` to same host use same IP.
- [ ] **Memory (Slice 11a):** Often missing **`vector.hnsw.m` explicit** — verify schema migration sets M=32 (not default 16).
- [ ] **Memory (Slice 11a):** Often missing **`(name, type) IS UNIQUE` constraint** on Entity — verify schema; verify chaos test on concurrent ingest.
- [ ] **Memory (Slice 11b):** Often missing **NFKC + case-fold + whitespace normalize** before MERGE — verify chaos test.
- [ ] **Backup (Slice 0.5 + 0.7):** Often missing **restore drill** — verify nightly CI job; verify last successful restore < 7 days old.
- [ ] **Cron (Slice 6):** Often missing **advisory lock + heartbeat** — verify chaos test with network partition.
- [ ] **Agent loop (Slice 0.9 + 1):** Often missing **wall-clock budget propagation** — verify swarm children inherit parent's remaining budget, not fresh.
- [ ] **Agent loop (Slice 1):** Often missing **dedup window** on tool calls — verify same `(tool, args)` 3× in a row triggers forced `text_response`.
- [ ] **Sidecars (cross-cutting):** Often missing **`condition: service_healthy`** — verify compose; verify Aura boot blocks for `up to AURA_STARTUP_HEALTHCHECK_TIMEOUT_SEC=120`.
- [ ] **Telegram (Slice 9b):** Often missing **conflict detection at boot** — verify Aura exits on `409 Conflict` from `getUpdates`.
- [ ] **Cost (Slice 13a):** Often missing **fallback token-price calculation** — verify if `usage.cost` absent, tracker still trips threshold.
- [ ] **AG-UI (Slice 8):** Often missing **reconnect semantics** — verify either resumable OR poll-status-on-reconnect.
- [ ] **Background goroutines (cross-cutting):** Often missing **jitter** — verify each `time.Sleep(interval)` adds `+ rand.Int63n(interval/4)`.
- [ ] **Logs (cross-cutting):** Often missing **request_id propagation** — verify integration test correlates ≥3 services per request.
- [ ] **PRD-sync (cross-cutting):** Often missing **PRD-amendment commit before code commit when spec changed** — verify `git log --grep='prd:'` shows recent activity matching code changes.

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Sandbox escape (Pitfall 1) | HIGH | (1) Isolate compromised host; (2) audit `aura.skill_audit` for skill mutations during window; (3) `git push --force-with-lease` only after audit; (4) rotate every secret in `.env`; (5) refuse all `execute` until seccomp re-validated. |
| Workspace symlink escape (Pitfall 2) | HIGH | (1) Same as above + (2) audit `$AURA_RUN_DIR` host walker logs; (3) backup integrity check (was `pg_dump` corrupted?). |
| KV cache poisoning (Pitfall 3) | LOW | (1) Identify mutation site via `git bisect` on hit-rate metric; (2) revert + add invariant test; (3) deploy fix; cache rebuilds on next turn. |
| Skill injection bypass (Pitfall 4) | MEDIUM | (1) Audit `aura.skill_audit` for suspicious creates in window; (2) move suspect skills to `quarantine/`; (3) deploy fixed validator; (4) re-evaluate skills manually. |
| SSRF exfiltration (Pitfall 5) | HIGH | (1) Rotate ALL cloud credentials; (2) audit metadata service logs; (3) deploy fixed blocklist; (4) check for any agent_jobs scheduled during window. |
| Audit bypass (Pitfall 6) | HIGH | (1) Cannot recover historical audit (gone); (2) DB role separation immediately; (3) Postgres replication log review if available; (4) accept forensic gap. |
| Embedding swap corruption (Pitfall 7) | MEDIUM | (1) Restore from pre-swap backup; (2) export memory; (3) DROP + CREATE index with correct dim; (4) re-ingest (cost ~hours of embed time). |
| Secrets in turn content (Pitfall 8) | HIGH | (1) Rotate exposed credentials immediately; (2) `UPDATE aura.conversation_turns SET content = redact(content)` for affected rows; (3) re-encrypt backups containing the row; (4) audit Telegram chat history (limited to delete-for-me). |
| Infinite tool loop (Pitfall 9) | LOW | (1) `aura chat abort <id>`; (2) deploy fix to add step cap; (3) refund users for runaway cost (if multi-user). |
| Goroutine leak (Pitfall 10) | LOW | (1) `aura serve` restart releases everything; (2) `goleak` + fix; (3) memory profile to confirm. |
| `ask_user` deadlock (Pitfall 11) | LOW | (1) `aura chat resume <id> --force-reject-all` (new tool to spec); (2) deploy fix. |
| Preview truncation wrong answer (Pitfall 12) | LOW | (1) User-correctable per conversation; (2) deploy content-type-aware preview. |
| Boot order race (Pitfall 13) | LOW | (1) Manual restart in correct order; (2) deploy healthcheck gating. |
| Cron double-execution (Pitfall 14) | MEDIUM | (1) Identify duplicates from `agent_job_runs`; (2) refund/undo where possible; (3) deploy advisory-lock fix. |
| Entity duplication (Pitfall 15) | MEDIUM | (1) `MATCH (e:Entity) WITH e.name, count(*) AS c WHERE c > 1` → manual merge via `apoc.refactor.mergeNodes`; (2) deploy constraint + MERGE pattern. |
| HNSW recall regression (Pitfall 16) | HIGH | (1) DROP + CREATE index with M=32 + efConstruction=200; (2) re-embed nothing (index rebuild only, cost ~hours); (3) re-test recall@5. |
| Compose drift (Pitfall 17) | LOW | (1) `aura serve --print-config` diff dev/prod; (2) align via override files. |
| Telegram MarkdownV2 inject (Pitfall 18) | MEDIUM | (1) Audit messages sent during window for suspicious links; (2) hotfix to HTML mode; (3) user notify if phishing risk. |
| Run dir full (Pitfall 19) | LOW | (1) `aura disk cleanup` (new tool); (2) `pg_dump` retry; (3) deploy GC goroutine. |
| Background stampede (Pitfall 20) | LOW | (1) Stagger via env override; (2) deploy jitter fix. |
| PRD drift (Pitfall 21) | MEDIUM | (1) `/gsd-codebase-mapper` re-run; (2) PRD-amendment commits to catch up; (3) review with reviewer. |
| Test loosened (Pitfall 22) | MEDIUM | (1) `git log -p -- '*_test.go'` to find loosening commits; (2) revert and fix underlying flakiness. |
| Logs uncorrelated (Pitfall 23) | LOW | (1) Deploy request_id propagation incrementally; (2) older logs are lost-cause. |
| Backup unverifiable (Pitfall 24) | HIGH | (1) Manual restore attempt; (2) likely have to accept partial data loss; (3) deploy CI restore drill going forward. |
| Telegram polling collision (Pitfall 25) | LOW | (1) Stop dev instance; (2) separate `@aura_dev_bot` token; (3) document. |
| Scope creep (Pitfall 26) | MEDIUM | (1) Honest "is this in scope?" review; (2) move out-of-scope to backlog; (3) finish current substrate work first. |
| Bench deferred (Pitfall 27) | LOW | (1) Run the bench; (2) update default if measurement contradicts current. |
| AG-UI no reconnect (Pitfall 28) | LOW | (1) UI fix to poll on reconnect; (2) server-side resume buffer for v2. |
| Cost runaway (Pitfall 29) | MEDIUM | (1) `aura serve` stop; (2) audit `aura.openrouter_audit` (if exists) or OpenRouter dashboard; (3) deploy fallback cost calc; (4) lower `AURA_LLM_DAILY_CALL_LIMIT`. |
| Snippet TTL data-loss perception (Pitfall 30) | LOW | (1) `aura skills list --include-archived` shows everything; (2) un-archive on first use; (3) deploy Notifier pre-archive alert. |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| 1 — Sandbox escape | Slice 2a | SandboxEscapeBench escape rate < 5%; positive-allowlist seccomp |
| 2 — Workspace symlink | Slice 2b | `grep -rE 'filepath\.Walk' internal/sandbox/` audit; chaos test |
| 3 — KV cache poisoning | Slice 4 + cross-cutting 1.8/5/7e/10/11e | `cache_invariant_audit.sh` CI job |
| 4 — Skill injection bypass | Slice 7a (validator) + 7c (writer) | Fuzz test on Unicode mutations |
| 5 — SSRF | Slice 5 | `dnslib` rebinding test + IPv6 blocklist verify |
| 6 — Audit TRUNCATE | Slice 0.5 (roles) + 7c (trigger) | Test: `DELETE/TRUNCATE/DROP` as `aura_app` → denied |
| 7 — Embedding dim swap | Slice 0.7 + 11a + 11b | Boot-time assertion test |
| 8 — Secrets in turn | Slice 1.8 (persistence) + 9b (channel) | Gitleaks pattern scanner; backup grep check |
| 9 — Infinite loop | Slice 0.9 + 1 + 3 + 6 | Loop budget test; goleak verify |
| 10 — Ctx cancel | Slice 0.9 + every tool slice | Per-tool cancel test; staticcheck SA1029 |
| 11 — Multi-pause deadlock | Slice 1.5 + 3 | `TestSpawnInteractive_MultiPause_AllResolved` |
| 12 — Preview truncation | Slice 1 | Content-type-aware preview test |
| 13 — Boot order | Slice 0.5 + 0.7 + 9c + 13 | Compose healthcheck; integration smoke `compose_integration` |
| 14 — Cron double exec | Slice 6 | Chaos test (network partition + advisory lock + heartbeat) |
| 15 — Entity dup | Slice 11a + 11b | Chaos test (concurrent ingest + UNIQUE constraint) |
| 16 — HNSW M default | Slice 11a + 11d | recall@5 ≥ 0.8 at 10K + 100K |
| 17 — Compose drift | Slice 0.5 + Gate 3 cross-cutting | `aura serve --print-config` diff; `scripts/check_compose_drift.sh` |
| 18 — Telegram MarkdownV2 | Slice 9b | Fuzz test on escape function; HTML mode default |
| 19 — Run dir full | Slice 1.8 | GC goroutine + chaos test fill 90% |
| 20 — Background stampede | Slice 0.9 + 7e/11c/11e | Load test trigger all timers simultaneously |
| 21 — PRD drift | Process — every Gate 1 + Gate 3 | `git log --grep='prd:'` monthly review |
| 22 — Tests modified | Gate 2 + Gate 3 | PR template "Why test changed?"; `scripts/check_test_diff_justification.sh` |
| 23 — Logs uncorrelated | Slice 0.9 + 1 + cross-cutting | Integration test grep request_id across services |
| 24 — Backup unverified | Slice 0.5 + 0.7 | CI nightly restore drill |
| 25 — Telegram collision | Slice 9b | `getUpdates` conflict integration test |
| 26 — Scope creep | Process — every Gate 1 DoR | Reviewer crosscheck Out of Scope list |
| 27 — Bench deferred | Gate 1 + 3 for bench OQs | `docs/aura-quality-snapshot.md` updated; CI gate |
| 28 — AG-UI reconnect | Slice 8 | Network-drop simulation test |
| 29 — Cost runaway | Slice 13a | Mock OpenRouter without `usage.cost`; tracker still trips |
| 30 — TTL data loss | Slice 7e | Archive/un-archive reversibility test; Notifier pre-alert |

## Sources

- PRD `prd.md` §1051-1226 (Slice 2 sandbox), §1287 (multi-pause), §1310-1390 (Slice 4 KV cache), §1393-1487 (Test discipline), §1490-1577 (Slice Q&A discipline), §1812-2027 (Slice 7), §3175-3370 (Slice 11), §3981-4135 (Risk-Based Governance), §4139-4313 (Caps & Limits)
- `.planning/codebase/CONCERNS.md` (full document — pre-merge benchmarks, security concerns, performance considerations, fragile areas, operational concerns)
- `.planning/PROJECT.md` (Out of Scope, Constraints, Key Decisions)
- `CLAUDE.md` (Behavioral rules, Tool design deferred-tool pattern, Post-edit validation)
- Memory: `reference_aura_cache_poisoning_sites_2026-05-27`, `feedback_embedding_backend_stays_mistral`, `project_neo4j_spike_2026-05-27`, `feedback_minipc_cpu_budget`, `feedback_aura_as_product`, `feedback_probe_must_verify_artifact_not_reply`, `user_finishes_what_starts`
- [SandboxEscapeBench / Architectural Requirements for Agentic AI Containment (arXiv 2604.23425)](https://arxiv.org/abs/2604.23425)
- [5 Production Scaling Challenges for Agentic AI in 2026 — MachineLearningMastery](https://machinelearningmastery.com/5-production-scaling-challenges-for-agentic-ai-in-2026/)
- [AI Agent Sandbox Escape Research — buildmvpfast](https://www.buildmvpfast.com/blog/ai-agent-sandbox-escape-research-security-autonomous-2026)
- [Agentic Resource Exhaustion: The "Infinite Loop" Attack — InstaTunnel/Medium](https://medium.com/@instatunnel/agentic-resource-exhaustion-the-infinite-loop-attack-of-the-ai-era-76a3f58c62e3)
- [Agentic AI Fails: Loops, Planning & Unsafe Tool Use — StartupHub.ai](https://www.startuphub.ai/ai-news/ai-research/2026/agentic-ai-fails-loops-planning-unsafe-tool-use)
- [Container Escape Vulnerabilities — Blaxel Blog](https://blaxel.ai/blog/container-escape)
- [4 ways to sandbox untrusted code in 2026 — DEV Community](https://dev.to/mohameddiallo/4-ways-to-sandbox-untrusted-code-in-2026-1ffb)
- [The Sandbox Imperative — Medium](https://medium.com/@aibeginner/the-sandbox-imperative-why-autonomous-code-execution-demands-a-new-security-posture-c43c184bda54)
- [Augment Code — What Is an Agent Execution Sandbox?](https://www.augmentcode.com/guides/agent-execution-sandbox)
- [KV Cache & Prompt Caching Attacks — redteams.ai](https://redteams.ai/topics/llm-internals/kv-cache-attacks)
- [LLM Prompt Caching: Performance and Security Guide — Medium](https://medium.com/@michael.hannecke/llm-prompt-caching-what-you-should-know-2665d76d3d8d)
- [Can Transformer Memory Be Corrupted? Cache-Side Vulnerabilities (arXiv 2510.17098)](https://arxiv.org/pdf/2510.17098)
- [Honcho auto-injected context rebuilds cached system prompt — hermes-agent#13631](https://github.com/NousResearch/hermes-agent/issues/13631)
- [Neo4j Vector Index dimension mismatch — neo4j#13387](https://github.com/neo4j/neo4j/issues/13387)
- [langchain Vector Index dimension mismatch — langchain#16336](https://github.com/langchain-ai/langchain/issues/16336)
- [Neo4j Cypher Manual — Vector indexes](https://neo4j.com/docs/cypher-manual/current/indexes/semantic-indexes/vector-indexes/)
- [Neo4j Operations Manual — Vector index memory configuration](https://neo4j.com/docs/operations-manual/current/performance/vector-index-memory-configuration/)
- [How to Use Podman as a Sandbox for Untrusted Code — OneUptime](https://oneuptime.com/blog/post/2026-03-18-use-podman-sandbox-untrusted-code/view)
- [Coding Agent Sandbox — Bunnyshell](https://www.bunnyshell.com/guides/coding-agent-sandbox/)

---
*Pitfalls research for: Go-native agentic AI substrate (Aura)*
*Researched: 2026-05-29*
