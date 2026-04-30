# Installing Aura

Aura is your **personal Telegram second brain** — it remembers what you tell it, ingests PDFs and notes, and chats with you through your own private Telegram bot. Everything runs on your own machine. Your data never leaves it.

This guide walks a non-developer through getting Aura running from scratch in about **15 minutes**.

---

## What you'll end up with

- A private Telegram bot (only you can talk to it).
- A self-hosted server running on your computer (or a small VPS).
- A web dashboard at `http://localhost:8081` to browse your wiki, sources, tasks, and skills.
- A local SQLite database (`aura.db`) and a `wiki/` folder — both on your disk, both yours.

## What it costs

| Component | Cost |
|---|---|
| Aura itself | Free (open source) |
| Telegram bot | Free |
| Hosting on your own machine | Free |
| LLM API (OpenAI / Anthropic / Mistral) | Pay-per-message — typically $1–10/month for personal use |
| LLM API (local Ollama instead) | Free (your electricity only) |
| PDF OCR (Mistral, optional) | ~$0.001 per page |

You can run Aura **100% free** by pointing it at a local Ollama install instead of a cloud LLM. See the "Free local mode" section at the end.

---

## Step 1 — Create your Telegram bot (2 minutes)

Each Aura install needs its own Telegram bot. Bots are free and take 30 seconds to create.

1. Open Telegram and search for **@BotFather**. Start a chat.
2. Send `/newbot`.
3. Pick a display name (e.g. *My Aura*).
4. Pick a username ending in `bot` (e.g. `yourname_aura_bot`). It must be globally unique.
5. BotFather replies with a token that looks like:
   ```
   123456789:ABCdefGhIJKlmNoPQRstuVWXyz
   ```
6. **Copy this token somewhere safe.** You'll paste it in Step 4.

> Why one bot per person? Aura is a personal second brain — wiki, notes, budget, and ownership are tied to a single bot. Sharing a bot would mean sharing your brain.

---

## Step 2 — Get an LLM API key (3 minutes)

Aura needs a large language model to think. Pick **one** of these providers — any OpenAI-compatible API works.

### Option A — OpenAI (most popular)
1. Go to <https://platform.openai.com/api-keys>.
2. Sign up, add a payment method, set a usage cap (e.g. $10/month).
3. Click **Create new secret key**, copy the `sk-...` value.

### Option B — Mistral (cheaper, EU-based)
1. Go to <https://console.mistral.ai/api-keys>.
2. Sign up and create a key.

### Option C — Anthropic (Claude)
1. Go to <https://console.anthropic.com/settings/keys>.
2. Create a key. Note: Anthropic's API is Claude-native; it works through Aura's OpenAI-compatible client via a proxy or direct support.

### Option D — Free local mode (Ollama)
Skip this step. See "Free local mode" at the bottom of this guide.

> Tip: Set a **monthly spending cap** on your provider's dashboard. Aura also has its own `SOFT_BUDGET` and `HARD_BUDGET` env vars as a second safety net.

---

## Step 3 — Download Aura (1 minute)

