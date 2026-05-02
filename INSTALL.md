# Installing Aura

Aura is your **personal Telegram second brain** — it remembers what you tell it, ingests PDFs and notes, and chats with you through your own private Telegram bot. Everything runs on your own machine. Your data never leaves it.

This guide gets a non-developer running in **about 5 minutes**, no `.env` editing required.

---

## What you'll end up with

- A private Telegram bot (only you can talk to it).
- A self-hosted server running on your computer (or a small VPS).
- A web dashboard at `http://localhost:8080` to browse your wiki, sources, tasks, settings, and skills.
- A local SQLite database (`aura.db`) and a `wiki/` folder — both on your disk, both yours.

## What it costs

| Component | Cost |
|---|---|
| Aura itself | Free (open source) |
| Telegram bot | Free |
| Hosting on your own machine | Free |
| LLM API (OpenAI / Mistral / Anthropic) | Pay-per-message — typically $1–10/month for personal use |
| LLM API (local Ollama instead) | Free (your electricity only) |
| PDF OCR (Mistral, optional) | ~$0.001 per page |

You can run Aura **100% free** by pointing it at a local Ollama install. The setup wizard auto-detects Ollama if it's running.

---

## Step 1 — Create your Telegram bot (1 minute)

Each Aura install needs its own bot. Bots are free and take 30 seconds.

1. Open Telegram and search for **@BotFather**. Start a chat.
2. Send `/newbot`.
3. Pick a display name (e.g. *My Aura*).
4. Pick a username ending in `bot` (e.g. `yourname_aura_bot`).
5. BotFather replies with a token like `123456789:ABCdef...`. Copy it.

> Why one bot per person? Aura is a personal second brain — wiki, notes, budget, and ownership are tied to a single bot.

---

## Step 2 — Download Aura (1 minute)

