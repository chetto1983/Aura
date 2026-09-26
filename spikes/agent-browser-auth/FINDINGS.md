# Spike — can agent-browser be Aura's authenticated browser, without bespoke code?

**Date:** 2026-09-26 · **Artifact:** `agent-browser` 0.38.1 (npm, Apache-2.0), tarball
`sha512-k58FCz0y…hvnhw==` verified against the registry `dist.integrity`; native
`bin/agent-browser-linux-x64` sha256 `5100149a1903211c889de4e545bf36d90803740cea4f99aa22651649f9205ea1`
(18.2 MB, no Node needed by the daemon) · **Browser:** Playwright Chromium build 1194 ·
**Host:** the Claude Code cloud container, as root, **no Docker daemon** · **Target:** the local
fixture in this directory (`fixture_site.py`: password login → TOTP → HttpOnly session cookie →
protected PDF). No third-party site was automated.

## The claim under test

Hermes Agent's browser (≈14.7k LOC read at `NousResearch/hermes-agent@d0288be5`) sits on top of
agent-browser. The inventory of agent-browser's README says it already ships everything Aura would
otherwise write: an agent CLI + MCP server, an encrypted credential vault the model never reads,
encrypted session persistence, and a WebSocket viewport stream that accepts human input. If true,
Aura's authenticated browser is a sandbox-image addition plus two small integration seams, not a
subsystem.

## What was measured

| # | Question | Result |
|---|---|---|
| 1 | Cold `open` + snapshot | `open` 1.8 s cold; `snapshot` returns the a11y tree with `@eN` refs (`textbox "Email" [ref=e2]`…) |
| 2 | Vault save via stdin | `auth save --password-stdin` ok; password **absent** from CLI output and from every file under `$HOME` (plaintext grep) |
| 3 | Vault login | `auth login fixture` → server logged `username_ok=True password_ok=True`, landed on `/otp` |
| 4 | Vault at rest | `auth/fixture.json` mode 0600, `{iv, authTag, data, encrypted, version}`; decrypts (AES-256-GCM, Go stdlib) only with the daemon's key, to `{name,url,username,password}` |
| 5 | **Where the key comes from** | Read by the **daemon at spawn**, not by the CLI call. A daemon started without `AGENT_BROWSER_ENCRYPTION_KEY` generated `~/.agent-browser/.encryption-key` next to the vault and encrypted with it; the env key exported later failed GCM auth. With the key present from the first spawn, no key file was written and the env key decrypts. |
| 6 | TOTP | The vault stores username/password only (`auth --help`: no OTP field). The code was typed with `fill` → `OTP ok=True` |
| 7 | Protected download | `download @e5 <path>` → 193-byte `%PDF-1.4`, server `GET /docs/report.pdf -> 200` |
| 8 | Restore after graceful `close` | state file 64 B → 616 B at close; next `open /docs` lands on **Documents** (no login) |
| 9 | Restore after SIGKILL right after login | state still 64 B (empty); restore `loaded` but lands on **Sign in** — session lost |
| 10 | Autosave cadence | file 64 B at t=10/20 s, 616 B at t=35 s (default 30 s); SIGKILL at 50 s → restore lands on **Documents** |
| 11 | `AGENT_BROWSER_AUTOSAVE_INTERVAL_MS=2000` | 615 B at t=3 s; SIGKILL → restore lands on **Documents** |
| 12 | Wrong / missing key on restore | `restore: load_failed; save: skipped_restore_failed` → fail closed, state **not** overwritten, no key file generated |
| 13 | State file permissions | `sessions/*.json.enc` is **0644** (the vault is 0600) |
| 14 | Human login through the stream only | `stream_login.mjs` clicks and types over `ws://127.0.0.1:<port>` → `username_ok=True password_ok=True`, URL message `/otp` |
| 15 | Stream cost, 1280×720, default quality 80 | ≈10 fps while typing, median 10.3 KB / max 10.7 KB per frame on the fixture, 1–2 frames per 2 s idle; first frame 520 ms after a click |
| 16 | Stream bind | `127.0.0.1` only, OS-assigned port (in `~/.agent-browser/<session>.stream`), no auth |
| 17 | Memory | daemon 13 MB RSS; daemon + Chromium 370 MB PSS for one session |
| 18 | MCP surface (`agent-browser mcp`, default `core`) | 29 tools, `tools/list` = 66,733 bytes |
| 19 | Socket path limit | `$HOME/.agent-browser/<session>.sock` must fit 103 bytes: a long `$HOME` refused to start (`Socket path would be 113 bytes`) |
| 20 | Allowlist vs persistence | README: `--allowed-domains` is rejected together with restore/state replay/profiles |

