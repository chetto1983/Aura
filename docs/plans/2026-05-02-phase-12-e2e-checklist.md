# Phase 12 — Compounding Memory: Live E2E Checklist

> **Who runs this**: Davide, manually, against a live Telegram bot + dashboard.
> **When**: after `git pull && go build ./... && cd web && npm run build && cd ..` on master with all Phase 12 slices merged.
> **DB note**: all new migrations (`conversations`, `wiki_issues`, `proposed_updates`) run automatically on first start. No manual schema action required.
> **Default config**: `SUMMARIZER_MODE` defaults to `off`. For the summarizer sections you must set it to `auto` or `review` in `.env` and restart.

Mark each step ✅ as you go. All 21 steps green = PASS.

---

## 1. Setup

**1.1 Build and start**

```
go build ./... && go run ./cmd/aura
```

Expected log lines (in order, within a few seconds):
- `level=INFO msg="aura starting" version=...`
- `level=INFO msg="telegram bot started"`

No `level=ERROR` lines. If you see `failed to create telegram bot`, check `TELEGRAM_TOKEN` in `.env`.

- [ ] Bot starts cleanly with no errors

**1.2 Open the dashboard**

Navigate to `http://127.0.0.1:8080` (or the value of `HTTP_PORT` in your `.env`). Log in with a dashboard token (request one via Telegram: `/request_dashboard_token` or the existing LLM tool).

- [ ] Dashboard loads, shows existing health/wiki/sources cards

**1.3 Verify new sidebar items**

The left sidebar must show three new items below the existing navigation:

| Label | Icon |
|---|---|
| Conversations | speech-bubble squares (MessagesSquare) |
| Summaries | check-file (FileCheck) |
| Maintenance | wrench (Wrench) |

- [ ] All three items visible with correct icons

---

## 2. Conversation archive (slices 12a–12c)

**2.1 Send 5 turns to the bot in Telegram**

Include at least one explicit factual claim in the 3rd or 4th turn, e.g.:

> "My friend Marco lives in Bologna and works in fintech."

The other turns can be anything (greetings, questions, short answers).

- [ ] Bot responds to all 5 turns without errors

**2.2 Open `/conversations`**

Click **Conversations** in the sidebar. Enter your `chat_id` in the filter box (find it by sending `/start` or checking bot logs for `chat_id=...`). Set limit to 10.

Expected: a table showing all 5 archived turns, ordered newest-first. Columns: index, role, content preview, timestamp.

- [ ] All 5 turns appear in the table

**2.3 Inspect a turn in the drawer**

Click any row. A side drawer opens showing the full turn content, role, timestamps, and (for assistant turns with tool calls) an expandable JSON block for `tool_calls`.

- [ ] Drawer opens; content matches what you sent/received in Telegram

---

## 3. Auto-summarizer — `auto` mode (slices 12d–12f, 12i)

> **Prerequisite**: set `SUMMARIZER_MODE=auto` in `.env` and restart the bot.
> Default interval is 5 turns (`SUMMARIZER_TURN_INTERVAL=5`), cooldown 60s (`SUMMARIZER_COOLDOWN_SECONDS=60`).

**3.1 Trigger extraction**

After the bot restarts with `SUMMARIZER_MODE=auto`, send exactly 5 turns (you may reuse the same conversation). The 5th turn triggers extraction.

Wait ~5 seconds after the 5th turn, then tail the bot log. Look for:

```
level=INFO msg="summarizer decision" chat_id=... action=new fact="Marco lives in Bologna..."
```

or `action=skip` for facts that overlap existing wiki pages.

- [ ] At least one `summarizer decision` log line appears after the 5th turn

**3.2 Verify new wiki page**

If the Marco/fintech fact was scored ≥ `SUMMARIZER_MIN_SALIENCE` (default 0.5) and is genuinely new (no similar existing page), the auto-applier creates a wiki page:

