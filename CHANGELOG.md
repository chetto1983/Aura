# Changelog

Notable changes to Aura, newest first. Each section is also the text of its
[GitHub release](https://github.com/chetto1983/Aura/releases); the full history is in git.

## v1.2.0 — 2026-10-24

About 780 commits since v1.1.0 of 2026-10-08. The documents get a workspace of their own,
the agent gets a work board and per-identity tool policies, the six Craft apps open in the
cockpit, and the object store is backed up every night.

### Upgrading from v1.1.0

- **Back up first.** The release adds 4 database migrations (0139 to 0142), applied when Aura
  starts. See [Backup and restore](docs/BACKUP-RESTORE.md). v1.1.0 refuses a database at the
  new schema, so going back means restoring the pre-upgrade dump into a new database.
- **A manual embedding endpoint needs its own key.** Enter it in the embedding settings under
  *Endpoint API key* (`AURA_EMBED_CLOUD_API_KEY`, stored sealed, or set in the environment), then
  save. Until it is set, the route is refused, the preview names the missing key, and memory and
  documents are served by keyword search.
- **Browser logins are signed in again once.** The logged-in browser now runs in its own box.
  A profile left in the shell box by v1.1.0 is deleted when Aura starts, and nothing is copied:
  sign in again from the live view.
- **Deployment administration is for admins.** Writing a setting, `/api/admin/*`, installing,
  trusting, enabling or removing an MCP server, installing a skill and editing the house skills
  now need an admin. A member sees and manages only their own scheduled tasks, keeps
  authorizing their own MCP accounts and keeps writing their own skills.
- **gVisor is retired.** The installer no longer provisions it and the aura container runs on
  runc. A box created on gVisor is recreated on runc at its next start. On a host installed with
  `--gvisor`, runsc stays registered with Docker and nothing selects it. An `install.conf` from
  create-aura-appliance 0.2.10 or earlier still installs: `gvisor=true` only warns.
- **The Video Studio and the photo editor are gone from the cockpit.** The agent edits images
  and video with PhotoCraft and FilmCraft in its box.

### Documents

- **A Documenti view** beside the file manager's folders: the library as a grid with type,
  counterparty, document date, tags, the custom fields the operator switches on and the review
  state, filtered and sorted on the server. It has an inbox of documents a suggestion waits on,
  saved views, and thumbnails rendered by the Craft apps.
- **Fields the operator owns.** Lists and matching rules suggest a document's type,
  counterparty and tags, and an invoice's header lines suggest them too. The detail shows each
  suggestion with what it rests on. The operator's value always wins, a rejected suggestion is
  never offered again, and the history keeps every change. A bulk edit sends only the
  difference and names what it will change first.
- **The agent filters by those fields.** `document_search` takes type, counterparty, tags (any
  or all), a date range and whether unreviewed suggestions count. Every result names the
  library's types and tags with how many documents carry each, and each returned document's
  own fields, marked reviewed or suggested. With no query it lists the documents the filters
  admit, most recently added first, in pages. Asking for all of several tags, one of which is on
  no list, admits nothing and names the closest tags.
- **A document that carries the question's identifiers ranks first**, such as the report that
  holds `RC-2026-040`.
- **Workbooks are searchable by their rows.** Each sheet's rows become passages headed by the
  sheet's name.
- **Source files are documents.** Code and `.txt` files are read as text, and the cockpit, the
  CLI and Telegram accept them. A file Aura does not read is refused with its type named.
- An image or a scan is described or read once per content, not again on every re-index.
- An indexed document whose file left the bucket is removed after two cycles.
- pplx-embed's document floors are measured. `AURA_DOCUMENT_DENSE_MAX_DISTANCE_RATIO` and
  `AURA_DOCUMENT_MIN_RELEVANCE` override them on one `aura docs search` process, to measure
  another space.

### Craft apps in the cockpit

- The cockpit serves GridCraft, PdfCraft, WordCraft, DeckCraft, PhotoCraft and FilmCraft from
  `AURA_APPS_DIR`. A workbook previews in GridCraft and a deck in DeckCraft. **Edit** opens the
  others, where a save can be stored: the file manager and the Documenti detail.
- A save under the file's name replaces it, and any other name lands beside it. An unsaved
  edit is flagged, and the first close is held. Every app has a full-screen control.
- The agent drives the same six apps as built-in box servers, on by default, with one skill
  each.

### Agent and tools

- **A work board.** A Board mode beside Chat, on SVAR Kanban: columns with limits, cards, tags
  and saved views. The agent reads and edits it through a deferred `board` tool. Deleting a card
  asks the operator, and the approval names the card.
- **Tool policies.** An identity can set `ask` or `deny` on a tool or one of its actions. They
  outrank standing approvals and risk tiers: `deny` refuses even a read, `ask` always prompts.
  Set them in Settings under standing approvals, or with `aura gateway policy list|set|clear`.
  Under the `dev` and `local_trusted` profiles the cockpit says they are not applied.
- A refused tool call shows as Refused, and a failed one turns red as it happens.
- An approval prompt names what the call acts on when the tool can say it.
- Plugin connectors run in the sandbox box. `aura pack install` puts a plugin's stdio
  connector in each identity's box, never in the Aura container, whose environment holds the
  database credentials. Its first start may take up to 120 seconds. A plugin that passes a key
  to its stdio connector is refused at install, and the error names the key.
- The embedding picker offers only the cloud models that answer at the stored width, and the
  preview refuses the rest.
- A skill's instructions reach the agent whole, however close to the size cap they are.

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

### Browser

- The logged-in browser runs in its own box, `aura-browser-<identity>`, with its own egress
  sidecar. The shell's tools cannot read its cookies. Downloads cross into the shell's
  `/workspace/downloads`, regular files only, up to 50 MiB each.
- The browser box stays up, so the first browser call answers at once.
- The operator can take the browser in the live view. The agent's writes are refused while
  the operator drives, and **Let Aura drive** hands it back.

### Scheduled jobs

- A job that fails on a network or model error before it calls a tool that changes anything is
  fired again 2, 5, 15 and 30 minutes later, and a failed notice backs off for about an hour. An
  internet outage no longer loses a one-shot job.
- A task whose conversation was deleted opens a conversation of its own and keeps recording
  its runs.

### Backups

- The object store is copied every night at 01:30 (Europe/Rome) into `AURA_BACKUP_DIR/objects`,
  each identity's bucket with its own key. Only what changed is downloaded, and a deleted or
  replaced version is kept for 14 days. The job shows on the board and can be run now.
- `aura objectstore restore [--as-of RFC3339] [--bucket name]` puts back what a bucket lacks.
  It never overwrites a key, and it checks every file's hash first.
- Garage syncs metadata and data before it acknowledges a write.
- A fresh install's first database backup no longer fails on the login tables.

### Install and operations

- **Appliance updates wait for the night.** An edge appliance no longer restarts into a new
  build after 15 quiet minutes, which could catch someone working in the cockpit. With nobody
  asking, it installs at 03:00 host time (`AURA_UPDATE_AT`), once no task is running, and a night
  a running task fills moves it to the next night. **Update now**, postponing and the 24-hour
  limit work as before, and the admin's dialog says when the install comes.
- The stack comes back when Docker restarts, not only at boot.
- An update restarts every service whose mounted config file changed, and retries a stalled
  image pull once.
- The appliance images are on ghcr.io again. An edge host that moved to the GitLab copy in
  October comes back on its own.
- The aura and aura-ingest images install their Debian packages afresh every week, so a fix
  Debian ships reaches the next build. Every image the stack runs is scanned weekly.
- Telegram's commands act as the chat's linked identity: `/clear`, `/search`, `/cost` and
  `/whoami` used to act as the local operator.

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