Go to the [Releases page](https://github.com/chetto1983/Aura/releases) and download the file matching your operating system:

| OS | File |
|---|---|
| Windows | `aura_windows_amd64.exe` |
| macOS (Intel) | `aura_darwin_amd64` |
| macOS (Apple Silicon) | `aura_darwin_arm64` |
| Linux | `aura_linux_amd64` |

Put it in a folder you'll remember, e.g. `~/aura/` or `C:\Aura\`.

**On macOS / Linux** make it executable:
```bash
chmod +x aura_darwin_arm64
```

**On macOS** the first run may be blocked by Gatekeeper. Right-click the binary → **Open** → confirm. You only do this once.

---

## Step 4 — Configure Aura (3 minutes)

In the same folder as the binary, create a file called `.env` (note the leading dot). Paste this template and fill in the two `***FILL IN***` lines:

```env
# Required — from Step 1
TELEGRAM_TOKEN=***FILL IN***

# Leave blank on first run. The first /start message claims this Aura as yours.
TELEGRAM_ALLOWLIST=

# Required — from Step 2
LLM_API_KEY=***FILL IN***
LLM_BASE_URL=https://api.openai.com/v1
LLM_MODEL=gpt-4o-mini

# Budget guardrails (USD)
SOFT_BUDGET=10.0
HARD_BUDGET=25.0

# Storage paths (defaults are fine)
WIKI_PATH=./wiki
DB_PATH=./aura.db
SKILLS_PATH=./skills

# Web dashboard binds to localhost only.
HTTP_PORT=127.0.0.1:8080
```

### If you picked Mistral (Option B) instead of OpenAI:
```env
LLM_BASE_URL=https://api.mistral.ai/v1
LLM_MODEL=mistral-large-latest
```

### Optional — PDF OCR
If you want Aura to ingest PDFs you send through Telegram:
```env
MISTRAL_API_KEY=***FILL IN — same or different Mistral key***
OCR_ENABLED=true
```

### Optional — better wiki search via embeddings
```env
EMBEDDING_API_KEY=***FILL IN — Mistral key works***
EMBEDDING_BASE_URL=https://api.mistral.ai/v1
EMBEDDING_MODEL=mistral-embed
```

> The full list of options lives in [`.env.example`](.env.example) — copy from there if you want to tweak more.

---

## Step 5 — Run Aura (30 seconds)

Open a terminal in the folder containing the binary and `.env`.

**Windows (PowerShell):**
```powershell
.\aura_windows_amd64.exe
```

**macOS / Linux:**
```bash
./aura_darwin_arm64
```

You should see log lines like:
```
INFO  starting telegram polling
INFO  http server listening on 127.0.0.1:8080
```

Leave this terminal open. Aura runs as long as the terminal is open.

---

## Step 6 — Claim your bot (1 minute)

1. Open Telegram and search for the bot username you picked in Step 1.
2. Open the chat and tap **Start** (or send `/start`).
3. Aura replies and remembers you as the owner. From now on, only you can talk to it; new users are queued for your approval.

Send any message — "hello" — and you should get a reply within a few seconds.

Open <http://localhost:8080> in your browser to see the dashboard.

---

## Keeping Aura running

Aura stops when you close the terminal. To keep it always-on, pick one:

### macOS — `launchd` (built in, recommended)
Create `~/Library/LaunchAgents/com.aura.plist` with the standard `KeepAlive=true` template pointing at the binary. Load with `launchctl load`.

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
Then: `systemctl --user enable --now aura`.

### Windows — Task Scheduler
Create a task that runs the `.exe` "At log on" with "Restart on failure".

### Any OS — small VPS
A $5/month Linux VPS (Hetzner, DigitalOcean, Vultr) runs Aura 24/7 without using your laptop. Same install steps; the dashboard becomes accessible only through SSH tunnel by default (`HTTP_PORT=127.0.0.1:8080`).

---

## Where your data lives

Everything is in the folder where you ran the binary:

| File / folder | What it is |
|---|---|
| `aura.db` | SQLite database — chat history, tasks, budget, skills |
| `wiki/` | Markdown notes Aura builds about your topics |
| `skills/` | Installed skills (capabilities) |
| `.env` | Your config and API keys (back this up; never commit it anywhere) |

**Backups:** zip these four items. That's a complete backup. Restoring = unzip into a new install.

---

## Updating

1. Stop Aura (Ctrl+C in the terminal, or stop the service).
2. Download the new binary from Releases.
3. Replace the old one.
4. Start Aura again.

Your `.env`, `aura.db`, `wiki/`, and `skills/` are untouched.

---

## Free local mode (no API costs)

Run a local LLM with [Ollama](https://ollama.com) — Aura works fully offline.

1. Install Ollama from <https://ollama.com>.
2. Pull a model: `ollama pull llama3.1:8b` (or any model you like).
3. In `.env`, set:
   ```env
   LLM_BASE_URL=
   LLM_API_KEY=
   OLLAMA_BASE_URL=http://localhost:11434
   OLLAMA_MODEL=llama3.1:8b
   ```
4. (Optional) disable OCR and embeddings — `OCR_ENABLED=false` and leave `EMBEDDING_API_KEY` blank.

Quality is lower than GPT-4 but it's free and private. A 16GB Mac or a machine with an 8GB+ GPU runs an 8B model comfortably.

---

## Troubleshooting

**Bot doesn't reply after `/start`**
- Check the terminal — Aura logs every Telegram update. Look for errors.
- Verify `TELEGRAM_TOKEN` is correct and has no spaces or quotes.
- Make sure you're messaging the bot username from Step 1, not a different bot.

**`unauthorized` from the LLM**
- Your `LLM_API_KEY` is wrong, expired, or out of credits.

**Dashboard at localhost:8080 shows blank page**
- The binary should have the dashboard built in. If you built from source, run `make web-build` first.

**"budget exceeded" errors**
- Aura hit `SOFT_BUDGET` or `HARD_BUDGET`. Raise them in `.env` and restart.

**macOS: "cannot be opened because the developer cannot be verified"**
- Right-click the binary → **Open** → confirm. One-time prompt.

**Windows: Defender flags the binary**
- Unsigned binaries from GitHub Releases trigger this. Click **More info** → **Run anyway**.

---

## Building from source (developers only)

If you'd rather build instead of using a Release binary:

```bash
git clone https://github.com/chetto1983/Aura
cd aura
make web-build   # builds the React dashboard
make build       # builds the Go binary
./aura
```

Requires Go 1.25+ and Node 20+.

---

## Getting help

- Logs: the terminal running `aura` shows everything. `LOG_LEVEL=debug` for more detail.
- Issues: open a GitHub issue with the relevant log lines (redact your token).
- Health check: `curl http://localhost:8080/health` should return JSON with `"status":"ok"`.
