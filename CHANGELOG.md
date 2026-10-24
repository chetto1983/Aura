# Changelog

Notable changes to Aura, newest first. Each section is also the text of its
[GitHub release](https://github.com/chetto1983/Aura/releases); the full history is in git.

## Unreleased

### Appliance updates wait for the night

- An edge appliance no longer restarts into a new build after 15 quiet minutes, which could
  catch someone working in the cockpit. With nobody asking, it installs at 03:00 host time
  (`AURA_UPDATE_AT`), once no task is running, and a night a running task fills moves it to
  the next night. **Update now**, postponing and the 24-hour limit
  work as before, and the admin's dialog says when the install comes.

### Cloud services read their key from the settings

- Cloud embeddings, cloud speech-to-text and text-to-speech, and vision through an OpenRouter
  primary model read the services key (`OPENROUTER_API_KEY`) from the settings on every call,
  never from the chat route. With the chat on ChatGPT they used to get no key: memory and
  document search ran on keywords only, and the embedding settings reported "the hosted route has
  no credential".
- A rotated services key reaches the running embedders and speech clients on their next call,
  with no restart.
- The services key goes only to OpenRouter. A manual embedding endpoint no longer receives it,
  and vision through a primary model on any other host sends no key.

#### Upgrading

- **A manual embedding endpoint needs its own key.** Enter it in the embedding settings under
  *Endpoint API key* (`AURA_EMBED_CLOUD_API_KEY`, stored sealed, or set in the environment), then
  save. Until it is set, the route is refused, the preview names the missing key, and memory and
  documents are served by keyword search.

### Document search knows the operator's lists

- **Every result names the library's types and tags**, with how many documents carry each, most
  used first, up to 30 per list. The agent can filter by a tag that the question does not
  spell, such as "the internal documents" for the tag `interno`.
- **Every result names each returned document's own fields:** type, counterparty, date and
  tags, each marked as reviewed by the operator or suggested.
- **A search with no query and at least one filter lists the documents the filters admit.**
  The list is the cockpit's own: most recently added first, with how many match in all.
  "Show me the documents tagged Cliente" no longer depends on a passage clearing the
  relevance floor.
- **Fixed: unknown tags under `tags_mode: all`.** Asking for all of several tags, one of which
  is on no list, used to drop that tag and return documents with only the others. It now
  admits nothing, and the answer names the closest tags.

### Plugin connectors run in the sandbox box

- `aura pack install` puts a plugin's stdio connector in each identity's sandbox box, never in
  the Aura container, whose environment holds the database credentials. Its first start may take
  up to 120 seconds, the time a cold `uvx` needs to fetch a Python server and its dependencies.
  Remote connectors are unchanged.
- A box server takes no secrets, so a plugin that passes a key to its stdio connector is refused
  at install, and the error names the key.

### Financial analysis plugin (prototype)

- The read-only financial analysis MCP server for market analysis on Yahoo Finance data, with
  five skills that teach the agent to read each result, now lives in its own repository,
  [chetto1983/broker-analysis-mcp](https://github.com/chetto1983/broker-analysis-mcp). It ships
  as one plugin that Claude Code, Codex and Aura install as it is. It places no orders and gives
  no personalised advice.
- In Aura: `aura pack install chetto1983/broker-analysis-mcp/broker-analysis`, then
  `aura pack trust chetto1983/broker-analysis-mcp/broker-analysis --class sandboxed_local --reason "..."`.
  The server runs in each identity's sandbox box.
- yfinance reads unofficial endpoints, so production use needs a licensed data source.

## v1.1.0 — 2026-10-08

About 1,700 commits since the previous prerelease, v1.0.2-rc1 of 2026-08-30. Aura is now a
multi-user appliance: every person has their own identity, memory, sandbox, skills and model key.

### Upgrading from v1.0.2-rc1

- **Back up first.** The release adds 26 database migrations (0113 to 0138), applied when
  Aura starts. See [Backup and restore](docs/BACKUP-RESTORE.md). That backup is also the
  way back: v1.0.2-rc1 refuses a database at the new schema, so rolling back means
  restoring the pre-upgrade dump into a new database, not just changing the image.
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

- One-command installer: `npx create-aura-appliance`, local or on a remote host over SSH. It detects
  CUDA, Vulkan or CPU for the embedding model.
- The appliance can update itself from the edge channel. An admin can apply or defer a new
  build from the cockpit; otherwise the appliance restarts into it once nobody has used Aura
  for 15 minutes. (Corrected on 2026-10-24: this line used to say that it asks an admin first.)
- Remote access through Cloudflare, configured from the cockpit.
- Optional trust for the Caddy local certificate authority.
