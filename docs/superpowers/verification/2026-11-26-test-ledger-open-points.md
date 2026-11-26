# The test ledger's open points, measured (2026-11-26)

The four points left open by the 2026-11-25 consolidation (PR #280, spec
`docs/superpowers/specs/2026-11-25-test-ledger-design.md` §10), each measured before it was closed, on
the operator's decisions of 2026-11-26. Measured in the session container (4 vCPU, 15 GB, no GPU) on
a local replica of ci.yml's web-e2e job, and on PR #281's CI where stated.

## 1. JavaScript tests registered under a condition

| What | Measured |
|---|---|
| Test files scanned (`web/e2e/*.spec.ts`, `web/src/**/*.test.ts(x)`, `packages/create-aura/**/*.test.*`) | 504 |
| Tests registered under an `if`, a ternary, `&&` or `\|\|` outside a test body | **0** |
| The scanner on a fixture with three such registrations, a `test.skip(cond)` and a condition inside a test body | 3 found, the other two not |
| oxlint's built-in `vitest/no-conditional-tests` on the same forms | catches `if` around tests imported from vitest; misses `&&`, the ternary and every Playwright spec |
| `aura/no-conditional-test-registration` (`web/lint/aura.mjs`) on the same forms | catches all of them in Playwright and Vitest; accepts `test.skip(cond)`, conditions inside a test body, and loops |
| `web/scripts/lint-contract.mjs` | 11 probes pass, three of them new (both rules fire; a `test.skip`-gated spec is accepted) |

**Does not prove:** a test registered in a loop over a conditional list, or by a helper called under a
condition, is not caught; `packages/create-aura` has no linter.

## 2. The specs that provision a second identity

Provisioning needs a strict profile with MUSR isolation and names the bot through a live Telegram
`getMe`; without the name it answers 409 and the wizard shows "That email is empty or already in use".

| What | Measured |
|---|---|
| telebot's own client against the fake (`scripts/web_e2e_telegram_fake.sh`), with `SSL_CERT_FILE` on the throwaway bundle | `username: aura_ci_bot` |
| Requests the fake saw over two strict runs | 16, all `getMe` |
| What a strict, isolation-on `aura serve` also needed | a non-sample object-store key, `AURA_EMBED_MODEL_DIR`, and the box and egress images: it starts the operator's box at boot |
| `paperless-first-import` against a running ingest sidecar | failed: the sidecar wrote `ready, finished 4` for the new identity's empty bucket before the Library mounted, so `indexing` was not `null` |
| The same with the sidecar paused before the identity exists and resumed after the upload | 2 passed (40 s), then 2 passed (46 s), one worker, no retries |

`finished: 4` counts CocoIndex components, not documents. The `null` precondition is the point of the
spec: it pins 564aa3bc4, where a Library that mounted on `indexing: null` never refreshed.

**Does not prove:** the strict serve uses box images built from the commit, but the specs never call
a tool, so nothing here exercises the box itself.

## 3. The outward tiers

- The Telegram live job had `HAS_BOT_TOKEN: false` on every measured run and went green having sent
  nothing (2026-11-25 inventory). It now runs nightly in `live-outward.yml`, where a missing secret
  fails it; until the `TELEGRAM_BOT_TOKEN` and `AURA_E2E_CHAT_ID` secrets exist, the nightly is red.
- `remote-access-live` stays manual. It is an attended acceptance: an installed appliance
  (`AURA_E2E_APPLIANCE_DIR`), an owned Cloudflare zone, two Cloudflare API tokens, an OTP sent to an
  inbox, up to an hour. A Cloudflare token secret alone cannot run it.

## 4. `native-media-live` on a small local vision model

The spec asks the primary model to read `ZEBRA 417` from a 900×360 image through a full Aura turn.
Two levels: the model alone through `llama-server` (CPU build b11960, 4 threads, the spec's prompt, 10
calls), then the spec itself through `aura serve` with that server as the `llamacpp` primary.

| Model (GGUF) | Weights + mmproj | Alone: correct | Alone: time per call | Through Aura |
|---|---|---|---|---|
| SmolVLM-256M-Instruct Q8_0 | 175 + 104 MB | 10/10 | 3.5 s (prompt cache off) | cannot fit: trained context 8192 |
| SmolVLM-500M-Instruct Q8_0 | 437 + 109 MB | 10/10 | 4.0 s | cannot fit: trained context 8192 |
| Qwen2.5-VL-3B-Instruct Q4_K_M | 1930 + 845 MB | 10/10 | 18 s | **10/10**: the first turn 3.6 min (cold), the next nine 40-60 s |
| Qwen3-VL-2B-Instruct Q4_K_M (official GGUF) | 1107 + 445 MB | 10/10 | 7.6 s | 7/10: two over time (5 min; Aura's 120 s request limit), one answered with a made-up tool call |
| gemma-4-E2B-it-qat UD-Q2_K_XL (operator's pick) | 2186 + 986 MB | 10/10 with `max_tokens` 512; 0/10 at 32 (it reasons first) | 20 s | timed out: prefill 57-80 tokens/s, the 5-minute test limit passed |
| gemma-4-E2B-it-qat UD-Q4_K_XL | 2620 + 986 MB | 10/10 | 12 s | 6/10 (timeouts raised to 600 s): two over the 5-minute test limit, two answered with the file's metadata or a refusal |

Why the context decides it: an Aura turn sends about **15.7k tokens** to the model (its system prompt
and mounted tools are 6,901 fixed; `llama-server` counted the rest), and the request-time budget
holds an output reserve of 20,000 plus a provider reserve of 4,096, so `AURA_MODEL_CONTEXT_WINDOW`
must be about 40,960. At 16,384 the run failed with "conversation context exceeds the model window
(final request 14836 tokens, cap -7712)". On CPU the turn is therefore a prefill of ~15.7k tokens:
Qwen2.5-VL-3B runs it at ~100 tokens/s, Gemma 4 E2B at 112 (Q4) and 57-80 (Q2). Aura's default
request limit is 120 s (`AURA_LLM_TOTAL_TIMEOUT_SEC`): a cold prefill outruns it, `llama-server` keeps
the prompt it had read in its cache, and Aura's retry then finishes from it. That is where the cold
first turn's 3.6 minutes go; the warm turns that follow reuse the shared prefix.

Licences: Qwen2.5-VL-3B-Instruct's upstream card says `qwen-research` (research, non-commercial),
whatever ggml-org's GGUF repository tags; Qwen3-VL-2B and Gemma 4 E2B are Apache-2.0.

**Decision (operator, 2026-11-26):** Qwen2.5-VL-3B, the only candidate at ten out of ten, as a CI
fixture only (never served by the appliance), with its licence on record here. Web E2E runs the spec
with it on the CPU llama.cpp build the embedder uses, through `scripts/fetch_vision_model.sh`
(commit, size and SHA-256 pinned), with the measured configuration and Aura's default timeouts.

**Does not prove:** the speed of a GitHub runner, so the cold turn's margin under the spec's
5-minute limit is measured on this container only; anything about audio, which the spec does not
exercise.