```
wiki/marco-lives-in-bologna-and-works-in-fintech.md
```

Open it with any text editor or `cat`. Expected frontmatter: `category: person`, `tags: [auto-added]`, `sources: [turn:N]`. Body contains the fact text and `*Auto-extracted by Aura summarizer.*`.

- [ ] Wiki page file exists and contains the Marco/Bologna fact

**3.3 Verify `wiki/log.md`**

Open `wiki/log.md` (in the wiki directory, not the project root). The last few entries should include at least one line containing `auto-sum new` and/or `auto-sum skip`.

- [ ] `wiki/log.md` contains `auto-sum` entry from this run

**3.4 Check the Compounding rate card**

In the dashboard, navigate to the **Health** view (home `/`). A fifth status card labelled **Compounding rate** should be visible. It shows:

- Headline: `N%` (e.g. `8%`)
- Subtitle: `X pages auto-added this week / Y total`
- Tooltip on hover: "Pages added by Aura's auto-summarizer in the last 7 days"

After a successful auto-extraction, `auto_added_7d` ≥ 1 → rate > 0%.

- [ ] Compounding rate card visible with rate > 0%

---

## 4. Auto-summarizer — `review` mode (slices 12f, 12k)

> **Prerequisite**: set `SUMMARIZER_MODE=review` in `.env` and restart.

**4.1 Trigger a proposal**

Send another 5 turns including a new factual claim different from step 2.1 (to avoid a dedup-skip), e.g.:

> "I just started using Obsidian for note-taking and I love the graph view."

After the 5th turn, no wiki page is written directly. Instead a proposal is queued.

- [ ] No new wiki page appears immediately in `wiki/`

**4.2 Open `/summaries`**

Click **Summaries** in the sidebar. You should see proposal cards. Each card shows:

- Fact text
- Action badge (`new` / `patch`)
- Score badge (e.g. `0.87`)
- Source turn link (deep-links to `/conversations/{turn_id}`)
- **Approve** and **Reject** buttons

- [ ] At least one proposal card visible with the Obsidian/graph-view fact

**4.3 Approve a proposal**

Click **Approve**. A sonner toast shows progress. The card disappears from the list within ~1s. The corresponding wiki page is written.

- [ ] Card disappears; `wiki/obsidian-for-note-taking*.md` (or similar slug) appears in `wiki/`

**4.4 Verify source-turn deep-link**

Click the source-turn link on any remaining card. Browser navigates to `/conversations/{id}`. The drawer opens showing the turn that contained the fact.

- [ ] Deep-link opens the correct turn in the conversations drawer

**4.5 Reject a proposal (alternate scenario)**

If there is a second proposal card, click **Reject**. Toast confirms. The card disappears; status flips to `rejected`; no wiki mutation occurs. If you list proposals again (reload), the rejected card does not reappear in the default view.

- [ ] Reject works without wiki mutation

---

## 5. Wiki maintenance (slices 12g, 12h, 12l)

**5.1 Introduce a broken link (Levenshtein 1)**

Pick any existing wiki page (e.g. `wiki/index.md` or a page you created). Open it in a text editor and replace one valid `[[slug]]` link with a typo version, e.g.:

```
[[aura-architecture]]  →  [[aura-architecturee]]
```

Save the file. Do NOT commit — the maintenance job reads the files directly.

- [ ] File saved with the intentional typo link

**5.2 Wait for nightly maintenance (or trigger manually)**

The nightly task runs at **03:00** local time (`nightly-wiki-maintenance`, `ScheduleDaily: "03:00"`).

To avoid waiting until 3 AM: in the bot logs, find the scheduled `next_run_at` for `nightly-wiki-maintenance`. You can advance the system clock in a test environment, or manually trigger via:

```
# If the admin scheduler tool is exposed via Telegram or the dashboard,
# use it. Otherwise, wait for 03:00 or temporarily set ScheduleDaily to
# a time 1–2 minutes in the future, restart, and wait.
```

