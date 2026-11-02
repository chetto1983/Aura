# PIM changes contract — Implementation Plan

**Status (2026-11-02):** Tasks 1–13 done on the fork's branch `aura-watch`, off `main` at `7797d52`,
head `89a1c9b`, 51 commits, not pushed. Task 14 is done except for G3. An independent review found three
critical and four important defects and nine minor ones; Task 15 records what was done about each. A
second review, which read without running, found two critical, three important and five minor ones;
Task 16 records each with what it measured, and *Bounds on a conversion* says what those fixes do not
prove. The whole suite is green against real GreenMail and Dovecot containers. What was read in provider
documentation and exercised only against a fake is listed under *Read, faked, run*; the real-mailbox proof
(G3) is owed and is not claimed here.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan
> task by task. Steps use checkbox (`- [ ]`) syntax for tracking. One atomic commit per task. The code
> below already exists on `aura-watch`: this plan is its record, and each task names its commit.

**Goal:**
- Three reads tell a model the truth: Gmail searches at exact instants, "all calendars" means all
  calendars, and a mail body is its plain part or, for HTML-only mail, Markdown.
- `POST /admin/changes` tells Aura what changed in a mailbox or calendar since a cursor Aura holds,
  on Google, Microsoft 365, Outlook.com and IMAP, without an LLM, an outbox or a worker.
- `POST /admin/items` reads sender, subject and time of a mail or event by id, for the digest.

**Architecture:**
- **The rules live once.** `ChangesService` owns the tenant, the permission, the empty and the expired
  cursor, the limits. `ProviderFailures` is the only place a provider's exception becomes the
  contract's failure. A source only reads.
- **Sources.** `IChangesSource` (`Services/`) is implemented by `GoogleMailChanges`,
  `GoogleCalendarChanges`, `GraphMailChanges`, `GraphCalendarChanges` (`Changes/`) and by
  `ImapProviderService` through `ImapProviderService.Changes.cs`, because IMAP keeps one pooled
  connection per account. Google and Graph sources get an authenticated client from
  `IGoogleApiClients` and `IGraphApiClients`, which each provider implements in a small `.Clients.cs`.
- **The cursor.** `ChangesCursor`: base64url JSON `{v, k, f}`, a version, the source's kind and a map
  per folder or calendar. It is unsigned, so everything in it is untrusted on the way back in.
- **The body.** `Utilities/MailBody` runs ReverseMarkdown after a preprocessing pipeline.
  `HiddenText` removes what a reader cannot see, `LayoutTables` flattens tables that only place
  things. `Body` keeps what the provider holds; the conversion happens at the tool edge.

**Tech Stack:** C#, .NET 10, ASP.NET minimal APIs, MSTest 4.3, Rocks, Google.Apis (Gmail 1.75,
Calendar 1.75), Microsoft.Graph 6.5, MailKit/MimeKit 4.18.2, ReverseMarkdown 6.3.0, AngleSharp.Css
1.3.0 and AngleSharp 1.9.0, all pinned exactly; GreenMail 2.1.15 and Dovecot 2.4.5 in containers for the tests.

**Spec:** Aura `docs/superpowers/specs/2026-10-31-mail-calendar-watch-design.md`, sections 5.1, 5.6, 6,
7 (G1), 8 (steps 2 and 4). Fork differences: `AURA-FORK.md`, changes 1, 7 and 8.

**Verify:**

```bash
docker network create aura-pim-test
docker run -d --name pim-greenmail --network aura-pim-test greenmail/standalone:2.1.15
docker run -d --name pim-dovecot --network aura-pim-test -e USER_PASSWORD='{PLAIN}secret' \
  -v "$PWD/src/CalendarMcp.Tests/TestData/Dovecot/test.conf:/etc/dovecot/conf.d/test.conf:ro" \
  dovecot/dovecot:2.4.5
MSYS_NO_PATHCONV=1 docker run --rm --network aura-pim-test -v "$PWD:/src:ro" \
  -v aura-pim-nuget:/root/.nuget/packages -v aura-pim-artifacts:/artifacts \
  -e PIM_TEST_GREENMAIL=pim-greenmail:3143 -e PIM_TEST_DOVECOT=pim-dovecot:31143 \
  -w /src mcr.microsoft.com/dotnet/sdk:10.0 dotnet test calendar-mcp.slnx --artifacts-path /artifacts
```

Final count at `89a1c9b`, three runs in a row with no filter: **1712 passed, 0 failed, 0 skipped** each time.
(At `efb4f8b`, before the second review's fixes: 1512. At `c0f87a1`, after the first's: 1485. At `7a0c460`,
before them: 1223.) With no servers the same command gives 1626 passed and leaves the 86 IMAP tests
inconclusive, which `dotnet test` prints as skipped; with `CI=true` and no servers those tests fail, as
`ImapServers` is written to do, which was not re-run for this count.

## Global Constraints

- **Do not push, do not open a PR, do not touch `origin`.** The operator forbade it.
- **No real mailbox, no real credentials, nothing paid.** Invented data only, `example.com` addresses.
- **Nothing builds or runs on the Windows host.** Tests run in `mcr.microsoft.com/dotnet/sdk:10.0` with
  the tree mounted read-only, so build output stays in named volumes and git stays clean.
- **Files stay ≤ 600 LOC** for what the fork owns. `GoogleProviderService.cs`, `M365ProviderService.cs`,
  `OutlookComProviderService.cs` and `GetCalendarEventsToolTests.cs` were over that upstream, so new code
  went into new files to keep the fork mergeable (`AURA-FORK.md`, *Pulling upstream updates*), and they
  grew by 16, 5, 5 and 30 lines.
- **A test changes only for a stated reason.** Tests changed while writing this were my own wrong
  expectations in new tests. After the review, two existing tests changed, each with its reason in the
  commit body: `ForModel_LeavesPlainTextExactlyAlone` became a rule about what a person can read, because
  invisible characters now leave plain text too; and `ABlipDuringATokenRefresh_IsNotARevokedGrant` could not
  fail, because its stub bypassed the code it named.
- **A failure is an HTTP error, never an empty success, and a cursor is never logged.**

## Review Focus

The five failures most likely to reach a real person on a real mailbox, each pinned end to end through
the pipeline in `Changes/RealMailboxFailureTests.cs`:

1. **Back from two weeks away.** The provider has dropped the position. `resetRequired: true`, no
   changes, a fresh cursor: it must never read as "nothing new".
   `BackFromAHoliday_ThePositionIsGone_AndTheAnswerSaysSo`.
2. **A Monday burst larger than a page.** A cut page advances only past what it returned. PMSync
   returned the mailbox's current position here and lost the rest silently.
   `AMondayMorningBurst_ArrivesWholeAcrossPages`, plus a truncation test per adapter.
3. **A network blip while a token refreshes.** Aura pauses a watch on `provider_auth` and tells the
   person, so a blip must be `provider_transient` and a revoked grant must not.
   `ABlipDuringATokenRefresh_IsNotARevokedGrant`. This one found two bugs in code the contract only
   sits on top of: see Decisions, *The token paths*.
4. **A newsletter hiding instructions in text nobody sees.** A snippet is read by rules and by a
   classifier. `ANewsletterWithHiddenInstructions_GivesASnippetWithoutThem`.
