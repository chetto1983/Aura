# Mail and calendar watch: hear what matters, spend no tokens — design

Date: 2026-10-31
Status: **approved for steps 2-4 of §8 (operator, 2026-11-01: "procedi"); the local model stays open (§5.5).** Asked by the operator on 2026-10-31 after conversation
`01a1926d-e52d-704c-93f7-1317ac7a426c` on the lab VM ("hook su posta e calendari", "non solo google ma
tutti gli endpoint", "non costi una fortuna in token", "modelli NLP senza usare un LLM", "Aura è per
tutti ed è uno strumento professionale", "un dataset sintetico partendo dai dataset originali", "l'utente
con le correzioni fa da finetune").

This spec amends T1 (`docs/teable-behaviours-2026-10-30.md`) and both item 4 and P3's gate in
`2026-10-09-consolidation-best-of-design.md`. Only the spikes and the pilot in §7 were run. Each claim is
marked **measured** (read from the running stack), **read** (from code or documentation) or **assumed**.

## 1. What this is

Any Aura identity, on any provider the PIM supports, is told when something in their mail or calendar
needs them, and is not told otherwise. The reader is a professional or a small organisation (prd.md
§1), so the mailbox is often work mail that must not leave the operator's infrastructure.

One operator's inbox is never the product's definition of "important". Every default here must serve
a user Aura has never seen, in any language, on the first day.

## 2. What was found

| Fact | Status |
|---|---|
| The deployed PIM is not behind: VM digest `f0fb5f26…` = GHCR `latest` = `sidecar` = fork `7797d52`, 46 ahead of and 0 behind upstream 1.9.0 | measured |
| An empty-context model call costs 14-16k input tokens (2 samples, `gpt-6.1-sol`) | measured |
| One "read today's mail" question cost 196,240 input tokens over 8 calls; one mail body was 62k characters of raw HTML | measured |
| A job with a model every 15 minutes (T1's baseline) costs at least 96 × 15k = 1.4M tokens a day with no mail at all | arithmetic |
| `agent_job_runs` has no token column, and no daily spend ceiling exists | measured, read |
| `search_emails` truncates dates to a day, which Gmail reads as midnight PST (`GoogleProviderService.cs:231-235`), so "today" loses mail after about 08-09 in Italy. The clause was seen active; missing mail itself was not measured | read |
| `get_calendar_events` says "omit for all calendars", but Google reads only `primary` and Microsoft only the default calendar | read |
| The fork prefers `text/html` to `text/plain` (`GoogleProviderService.cs:1544`, `ImapProviderService.Support.cs:148`) | read |
| The dispatcher already delivers a handler's returned text, and skips delivery when it is empty (`internal/cron/dispatch.go:384-426`) | read |
| Aura already calls the PIM's `/admin` with an identity-scoped OAuth grant, injected server-side (`internal/agui/connect_pim_api.go`) | read |
| `semindex.Classifier.RankNearest` already gives kNN over a local embedder; the reasoning tier scores 54/58 with it | read, measured |
| Laya is a BERT encoder with a decision head: one forward pass, no generated text, Apache-2.0. `ggml-org/Laya-GGUF` is its English checkpoint, which its own card says collapses outside English while staying confident | read |
| `laya-multilingual` (322M parameters) has no ggml-org GGUF. Third parties publish a 357 MB Q8_0 with `decision_type: laya` | read |
| `POST /v1/systemone` landed in llama.cpp on 2026-10-02 (#29818), 215 commits after Aura's `v0.5.0` pin | read |
| Laya's own numbers: 0.36 zero-shot on typed decisions against a 0.318 random baseline, 0.766 fine-tuned; 193 ms per question for multilingual on 4 physical cores (torch, fp32); Italian 0.50 on 20-way intent, n=100 | read |

## 3. Decisions already on record that this keeps

- An appliance has no public endpoint for pushes, so "a mail arrives" is a poll with a watermark
  (`teable-behaviours`, T1). No webhooks, no bus, no second queue. `go-imap` IDLE is rejected because
  it bypasses the PIM.
- Aura never stores the mail (consolidation, "Not taken"). PMSync's sync engine stays out.
- A job with nothing new calls no model, and a failing source raises an alarm, never a "no change"
  (P2).
- Rules come before any learned piece, and doubt resolves to the harmless outcome (P3).
- BASELINE OR DROP for every learned piece.
- Behaviours transfer; code does not.

## 4. Goals, non-goals, anti-goals

**Goals**
- Mail and calendar changes on every PIM provider, on one contract.
- Zero LLM calls on the watch path, for a new mail as much as for an empty tick. Tokens are spent only when
  the person writes back.
- By default the only mail text that leaves the appliance is the notification on the person's own route
  (§5.2). A local decision model reads mail on the appliance, and a cloud one is an opt-in per account.
- Useful on day one with no training, and better with the identity's own corrections.
- Multi-language, per identity, and configurable with no source edit.

**Non-goals**
- Moving, labelling, replying to or deleting mail. The watch reads, and an Aura label (§5.8) never reaches
  the mailbox.
- Extracting deadlines, tasks or summaries. A model can do that when the person asks, in chat.
- Sub-minute latency. The scheduler's floor is 5 minutes (`MinScheduleEveryMinutes`).
- Push from providers, IDLE, or a local copy of the mailbox.
- Contacts. Nobody asked to be told about them, and recording changes nobody reads is dark code.

**Anti-goals: this has failed even if it works if**
- it stays silent because a source broke, and nobody finds out;
- it alarms so often that people mute it;
- its quality can only be shown by reading someone's work mail;
- a default was tuned to one person's inbox.

## 5. Shape

```text
scheduler (one task per account, every 5 min, no LLM)
  └─ pim_watch handler
       ├─ PIM POST /admin/changes(account, resource, cursor)  identity's OAuth grant
       ├─ triage: rules, then corrections and a classifier once they earn their place
       ├─ verdicts and cursor, one Postgres transaction
       └─ returns fixed text for `now`, or "" when nothing needs telling
             └─ dispatcher: existing Notifier, quiet hours, failure path

scheduler (one task per identity, at the digest hour, no LLM)
  └─ pim_digest handler: the day's verdicts, read live from the PIM by id
```

### 5.1 The PIM's `changes` contract

The contract is PMSync's `getInitialSyncToken` plus `getChangesSince` (the live path at
`sync-orchestration.service.ts:278`), re-specified in C# with its holes closed (§5.6). The PIM stays
stateless: Aura holds the cursor, so the sidecar needs no outbox, no worker, no service credential
and no tenant context outside a request.

`POST /admin/changes` with `{accountId, resource: "mail"|"calendar", cursor, limit}`, under the same
OAuth bearer as `/admin`, so the tenant comes from the token. It reads and changes nothing, and it is a POST
because a cursor holds delta links, one per calendar, which would overflow a request line and land in access
logs. It is not an MCP action: the model never sees it, and the `calendar` tool keeps its description budget.

```json
{
  "changes": [{
    "type": "added|updated|deleted",
    "id": "provider id",
    "calendarId": "for calendar resources",
    "occurredAt": "2026-10-31T14:47:00Z",
    "from": "sender",
    "subject": "subject",
    "snippet": "first text, plain, capped",
    "signals": {
      "providerImportant": true, "providerCategory": "promotions",
      "bulk": false, "authFailed": false, "threadId": "…", "writtenTo": true
    },
    "event": {
      "start": "…", "end": "…", "status": "confirmed|cancelled",
      "organizerIsSelf": false, "myResponse": "needsAction", "fingerprint": "hash"
    }
  }],
  "nextCursor": "opaque",
  "hasMore": false,
  "resetRequired": false
}
```

- An empty `cursor` returns `nextCursor` from now and no changes. Old mail is never read back.
- `nextCursor` covers only what was returned. `hasMore` means call again.
- `resetRequired: true` replaces an expired cursor; it comes with no changes and a fresh `nextCursor`.
- A provider failure is an HTTP error, never an empty success.
- Account permissions (`EmailRead`, `CalendarRead`) apply. A resource the provider lacks answers a
  typed "unsupported".
- The cursor can hold a provider URL. The PIM validates the host on use and Aura never logs it.
- `providerImportant` is Gmail's `IMPORTANT`, Graph's `importance: high`, or an `Importance` or
  `X-Priority` header, which IMAP accounts have too.
- `writtenTo` is rung 3's fact (§5.3): the account has sent mail to this sender, or the thread holds a mail it
  sent. The PIM asks the provider once per sender within a request, skips it for `bulk` mail, and answers
  `null` where it cannot tell, such as an IMAP account with no Sent folder. Aura keeps no address table.
- `POST /admin/items` with `{accountId, resource, items: [{id, calendarId}]}` (at most 50) reads sender, subject
  and time by id, for the digest and the decisions view. An item that no longer exists comes back in `gone`, so a
  deleted mail shows as gone instead of failing the digest.
- A failure names itself, because Aura treats the two kinds differently (§5.6). `503 {"error":"provider_transient"}`
  covers a provider timeout, 429 or 5xx. `502 {"error":"provider_auth"}` is a revoked or expired grant,
  `403 {"error":"permission_denied"}` a lost permission, and `422 {"error":"unsupported_resource"}` a resource the
  provider lacks.
- `fingerprint` hashes start, end and status. Provider cursors return the current event, not what
  changed, so this is how Aura tells a moved event from a new description or someone else's reply.
- Mail means INBOX in the first release. The cursor is a per-folder map from the start, so more folders
  later are a setting, not a new format. Mail a server-side filter files elsewhere is not watched, and
  the account card says so.

| Provider | Mail | Calendar | Cursor expiry | Status |
|---|---|---|---|---|
| Google | `history.list` | `events.list` `syncToken`, per calendar | 404, 410 | read |
| Microsoft 365, Outlook.com | delta per folder | event delta | 410, `syncStateNotFound` | read |
| IMAP | UIDVALIDITY + last UID; CONDSTORE for updates where offered | none | UIDVALIDITY changed | **assumed**: server support varies |
| CalDAV | none | `sync-collection`, else ETag diff | server-specific | **assumed**, no precedent |
| ICS | none | ETag or Last-Modified, then content diff | none | **assumed**, no precedent |
| JSON | none | content diff | none | **assumed**, no precedent |

PMSync covers the first three rows. Its CalDAV stops at a connection test, and it has no ICS feed or
JSON source, so those adapters are new. Calendar changes cover every calendar the account can edit,
which avoids bug 2 for the watch. Read-only subscribed calendars are left out of the default.

### 5.2 The tasks and their state

- **`pim_watch`, one task per watched account,** `every` 5 minutes by default. One account failing
  cannot lose another account's `now`, and a pause stops that account alone.
- **`pim_digest`, one task per identity,** daily at the identity's digest hour. It reads the day's
  verdicts across accounts, fetches sender and subject live from the PIM by id, and lists `now` first,
  capped at 20 numbered lines with a count of the rest. Listing `now` again is deliberate: a notification
  lost after the dispatcher's retries (8, about an hour) still reaches the person that day, with no delivery
  state to keep. The window runs from the last digest, not from midnight, so mail decided after the digest
  hour is not lost. The record keeps each number's account and id, so "apri la 3" needs no search.
- **At most 5 alerts a day per account,** which the person can change. The rest go in the digest with their
  `now` mark, so a noisy sender or a wrong model cannot teach the person to mute Aura.
- **Opt-in per account,** with a switch on the account's card in the cockpit and through the deferred
  `mail_watch` tool, which also takes corrections and rules in chat. The `task` tool stays what it is: it can
  list, pause and resume these tasks, but enabling a watch needs two tasks and a home it does not know about.
  The watch is live from the switch, because the ladder alerts narrowly and all the rest is one digest.
- **A notification always carries sender and subject** (operator, 2026-10-31), as plain text on the route the
  person connected. No switch hides them, because the person and the agent must both know which mail is meant.
- **The agent sees what the person sees.** A watch task has an origin conversation, the one of the route the
  person connected, so the existing `recordToOrigin` writes each notification and digest there with the account
  and the mail id. "Aprila" or "rispondi" then works on the next turn. No LLM runs until the person writes. A
  linked Telegram account has one deterministic thread, which is the origin. Otherwise the route is the cockpit
  and the task opens a conversation of its own, as `homeTask` already does for an `agent_job`.
- **The decisions view** on the account card lists the day's verdicts, read live by id, with a control
  to change a tier.
- **"Handled" means a verdict row.** Verdicts and the new cursor commit in one transaction before the
  handler returns.
- **Per-identity state, under RLS, with the next free migration number:**
  - cursor per (identity, account, resource), with status and last error;
  - one row per (identity, account, resource, item id): tier, who decided, when, and the fingerprint for
    an event. **No sender, subject or body.** This is the answer proposed to the mail-table decision:
    ids and verdicts, not a mail index. Rows expire with the retention sweep and on account removal;
  - rules and corrections: (identity, sender or domain key, tier, when). No text.
- **Two places keep sender and subject, and no place keeps a body.** The conversation keeps what was notified,
  under its own retention, and `pending_notifications` holds it only while it waits. `agent_job_runs.summary`
  holds counts. `Handler.Run` returns one string, so the dispatcher needs a small seam to give the notification,
  the conversation record and the ledger summary their own text.

### 5.3 Triage, with no LLM

Three tiers, the shape `mail-triage` proved useful:

| Tier | What happens |
|---|---|
| `now` | fixed-text notification; quiet hours hold it |
| `today` | one digest, fixed text, no model, at the identity's digest hour |
| `later` | nothing; the mailbox is untouched |

The ladder stops at the first decision, except at rung 3:
1. **The identity's rules** win over everything (§5.4). A rule maps a sender, a domain or a label to a tier.
2. **Authentication failed:** `today`, marked as failed. Never `now`.
3. **People, a floor.** A reply in a thread the identity wrote in, or mail from someone they have written
   to, is never below `today`, and is `now` if `providerImportant`. "Written to" is a provider query, cached
   within the run, with no address table (P3). This is the one defect `mail-triage` guards hardest against:
   a person filed unseen. The rung does not stop the ladder, so rung 5 can still raise the mail to `now`: in
   the pilot a stop here cut Jev from 21 to 8 of 23 `now`.
4. **Provider category.** Gmail's promotional and social categories go to `later`. Graph's `other` says
   nothing about content, so it stays undecided.
5. **The identity's corrections, then the local classifier**, once they earn their place (§7), on
   sender, subject and snippet.
6. **Nothing decided:** `today`.

So a sender never written to reaches `now` only through a rule or the classifier, which is P3's
never-contacted rule. A first CI failure from GitHub lands in the digest, and corrections can raise it.

**`providerImportant` counts only at rung 3,** because on Graph and IMAP the sender sets it. In the pilot it
was true on 25 mails, and 23 of them were `later`.

**"Non lo so" is an answer.** A learned piece may file mail to `later` only above a confidence gate fitted on
held-out data, and everything under the gate is `today`. Doubt never reaches `later`. Without a gate the best
local arm lost 7 `now` and 19 `today` unseen, and with one it lost none (§7). Rizzo Flow's `__insufficient__`
and per-question `policy` are the same behaviour, read and not run.

**Two local pieces check each other.** Laya raises an alert, and the local head can veto it when it is sure the
mail is `later`. In the pilot that halved the false alarms, 18 to 9, and cost no `now`.

**Bulk headers are a signal, never a verdict.** GitHub documents `List-Id` on its notifications
(`docs.github.com/.../email-notification-headers`), so "list mail is advertising" would silence a
failed CI run, and any automated alert a professional depends on.

**Calendar changes need no classifier.** A new invitation, a moved event and a cancelled event are
structural facts: `now` if the event starts within 24 hours, otherwise `today`. A new description or
someone else's reply leaves the fingerprint unchanged and says nothing.

### 5.4 Learning, per identity

The person's corrections are the fine-tune (operator, 2026-10-31). What ships is where every identity
starts, and each identity's corrections change that identity only.

**Two levels, because training a large model needs a GPU.**
- In development, once per model release: the corpus of §7.1 fine-tunes the model, if G2 says that earns
  its place. No real mail is involved.
- On the appliance, continuously: corrections. No large model is trained there. Laya's
  `--freeze-encoder` fits a CPU, but its own docs say it gains much less, and it would put Python and
  torch on the appliance. A small head over the EmbeddingGemma vectors is the exception, because it
  refits on a CPU in milliseconds (§7, arm B).

**In the first release,** changing a tier in the decisions view records the correction. The second
correction for a sender outside a 15-minute window becomes a rule (P3), and the cockpit can also write
a rule directly. The person can do the same in chat ("di queste non avvisarmi più") through `mail_watch`, which
spends tokens only because they wrote. This ships before any classifier, because it is the feed that everything
later needs.

**Corrections by similarity, only if G2b earns them.** A correction then also stores a vector, never the
text, and applies from the next mail. It goes when the account goes. These corrections sit in front of
whichever classifier G2 ships, so this does not depend on which one wins. A correction reaches only
mail close to the corrected one: on the reasoning tier, the 2026-10-06 probe kept its gate at 42 of 45
with a distance bound of 0.30, and fell to 39 with no bound. The value is re-measured in G2b.

Paperless's discipline, as behaviour (GPL, so no code): learn only from what the person reviewed, only
for what they opted into, abstain when unsure, recompute only when labels change.

- **The feed must be proven non-empty.** The tier classifier's learning stalled at 0 of 13 overrides,
  and the adaptive plane was deleted with 11 of its 12 tables empty. If live use shows no corrections,
  learning is dropped and the rules stay.
- `Notifier.Notify` carries text only, so inline correction buttons need a new seam. The first release
  corrects from the cockpit and in chat.

### 5.5 Shadow is for learned pieces

The first release has no shadow, because it has nothing learned. When a classifier lands, it runs beside
the ladder on each identity for a default of seven days: it decides, the decisions view shows where it
differs from the ladder, and it notifies nothing. It goes live only when the person switches it on.
Counts of differences and corrections stay on the appliance. This is how quality is shown on mail no
developer may read.

**Laya is the exception** (operator, 2026-10-31). Once its conversion passes the checks of §7 it is on by default,
with no shadow, and a person can turn it off per account. The decisions view marks each `now` it raised, so a
correction takes one step. The evidence is the pilot's held-out result, far smaller than G2, so live counts of
alerts and corrections are its gate: if it alarms more than it finds, it goes off. The local head of §5.3 keeps
shadow, because it files mail out of the digest, which is the worse mistake.

**Which model is open.** The operator chose Laya before Decider 2B was measured, and Decider then found 22 of 23 `now`
against Laya's 9 (§7). Everything above applies to whichever local model is chosen.

### 5.6 Failure

The traps below are in PMSync's code and must not be ported:
- **A cursor advances only past what was handled.** PMSync returns `newSyncToken` as current even when
  pagination was cut, and nothing reads `hasMore` or `fullSyncRequired`, so a gap is lost silently.
- **An error is an error.** PMSync's IMAP path answers success with a timestamp token, which the next
  call reads as a modseq, blinding INBOX on a server with small modseqs.
- **`added` is real.** PMSync's IMAP delta reports new mail as `modified` and misses expunges.

And what Aura does:
- **A transient failure** (timeout, 429, 5xx) does not count toward the pause, because the next tick is
  the retry and three of them would be a 15-minute outage. The handler keeps it on the cursor row and the run
  completes, with a summary that says so. An account failing for an hour raises one alarm.
- **A hard failure** (revoked grant, lost permission) pauses that account's task
  (`AURA_SCHEDULER_PAUSE_AFTER_FAILURES`) and says so once, on the identity's route and on the card. Each failure
  before the pausing one stays silent, and the run that pauses is always reported.
- **`resetRequired`** restarts from the new cursor and raises one alarm that names the account and the
  gap. It never reads as "nothing new".
- A subject is attacker text. It is rendered as plain text, with no link preview.

### 5.7 Why this needs no model, and what that buys

On this path no mail reaches an LLM. So F1 (MCP results are trusted), item 12's headless-erase concern
and item 7 (PII tokenization) do not block the watch: an injected mail has nothing to instruct. They still
matter in chat, where a person asks a cloud model to read a mail.

The recorded sender and subject (§5.2) are the one exception, because they are attacker text that reaches a
model on the person's next turn. They enter the conversation as untrusted data under F1's envelope
(`2026-10-31-mcp-result-trust-design.md`), so that part of F1 lands before the record does. That part is only
`agent.FrameUntrustedPromptContent`, which exists, so the record does not wait for F1's decision on MCP results.
The cockpit shows the line without its frame.

An opted-in decision model changes privacy, not this. It generates no text and calls no tools, so an
injected mail can at worst change a tier, and the people floor still holds.

### 5.8 Labels, in the person's words

A label says what a mail is about, a tier says how soon it matters, and the two stay separate (operator,
2026-10-31: "cliente, lavoro, personale, opportunità"). A client's mail can be a stopped line or a thank-you,
so a label implies no tier.

- Aura ships a small default set in the identity's language, which the person can rename, remove or extend.
- A mail has one label or none. None reads "Da smistare": it is the label's "non lo so", and it goes in the
  digest like any other.
- The digest groups by label, so it is useful before any detector of `now` is. The decisions view changes
  both the label and the tier.
- A label may carry a tier, which then acts as a rule: "Pubblicità" can mean `later`. "Cliente" carries none.
- Labels live in the verdict rows and per-identity settings, with no mail text, and are never written to
  the mailbox.

**Labels need examples.** In the first release a label comes from a rule or from the person moving a mail. A
local head fills them in only if it earns that in G2. Naming a label is rarely enough: only Decider beat "always
the commonest label" zero-shot, by 7.5 points (§7). So a shipped label would carry seeds from the corpus, a label
the person invents starts empty, and under the gate of §5.3 a mail stays in "Da smistare".

## 6. Fixes that come first

Independent of the watch, and owed to the briefing too. The briefing's grounding check compares its
text with the same run's tool results, so a calendar never read passes it.

1. `search_emails` uses exact instants on Gmail, with epoch seconds, as Gmail's filtering guide says.
2. `get_calendar_events` honours "all calendars" on every provider, or says it does not.
3. A mail body prefers `text/plain`. HTML-only mail goes through readability and reaches the agent as Markdown
   (operator, 2026-11-01), because links, lists and tables are what an order or an invoice carries. The conversion
   lives in the PIM, which is C#, so Aura's `internal/web/html.go` (`ExtractMarkdown`) gives the behaviour and no
   code. The `changes` snippet stays plain text.

## 7. Measurement

**G1, the contract.** Unit tests per adapter, and GreenMail plus fakes for cursor expiry, truncation,
provider errors and UIDVALIDITY change. `sinabis-group/*-synthetic-emails` (CC-BY-4.0, `.eml` with
attachments and invites) seeds a realistic mailbox. A corpus of invented mail involves no real person.

**G2, the classifier, BASELINE OR DROP.** No public dataset can judge this: importance is personal, and
no real labelled Italian mail corpus was found on Hugging Face or Kaggle
(`weijianzhg/email-triage-action-seed` ties priority to category, `urgent` meaning phishing only).
- **Corpus.** §7.1. Nobody's real mail.
- **Arms.**
  - A: the rule ladder alone, the baseline.
  - B: A plus classic ML on EmbeddingGemma vectors, kNN or a small softmax head, seeded from the training
    personas, with the gate of §5.3. It also runs with an English-only bank on Italian mail, which decides
    how many languages a shipped bank must hold.
  - C: A plus `convaiinnovations/laya-multilingual` zero-shot, asked facts, Q8_0 GGUF on `llama-server`.
  - D: A plus the same model fine-tuned on the training personas, asked the tier.
  - E: A plus `typesafe/jev-1.14` through OpenRouter, asked the tier with the profile in the state. It
    sees only sender, subject and snippet of mail the rules left below `now`. If it fails, that mail keeps
    the tier the ladder gave it.
  - G: A plus `Mapika/decider-2b` zero-shot, asked the tier with the profile, on ≤4 CPU threads.

  Dropped by the pilot: F, the EmbeddingGemma 2 typed-decision head.
  `Ippoboi/mmbert-s-email-classifier` (no Italian) and `knowledgator/gliclass-multilang-edge` (a Python
  sidecar) are no longer arms: Laya's multilingual checkpoint covers Italian and runs on the server
  Aura already operates. The English checkpoint is out, because a mailbox is not always English and its
  confidence gives no warning.
- **What Laya is asked.** C asks a fact, not importance: does the mail report a problem. The fact can raise a
  mail to `now`, and the ladder keeps the decision. `deadline` is left out, because it found 2 of 23 `now` with
  14 false alarms in the pilot. D asks the tier, with a few lines of profile in the state. Gate on
  `answer_confidence` after fitting temperatures on held-out data: `confidence` is an entropy, and
  `action.act_probability` carries no signal.
- **Metrics.** `now` precision and recall per language, `today` floor misses, exact McNemar against A,
  p95 per mail, resident memory, on ≤4 threads with no GPU.
- **Rule.** An arm ships only if it beats A by a margin written before the run. C, D or G must also beat B
  by a further margin, because they add a sidecar, of about 360 MB or 1.2 GB. E is judged against A alone and
  ships only as an opt-in. If none beats A, the classifier is dropped and the ladder ships.
- **Spike, 2026-10-31, throwaway, measured.** A third-party Q8_0 (`andreilungeanu`, revision `d481f236`)
  on `b11960`, 4 CPU threads on the workstation, 20 invented Italian mails, two personas. One question
  takes 314 ms p50, and four take 704 ms. Asked the tier, it scored 9 of 40 against 25 for always `later`,
  answered `now` 29 times, and the profile changed 5 answers where 12 should have changed. Asked facts:
  `problem` 18 of 20 (majority 13), `deadline` 16 of 18 (majority 12), `action` 10 of 15 (majority 8) and
  `ad` 15 of 20 (majority 16). `ad` is out: it called a tax notice advertising at 1.00. Failed CI scored
  0.45 on `problem`, just under an unfitted 0.5. These are smoke numbers on mail labelled by their author.
- **Jev 1.14 on the same mail and questions, 2026-10-31, measured.** Asked the tier: 33 of 40 against 25
  for always `later`, all 8 `now` found with 1 false alarm. The profile changed 12 answers, 10 of them
  rightly, so failed CI is `now` for the DevOps lead and `later` for the accountant with no training.
  Asked facts: `ad` 20 of 20, `problem` 20 of 20, `action` 14 of 15, `deadline` 17 of 18. 314 ms p50 with
  the network, 489 input tokens per mail at $0.042/M: 2 cents per 1,000 mails. Same small, self-labelled
  set. TypeSafe states it does not train on input, sets no retention limit and hosts in the US.
  OpenRouter reported 96.5% uptime over the last day, and its own retention was not read.
- **Classic ML on the same mail, 2026-10-31, measured.** Per persona, leave one out, so each mail is judged
  from the other 19 with no seed bank. kNN: 23 of 40, 5 of 8 `now`, 3 false alarms, 0.2 ms. Softmax head:
  25 of 40, 0 of 8 `now`, 39 ms per refit in numpy. The head learned "always `later`", which is what 19
  examples can teach. This is B's worst case, not its verdict. The pilot below measures seeds and corrections.
- **Pilot, 2026-10-31, throwaway, measured.** 200 invented Italian mails, four personas (accountant, DevOps
  lead, lawyer, machine shop), 50 each over a month: 23 `now`, 72 `today`, 105 `later`. Labels were frozen
  before any model ran. Seeds for each persona are the other three, and a replay corrects every mistake in
  date order. `now` lost and `today` lost mean filed to `later`, which is the failure that matters.

  | Arm | `now` found of 23 | False alarms | `now` lost | `today` lost | `later` filed of 105 |
  |---|---:|---:|---:|---:|---:|
  | Always `today` | 0 | 0 | 0 | 0 | 0 |
  | Always `later` | 0 | 0 | 23 | 72 | 105 |
  | Ladder, list mail not counted as people | 1 | 0 | 0 | 0 | 0 |
  | Same, `List-Unsubscribe` means `later` | 1 | 0 | 0 | 0 | 29 |
  | Laya `problem` raises to `now`, p ≥ 0.9 (C) | 9 | 16 | 0 | 0 | 0 |
  | EG2 typed decision, tier (F) | 0 | 0 | 1 | 22 | 33 |
  | EG2, labels that carry a tier (F) | 4 | 8 | 6 | 36 | 57 |
  | Softmax head, seeds and corrections (B) | 4 | 3 | 7 | 19 | 86 |
  | **Same, `later` only at p ≥ 0.7, people floor** | **4** | **3** | **0** | **0** | **52** |
  | Laya `problem`, then the gated head, people floor | 11 | 18 | 0 | 0 | 46 |
  | **Same, the head vetoes an alert at p(`later`) ≥ 0.6** | **11** | **9** | **0** | **0** | **52** |
  | Jev alone (E) | 21 | 31 | 0 | 0 | 16 |
  | **Jev `now`, then the gated head, people floor** | **21** | **33** | **0** | **0** | **42** |
  | Same, Jev `now` only at p ≥ 0.7 | 17 | 8 | 0 | 0 | 49 |
  | **Decider 2B Q4_K_M, p(`now`) ≥ 0.5, gated head, veto, people floor** | **23** | **24** | **0** | **0** | **52** |

  - Accuracy misleads: always `later` scores 52.5%, above Jev's 47%, and loses every mail that matters.
  - `now` is not in the headers: the ladder finds 1 of 23. The best all-local shape finds 11 with 9 false alarms.
    Jev finds 21 with 33, or 17 with 8 at p ≥ 0.7, on mail that leaves the appliance. kNN found 7 with 19 false
    alarms, so the head's job is `later` and the veto, not `now`.
  - Out of sample the shape holds, and only for technical mail. With thresholds chosen on three personas and tested
    on the fourth it finds 9 of 23 `now` (95% interval 22% to 59%) with 9 false alarms, nothing lost and 52 `later`
    filed. By persona that is 6 of 7 for DevOps, 2 of 6 for the accountant, 1 of 3 for the lawyer and 0 of 7 for
    the machine shop, where Jev finds 6 of 7. Outside technical work, `now` still comes from rules or Jev.
  - Decider 2B is the first local arm that finds `now` on every profession. Out of sample it finds 22 of 23 with 24
    false alarms, nothing lost and 52 `later` filed: 6 of 6 for the accountant, 7 of 7 for DevOps, 3 of 3 for the
    lawyer and 6 of 7 for the machine shop. That is above Jev on this mail, zero-shot, on 4 CPU threads at about 2.8 s
    per mail for three questions. Alone it files 36 `today` to `later`, so the gate stays. At p ≥ 0.6 it finds 19
    with 18 false alarms.
  - More questions do not help. `blocked`, `harm`, `soon` and `person` add at most 1 `now` for 2 more false alarms.
    Sending Jev only the mail Laya doubts finds 13 with 24 false alarms, so it is not a way to send less mail out.
  - Jev's zero losses are partly cheap: it answers `later` only 16 times, so it escalates more than these strict
    labels want, and its digest stays long unless the local head trims it.
  - The gate removes the silent losses and costs 40% of the head's filing, 86 down to 52. 0.7, the veto's 0.6 and
    Laya's 0.9 were read on these same 200 mails, so they are values to re-fit, not results.
  - F is out. EG2 found 0 of 23 asked the tier, scored below always `later`, and took 1.45 s per mail for two
    questions on 4 CPU threads.
  - C stays, with limits: it runs on the unverified third-party conversion, 0.84 s per mail for two questions,
    and adds the sidecar and the 360 MB that §7's rule charges it for.
  - Every number rests on few `now`, and avvocata holds only 3. The writers set `known_correspondent` on 154 of
    200 mails, 13 of them with `List-Unsubscribe`, so the ladder rows use it for mail with no list header only.
    The first scoring let that flag shield 166 mails from the models and made C look worthless, so these rows
    let the fact see all 200. Claude labelled three personas from the rubrics, because a separate pass was
    refused, before any model ran. Only DevOps had a separate pass.
- **Topic labels on the same mail, 2026-10-31, measured.** A separate pass labelled the 200 mails: `Lavoro` 94,
  `Cliente` 62, `Pubblicita` 35, `Opportunita` 7, `Personale` 2, with 34 marked as fitting two labels. Each model
  saw a one-line description, as a person would write it.

  | Arm | Right | Second half of the month | Corrections |
  |---|---:|---:|---:|
  | Always the commonest label | 47.0% | | |
  | EG2 zero-shot | 23.5% | | |
  | Decider 2B zero-shot | 54.5% | | |
  | EmbeddingGemma, nearest label description | 26.0% | | |
  | Per-sender memory | 31.5% | 38.0% | 137 |
  | Nearest label plus corrections, kNN | 52.0% | 58.0% | 96 |
  | Softmax head, seeds only | 60.5% | | 0 |
  | Softmax head, corrections only, as for a label the person invents | 62.5% | 68.0% | 75 |
  | **Softmax head, seeds and corrections** | **73.0%** | **72.0%** | **54** |

  With a gate at p ≥ 0.5 the best arm is right on 65%, wrong on 33 mails instead of 54, and leaves 37 in "Da
  smistare". Only Decider beat the commonest label zero-shot, so a label works better with examples. `Personale` and
  `Opportunita` are unjudged: seeds alone found neither. The replay assumes each mistake is corrected, about 13 per persona
  over a month, and real people will correct less. Jev was not run on topics, since that is a paid run nobody
  asked for.
- **Rizzo Flow, 2026-10-31, read and not run.** A Jev-shaped API on Spark-X2.5-4B, Apache-2.0: 4.4 GB at Q8_0,
  about 5.6 GiB of GPU memory, 0.648 on typed decisions against 0.727 published for Jev 1.13, and no GPU-free host
  tested by its authors. The footprint keeps it out of the appliance, and its abstention is taken as behaviour.
- **Decider 2B, 2026-10-31, measured as above; only its price is read.** `Mapika/decider-2b-GGUF`, third-party,
  Apache-2.0, 1.2 GB at Q4_K_M, trained on English only and strong on Italian mail all the same. It ran through
  `decider-ai` 1.10.0 on `llama-cpp-python`, and the container peaked near 2.8 GiB, page cache included, so resident
  memory on an appliance is unmeasured. Its engine only reads option-letter logits at an answer position, so
  `llama-server` without Python looks possible (**assumed**, not tried; the vendor warns that batching rows moves
  probabilities). That is one thing to prove before it replaces Laya.
- **Other open decision models, 2026-10-31, read and not run.** `ZefanCai/Open-Jev-2B` is a LoRA on Qwen3.5-2B and needs
  a GPU, and Clef-flash is 9.4B. Benchmarks do not transfer: EG2 scored 0.764 on typed decisions and found 0 of 23
  here, so a candidate counts only on this mail.
- **Before C and D.** Convert the multilingual checkpoint with llama.cpp's own converter from the official
  safetensors, pinned by SHA-256. On the CPU image with ≤4 threads, measure agreement with the Python
  reference on 100 decisions (#29818 checked the English checkpoint only) and resident memory. The spike
  skipped both, so a bad conversion could explain part of its numbers.

**G2b, the corrections, BASELINE OR DROP.** Replayed on the held-out personas of §7.1. Each stream runs
in date order, its mistakes are corrected as that persona's rubric says, and accuracy is read on the
mail that follows, at 0, 5, 20 and 50 corrections. The baseline is a plain per-sender memory: the last
tier corrected for this sender. If the vector bank does not beat it, only that memory ships and no
vector is stored.

**G3, the watch, on the lab VM.** Five working days on one real mailbox per first-release adapter
(Google, Microsoft, IMAP): zero missed, zero duplicates, zero model calls on the path, and every `now`
delivered on its route. The truth is the provider's own INBOX list at the end of the window against the
verdict rows. An adapter with no real mailbox closes as unproven, and its verification file says so.

This replaces T1's comparison with an `every 15m` agent job. That comparison would spend at least 7.2M
tokens against a path with no model, and it would put real mail in front of a headless model.

**G4, in use.** Counts of alerts and corrections on any deployment, kept local.

### 7.1 The corpus

The operator decided this on 2026-10-31: Claude writes a synthetic set from the public datasets and
translates it into Italian, and people's corrections adapt the result (§5.4). This replaces P3's gate
of an operator-labelled week, which would tune a product default to one inbox. P3's never-contacted
rule stays, inside arm A. The rest of this section is the engineer's call.

**Sources.** Only what a licence allows: `sinabis-group/*-synthetic-emails` (CC-BY-4.0, so the corpus
carries an attribution notice), `imnim/multiclass-email-classification` (MIT) and
`tobniislam/email-categories-dataset` (Apache-2.0). Left out: `Dc-4nderson/email-importance` (no
licence), `kevinzb56/unlabelled-multicass-emails` (no provenance) and the Enron corpus (real people's
mail). The sources give variety of form. Their labels are not reused, because none measures importance.

**Streams, not rows.** Importance depends on who reads, so a mail has no label of its own. The corpus
is ten personas in different jobs, each with a month of about 150 messages from recurring senders. The
same mail can be `now` for one persona and `later` for another. Recurring senders are also what make
corrections testable.

**Italian is localised, not only translated.** A translated American invoice reminder teaches nothing
about an F24 deadline, a PEC or a rejection from the Sistema di Interscambio. Each stream exists in
English and in an Italian version adapted to Italian institutions and habits. Other languages follow
the same recipe.

**Labels.** One written rubric per persona, frozen first. A separate pass labels each mail from the
rubric alone, without seeing the writer's intent or any arm's output. The operator labels a blind
sample of 100 from the rubrics. If Cohen's kappa against the labelling pass is under 0.6, the rubrics
are rewritten before any arm runs. Each mail takes a topic label as well as a tier, in a pass that sees neither
the tier nor the rubric.

**Fields.** Every message is an `.eml` with sender, threading, `List-Id`, `List-Unsubscribe` and
`Authentication-Results`, a sender-set `Importance` where a real sender would set one, plus a flag for
whether the persona has written to that sender. Gmail and Graph scores are not simulated, because that
would measure the simulation. G2 therefore runs as an IMAP account would, and G3 and G4 show what
provider signals add. Those flags come from a sender table fixed per persona before writing, never from the
writer: the pilot's writers marked 154 of 200 mails as known correspondents, newsletters included, and that
decided the ladder's rows until they were re-scored.

**Coverage.** Streams must hold personal mail and real opportunities, because the pilot's work mailboxes gave 2
and 7 and left those labels unjudged. A notification from a real tool carries `List-Unsubscribe` too (**assumed**
for GitHub), so the corpus needs wanted mail with that header. The pilot had none, which is why its 29 of 29
`later` is no reason to make the header a verdict.

**Splits.** Four personas are held out whole, because a person Aura has never seen is the first-day
case. A translation always stays in the split of its original, or a mail would be tested against
itself. Seeds and tuning use the six training personas only. The split is hash-locked and frozen before
any arm runs.

**Size.** 10 × 150 messages × 2 languages = 3,000 messages, about 750k output tokens at 250 per message.
It is written once, in a development session, never on an appliance. Held-out mail must hold at least
100 `now` per language: at 0.9 recall that gives about ±6 points at 95%, which is the resolution of this
gate. A pilot of 50 messages per language comes first and is read by eye.

**Fine-tuning, deferred** (operator, 2026-10-31: "se il fine tune non serve"). The first release runs Laya
zero-shot, because nothing is lost without a fine-tune: mail it misses still reaches the digest. A fine-tune comes
back only if live counts show people raising mail to `now` by hand, which the machine shop's 0 of 7 predicts
outside technical work. If it does, it runs on Modal, and the rest of this paragraph is what it needs.

`ZefanCai/Open-Jev` (read, not used) could replace most of the writing. Its generated records are CC0, it states
they are not Jev predictions, `release-v2-redistributable` has 113,568 rows, and `mailroom-control-v1` has 114,800
rows on invented mail in English, Chinese and Turkish. It holds no Italian and no urgency: `mailroom` labels kind and
category, so it can teach a model to read decisions and spot `later`, not what a person needs now. Leave out the
customer-control questions, which carry no verified licence. The pilot's 200 mails were never in it, which makes
them a clean test.

**Tried on 2026-10-31, and it did not help.** `laya-multilingual` fine-tuned on 26,000 Open-Jev rows (mailroom 12,000,
workflows 8,000, reasoning 3,000, silent failure 3,000; `soft-ce`, 3 epochs, 44 minutes on an A10, about $0.80) reached
0.94 on its own calibration slice and then did worse on the pilot's mail. On the same harness, held out, `problem` found
7 of 23 against 10 for the base checkpoint, the tier found 4 against 6, and the machine shop stayed at 0 of 7. It is one
run with one seed, and it answers only for this route: public decisions in other languages did not teach urgency in
Italian mail. A fine-tune on Italian urgency labels stays unmeasured.

An item is a decision, not a mail: Laya's reference is 1,200
states with 5 questions each, about 5,600 training items and 352 updates, and a head collapsed at 72 (issue
#963, 1,100 items). The pilot gives about 150 mails per fold with one tier each, which can teach nothing, so a
first fine-tune needs about 1,000 Italian mails labelled on five questions: tier, topic, problem, deadline and
action. The vendor reports 4 to 6 minutes for 6,000 decisions on two T4s, and Modal lists a T4 at $0.59 an hour
with $30 of free monthly credit, so several seeds should cost under $2 (**assumed** until run). Tokens to write
and label the mail are the real cost. Each run starts from `--base multilingual` with `--loss soft-ce`, holds out
whole personas, repeats seeds (9 of 15 left the plateau in #963), and starts with a positive control. The builder
reads the update budget from `laya-train --dry-run`. Temperatures must survive conversion into the shipped GGUF,
with any inherited `temperature_by_options` removed. Modal may hold this corpus because it is invented, and it
never holds real mail.

## 8. Order of landing

1. llama.cpp to a current release, as its own job (`b11960` for the embedding sidecar, measured in
   `docs/superpowers/verification/2026-10-31-llama-cpp-b11960.md`). Only step 5 depends on it.
2. The fixes of §6, in the fork, with their own tests.
3. Record the amendments to T1, item 4 and P3, and the mail-table answer of §5.2, in prd.md §11 and §15.
4. `changes` for Google, Microsoft and IMAP, `pim_watch`, `pim_digest`, rules, labels, corrections and the
   decisions view, with no classifier. G1, G3.
5. Laya zero-shot on by default and the local head in shadow, after the checks of "Before C and D" in §7. The
   full corpus, the fine-tune and the rest of G2 wait for a reason from live counts.
6. Corrections by similarity, once G2b passes and live use shows a feed.
7. CalDAV, ICS and JSON adapters, in the same contract.

Only 1 to 4 are planned now (at most two phases staged ahead).

## 9. The engineer's calls, for the operator to overturn

- Three tiers, with the existing briefing untouched and a separate fixed-text digest.
- `/admin/changes` rather than an MCP action.
- One watch task per account and one digest per identity.
- Opt-in per account, live from the switch. Shadow comes only with a classifier.
- Undecided mail goes to `today`. A known correspondent reaches `now` when marked important or when rung 5
  raises it, and never falls below `today`.
- A confidence gate on `later`, with doubt going to the digest.
- Labels separate from tiers, one per mail, a small default set, and never written to the mailbox.
- INBOX only at first, and read-only subscribed calendars left out by default.
- Contacts out, and an event within 24 hours is `now`.
- A watch task's origin conversation is the one of the route the person connected.
- In the first release, corrections in chat, a cap of 5 alerts a day per account and a numbered digest. Learning
  from what the person does with the mail, the calendar link and reminders for unanswered mail come right after.
  The operator was asked about this order and has not answered.
- `mail_watch` as its own tool instead of a `task` kind, `writtenTo` as a signal instead of an endpoint, and
  `/admin/items` for reads by id.
- A digest window that runs from the last digest, and fixed text in the identity's `Lang`, else English.
- No `every 15m` comparison in G3, so no token column is added for it.
- No large model trained on the appliance. Its fine-tune happens in development, on invented mail only.
- The corpus shape: ten personas, four held out, Italian localised, and a blind sample of 100 labelled
  by the operator.
- Laya's multilingual checkpoint in place of mmBERT-S and GLiClass, asked `problem` until it is fine-tuned.
- Jev as an opt-in per account, off by default, and only on mail the rules left below `now`.

## 10. What this does not establish

- That any local classifier beats the rules. That is G2, and "rules only" is a good outcome.
- That a person with no cloud arm is told about most urgent mail. The best all-local shape found 11 of 23 `now`,
  and needs a sidecar the ladder does not.
- That Laya zero-shot helps outside technical work. It found 0 of 7 `now` for the machine shop.
- That Decider's lead survives more mail. It rests on one quantisation, 200 mails and 23 `now`, and its runtime and
  memory on an appliance are unproven.
- That Laya on by default keeps alerts tolerable. The pilot's 9 false alarms in 200 mails would be about 1 a day
  on 30 mails a day, and the veto's threshold was read on those same mails. The corpus cannot prove it on real mail.
- That the pilot's numbers hold. It has 200 mails, 23 `now`, one language and thresholds read on the same data.
- That labels work for what they are most wanted for: `Personale` and `Opportunita` are unjudged.
- That people correct enough. The replays correct every mistake.
- That Laya works here. The spike is 20 mails and the pilot 200, both on an unverified conversion, and the
  vendor's numbers are on torch. Its 0.732 on email triage is the English checkpoint on a family it trained on.
  That fine-tuning fixes the tier on Italian mail is the vendor's result on other decisions, not a measure here.
- That invented mail predicts real mail. G3 and G4 are the check, and a person's work mail is never
  inspected by a developer.
- That a corpus score is independent. Claude writes both the mail and the rubrics, so G2 ranks the arms
  against those rubrics, not against a professional's judgement. The operator's sample tests that gap on
  100 mails only.
- That corpus precision transfers. `now` is over-represented on purpose, and a real inbox holds fewer.
- That missing mail occurred in the VM conversation: the date bug is read, not measured there.
- Provider rate limits at 5 minutes across many identities and accounts. No such load was measured.
- That CONDSTORE, `sync-collection` or ETag behave on real servers.
- That a cursor outlives a long appliance shutdown: expiry is provider-defined, and a gap raises the
  alarm of §5.6 rather than being recovered.
- That shadow's seven days hold enough corrections on a quiet mailbox.