After the job runs, look for:
```
level=INFO msg="nightly wiki maintenance complete"
```

- [ ] Maintenance job runs (either nightly or manually triggered)

**5.3 Verify auto-fix**

Re-open the page you edited in step 5.1. The `[[aura-architecturee]]` typo should now read `[[aura-architecture]]` (Levenshtein distance 1 — single candidate → auto-fixed).

- [ ] Broken link repaired in place

**5.4 Open `/maintenance`**

Click **Maintenance** in the sidebar. Any issues that were *not* auto-fixed (orphans, ambiguous broken links with multiple candidates, missing categories) appear as severity-grouped cards (High / Medium / Low). Each card shows the affected page slug, message, and a **Mark resolved** or **Apply auto-fix** button where available.

If the typo from step 5.1 was the only issue and it was auto-fixed, the list may be empty — that is a PASS.

- [ ] `/maintenance` route loads without error; auto-fixed issue does not appear; any remaining issues are listed correctly

**5.5 High-severity Telegram notification**

If any unresolvable broken links (ambiguous or no candidate) were found, the bot should have sent you a Telegram DM from itself:

> "Aura wiki maintenance: N high-severity issue(s) found. Check the dashboard /maintenance."

- [ ] DM received (or no high-severity issues exist — both are a PASS; mark accordingly)

---

## 6. Chord shortcuts (slice 12n)

From any dashboard route, test the following keyboard sequences. Each chord has a 1.2s timeout between the two keys.

| Keys | Expected destination |
|---|---|
| `g` then `v` | `/conversations` |
| `g` then `u` | `/summaries` |
| `g` then `x` | `/maintenance` |

Also test the help dialog:

- Press `?` (Shift+/ on most keyboards) → a **Keyboard shortcuts** dialog opens, listing all chords including the three new ones.
- Press `Escape` or click outside → dialog closes.

Note: chords are disabled when focus is inside an `<input>`, `<textarea>`, or `<select>` to avoid hijacking form typing.

- [ ] `g v` navigates to Conversations
- [ ] `g u` navigates to Summaries
- [ ] `g x` navigates to Maintenance
- [ ] `?` opens the shortcuts help dialog; new shortcuts listed

---

## 7. Shutdown

**7.1 Stop the bot**

Press `Ctrl+C` in the terminal. Expected log lines:

```
level=INFO msg="shutting down"
level=INFO msg="telegram bot started"   ← this is from the Run loop, ignore
```

The process exits within ~3s. No panic, no deadlock.

- [ ] Bot stops cleanly

**7.2 Restart succeeds**

Run `go run ./cmd/aura` again. Same startup sequence as step 1.1. Existing conversations, wiki pages, and maintenance issues persist (SQLite + wiki files survive restart).

- [ ] Restart succeeds; data from the session is still present

---

## Pass criteria

- All 21 checkboxes above ✅
- No `level=ERROR` lines in bot logs during the run (warnings are OK)
- Frontend in the browser shows no console errors for the new routes
- `go test ./...` (skip `-race` on Windows — linker conflict) is green
  - Run: `go test ./internal/conversation/... ./internal/conversation/summarizer/... ./internal/scheduler/...`

---

## Known limitations / notes

- `-race` is skipped on Windows due to a linker conflict with `C:\Program Files (x86)\HMITool7.0\Marco\X86\bin/ld.exe`. Race coverage runs on Linux CI.
- `SUMMARIZER_MODE` defaults to `off`. You must explicitly set `auto` or `review` in `.env` for steps 3–4.
- The compounding-rate card counts `[auto-sum]` entries in `wiki/log.md` from the last 7 days. On a fresh DB the rate is 0% until an extraction runs.
- Maintenance at 03:00 local time is not easy to test without clock manipulation. The auto-fix is thoroughly unit-tested (slice 12q); manual verification of the timing is optional.
