# Spike — can agent-browser be Aura's authenticated browser, without bespoke code?

**Date:** 2026-09-26 · **Artifact:** `agent-browser` 0.38.1 (npm, Apache-2.0), tarball
`sha512-k58FCz0y…hvnhw==` verified against the registry `dist.integrity`; native
`bin/agent-browser-linux-x64` sha256 `5100149a1903211c889de4e545bf36d90803740cea4f99aa22651649f9205ea1`
(18.2 MB, no Node needed by the daemon) · **Target:** the local fixture in this directory
(`fixture_site.py`: password login → TOTP → HttpOnly session cookie → protected PDF). No
third-party site was automated.

Two rounds: **(A) native** on the Claude Code cloud container (Playwright Chromium 1194, no Docker
daemon at the time), then **(B) inside Aura's per-identity box** — `docker/aura-sandbox` built
unchanged except two spike-only layers (the session proxy CA, and the agent-browser binary),
started, suspended, resumed and recreated by Aura's own `usersandbox.DockerBackend` through
`boxrun/`, with the `aura-egress` sidecar applying the tenancy floor (runc, Docker 29.3.1, cgroup v1).

## The claim under test

Hermes Agent's browser (≈14.7k LOC read at `NousResearch/hermes-agent@d0288be5`) sits on top of
agent-browser. The inventory of agent-browser's README says it already ships everything Aura would
otherwise write: an agent CLI + MCP server, an encrypted credential vault the model never reads,
encrypted session persistence, and a WebSocket viewport stream that accepts human input. If true,
Aura's authenticated browser is a sandbox-image addition plus two small integration seams, not a
subsystem.

## (A) Native, outside the box

| # | Question | Result |
|---|---|---|
| A1 | Cold `open` + snapshot | `open` 1.8 s cold; `snapshot` returns the a11y tree with `@eN` refs (`textbox "Email" [ref=e2]`…) |
| A2 | Vault save via stdin | `auth save --password-stdin` ok; password **absent** from CLI output and from every file under `$HOME` (plaintext grep) |
| A3 | Vault login | `auth login fixture` → server logged `username_ok=True password_ok=True`, landed on `/otp` |
| A4 | Vault at rest | `auth/fixture.json` mode 0600, `{iv, authTag, data, encrypted, version}`; decrypts (AES-256-GCM, Go stdlib) only with the daemon's key, to `{name,url,username,password}` |
| A5 | **Where the key comes from** | Read by the **daemon at spawn**, not by the CLI call. A daemon started without `AGENT_BROWSER_ENCRYPTION_KEY` generated `~/.agent-browser/.encryption-key` next to the vault and encrypted with it; the env key exported later failed GCM auth. With the key present from the first spawn, no key file was written and the env key decrypts. |
| A6 | TOTP | The vault stores username/password only (`auth --help`: no OTP field). The code was typed with `fill` → `OTP ok=True` |
| A7 | Protected download | `download @e5 <path>` → 193-byte `%PDF-1.4`, server `GET /docs/report.pdf -> 200` |
| A8 | Restore after graceful `close` | state file 64 B → 616 B at close; next `open /docs` lands on **Documents** (no login) |
| A9 | Restore after SIGKILL right after login | state still 64 B (empty); restore `loaded` but lands on **Sign in** — session lost |
| A10 | Autosave cadence | file 64 B at t=10/20 s, 616 B at t=35 s (default 30 s); SIGKILL at 50 s → restore lands on **Documents** |
| A11 | `AGENT_BROWSER_AUTOSAVE_INTERVAL_MS=2000` | 615 B at t=3 s; SIGKILL → restore lands on **Documents** |
| A12 | Wrong / missing key on restore | `restore: load_failed; save: skipped_restore_failed` → fail closed, state **not** overwritten, no key file generated |
| A13 | State file permissions | `sessions/*.json.enc` is **0644** (the vault is 0600) |
| A14 | Human login through the stream only | `stream_login.mjs` clicks and types over `ws://127.0.0.1:<port>` → `username_ok=True password_ok=True`, URL message `/otp` |
| A15 | Stream cost, 1280×720, default quality 80 | ≈10 fps while typing, median 10.3 KB / max 10.7 KB per frame on the fixture, 1–2 frames per 2 s idle; first frame 520 ms after a click |
| A16 | Stream bind | `127.0.0.1` only, OS-assigned port (in `~/.agent-browser/<session>.stream`), no auth |
| A17 | Memory | daemon 13 MB RSS; daemon + Chromium 370 MB PSS for one session |
| A18 | MCP surface (`agent-browser mcp`, default `core`) | 29 tools, `tools/list` = 66,733 bytes |
| A19 | Socket path limit | `$HOME/.agent-browser/<session>.sock` must fit 103 bytes: a long `$HOME` refused to start (`Socket path would be 113 bytes`) |
| A20 | Allowlist vs persistence | README: `--allowed-domains` is rejected together with restore/state replay/profiles |

## (B) Inside Aura's box