5. **A colleague's reply in a thread the person started.** The one mail that must never be filed
   unseen: `writtenTo` is true for a stranger's reply in such a thread, and a thread that cannot be
   read fails the page with 503 rather than answering null.
   `AColleaguesReply_CountsAsWrittenTo`, `AColleaguesReply_WhoseThreadCannotBeRead_IsRetriedNotFiled`.

Also worth a reviewer's eye:

- **`GraphLinks.Checked`.** `BearerTokenAuthenticationProvider` puts the account's token on any host,
  so this one method is what stands between a forged cursor and the token.
- **`BoundUid` in `ImapPosition`.** Arriving gives a mail a mod-sequence, so without the bound every
  new mail would also be reported as updated. The test that found this is real Dovecot behaviour.
- **`GmailHistory.Net`.** Whether a mail was in the Inbox before the page and after it is the whole
  algorithm. It decides "archived is deleted".
- **`ChangesCursor`'s serializer options.** They are the whole shape check on an unsigned cursor. Taking
  either one off lets a damaged position through to a source.
- **Read-only opens.** A mail staying unread proves nothing about them, because `BODY.PEEK` keeps `\Seen`
  on its own. `WatchingAMailbox_OpensEveryFolderReadOnly` counts `\Recent` from outside instead.

## Decisions taken while writing this plan

**The contract**
- **Conversion is bounded, once, for every provider.** A body goes through one cap of 1 MiB, an AngleSharp
  `MaximumDepth` of 256, and a time budget that the parser, the hidden-text walk, the table flattening and
  the plain-text walk all read off the clock: 500 ms for a snippet, 3 s for `get_email_details`. A tree that
  reaches the depth limit is refused whole, because past it AngleSharp attaches a node's children beside it,
  which can lift text out of a hidden ancestor. A page spends at most 30 s on snippets; the rest of its mails
  arrive with an empty snippet and the page returns as usual, with no early-stop cursor. IMAP converts after
  it releases the account's gate, and holds at most 16 MiB of bodies for a page.
- **A forged cursor or item is a 400, never a 500.** A 5xx tells Aura to retry what can never work. A cursor
  for an `inboxFolder` the account no longer watches is not forged but expired: the watch starts again from now.
- **A stored password that cannot be decrypted is `provider_auth`.** The key ring is gone, and only a person
  can enter the password again. Whether a temporary login failure on IMAP should stay `provider_transient` is
  not decided: it goes to G3.
- **A Sent folder that has gone leaves `writtenTo` null**, as an account with no Sent folder does. Measured:
  GreenMail refuses the folder; Dovecot 2.4.5 with its default mailboxes makes a deleted special-use folder
  again at once, so there it is a Sent folder holding no mail, and `writtenTo` is false. A NO on EXAMINE
  proves nothing by itself, because RFC 5530's UNAVAILABLE and INUSE are NOs too, and a page that read a blip
  as gone would answer 200 and move its cursor past those mails. So a NO is confirmed with a fresh LIST, and
  a folder still listed fails the page.
- **A mail's body is chosen by what its parts mean.** `text/plain` wins only inside `multipart/alternative`,
  anywhere else the first part with words in it does, and a blank part is no part.
- **One mail never blocks a page.** A page is all or nothing, and a page that fails is replayed from the
  same cursor, so one mail that cannot be read would stop every mail behind it for good. So a mail whose
  body cannot be converted, for any reason, arrives with an empty snippet, like a body over 1 MiB. The
  verdict ladder does not read the snippet. `get_email_details` answers the metadata with `bodyFormat`
  `omitted` and says why in `body`; it never falls back to raw HTML, because that is what the filter
  exists to keep from the model. A provider failure is different and still fails the page: the rule is
  about what a mail contains, not about whether the provider answered.
- **`limit` defaults to 50, at most 200.** An added mail costs one provider read, at most two
  `writtenTo` queries and its conversion, sequentially. So 200 HTML newsletters are a request of a
  minute or two at worst, and Aura's HTTP timeout has to allow for that, while 50 stay under half a
  minute. A 5-minute tick at 200 drains 2,400 mails an hour, more than any person receives. Zero,
  negative or missing means the default. A larger value is clamped, not refused.
- **`limit` is soft.** A cursor can only stand where a provider can resume, so a page can run over
  rather than lose a change: a Gmail history record can hold several messages, a Graph or Google page
  is one unit, and IMAP's expunges and mod-sequence groups are not split.
- **`snippet` is at most 400 characters.** That holds a greeting and the first sentences, which is
  what a triage classifier reads. It is cut at a word.
- **`/admin/items` answers `{items, gone}`.** Each source reads an item itself with no body: Gmail
  `format=metadata`, Graph three fields, IMAP an envelope. Going through `get_email_details` would
  fetch 50 whole mails with their attachments to learn 50 subjects. An event's time is in `event.start`.
- **Failures beyond §5.1.** Malformed requests answer `400`: `invalid_cursor`, `invalid_account`,
  `invalid_resource`, `invalid_items`, `too_many_items`. A missing or foreign account answers
  `404 account_not_found`, byte for byte alike. A cursor from another version is `resetRequired`, because
  it was valid once; text that is not a cursor is `invalid_cursor`, and so is a cursor of another source.
- **Unknown exceptions stay 500.** `ProviderFailures` names what it recognises; a bug does not get
  dressed as `provider_transient`. Any other 4xx from a provider is transient, because it says nothing
  about the grant, and Aura's alarm for an account failing an hour covers one that never clears.
- **A missing folder is `provider_auth`.** It is not a blip, and only a person can fix the setting.
- **`fromName` and `signals` are additive.** `fromName` is new beside `from`. `event.end` is null for a
  cancelled Google instance, which guarantees only its original start.
- **A disabled account answers like an enabled one,** as every single-account tool does.
- **Dates are written with `Z`,** as §5.1's example shows, not `+00:00`. An instant with an offset is
  turned to UTC on the way out, so what Aura stores compares the same whatever the provider sent.
- **A cursor field is never optional.** System.Text.Json fills a missing constructor parameter with null
  whatever its type says, so a damaged cursor used to reach a source in pieces: a 500 for a Graph stream
  or an IMAP known set, and a Google position with no sync token would have read a whole calendar as
  new. The serializer now refuses it, as `invalid_cursor`, with one option and no check in any source.
  A nullable parameter with no default is required too, by that option's documentation, so nulls are
  written out and a cursor grows by a few bytes.

**`writtenTo`**
- **One query per sender, plus one per thread when it can change the answer.** §5.1 says once per
  sender and also that the fact includes "the thread holds a mail it sent", which a sender query
  cannot see. So the thread is asked only for a reply (`In-Reply-To` or `References`) from someone the
  account has not written to, and is cached per thread for the request.
- **A failed query fails the page.** Null means "cannot tell", which Aura treats as no floor, so a
  blip could file a person unseen.
- **A sender is attacker text.** An address outside a plain `local@domain` shape never reaches a search
  query, on any provider, and answers null. On IMAP a hit is confirmed against the envelope, because
  SEARCH matches substrings: `supermario@` is not `mario@`. Only the newest 20 hits are confirmed, so with
  more hits than that and no match the answer is null, not false. A thread's ids do the same.

**Gmail**
- **No `labelId` on `history.list`.** A filtered history cannot show a mail leaving the Inbox.
- **Mail leaving the Inbox is `deleted`.** Mail means INBOX (§5.1), so archiving a mail removes it
  from what is watched. Mail entering the Inbox by a label is `added`, whatever its age.
