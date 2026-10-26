# The chosen model reads images, ChatGPT included — lab VM, 2026-10-25

Plan `docs/superpowers/plans/2026-10-25-media-vision-primary.md`, spec
`docs/superpowers/specs/2026-10-25-media-vision-primary-design.md`. The operator drove the cockpit
for A and B; Claude read the results back over ssh, read-only, and drove the Task 8 run through the
cockpit API with the operator's own account.

## Build and gates

- **Tasks 1–6** shipped as `c6ce97bd8`, pushed from WSL with the pre-push gates. CI, Skills, CodeQL,
  "Publish Aura edge image", "Publish ArcadeDB MCP" and "Verify and publish create-aura-appliance"
  were green on that SHA. The VM's updater moved to it, and `aura` started on it at 20:10:40Z with
  the chat on Ollama `gemma4:31b-cloud`.
- Before that push, `make quality` passed and the coverage gate measured owned internal coverage
  **50200/57393 (87.5%)** ≥ 85%, with the package-local policy passing.
- `make ingest-test` passed except 12 tests that failed on the local ArcadeDB's authentication
  (HTTP 403, the PC's own database). They were not retried, to avoid a lockout. CI ran the same suite
  green on `c6ce97bd8`.

## The VM

`aura-ingest` runs with `aura-home` mounted at `/var/lib/aura` and
`AURA_SKILLS_DIR=/var/lib/aura/skills`, and sees the daemon's `chatgpt-plan` store there. Its
`MULTIMODAL_BASE_URL` is `http://aura-ocr-vl:8082/v1`, a name that does not resolve inside the
container: the VM runs no OCR sidecar. Every image below was therefore read by the chat's model or
by nothing.

The operator switched the chat to ChatGPT `gpt-6.1-sol` in the cockpit at 20:15:40Z.

### A — a scan with no text layer, through the media indexer

The operator uploaded `verbale_scansionato_italiano.pdf` to Documents. It is the file that failed on
2026-10-23 (`dial tcp: lookup aura-ocr-vl ... server misbehaving`). This time it was indexed, with the
page read exactly:

`VERBALE SCANSIONATO AURA / CODICE PRATICA: ITALIA-7391 / IMPORTO APPROVATO: 48270 EURO / RESPONSABILE: GIULIA BIANCHI`

Asked in chat, the agent ran `document_search` with `exact_terms: ["ITALIA-7391"]` and answered
"48.270 euro", citing `doc_a0f0a3932fd9fd01229a56606d35ac9d` (20:20:36Z).

The operator then uploaded two cockpit screenshots to Documents (two wiring diagrams). Both were
indexed within about a minute (20:28:37Z, 20:29:52Z), each with a description and its legible
labels (`K_A`, `K_B`, `OSSD 1`, `OSSD 2`, `Y36/Y37`).

### B — the daemon's summary of an image attached in chat

The daemon had booted on Ollama, and its summary route was then a boot snapshot (spec §4.4 before
the amendment). After `docker restart aura` at 20:21:57Z, with the operator's consent, the operator
attached `Screenshot 2026-08-25 082130.png` in chat. Its asset row, owned by identity `448ddbe1`:
`status complete`, `vision_model gpt-6.1-sol`, a 637-character summary of the wiring diagram
(20:32:20Z). The same file, attached at 14:33 while the chat was on OpenRouter, had been summarised
by `deepseek/deepseek-v4.1-flash`.

### C — the owner's account

Every file above belongs to identity `448ddbe1`, the operator, whose ChatGPT account is the one
connected. The VM has one identity, so the run cannot show a member's file read with the member's
own account; the unit tests cover that boundary (spec §5.7, §5.8).

## Task 8 — a model switch reaches the upload summary without a restart

Build `c034fb3c5` (Task 8 and its review fix). Before the push, `make quality` passed and the coverage
gate measured **50212/57407 (87.5%)**, with the package-local policy passing. The pre-push gates (build,
deadcode, tagged-tier-compile) passed. All six workflows were green on the SHA: CI, Skills, CodeQL,
"Publish Aura edge image", "Publish ArcadeDB MCP" and "Verify and publish create-aura-appliance".

The updater downloaded the build. Applying it from the cockpit API was refused to Claude by its
permission classifier as a production deploy, and the operator was away, so the night window applied
it: `aura` started at 03:04:29Z on `c034fb3c5`, and `aura-ingest` at 03:06:10Z.

Claude then drove the cockpit API with the operator's own account (2026-10-26). In one conversation:

| Time (Z) | Step | Result |
|---|---|---|
| 03:07:51 | `PUT /api/settings/llm-profile` → Ollama `gemma4:31b-cloud` | `restart_required: false`; log `primary LLM profile updated provider=ollama` |
| 03:07:51–03:07:57 | attach `Screenshot 2026-10-23 124113.png` (a cockpit page) | asset `5ab2f5b6`, `complete`, `vision_model gemma4:31b-cloud`; the summary quotes "Indicizzatore in attesa", "Problemi di indicizzazione segnalati: 1", "23/10/26, 12:40" |
| 03:07:57 | `PUT /api/settings/llm-profile` → ChatGPT `gpt-6.1-sol` | `restart_required: false`; log `primary LLM profile updated provider=chatgpt` |
| 03:07:57–03:08:09 | attach `Screenshot 2026-10-23 085931.png` (the embedding settings) | asset `440fd44e`, `complete`, `vision_model gpt-6.1-sol`; the summary quotes "perplexity/pplx-embed-v1-4b" and "Configurato" |

Afterwards `aura` still showed `StartedAt 03:04:29Z` with 0 restarts, and the route rows were
`chatgpt` / `gpt-6.1-sol` / `https://api.openai.com/v1` again. The run took 18 s. The two assets and
the conversation were deleted.

The OpenRouter arm, which carries the services key, was not switched to on the VM. Its key and
capability are pinned by `TestLiveVisionReadsWithOpenRouterAfterASwitch`.

## What this run does not show

- A member identity's documents: the VM has one identity.
- Telegram attachments: not exercised.
- Latency and plan cost per page or per image: not measured.
- The sidecar default (spec §4.7): still open for the operator.
- A model whose catalogue entry says it reads no images: only `gpt-6.1-sol` was used on ChatGPT.
- A live switch to OpenRouter on the VM (unit-tested only).
- An image being read at the very moment of a switch (it finishes on its old route, by design).
