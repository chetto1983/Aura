# Changelog

Notable changes to Aura, newest first. Each section is also the text of its
[GitHub release](https://github.com/chetto1983/Aura/releases); the full history is in git.

## v1.1.0 — 2026-10-08

About 1,700 commits since the previous prerelease, v1.0.2-rc1 of 2026-08-30. Aura is now a
multi-user appliance: every person has their own identity, memory, sandbox, skills and model key.

### Upgrading from v1.0.2-rc1

- **Back up first.** The release adds 26 database migrations (0113 to 0138), applied when
  Aura starts. See [Backup and restore](docs/BACKUP-RESTORE.md).
- **The model key and the Telegram token moved to the cockpit.** `compose.yaml` no longer
  passes `OPENROUTER_API_KEY`, `TELEGRAM_BOT_TOKEN` or `AURA_LLM_*` to Aura. An admin sets
  the route, the model, the OpenRouter management key and the bot token in the first-run
  setup or in Settings. They are stored encrypted in Postgres and kept out of the process
  environment. A key that lived only in `.env` must be entered again.
- **The strict profile is the default.** An unset `AURA_PROFILE` now means
  `single_user_hardened`: stricter config validation, fatal sample credentials, and shell
  and file tools routed through the per-identity sandbox. Set `AURA_PROFILE=dev` to keep the
  old behaviour on a development machine.
- **The installer config changed.** `install.conf` is now format 2 and only asks for the
  infrastructure. A format 1 file is refused rather than half-applied.
- **Removed settings.** Several memory tuning knobs are gone from `compose.yaml`
  (`AURA_MEMORY_*_MAX_*`, `AURA_MEMORY_LEXICAL_MIN_SCORE`, `AURA_REASONING_FIFO_RUNES`,
  `AURA_VISION_CLOUD`, `AURA_PROFILE_DIR` and others). Values left in `.env` are ignored.

### Multi-user

- Each identity gets its own memory database, sandbox box, skills folder and OpenRouter key.
- Admins mint each person's key from one management key, with a credit cap, and see spend
  on a credit dashboard. Web and Telegram turns are billed to the person's own key.
- Two roles. Admins create and remove identities from a roster, and set the OAuth client
  for each mail and calendar provider. Members only connect their own accounts.
- Skills belong to an identity and can be shared with a named person. The house library
  stays readable by everyone and editable only by admins.
- Removing an identity tears down its sandbox, key and memory, and a failed removal can
  be finished later.

### Memory

- One recall over facts, past conversation turns and the agent's own reasoning traces.
- Entities are typed as person, object, location or event, and recall follows a second hop
  through shared mentions.
- The agent receives the part of the graph a question lands on, not a list of sentences.
- Every vector records the embedding model that produced it. Changing the embedding route
  goes through a preview and a confirmation, then re-embeds memory and documents without
  extracting them again.
- Each turn decides once how much effort it needs and preloads the tools it used before.

### Documents

- Hybrid search is reranked and can answer that the corpus holds nothing relevant.
- A hit can bring the passages around it, and the search reports how far ingestion has got.
- When dense search cannot run, documents are answered from the full-text index.

### Images, video and audio

- **Studio**: generate images and video clips through OpenRouter, with the price shown
  before you spend.
- Photo and video editors open from Studio results, chat media and attachments.
- **Video Studio**: a timeline editor with transitions, audio lanes, voice recording,
  text to speech, in-browser noise removal and automatic ducking under speech.
- Telegram receives generated images and clips as native media.
- Hands-free voice mode in the cockpit.

### Agent and tools

- A real browser runs in each person's sandbox, with a live view in the cockpit.
- Stdio MCP servers can run inside each identity's sandbox (`aura mcp add --box`), and
  streamable HTTP servers can be added with `aura mcp add --url`.
- MCP servers can ask the user a question mid-run, answered in the chat. The run's time
  limit stops while the user answers.
- A tool call that outlives its window, or a shell command that reaches its cap, moves to
  the background instead of being killed.
- Outbound messages are saved as drafts and reviewed before they are sent.
- Web fetch reads PDFs and hands script-only pages to the browser.
- Scheduled tasks can be paused and resumed. A task that fails three runs in a row pauses
  itself (`AURA_SCHEDULER_PAUSE_AFTER_FAILURES`, 0 disables).
- A ChatGPT subscription can be connected through the sandbox browser.

### Cockpit

- Tool results render as what they are: terminal output, patches, file text, search
  results, images, memory reads and todo lists.
- Conversations can be archived and deleted in bulk.
- Inline and full-screen previews for HTML artifacts.
- Several files can be selected and handled at once on a phone.

### Install and operations

- One-command installer: `npx create-aura`, local or on a remote host over SSH. It detects
  CUDA, Vulkan or CPU for the embedding model.
- The appliance can update itself from the edge channel, and asks an admin before it
  restarts into a new build.
- Remote access through Cloudflare, configured from the cockpit.
- Optional trust for the Caddy local certificate authority.