- **`messages.get` in format `full`, not `metadata`.** The reference does not list `snippet` for
  metadata, and the snippet has to come through the hidden-text pipeline anyway.
- **`providerImportant` is the label only.** §5.3 keeps header claims to the providers with nothing better.

**Google Calendar**
- **The role is read from `calendarList` entries, not `minAccessRole`.** The reference gives no order
  among roles, so `writer` may or may not take in `writerWithoutPrivateAccess`. Hidden calendars count.
- **`maxResults` is constant at 200,** because the sync guide promises undefined behaviour if a
  parameter changes between a listing and its tokens. `singleEvents=true` gives a moved instance its own
  start; the price is one row per instance when a series changes.
- **"New" comes from `created`, with 2 minutes of tolerance.** A token returns the current event and not
  what happened, so an invitation to an older event reads as `updated`, and an update can read as new
  within the tolerance. Aura's verdict rows settle both.

**Microsoft mail**
- **Three streams with `changeType`.** A delta row does not say what happened, and guessing `added` from a
  date would make a slow delivery arrive as `updated`. It costs three calls a tick.
- **`$filter=receivedDateTime ge`, so a first round is empty.** Otherwise Start walks the whole
  Inbox three times, and on 200,000 messages it would outlive the request and never finish. The
  price: a mail moved into the Inbox with an old date is not `added`. Unlike Gmail, it is not seen at all.
- **A stream lives one day.** The page says a filtered delta "returns only up to 5,000 messages", and
  not whether per round or for life. If for life, a stream would go blind with no error. The
  successor opens before the old stream's last read, reaching back an hour, so they overlap: a mail in
  the overlap can come twice, which Aura's verdict rows absorb.
- **Snippets come from `body`, not `bodyPreview`,** which knows nothing of hidden text.
- **Ids are Graph's default, not immutable ones,** as the existing tools use. A mail moved out of the
  Inbox changes id, so `/admin/items` reports it `gone`.

**Microsoft calendar**
- **Calendar view delta with a window of a day back to 400 days ahead,** because delta with no range is
  beta only. A token keeps its window for life, so one under 200 days is replaced the same way as a mail
  stream. Its first round is every event in the window, walked and thrown away: there is no `$filter`.
- **`@removed` is not a deletion.** The reference says it also marks events outside the window that were
  added or changed. A row has no date to filter on, so the event is read: a 404 is `deleted`, and
  anything else is `updated` with its real start, which must not reach a person as a cancellation.
- **An organizer's cancellation is `updated` with status `cancelled`.** The event stays on the attendee's
  calendar with `isCancelled`, and the status moves the fingerprint.
- **`$deltatoken=latest` does not apply.** The overview documents it for Entra resources and
  `token=latest` for OneDrive and SharePoint. Neither covers messages or events, which is what the
  spec asked.

- **Graph `updated` and `deleted` reach only as far back as the stream's filter** (M7). That is about a day,
  so whether a mail moved into the Inbox is seen depends on the age of the stream. Aura ignores these types
  for mail today. If it starts to read them, this is the limit it inherits.

**IMAP**
- **QRESYNC is left out.** RFC 7162 has CONDSTORE report no expunges, and QRESYNC would close that, but
  it is a second code path for one fact, and GreenMail cannot exercise it. The newest 1,000 UIDs are
  compared with what the server has instead, so **a deletion older than that goes unnoticed**. Nothing
  depends on it: a person's digest is about recent mail.
- **Dovecot joined the tests.** GreenMail 2.1.15 offers no CONDSTORE and accepts any password, so it could
  prove neither updates nor a hard failure. The `CI` rule covers both servers.
- **`GetStreamAsync`, not `GetBodyPartAsync`.** Measured: it reads an empty body from GreenMail, which
  answers the part before its MIME headers, as RFC 9051 allows.
- **A body over 1 MiB is not read for its snippet.** The mail still arrives, with an empty snippet.
- **`threadId` is the first of `References`, then `In-Reply-To`, then its own `Message-Id`.**
  `providerCategory` is null: IMAP has none.