Grab the binary for your OS from the [Releases page](https://github.com/chetto1983/Aura/releases):

| OS | File |
|---|---|
| Windows | `aura_windows_amd64.exe` |
| macOS (Intel) | `aura_darwin_amd64` |
| macOS (Apple Silicon) | `aura_darwin_arm64` |
| Linux | `aura_linux_amd64` |

Drop it in a folder you'll remember (e.g. `~/aura/` or `C:\Aura\`).

**On macOS / Linux:** `chmod +x aura_*`

**On macOS:** the first run may be blocked by Gatekeeper. Right-click → Open → confirm.

---

## Step 3 — Run Aura (30 seconds)

Open a terminal in the same folder and run the binary:

**Windows (PowerShell):** `.\aura_windows_amd64.exe`
**macOS / Linux:** `./aura_*`

You'll see something like:

```
INFO  setup wizard listening url=http://127.0.0.1:8080
INFO  open the URL above in your browser to finish setup
```

Leave the terminal open. Aura runs as long as the terminal is open.

---

## Step 4 — Finish setup in the browser (2 minutes)

1. Open <http://127.0.0.1:8080> in your browser.
2. The **first-run wizard** asks for two things:
   - **Telegram bot token** — paste the token from Step 1.
   - **LLM provider** — pick a preset (OpenAI / Mistral / Anthropic / Ollama / Groq / DeepSeek / Together / Custom). Each preset fills the URL and a sensible default model. Paste your API key (or skip the key for Ollama).
3. Click **Test connection** to verify the URL + key. You'll see "✓ Connected — N models available" if it works.
4. Optional: open the **Embeddings** and **OCR** sections to add a Mistral key for wiki vector search and PDF ingestion. You can do this later instead.
5. Click **Save and start Aura**. The wizard writes the token to `.env`, everything else to `aura.db`, and starts the bot.

> Free local mode: install [Ollama](https://ollama.com), `ollama pull llama3.1:8b`, run Aura. The wizard auto-detects Ollama on `localhost:11434` and shows a one-line hint at the top of the page.

---

## Step 5 — Claim your bot (30 seconds)

1. Open Telegram and search for the bot username you picked in Step 1.
2. Tap **Start** (or send `/start`).
3. Aura remembers you as the owner. From now on, only you can talk to it; new users get queued for your approval at <http://localhost:8080/pending>.

Send any message — "hello" — and you should get a reply within a few seconds.

---

## Editing settings later

Open the dashboard at <http://localhost:8080> and click **Settings** in the sidebar.

You can change:

- LLM provider, model, base URL, API key
- Embeddings + OCR keys
- Soft / hard budget caps
- Summarizer mode, allowlist, OCR page limits, etc.

Most edits take effect on the next conversation turn — no restart. Bootstrap fields (Telegram token, dashboard port, file paths) live in `.env` and need a restart to change.

You can also click **Test connection** in Settings to validate a new provider before saving.

## Managing scheduled tasks

The **Tasks** sidebar entry lists every scheduled job. Three recurrence modes are supported:

- **Daily at HH:MM** — runs every day at the same local time. Used for the nightly wiki maintenance pass.
- **Every N minutes** — fires on a fixed interval. Examples: 60 = hourly, 1440 = daily, 10080 = weekly.
- **Once at** — one-shot reminder at a specific UTC timestamp.

Each row has two cleanup actions:

- **Cancel** marks the row as cancelled but keeps it in the table (audit trail).
- **Delete** permanently removes the row.

## Managing the conversation archive

The bot archives every Telegram turn to `aura.db` so the dashboard can show history and the summarizer can surface recurring facts. The **Conversations** page shows total row count + oldest entry next to the title and gives you three cleanup buttons:

- **Purge older than…** prompts for a number of days and deletes anything older.
- **Wipe this chat** appears when you have a `chat_id` filter set; deletes only that chat's history.
- **Wipe all** drops every archived turn (confirm prompt; can't be undone).

Set `CONV_ARCHIVE_ENABLED=false` in Settings to stop the bot from archiving in the first place — wiki/source memory still works since those are independent.

---

## Keeping Aura running

Aura stops when you close the terminal. To keep it always-on:

### macOS — `launchd`
Create `~/Library/LaunchAgents/com.aura.plist` with `KeepAlive=true` pointing at the binary. `launchctl load`.

### Linux — `systemd`
Create `~/.config/systemd/user/aura.service`:
```ini
[Unit]
Description=Aura
After=network.target

[Service]
WorkingDirectory=/home/YOU/aura
ExecStart=/home/YOU/aura/aura_linux_amd64
Restart=always

[Install]
WantedBy=default.target
```
Then `systemctl --user enable --now aura`.

### Windows — Task Scheduler
Run the `.exe` "At log on" with "Restart on failure".

### VPS
A $5/month Linux box (Hetzner / DigitalOcean / Vultr) runs Aura 24/7. Same install steps; SSH-tunnel the dashboard since `HTTP_PORT=127.0.0.1:8080` stays loopback-only.

---

## Where your data lives

Everything is in the folder where you ran the binary:

| File / folder | What it is |
|---|---|
| `aura.db` | SQLite — settings, chat history, tasks, budget, auth, embeddings cache |
| `wiki/` | Markdown notes Aura builds about your topics |
| `skills/` | Installed skills (capabilities) |
| `.env` | Bootstrap-only config: Telegram token, dashboard port, paths |

**Backups:** zip these four. Restore = unzip into a fresh install.

---

## Updating

1. Stop Aura (Ctrl+C, or stop the service).
2. Download the new binary, replace the old.
3. Start Aura.

Your `.env`, `aura.db`, `wiki/`, and `skills/` are untouched.

---

## Troubleshooting

**Setup wizard doesn't appear**
The wizard only runs when `TELEGRAM_TOKEN` is blank in `.env`. Open `.env`, clear the `TELEGRAM_TOKEN=` line, restart.

**Bot doesn't reply after `/start`**
Check the terminal for errors. Verify the token is correct (no spaces, no quotes). Make sure you're messaging the right bot.

**`unauthorized` from the LLM**
Open `/settings` in the dashboard, click **Test connection**, fix the URL or key, save.

**Dashboard at localhost:8080 shows blank page**
The binary should have the dashboard built in. If you built from source, `cd web && npm run build`.

**Budget exceeded errors**
`/settings` → Budget section → raise `SOFT_BUDGET` / `HARD_BUDGET`.

**macOS: "cannot be opened because the developer cannot be verified"**
Right-click → Open → confirm. One-time.

**Windows: Defender flags the binary**
Unsigned binaries from Releases trigger this. Click More info → Run anyway.

---

## Building from source

```bash
git clone https://github.com/chetto1983/Aura
cd aura
make web-build
make build
./aura
```

Requires Go 1.25+ and Node 20+.

---

## Getting help

- Logs: terminal running `aura` shows everything. Set `LOG_LEVEL=debug` for more.
- Issues: open a GitHub issue with relevant log lines (redact your token).
- Health check: `curl http://localhost:8080/api/health` should return JSON with the rollup.
