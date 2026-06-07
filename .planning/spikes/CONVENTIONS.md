# Spike Conventions

Patterns and stack choices established across spike sessions. New spikes follow these unless the question requires otherwise.

## Stack

- Spike harnesses are **Go `package main` programs inside the repo** (`.planning/spikes/NNN-name/main.go`) so they can import `internal/*` (the `internal` rule allows it — same module tree). Run with `go run ./.planning/spikes/NNN-name`.
- Third-party servers/sidecars are cloned and built in **`D:\tmp`** (Node) or **WSL `~`** (anything needing CGO/uv/Linux tooling — WSL is the primary dev env). Never vendored into the repo; reproduction recipes + patches live in the spike dir.
- `.planning/` is excluded from golangci-lint (both linters and formatters paths) — spike code is not production code; `go vet`/`go build ./...` still cover it.

## Structure

- One dir per spike: `main.go` (harness) + `README.md` (frontmatter, investigation trail, results) + artifacts (e.g. `bridge-patch.diff`).
- Harness output = forensic log: ISO-timestamped `[CATEGORY] message` lines, `[SUMMARY]` verdict line, exit 0 = VALIDATED / 1 = failure.
- Live side-effecting probes (email/WhatsApp sends) target ONLY the operator's own account and embed a unique `AURA-SPIKE-NNN-<unix>` tag for read-back assertion.

## Patterns

- **Compose-override-per-spike** for service mutations: a `compose.<spike>.yaml` in the spike dir with apply/restore commands in its header comment (`docker compose -f compose.yaml -f <override> up -d <service>`; restore = same without the override). Pins the exact shape tested; production stays untouched (spikes 005/006).
- **Windows UAC Installer Detection**: never `go run` a spike whose dir/exe name contains "install"/"setup"/"update" — Windows demands elevation. `go build -o /d/tmp/<neutral-name>.exe` then run (spike 004b).
- **Strict-decode tripwire**: first probe of an external JSON API uses `json.Decoder.DisallowUnknownFields()` deliberately — unknown-field errors are findings (schema drift surface), then retry lax. Prod clients always decode lax (spike 003).
- **Scratch dirs**: `D:\tmp\spike-NNN*` per spike; never inside the repo; volatile by design.

- **MCP server registration**: through the managed config (`~/.aura/mcp/servers.json`, `aura mcp add` shape) — the same path production boot uses. Secrets pulled from `.env` by a one-off node script, never committed, never echoed.
- **WSL-resident MCP servers**: `ServerConfig{Command:"wsl", Args:["-e","bash","-lc","cd … && uv run main.py"]}` — stdio pipes through wsl.exe with no Aura changes.
- **Read-back ground truth**: send with unique tag → poll the read tool (5s interval, 60-90s deadline) → assert tag. Mirrors memory `probe-must-verify-artifact-not-reply`.
- **Process hygiene in WSL**: never `pkill -f <pattern>` when the invoking shell's command line contains the pattern (it kills itself, exit 15); identify processes by `/proc/<pid>/fd/1` stdout target instead.
- **Leashed live runs never pipe through `tail`/`grep` alone** — a timeout kill loses the entire buffered output. `tee /d/tmp/spike-NNN*.log` or redirect; the log survives the kill (spike 012).
- **Live LLM comparison harness**: build per-variant registries over the REAL agent loop (`agent.NewLlmAgent` + exported tools), capture ACTION-AWARE from structured tool args (never tool names alone — the shipped eval capture is blind to skill actions), score on artifact ground truth (`docker exec find /workspace -name '*.x' -newermt '<run-start>'`), N≥3 runs, paid+OPENROUTER-gated (spike 012).
- **OpenRouter first-byte flake**: a run dying at `Post …: context deadline exceeded` (AURA_LLM_TOTAL_TIMEOUT_SEC=120) or hanging silently at SETUP is infra noise — exclude the run and retry; don't score it against the model.
- **Read the D:\tmp reference repos BEFORE designing a spike harness** — nanobot already shipped the skill-driven self-extension architecture (242-LOC loader + clawhub SKILL.md) that spike 012 spent paid runs proving; the reference would have framed the spike in minutes (memory: check-tmp-sources-then-brainstorm-best).
- **Build-tag-gated harnesses for not-yet-adopted Go deps** (spikes 014-016): when a spike needs a module Aura hasn't adopted, `go get` it live during the session, gate every harness with `//go:build spike_<topic>`, and `git checkout go.mod go.sum` at session end — the committed harness never breaks `go build ./...`, and the README records the exact `go get` to re-arm it. The dep lands for real with its phase.
- **Module-cache source enumeration as research ground truth** (spikes 014-016): after `go get`, read the SDK sources at `$(go env GOMODCACHE)/<module>@<version>/` directly (grep constructors, structs, validation) instead of trusting docs pages — wire shapes, deprecations, and Validate() rules live only in the source.
- **Pinning a module nothing imports needs two `go get`s** (spike 014): `go get <module>@<sha>` records the require but not the transitive sums; the first build fails with `missing go.sum entry` until `go get <module>/pkg/<leaf>@<pseudo-version>` runs.
- **Bot-API send responses ARE the read-back ground truth** (spikes 017/018/019): bot-sent messages never appear in getUpdates — assert on the sendMessage/sendPhoto/sendDocument response payload (rendered text, entities[], Telegram-detected MIME, served PhotoSize) instead. Negative controls first: prove the API rejects the malformed shape (e.g. unescaped MarkdownV2 → 400) before trusting the green path.
- **On-device human checkpoint for rendering comparisons** (spike 018): wire-level VALIDATED is not the verdict for anything visual — send every variant to the operator's real chat back-to-back, include a self-measuring probe (width ruler 20→56 chars), and collect per-case winners via structured questions (common case + stress case separately).

## Tools & Libraries

- `martinzarfl/mail-mcp` (Node 22, stdio, SMTP/IMAP env config) — works as-is; `search_emails` takes `{query}` (required), not a criteria object.
- `lharries/whatsapp-mcp` — REQUIRES maintenance: whatsmeow must be bumped to latest (servers 405 old clients) + 5 context.Context call-site fixes + the Aura REST-send persistence patch (`002-whatsapp-mcp-pairing/bridge-patch.diff`).
- whatsmeow pairing: QR-only, rotates ~20-60s; session persists in `store/whatsapp.db` (re-auth without QR on restart).