**The body (spec §6 item 3, the operator's change of 2026-11-01)**
- **ReverseMarkdown 6.3.0, MIT, as the operator asked.** Also read: Html2Markdown (Apache-2.0), rejected
  because ReverseMarkdown was named and has the pipeline; Goldziher.HtmlToMarkdown, rejected for its Rust
  native bindings; BFound.HtmlToMarkdown, HtmlToMarkdownConverter; NReadability; MimeKit's
  `HtmlToHtml`, which writes no Markdown.
- **`GithubFlavored = true` on the default flavor, not `Flavor = GitHub` as the brief suggested.**
  Measured on the fixture: `Flavor = GitHub` passed the input through, 62,890 characters in and out,
  because that flavor keeps raw HTML. `docs/configuration.md` and `docs/preprocessing.md` of the
  package say so. The flag still gives GFM tables.
- **The URI whitelist is http, https, mailto.** `javascript:` and `data:` links lose their target and keep
  their text, under test.
- **No readability library.** Measured below: SmartReader 0.11.1 (Apache-2.0) did not help a newsletter,
  which is not an article. It answered `readable=False` and still returned 36,356 characters of HTML, and
  its Markdown came to 3,986 characters with **no link left**, "Leggi l'articolo" pointing nowhere, while
  navigation and footer stayed. Preprocessing alone reached the target, and one dependency is better
  than two.
- **AngleSharp.Css is the one extra dependency, and why.** ReverseMarkdown's `RemoveHidden()` reads
  inline styles only, and hiding by a `<style>` rule is ordinary in mail. The cascade costs time, below.
- **Tracking parameters are cut; the body is not capped.** `utm_*`, `mc_eid`, `gclid` and similar leave
  links, with a test pinning them. The fixture lands at 11,337 characters with no cap, so none was needed.
- **Judge everything, then remove.** A `<style>` element computes as `display:none`: removing as the walk
  goes emptied the cascade that later nodes were judged by.

### Measured on the newsletter fixture

`TestData/Mail/newsletter-large.html` is an invented newsletter of 62,890 characters on `example.com`
domains, in the shape of real ones: nested layout tables, a preheader, tracking redirects, a footer.

| Step | Characters | HTML tags left | Links |
|---|---:|---:|---:|
| Raw HTML | 62,890 | | |
| `Flavor = GitHub`, alone | 62,890 | all | |
| Defaults, alone | 57,667 | 248 | 7 |
| GitHub flavor plus preprocessing | 37,792 | | |
| ...plus unwrapping layout tables | 31,306 | 84 | |
| Default flavor, `GithubFlavored`, preprocessing, unwrapping | 25,571 | 0 | 83 |
| ...plus tracking parameters cut, images to alt, empty anchors gone | 11,271 | 0 | 46 |
| **Final pipeline, with the cascade** | **11,337** | **0** | **46** |
| Rejected: URLs capped at 120 characters | 3,957 | 0 | 0 |
| Rejected: SmartReader first | 3,986 | 0 | 0 |
| `ToPlainText`, for the snippet | 3,895 | 0 | 0 |

Conversion takes about 0.38 s the first time in a process, 0.31 s of it a warm-up that runs once and
before any budget starts, and about 40 ms after that for this fixture, measured in the SDK container on
a Release build at `efb4f8b`, medians of five fresh processes: Markdown 68 ms on its first run and 36 ms
warm, the snippet's plain text 61 ms and 34 ms. At `6e86d7e`, before the review's fixes, the same four
figures were 216, 54, 240 and 55 ms. The limit on pasted variables costs nothing measurable: `c0f87a1`
gave 81, 43, 61 and 40 ms. At `8be80a2`, with a thread of its own for each conversion, a selector estimate
and a shape check, the same four were 63, 35, 66 and 41 ms, and the warm-up 302 ms. A page of 200 such
newsletters would take about 8 s, and that is the worst case. The thread itself costs about 0.4 ms at the
deployment's half a CPU, measured in the image: 2,000 empty runs at a time, three rounds.

### Hidden text

| Covered, under test | Not covered |
|---|---|
| `display:none`, `visibility:hidden` or `collapse`, the `hidden` attribute | External `<link>` stylesheets, never fetched |
| Font size zero or near it: 0, 0px, 0.0em, 1px, 1pt, 10%, and `smaller` or `larger` of those. A keyword names its own size and undoes a zero wrapper | `::before` and `::after` content |
| `opacity` of 0.1 or less, as a number or a percentage, and `mso-hide:all` | Opacities that only multiply below 0.1 across levels |
| Text in the colour of its background, within a small distance, `bgcolor` included | `transform` in any form, `scale(0)` and `translate` off screen alike, and overlays |
| Boxes of 2 px or less high or wide with `overflow` hidden or clipped. A table cell only as its table lets it clip: by `width` in a fixed table, by `max-width` in a table with no usable width | A small box that lets its text overflow, which a reader can see. A cell squeezed to nothing by a neighbour that takes the whole width of its table, which only a layout could see |
| Off screen: `text-indent`; `position` absolute, fixed or relative past −100 px by `left` or `top`, with what relative boxes above have added; past −1024 px by `right` or `bottom`, or −100 px on a fixed box, which the page does not hold; past the screen's width by `left`, `right` or `bottom`, and by `top` on a fixed box | `position` offsets short of those, and an absolute or relative `top` of any size, where the page grows to hold the box |
| Negative margins of a screen's width, 1024 px, or more | Negative margins short of that, which layouts use to overlap |
| `clip: rect(0…)`, and `clip-path: inset()` whose opposite sides meet | `clip-path` with any other shape, such as `circle(0)` |
| All of those by inline style or stylesheet, inherited or not | Low contrast further than the colour check's distance |
| `@media` rules, read for a 1024×768 screen: a rule that applies at that size hides, and one for a phone or for print does not | Text hidden only on a phone, or only at any size but that one |
| `<template>`, `<noscript>`, comments, scripts, `alt` of hidden images and of images 2 px or less high or wide | Text inside images |

A colour check assumes a white page where no background is set, and treats a background image as
unknown, so white text on a photo is kept. The rendered-text tests pin the false positives that matter:
a ghost column, a font reset, white text on a coloured or dark cell, a small relative nudge, a hero
overlap by negative margin, a clip that leaves a band, the `alt` of an image with no stated size.
Invisible Unicode is dropped from both formats: format characters and everything Unicode marks
`Default_Ignorable_Code_Point`, which includes the tag block and the variation selectors.

The rules for font keywords, offsets and table cells come from rendering each shape in Chromium 131 at
1024×768 in a container, and measuring what is left to see. It did not always agree with the review:
`position:relative;left:-50%` alone does leave the screen, so it stays removed, and it is the centring trick
only inside a wrapper with `left:50%`, which is why the walk adds the two up. Text a reader can see that
is still removed, each measured visible and each left because the fix would not be small: `<font size>`
under a zero-size wrapper, a `tr` with `height:0` and `overflow:hidden`, a `div` with `display:table-cell`
in the ellipsis trick, and the centring trick whose outer box floats right and moves `left:-50%`.

**One way around all of this stays open:** a `multipart/alternative` mail can say one thing in HTML and
another in its `text/plain` part, which no mail client shows. §6 prefers the text part, so a model reads
what the sender wrote there. Comparing the two is not in this plan.

**A preview cannot be filtered.** Gmail's `Snippet` and Graph's `BodyPreview` are built by the provider
from the whole mail, hidden text included, and nothing here can tell what in them was hidden.
`get_contextual_email_summary` uses the converted body where it has one. On a list it has only the preview,
so it strips invisible characters, caps it, and says in its description, and in the curated tool's, that a
preview cannot be filtered for hidden text. The contract's snippets come from `body` for this reason.

An event's body goes the way a mail's does, because a stranger's invitation lands on a calendar unasked.
Google gives a `description` no type and says it "can contain HTML", so one with a tag in it is read as
HTML. Reading plain text as HTML would lose its line breaks, so one with no tag stays text.

### Bounds on a conversion

One stranger's mail must not be able to stop a watch or a server. Two layers, because each covers what the
other cannot.

**Layer one reads the mail and refuses before the work.** A refusal is `omitted` with the other fields
intact, and an empty snippet.

| Bound | What it stops | Set at | What it keeps |
|---|---|---|---|
| `MarkupShape`, start tags × tokens, read from the raw text | End tags and text walking the open elements, where no element is created and no budget is read: 100,000 `<x>` and 185,000 `</y>` ran past 90 s | 2×10⁹, about 10 s of one thread | 1 MiB report 1.4×10⁹, 1 MiB of paragraphs 1.9×10⁹, newsletter ×16 1.1×10⁸ |
| `MarkupShape`, units the tokenizer recurses on (`</>`, `--!`) | Stack overflow on cold code, which kills the process: 43,288 units survive 8 MiB and 43,597 do not | 1,024, a forty-second of that | A mail has a handful |
| Parse allocation | `<p><b a{i}>x</p>`, which copies formatting: 33 KB allocated 1.2 GB | 1 KiB per character, floor 64 MiB | 240 at worst on ordinary mail |
| `SelectorCost`, on the mail's real depth, longest sibling run and each rule's combinators, through `:has()`, `:is()`, `:not()`, `:where()` | Matching that tries every path with no memo: the review's mail estimates 4×10²⁵ steps | 2.5×10⁸ on one element, 1.3 s warm, 6 s on columns | Newsletter 10, ×16 160, a chain of 7 on 51 levels 1.4×10⁸ |

**Layer two limits the wait, whatever got past.** `ConversionThread` runs a conversion on a thread of its own,
on both the snippet path and `get_email_details`, outside IMAP's account gate. It waits the budget and 500 ms,
then leaves the thread behind and answers `omitted`. Two run or are left behind at once; past that a
conversion is refused at once until those threads end or the process restarts. Each leaving logs a warning
with no mail content.

**The stack doubt is gone for conversion.** The thread's stack is set in code to 8 MiB, so `ulimit` no longer
decides it. Read from `pthread_getattr_np` in the image: at `ulimit -s` 8192, unlimited and 1024 a pool thread
has 8192, 2048 and 1024 KB, and a conversion has 8192 KB in all three. The worst allowed mail needs 608 KB.
So Kubernetes, whatever its runtime sets, cannot change this for conversion. The rest of the server still
runs on what it is given.

**What the two layers do not prove:**
- **Layer two limits a wait and stops no work.** .NET cannot end a thread. A left-behind thread runs and
  holds what it allocated until it ends, and one that never ends holds its place until a restart. Two of
  those stop all conversion until then. The server and the watch keep running, with empty snippets.
- **A stranger can deny conversion, though not the watch.** A 268 KB mail just under `MarkupShape`'s bound
  is parsed, and is read on both paths, so it took both places. At the deployment's half a CPU it held them for
  48 to 57 s over five such mails, which is the 10 s above shared. In that time other mail arrives with an empty snippet and an `omitted`
  body, and the page answers 200, so a stranger sending one a minute could keep it that way. Nothing hangs,
  and a mail asked for again converts. Lowering the bound would refuse large ordinary mail, because the shape
  check cannot tell a table that closes what it opens.
- **Layer two covers conversion only.** It does not cover MimeKit reading a message, MailKit, an IMAP read or
  anything else on the way to it. A stack overflow is not an exception and no thread survives it, so depth and
  recursion rest on layer one's bounds alone.
- **Layer one's numbers are measurements of one engine.** AngleSharp 1.9.0 and AngleSharp.Css 1.3.0, both
  pinned. `SelectorCost` models the matcher as read in their source and is calibrated, not proven. A pseudo-class
  it does not know costs infinity, so what is new is refused and does not get through. Upgrading either reopens every figure.
- **Long parser paths were searched, not proved absent.** About 29,700 shapes, every short unit in 18 contexts at
  200 KB, ran on forced-cold code in the image with no crash, and 69 were slow, the ones printed stopping at
  their 2 s budget. That is a search. `<a>` in a table can overrun its budget by seconds, which is layer two's job.
- **The Markdown reader reads no budget.** Only layer two limits what it costs.
- **False refusals.** A bare table of no styling is refused above about 320 KB, because it scores like a
  flood: 1 MiB of it would take 1.7 s and is refused. A styled table is under the bound at the cap.
- **Memory under the limit was measured for one conversion.** A bomb under `--memory 512m` became a caught
  `OutOfMemoryException` at a peak of 465 MB, and the server lived. Three at once were not measured at their worst.
- **A warm-up that fails fails every conversion after it.** `MailBody.WarmedUp` is a `Lazy` that caches its
  exception, and `WarmUp` runs outside the `try` in `SnippetPage` and `MailSnippet`. It has not happened, and
  the 64 MiB floor is there so it does not. It stays latent.
- **Neither layer says the filter is complete.** They bound what a mail costs, not what it hides. *Hidden text*
  above lists what passes.

Deep MIME is the same honesty for a body. A mail nested past `MailParts`' 32 levels read as empty, which a
stranger could use to make any mail look as if it had nothing to say. It now says its body is left out.
That was this provider, not MailKit or a server. Measured: a mail reads at 32 levels and goes empty at 33
on GreenMail and Dovecot. At 5,000 levels GreenMail overflows its own stack in `parseBodyStructure` and drops
the session, which is why the row at that depth runs on Dovecot alone, where the answer is the same. Whether
Gmail or Graph send such a tree back is not known.

## Read, faked, run

Read means the page was read on 2026-11-01. A fake is a `FakeHttp` shaped from that page's samples. Run
means a real server. **No behaviour here has met a real Gmail, Google Calendar, Microsoft 365 or
Outlook.com account: G3 is owed and is not claimed.**

| Behaviour | Source | Exercised |
|---|---|---|
| `history.list`: 404 on an old id, chronological, `historyId`, `maxResults` | Gmail `users.history.list`, sync guide | fake |
| `getProfile.historyId`; `messages.get` formats; `threads.get` minimal; `messages.list` `labelIds`, `q` | Gmail reference | fake |
| A message gone before `messages.get` answers 404 | Not in the sync guide: assumed from the API's errors | fake |
| Headers come in message order, so the first `Authentication-Results` is the topmost | Not documented: assumed | fake |
| 403 reasons, 429, 5xx | Gmail, "Resolve errors" | fake |
| `to:`, `cc:`, `bcc:`, `OR` in `q` under `labelIds=SENT` | Gmail search operators | fake |
| `nextSyncToken` on the last page, 410 expiry, parameter rules, `showDeleted`, cancelled fields | Calendar sync guide, `events` | fake |
| `calendarList` roles and pages | `calendarList.list` | fake |
| `changeType`, links, `@removed`, `$select` | Graph `message: delta`, delta query for messages | fake |
| **`changeType` together with `$filter`** | **Never shown together: unproven, and loud if refused** | fake |
| **`/me/calendars/{id}/calendarView/delta`** | **Reference names the primary calendar only; the concept page and the SDK say otherwise: unproven** | fake |
| `@removed` outside the range; state tokens encode the range | Graph `event: delta` | fake |
| 410 and `syncStateNotFound`-style 40X | Delta query overview | fake |
| `internetMessageHeaders`, `$search="recipients:…"`, `conversationId` filter on Sent Items | Graph message resource, search parameter | fake |
| `$search` on a personal Outlook.com account | Permissions listed; the search itself is not shown for personal accounts | fake |
| UIDs, UIDVALIDITY change, EXAMINE, expunge, `n:*`, body parts, SEARCH, Sent lookups | RFC 9051 | **run: GreenMail 2.1.15** |
| CONDSTORE: `CHANGEDSINCE`, shared mod-sequences, a wrong password | RFC 7162 | **run: Dovecot 2.4.5** |
| `\Recent` stays through EXAMINE and STATUS, and a read-write session takes it | RFC 3501 sections 2.3.2, 6.3.2, 6.3.10. RFC 9051 deprecates the flag, so the proof needs a rev1 server | **run: Dovecot 2.4.5** |
| A missing constructor parameter and an explicit null are refused, a nullable parameter with no default included | .NET, "Require properties for deserialization" and "Respect nullable annotations" | run in process |
| NOMODSEQ mailboxes | RFC 7162 | not run: no server here reports it; handled by `Supports(ModSequences)` |
| Authentication-Results values | RFC 8601, Microsoft's anti-spam headers page | fixture strings only |
| `M365AuthenticationService` | MSAL's own exceptions | **not run against MSAL**: that needs a cached real account. The decision (`NullWhenGrantIsGoneAsync`: only `MsalUiRequiredException` is a revoked grant) is tested with the real method and a thrown exception; `ProviderFailures` and the provider above them are tested |
| The `GetCredentialAsync` filter | Google.Apis.Auth 1.76.0 | **run in process**: Google's own `UserCredential` and `GoogleAuthorizationCodeFlow` on a scripted token endpoint, with the library raising the exception. Six mutants of the filter killed. Not run against Google |
| A deleted Sent folder | RFC 9051 (a refused EXAMINE is a NO) | **run: GreenMail 2.1.15** (null) and **Dovecot 2.4.5** (recreated empty, so false) |
| A temporary NO on a Sent folder still listed | RFC 5530 UNAVAILABLE, INUSE | **run: GreenMail 2.1.15 and Dovecot 2.4.5**, with a proxy answering the NO, since neither server gives one on its own |
| SEARCH TO as a substring, and more near misses than are compared | RFC 9051 section 6.4.4 | **run: Dovecot 2.4.5.** GreenMail 2.1.15 takes the whole address, so a near miss is never a hit there. HEADER is a substring on both |
| A body nested past what is walked | MIME, RFC 2046 | **run: GreenMail and Dovecot** at 1, 32, 33, 50 and 1,100 levels, **Dovecot** at 5,000. Gmail: fake |
| An HTML event description | Calendar `events`: "can contain HTML" | fake |
| A conversion on its own stack, and layer two leaving a thread behind | .NET `Thread(maxStackSize)` | **run: the image at `89a1c9b`**, under 512 MiB and half a CPU, as `k8s/deployment.yaml` sets, through `/admin/changes` and MCP. Layer one's mails on both paths too. Kubernetes itself was not run |

The spec's row marked **assumed** for IMAP: RFC 9051 makes UIDNEXT and UIDVALIDITY required in SELECT, so
the first half holds everywhere. CONDSTORE is optional and per mailbox, as assumed, and it does not report
expunges, which the spec did not say and which is why `known` exists. The rows for CalDAV, ICS and JSON
stay assumed: they answer `422` until step 7.

## File structure

| File | Change |
|---|---|
| `src/CalendarMcp.Core/Providers/GmailQuery.cs` | New: date bounds as epoch seconds. |
| `src/CalendarMcp.Core/Providers/AllCalendars.cs`, `GraphCalendarList.cs`, `GoogleTransport.cs` | New: every calendar; the Graph list's pages; the test seam. |
| `src/CalendarMcp.Core/Models/PartialCalendarRead.cs` | New: events plus the calendars not read. |
| `src/CalendarMcp.Core/Utilities/MailBody.cs`, `HiddenText.cs`, `LayoutTables.cs` | New: Markdown and plain text from HTML. |
| `src/CalendarMcp.Core/Utilities/MarkupShape.cs`, `SelectorCost.cs`, `ConversionThread.cs` | New: *Bounds on a conversion*. |
| `src/CalendarMcp.Core/Tools/GetEmailDetailsTool.cs`, `GetCalendarEventDetailsTool.cs`, `GetContextualEmailSummaryTool.cs`, `CalendarActionTool.cs` | The model edge. |
| `src/CalendarMcp.Core/Models/Changes.cs`, `UtcInstantConverter.cs` | New: the wire records, and how they write an instant. |
| `src/CalendarMcp.Core/Services/IChangesSource.cs`, `IGoogleApiClients.cs`, `IGraphApiClients.cs`, `IProviderServices.cs` | New interfaces, inherited by the provider interfaces. |
| `src/CalendarMcp.Core/Changes/` | New: `ChangesService`, `ChangesCursor`, `ChangesProviders`, `ChangesException`, `ProviderFailures`, `MailHeaderSignals`, `MailSender`, `MailSnippet`, `EventChanges`, `WrittenToLookup`. |
| `src/CalendarMcp.Core/Changes/Google/` | New: `GmailHistory`, `GoogleMailChanges`, `GoogleCalendarChanges`. |
| `src/CalendarMcp.Core/Changes/Graph/` | New: `GraphLinks`, `GraphMailChanges`, `GraphCalendarChanges`. |
| `src/CalendarMcp.Core/Changes/Imap/ImapDelta.cs`, `Providers/ImapProviderService.Changes.cs` | New: the arithmetic, and the partial that talks to the server. |
| `src/CalendarMcp.Core/Providers/*.Clients.cs` | New: the seams, one per provider. |
| `src/CalendarMcp.Core/Providers/GoogleProviderService.cs`, `M365ProviderService.cs`, `OutlookComProviderService.cs`, `M365AuthenticationService.cs`, `ImapProviderService.Support.cs` | Small edits, listed for a sync in `AURA-FORK.md`. |
| `src/CalendarMcp.Core/Configuration/ServiceCollectionExtensions.cs` | Registers the service and seven sources. |
| `src/CalendarMcp.HttpServer/Admin/ChangesEndpoints.cs`, `AdminEndpoints.cs` | New routes, one line in the existing map. |
| `src/CalendarMcp.Tests/Changes/`, `Helpers/FakeHttp.cs`, `ProviderFakes.cs`, `FixedClock.cs`, `TestData/` | Tests, fakes, the fixture, Dovecot's settings. |
| `.github/workflows/ci.yml` | GreenMail and Dovecot, with the variables set. |
| `AURA-FORK.md` | Changes 1, 7 and 8, and the sync rules. |

## Tasks

### Task 1 — `search_emails` uses exact instants on Gmail
- [x] Files: `Providers/GmailQuery.cs` (new), `Providers/GoogleProviderService.cs`.
- [x] Interface: `GmailQuery.WithDateBounds(query, from, to)` writes `after:` and `before:` as epoch
  seconds. A date-only `to` adds a day, because the tool says "on or before this date".
- [x] Tests: `Providers/GmailQueryTests.cs`.
- [x] Commit `f569021` Search Gmail by exact instants instead of Pacific midnights.

### Task 2 — `get_calendar_events` honours "all calendars"
- [x] Files: `Providers/AllCalendars.cs`, `Providers/GraphCalendarList.cs`, `Providers/GoogleTransport.cs`,
  `Models/PartialCalendarRead.cs` (new); Google, M365 and Outlook providers; `Tools/GetCalendarEventsTool.cs`.
- [x] Interface: `AllCalendars.ReadAsync(calendars, read, count)`. An omitted id reads every calendar; the
  alias `primary` still reads the default one. A partial read carries `Unread`, which the tool
  reports in `warnings`. All failing fails the call. `Transport` on each provider is the test seam.
- [x] Tests: `Providers/AllCalendarsReadTests.cs`, `Tools/GetCalendarEventsToolTests.cs`,
  `Helpers/FakeHttp.cs`, `Helpers/ProviderFakes.cs`.
- [x] Left as found: DAV's `primary` already reads all calendars, and changing it is not this fix.
- [x] Commit `f304bed` Read every calendar when get_calendar_events is given no id.

### Task 3 — A mail body prefers text, and HTML-only mail arrives as Markdown
- [x] Files: `Utilities/MailBody.cs`, `HiddenText.cs`, `LayoutTables.cs` (new);
  `Tools/GetEmailDetailsTool.cs`; `GoogleProviderService.GetMessageBody`, which now answers the format
  from the part it read and skips attachments and forwarded messages; `ImapProviderService.ExtractBody`;
  `Models/EmailMessage.cs`; `Apps/ui/view.html`, whose comment described the Gmail bug.
- [x] Interface: `MailBody.ForModel(body, format)` returns `(text, "text")` or `(markdown, "markdown")`.
  `MailBody.ToPlainText(html)` serves the snippet. `get_email_details` says which in `bodyFormat`.
- [x] Before changing anything, everything reading `Body` was found: no reply, forward or quote path sends
  it out, so converting at the tool edge was the safe side regardless, and `Body` keeps the HTML.
- [x] Tests: `Utilities/MailBodyTests.cs` with the 62,890-character fixture, including every row of the
  hidden-text table; `Providers/MailBodyPreferenceTests.cs`; `Tools/GetEmailDetailsToolTests.cs`.
- [x] Commit `abc30f9` Read mail as its plain part, or as Markdown when it is HTML only.

### Task 4 — The contract, with no source behind it
- [x] Files: `Models/Changes.cs`, `Services/IChangesSource.cs`, `Changes/ChangesCursor.cs`,
  `ChangesException.cs`, `ChangesProviders.cs`, `ChangesService.cs`, `ProviderFailures.cs`,
  `HttpServer/Admin/ChangesEndpoints.cs` (new); `AdminEndpoints.cs`; `Configuration/ServiceCollectionExtensions.cs`.
- [x] Interface: `IChangesSource { Provider; Resource; StartAsync; ReadAsync }`, and
  `ChangesService.ReadAsync(accountId, resource, cursor, limit, ct)`.
- [x] Fixed on the way: `M365AuthenticationService.GetTokenSilentlyAsync` no longer returns null for every
  exception, and `GoogleProviderService.GetCredentialAsync` no longer takes a token endpoint's 5xx for
  a revoked grant. Either would have reported a blip as `provider_auth`.
- [x] Tests: `Changes/ChangesCursorTests.cs`, `ProviderFailuresTests.cs`, `ChangesEndpointTests.cs`, through
  `ChangesPipeline.cs`, the real admin pipeline in memory. They cover the foreign tenant answering byte
  for byte like a missing account, 422 for ICS, JSON and DAV, 403 before any provider call, and
  `NoCursorEverReachesTheLogs`, which captures every log line the pipeline writes.
- [x] Commit `eff7ad6` Add the changes contract with no source behind it yet.

### Task 5 — Signals, snippet, fingerprint, written-to
- [x] Files: `Changes/MailHeaderSignals.cs`, `MailSnippet.cs`, `EventChanges.cs`, `WrittenToLookup.cs` (new).
- [x] Interface: `MailHeaderSignals.AuthenticationFailed(topmost)`, `MailSnippet.Of(body, format)`,
  `EventChanges.Fingerprint(start, end, status)`, `WrittenToLookup.ForMailAsync(sender, bulk, reply, thread, ct)`.
- [x] Tests, daemon-free: `Changes/MailHeaderSignalsTests.cs`, `MailSnippetTests.cs`, `EventChangesTests.cs`,
  `WrittenToLookupTests.cs`.
- [x] Commit `1f2233b` Read signals, snippets and fingerprints the same way for every provider.

### Task 6 — Gmail
- [x] Files: `Changes/Google/GmailHistory.cs`, `GoogleMailChanges.cs`, `Changes/MailSender.cs`,
  `Services/IGoogleApiClients.cs`, `Providers/GoogleProviderService.Clients.cs` (new).
- [x] Interface: `GmailHistory.Net(records)`, pure, gives each id its net change for the Inbox.
- [x] Tests: `Changes/GmailHistoryTests.cs`, `GoogleMailChangesTests.cs`, against `FakeHttp`.
- [x] Commit `215aef1` Read Gmail changes from its history.

### Task 7 — Google Calendar
- [x] Files: `Changes/Google/GoogleCalendarChanges.cs` (new), `Helpers/FixedClock.cs`.
- [x] Interface: a position per calendar, `{sync, page, since}`. The token moves only when a listing ends.
- [x] Tests: `Changes/GoogleCalendarChangesTests.cs`, including one calendar expiring while the others keep
  their place, gained and lost calendars, and a cancelled instance keeping its start.
- [x] Commit `6e45510` Read Google Calendar changes, one sync token per calendar.

### Task 8 — Microsoft mail
- [x] Files: `Changes/Graph/GraphLinks.cs`, `GraphMailChanges.cs`, `Services/IGraphApiClients.cs`,
  `Providers/M365ProviderService.Clients.cs`, `OutlookComProviderService.Clients.cs` (new).
- [x] Interface: `GraphLinks.Checked(link)`, `GraphLinks.IsExpired(failure)`; streams `{link, successor, openedAt, roundDone}`.
- [x] Tests: `Changes/GraphMailChangesTests.cs`, including forged links on eight shapes with zero requests made.
- [x] Commit `c9b3a9e` Read Microsoft mail changes through delta on the Inbox.

### Task 9 — Microsoft calendar
- [x] Files: `Changes/Graph/GraphCalendarChanges.cs` (new).
- [x] Interface: a position per editable calendar, `{link, next, since, windowEnd, roundDone}`.
- [x] Tests: `Changes/GraphCalendarChangesTests.cs`, including `@removed` for a gone event and for one
  that moved away, and a window replaced with no gap.
- [x] Commit `17a5615` Read Microsoft calendar changes through calendar view delta.

### Task 10 — IMAP
- [x] Files: `Changes/Imap/ImapDelta.cs`, `Providers/ImapProviderService.Changes.cs` (new); `.github/workflows/ci.yml`;
  `TestData/Dovecot/test.conf`.
- [x] Interface: `ImapPosition(UidValidity, LastUid, BoundUid, ModSeq, Known)`; `ImapDelta`, pure:
  `Compact`, `Expand`, `Vanished`, `NewSince`, `KnownAfter`, `CutUpdates`.
- [x] Tests: `Changes/ImapDeltaTests.cs`, daemon-free; `ImapChangesTests.cs` on GreenMail, `ImapCondStoreTests.cs`
  on Dovecot, through `ImapMailbox.cs`, which gives each test a new invented user and a second
  connection acting as the outside world. Under `CI` an unset server variable fails: run with `CI=true`
  and no servers, 37 tests failed; without `CI`, the same 37 were skipped.
- [x] The real servers found what a fake could not: MailKit forgets a folder's mod-sequences once another
  is selected; `GetBodyPartAsync` reads an empty body from GreenMail; a single-part mail has no part
  specifier, so an empty section is the whole message. A forged range ending at `uint.MaxValue` looped
  forever, caught by a daemon-free test that took the test host down.
- [x] Commit `92229ee` Read IMAP changes from UIDs, with CONDSTORE where the server has it.

### Task 11 — `POST /admin/items`
- [x] Files: `IChangesSource.ReadItemAsync` on every source; `ChangesService.ReadItemsAsync`;
  `ChangesEndpoints.cs`; `MailSender.ReadDate`; `Changes/ItemsEndpointTests.cs`, `ChangesRegistrationTests.cs`.
- [x] Tests: `gone`, 50 items and 51, a foreign tenant, 422, 403, a provider failure not reading as gone, each
  source, and `ChangesRegistrationTests`: the seven sources resolve from `AddCalendarMcpCore`, and
  `MapAdminEndpoints` maps both routes.
- [x] Commit `186b489` Read mail and events by id for the digest.

### Task 12 — The five failures, end to end
- [x] Files: `Changes/RealMailboxFailureTests.cs` (new), `ImapChangesTests.cs`, `ImapMailbox.cs`.
- [x] Tests: Review Focus, 1 to 5; and on IMAP, the same cursor twice, a body over the limit, a mailbox past
  the window of known UIDs.
- [x] Commit `249b394` Pin the five failures most likely to reach a real person.

### Task 13 — What the mutation run and the review found
- [x] Files: `Changes/ChangesCursor.cs`, `Models/Changes.cs`, `Models/UtcInstantConverter.cs` (new),
  `Changes/Google/GmailHistory.cs`.
- [x] Interface: the cursor's serializer requires every field, so a damaged position is `invalid_cursor`
  before a source sees it, and its envelope has no nullable field left. `OccurredAt` on `Change` and
  `ItemFacts` goes through `UtcInstantConverter`.
- [x] Tests: `ChangesCursorTests`, an envelope or a folder with a field gone and a null that may stay;
  one per source at risk: `ACursorWithAStreamMissing_IsRefusedBeforeAnyRequest`,
  `APositionWithNoKnownSet_IsRefusedBeforeAnyServerIsAsked`,
  `ACalendarWithNoSyncToken_IsRefusedRatherThanReadWhole`. `AnInstantWithAnOffset_IsWrittenInUtc`,
  `AWrittenInstant_ReadsBack`, and `occurredAt` on an item. `OutlookCom_IsReadThroughItsOwnProvider`.
  `WatchingAMailbox_OpensEveryFolderReadOnly`, on Dovecot. `ALabelRowWithNoMessage_CostsNothingElseOnThePage`.
- [x] Tests that changed, all this branch's own: `Page_IsWrittenAsTheContractPinsIt` had pinned the
  serializer's `+00:00` and now pins §5.1's `Z`. Test positions that left fields out state them, and
  `AGarbledKnownSet_IsRefusedBeforeTheServerIsAsked` needed that most, because a missing field would
  have refused it for the wrong reason.
- [x] Commits `7fcbfff` Refuse a cursor with a field gone, and write dates as the contract shows;
  `7a0c460` Survive a Gmail label row that carries no message.

### Task 14 — Close
- [x] `AURA-FORK.md` records the changes and the sync rules. `ci.yml` runs both servers. Commit `5f182c7`
  Record the changes contract in the fork's notes.
- [x] Mutation spot-check, by hand, one `sed` change per run against the tests that should catch it:
  22 mutants on the riskiest lines at `249b394`, 21 killed. The survivor opened a folder read-write,
  which no test could see. Task 13 added the test that can, and a second round of 11 mutants on that
  commit's own claims was killed 11 of 11: each of the four read-only opens, both serializer options, the
  null writer, the date converter at both properties, and Outlook.com's provider. The first round was not
  re-run after Task 13, which only tightened a shape check and added tests.
### Task 15 — What the independent review found
- [x] Critical: a hostile attribute no longer stops a page, and one mail never blocks one (`b597c4b`);
  conversion bounded on every provider, with AngleSharp pinned at 1.9.0 (`7f8d271`, `38c437f`); a stranger
  cannot forge `writtenTo` on IMAP by SEARCH HEADER's substring match (`23a0010`). Found after the review:
  CSS variables pasted past every bound into a recursive parser, and a 560-byte mail killed the server, so
  a value that would paste more than 2,048 characters is now refused whole (`efb4f8b`).
- [x] Important: a 30 s page budget for snippets (`6b69d47`); the body chosen by what its parts mean
  (`8f1ca94`); a cancelled Google event read for what its row left out (`bbacf41`); the hiding tricks the
  filter let through (`2c768e5`).
- [x] Minor: M1 forged input is a 400 (`efcc87f`); M2 an undecryptable password is `provider_auth`
  (`8a2b9ce`); M3 a cursor for another inbox folder is expired (`94812a2`); M4 a gone Sent folder leaves
  `writtenTo` null (`20078b2`); M5 the token-refresh test can fail, and the failure table has its missing
  rows (`3651ee2`); M8 a flattened table keeps its caption (`921dab2`); M9 this record and `AURA-FORK.md`
  (`c0f87a1`). M6 and M7 are not code: they are in G3 and in the Graph decisions above.
### Task 16 — What the second review found

The review read the code without running it, so each finding was a claim, and each got a failing test first.

- [x] **Critical 1, a stylesheet that hangs a conversion.** Reproduced: the review's mail and its variants were
  still running at 6 s on both paths. Layer two `c90db6b`, layer one `0306a1f`. Six mutants of the estimate
  and six of the thread killed. The review's mail is now refused in about 0.2 s.
- [x] **Critical 2, the end-tag flood.** Reproduced, and the estimate was low: 5k×10k took 435 ms, 10k×20k
  1.39 s, 20k×40k 5.2 s, 40k×80k 27.6 s, 100k×185k over 90 s, with the same for `</div>`, `</a>`, `</li>`, `</h1>`,
  `</object>`, `</body>`, in SVG and in MathML. `7ee10c9`. Looking for other long paths found two more, neither in
  the review. `</>` and `--!` floods overflow 8 MiB on cold code and kill the process (`14c1423`). `<p><b a{i}>x</p>`
  allocated 1.2 GB from 33 KB (`7ff6bba`). `ReconstructFormatting` on text took 27 s, and `MarkupShape` holds it.
- [x] **Important 3, a long number passes hidden text.** Reproduced, 12 of 15 forms. Any length is read now, and
  the regular expression is linear. `7e60f98`.
- [x] **Important 4, any refused EXAMINE reads as gone.** Reproduced: 200 instead of 503 on both servers.
  `8911a1f`. Both branches tested and both mutants killed.
- [x] **Important 5, two bodies outside the pipeline.** Reproduced, 9 tool tests red, and the upstream preview
  regular expression took 14 s on a megabyte of `<`. A Google description has no type, which the review did not
  name and which needed the same fix. `c2bf46d`.
- [x] **Minor 6, font keywords.** Reproduced, 8 rows. `a9edf0c`.
- [x] **Minor 7, offsets that stay on screen.** Reproduced for `bottom`, `right` and the wrapped centring trick, 6
  rows. Not reproduced for `left:-50%` alone, which does leave the screen and stays removed. Three shapes that
  leave the screen and passed were found by the same measuring and are refused. `61709f1`.
- [x] **Minor 8, table cells.** Reproduced, 9 rows, and the hostile cells stay removed. `c15738f`.
- [x] **Minor 9, a false over the cap.** Reproduced on Dovecot, and on both servers for a thread. `ef42852`.
- [x] **Minor 10, Google's filter undriven.** Reproduced by mutation: nothing failed. Six mutants now each fail
  a test on Google's real flow. The filter did not change. `5579314`.
- [x] **From the stack pass.** Deep MIME reads as empty: found to be this provider, fixed (`7a5c429`). The
  recursion bound got its measured threshold (`8be80a2`), and the walk's bound its figure at half a CPU
  (`1213862`). `8ab3850` drops an assertion of mine that could not fail. `89a1c9b` is the fork's notes.
- [x] **Run on `89a1c9b`.** The whole suite three times in a row, 1712 passed each. Under forced-cold settings
  the 443 deep and hostile tests passed. The image, at 512 MiB and half a CPU: 29 mails on one server through both
  paths, with the server up and the next ordinary mail converted after each, and no leak. 20 fresh servers, one
  mail each, forced-cold, with `ulimit -s unlimited`, and with both: none died. Layer two driven by mails just under the
  bound: thread left behind, refused past two, warnings with no mail content, recovered unaided.
- [ ] **G3, owed:** a real Gmail, Microsoft 365 and Outlook.com account through the running sidecar, on
  the lab VM, with its dated file under `docs/superpowers/verification/`. It decides the two unproven
  rows above before anything rests on them. It also owes, from the review:
  - **M6, Graph all-day events.** `GraphCalendarChanges.When` ignores `time.TimeZone` and takes UTC
    midnight. The fake cannot show whether an all-day event in Rome comes back a day early.
  - **Gmail resuming from a history record's `id`.** A page that stops partway through a history list
    resumes from a record's `id`; that Gmail accepts it as `startHistoryId` and neither repeats nor skips
    a record is shown only against a fake.
  - **A cancelled event's real shape.** Google's reference guarantees a deleted event its `id` and a
    cancelled instance `id`, `recurringEventId` and `originalStartTime`; the read of such a row
    (`bbacf41`) and its `recurringEventId` parent are shaped from that page, not from a real calendar.
  - **IMAP login failures.** `AuthenticationException` is `provider_auth`. A server that refuses a login
    only for a while (throttling, a maintenance window) pauses a person's watch the same way. Decide
    whether a temporary refusal should be `provider_transient`, from what real servers answer.