## Gotchas that bite an integration

- **Key before daemon.** The encryption key must be in the daemon's environment at its first spawn
  (row 5). Otherwise the vault silently falls back to a key file beside the ciphertext — Hermes'
  weakness, reproduced. Aura must inject a per-identity key (from `internal/secret` HKDF) into the
  box environment, never let a daemon start without it, and treat an `.encryption-key` file as a
  failed invariant.
- **A killed box loses the last ≤30 s of login.** SIGKILL before the first autosave loses the
  session (row 9). Either suspend the box with a graceful `close`, or set a short
  `AGENT_BROWSER_AUTOSAVE_INTERVAL_MS` (row 11 measured 2 s).
- **The stream client must send real virtual key codes.** Sending a character code as
  `windowsVirtualKeyCode` turned `.` into VK_DELETE (46) and dropped it — found by this spike in its
  own client, fixed by sending `0` and the character in `text`. The bundled dashboard forwards the
  DOM event's `keyCode`; a cockpit viewer must do the same.
- **No domain allowlist once sessions persist** (row 20): the box's egress sidecar remains the only
  network boundary, which is the boundary Aura already trusts.
- **State file is world-readable** (row 13). Harmless inside a single-identity box with the key
  outside it, but worth a `chmod`/umask in the image.
- **The MCP profile is heavy** (row 18, ≈66 KB of schemas). Driving the CLI through the existing
  `shell_exec` plus the shipped skill (`agent-browser skills get core`) costs no manifest.

## What this spike does NOT prove

- **Nothing ran inside Aura's per-identity box.** No Docker daemon was available here: not the
  `aura-sandbox` image, not the egress sidecar, not gVisor `runsc`, not the box's user, `$HOME`,
  or which paths survive a box recreate (`/workspace` vs tmpfs `/workspace/.scratch`).
- **No real site.** Anti-bot checks, CAPTCHAs, SSO redirects, iframes, passkeys and WebAuthn were
  not exercised; the fixture is plain HTML over loopback HTTP.
- **No viewer in the cockpit.** The stream was driven by a Node script on the same host; the
  gateway proxy, cockpit auth and latency across the LAN/Cloudflare path are unmeasured.
- **No redaction check through Aura.** The model-facing output was inspected by eye on the CLI,
  not through `tool_invocations`, traces or `internal/redact`.
- The fps/KB figures are for one near-static page; a busy page will be larger (README quotes
  ≈54 KB at quality 80 on a busy 1280×720 page).

## Verdict

REUSE holds for the browser, the vault, persistence and the live stream: every capability in the
table worked without writing a component. The remaining Aura-owned work is two seams —
(1) per-identity key injection plus a suspend hook that closes or saves before the box stops, and
(2) an authenticated cockpit proxy for the loopback stream — and both need a measurement **inside the
box** before any PRD amendment is written.

## Reproduce

```bash
python3 fixture_site.py 8765 &                       # fixture
export HOME=/tmp/abh AGENT_BROWSER_EXECUTABLE_PATH=<chromium> AGENT_BROWSER_ENCRYPTION_KEY=$(openssl rand -hex 32)
printf '%s' 'Sp1ke-Passw0rd!x7' | agent-browser auth save fixture \
  --url http://127.0.0.1:8765/login --username alice@example.test --password-stdin
./login_flow.sh s1                                  # vault login + TOTP → "LOGGED_IN s1"
agent-browser --session s1 --restore close          # persist
agent-browser --session s1 --restore open http://127.0.0.1:8765/docs   # → Documents
# human path: open /login in a session, read ~/.agent-browser/<session>.stream, then
node stream_login.mjs <port> '<email box json>' '<password box json>' alice@example.test 'Sp1ke-Passw0rd!x7'
```