| # | Question | Result |
|---|---|---|
| B1 | Box as Aura builds it | `aura-box-spike-agent-browser` + `aura-egress-…` sidecar, `HOME=/root`, uid 0, `/workspace` named volume, `/workspace/.scratch` tmpfs, Node 24.21, image 4.74 GB |
| B2 | Egress floor | `172.17.0.1`, `10.0.0.1`, `169.254.169.254` all time out from the box (`floor applied`) |
| B3 | Browser discovery | agent-browser finds the image's Playwright Chromium (build 1234) with no `EXECUTABLE_PATH`; cold `open` 1.2 s |
| B4 | **Key through `Exec` env** | **Dropped.** `usersandbox.scrubEnv` removes secret-named variables (`secret.IsSecretEnvVar`): `BOX_ENV_FOO` arrives, `AGENT_BROWSER_ENCRYPTION_KEY` arrives with length 0, so the daemon writes `.encryption-key` beside the vault |
| B5 | Key as a file | Key delivered by `CopyFileIn` to `/run/aura-abkey` (0600) and exported by the in-box driver → no `.encryption-key`, vault decrypts only with the Aura-supplied key, password absent from every file under `/root` |
| B6 | Vault login + TOTP + download | `username_ok=True password_ok=True`, `OTP ok=True`, `GET /docs -> 200` |
| B7 | Aura `Suspend` right after login | state file never written; after `Resume`: `restore: loaded` → **Sign in** (lost). `Suspend` = `ContainerStop` with a 2 s grace on a `sleep infinity` PID 1, measured 4.3–4.6 s end to end |
| B8 | Aura `Suspend` 35 s after login | 616 B saved; after `Resume` → **Documents** |
| B9 | `AGENT_BROWSER_AUTOSAVE_INTERVAL_MS=2000` (not secret-named, passes `Exec`) | 615 B at t=3 s; `Suspend`/`Resume` → **Documents** |
| B10 | Box **recreated** (container removed, volumes kept, `Resolve` again) | `HOME=/root`: `restore: missing` → **Sign in** (state lived in the container layer). `HOME=/workspace/.abhome`: `restore: loaded` → **Documents** |
| B11 | Human login via the stream, in the box | 9.7 fps, median 9.7 KB/frame, `/otp` reached, `username_ok=True password_ok=True` |
| B12 | **Stream reachability** | the WebSocket binds `127.0.0.1` inside the box netns; Aura's only channel is `exec`. `ExecStream` is output-only (no stdin) |
| B13 | Relay over `docker exec -i` (`ws_relay.mjs` ↔ `relay_login.py`) | login succeeded from the host with no published port: 14.0 fps, 13.1 KB per NDJSON line (base64), first frame 56 ms after the click, capture→host median 10 ms |
| B14 | **What the model's shell can read** | a second `exec` (= `shell_exec`) reads the key from the daemon's `/proc/<pid>/environ` and from the key file; with it the vault and the session state decrypt. `auth show` does not print the password — an output convention, not a boundary |
| B15 | Side finding (Aura bug, fixed in the same change) | `CopyFileIn` to `/workspace/.scratch/…` returned nil and the file never appeared (Docker's archive API cannot write a tmpfs), so `write_file` reported `wrote N bytes … verified:false`. Now refused with an explicit error |

## (C) The shipped live view, end to end on a running Aura

`aura serve` on Postgres 18.4 (106 migrations), Authula operator seeded, the real box image with
agent-browser and the relay, the egress floor. `live_view.e2e.ts` drives the cockpit with
Playwright: open `/browser/<session>` (the stream route resolves the box), start the site and open
its login page in the box the way the agent would, then log in by clicking and typing in the
cockpit only.

| # | Result |
|---|---|
| C1 | 3/3 runs pass: box browser at `/otp` then `/docs`, the cockpit URL bar follows, the site grants exactly one new session |
| C2 | **Bug found:** `deviceHeight` is the screen (720), the frame is the viewport (1280x577): y mapped by height landed 25% low. Fixed: one width scale for both axes |
| C3 | **Bug found:** Enter without `text: "\r"` submits nothing through CDP. Fixed in `keyEvent` |
| C4 | **Bug found:** a mousedown with `preventDefault` never focused the stage, so keys typed after a click went nowhere. Fixed: the stage focuses itself |
| C5 | Each open session: ~142 tasks, ~180 MB (measured by closing four one at a time: 504 → 361 → 219 → 77 pids). A 512-pid box holds three |
| C6 | **Bug found (pre-existing):** PID 1 `tail -f /dev/null` never reaps: 177 zombies after four sessions, all counted against the pid cap. Fixed with `HostConfig.Init`; after three more runs: 3 tasks, 0 zombies |
| C7 | **Gap (pre-existing, not fixed):** an existing box keeps its old image and host config; nothing recreates it on upgrade. The first E2E hit a box built before the relay existed |

## Gotchas that bite an integration

- **Key before daemon, and not through `Exec` env.** The key must be in the daemon's environment
  at its first spawn (A5), and Aura's `Exec` scrubs it by name (B4), so the vault silently falls
  back to a key file beside the ciphertext — Hermes' weakness, reproduced twice. Measured working:
  Aura delivers the per-identity key as a 0600 file outside the workspace volume on every
  `Resolve` (B5), and the command that first spawns the daemon exports it. An `.encryption-key` file
  is a failed invariant.
- **Suspend kills the browser without a save.** `Suspend` gives PID 1 (`sleep infinity`) a 2 s
  grace and the daemon is SIGKILLed: a login younger than the 30 s autosave is lost (B7).
  `AGENT_BROWSER_AUTOSAVE_INTERVAL_MS=2000` fixed it (B9); a pre-suspend `close` would too.
- **State must live on the volume.** Under `HOME=/root` a box recreate loses every session (B10);
  `HOME` on `/workspace` survives it. The socket path must still fit 103 bytes (A19;
  `/workspace/.abhome/.agent-browser/vol1.sock` is 43).
- **The live view needs a stdin-capable exec.** The stream is loopback inside the box netns (B12).
  A relay over `docker exec -i` works (B13) and publishes no port, but `usersandbox.ExecStream`
  has no stdin today: that is the one backend extension this path needs.
- **The stream client must send real virtual key codes.** A character code as
  `windowsVirtualKeyCode` turned `.` into VK_DELETE (46) and dropped it (A14); the bundled dashboard
  forwards the DOM event's `keyCode`, and a cockpit viewer must do the same.
- **No domain allowlist once sessions persist** (A20): the egress sidecar stays the only network
  boundary (B2), which is the boundary Aura already trusts.
- **Session state file is 0644** (A13), the vault 0600.
- **The MCP profile is heavy** (A18, ≈66 KB of schemas). The CLI through `shell_exec` plus the
  shipped skill (`agent-browser skills get core`) costs no manifest.

## Threat model this spike establishes

Nothing inside the box is secret from the model's shell (B14). The vault keeps passwords out of
**normal tool output** and encrypts them **at rest outside the box** (volume backups, a stolen
disk); it does not stop a prompt-injected model that runs `shell_exec` from reading the key and
decrypting the vault or the session cookies. Hermes has the same property (its terminal tool can
read `vault.key`). Consequences for the design:

- Prefer the **human login through the live view** for anything sensitive: the box then holds a
  session, never a reusable password.
- A stored password is a convenience for low-value accounts, and must be presented to the user as
  "readable by the agent's sandbox", not as "the model never sees it".

## What this spike does NOT prove

- **Only runc was measured.** The operator confirmed gVisor (`runsc`) is not used by Aura.
- **No real site.** Anti-bot checks, CAPTCHAs, SSO redirects, iframes, passkeys and WebAuthn were
  not exercised; the fixture is plain HTML over loopback HTTP.
- **The live view was proven on loopback only** (C): not on a phone, not over Cloudflare, not
  with two viewers in two real browsers (the takeover is unit-tested only).
- **No redaction check through Aura's tool pipeline** (`tool_invocations`, traces, `internal/redact`).
- The box ran with the spike image's proxy-CA environment (`SSL_CERT_FILE`, `NODE_EXTRA_CA_CERTS`),
  which the production image does not carry; the fixture was loopback, so no request used it.
- fps/KB are for one near-static page; the README quotes ≈54 KB/frame on a busy 1280×720 page.

## Verdict

REUSE holds inside Aura's box for the browser, the vault, persistence and the live stream: every
capability worked without writing a component. The Aura-owned work is now measured, not guessed:

1. a per-identity key file delivered on `Resolve` + `HOME` on the workspace volume + a short
   autosave interval (configuration, no new component);
2. stdin on `ExecStream`, so a cockpit viewer can relay the loopback stream without publishing a port;
3. the cockpit viewer and its authenticated gateway route.

(2) and (3) are bespoke and need the user's go-ahead before a line is written. Update: the user
approved them; they shipped and were proven end to end in (C).

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

Inside the box (needs the spike image and `aura-egress:spike`, see B):

```bash
go build -o /tmp/boxrun ./spikes/agent-browser-auth/boxrun
/tmp/boxrun resolve && docker cp spikes/agent-browser-auth/. aura-box-spike-agent-browser:/workspace/spike/
/tmp/boxrun put /run/aura-abkey key.hex
R() { /tmp/boxrun exec "KEY_FILE=/run/aura-abkey bash spike/box_flow.sh $*"; }
R fixture; R login s1; /tmp/boxrun suspend; /tmp/boxrun resume; R fixture; R check s1
/tmp/boxrun exec 'bash spike/stream_box.sh'                     # human login via the stream
python3 spikes/agent-browser-auth/relay_login.py aura-box-spike-agent-browser <port> '<box>' '<box>'
/tmp/boxrun destroy
```

The live view on a running Aura (C), from `web/` with `aura serve` up and the operator seeded:

```bash
NODE_PATH=$PWD/node_modules AURA_E2E_ORIGIN=http://127.0.0.1:9080 \
AURA_E2E_AUTHULA_EMAIL=... AURA_E2E_AUTHULA_PASSWORD=... AURA_E2E_CHROMIUM=<chromium> \
npx playwright test -c ../spikes/agent-browser-auth/live_view.config.ts
```
