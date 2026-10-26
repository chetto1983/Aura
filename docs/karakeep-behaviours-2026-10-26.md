# Karakeep: behaviours for Aura, 2026-10-26

Asked by the operator on 2026-10-26: "find what we can take for Aura, a deep audit" of
`github.com/karakeep-app/karakeep`.
- **Source.** A shallow clone of `main` at `ba2acc3` ("release(mobile): prepare 1.14.0",
  2026-10-26), read only. Paths below are relative to the clone's root.
- **Licence.** Karakeep is AGPL-3.0 and Aura is MIT, so this file describes behaviour only. It does
  not reproduce Karakeep code, prompts or regexes.
- **What it is.** Karakeep (formerly Hoarder) is a self-hosted read-it-later app. It saves links,
  notes, images and PDFs, then crawls, tags and indexes them. It is built on Next.js, tRPC,
  Drizzle on SQLite, Meilisearch and Playwright, with background workers in `apps/workers/`.
  Aura's side of each item was read on the same day at `baa95bb`.

## Verdict

Karakeep is a product. Its core loop is save, crawl, tag and find again. Aura has the crawl, at a
stricter bar than Karakeep's. It has the find, through retrieval with citations. It does not have
the save. So the audit yields one feature candidate, one measured defect, and a few design rules.

**The feature: a saved link becomes a document in the identity's library (K2).** `web_fetch`
reads a page and forgets it. Nothing persists a page, so the agent fetches the same URL again in
every conversation. Karakeep's value is exactly the step Aura skips. The behaviours worth taking
are:
- one canonical URL per saved page;
- an explicit policy for what happens when a page is saved twice;
- metadata the page already declares: author, date published, site, language;
- refusing to save a bot-challenge page as if it were content.

These behaviours land on Aura's existing ingest. They need no new store. The gate is the
operator's demand, measured first.

**The defect, measured during this read (K1).** Aura's SSRF classifier does not block the IPv6
transition ranges that embed an IPv4 address, or the IPv4 reserved ranges. The classifier exists
in two copies, `internal/web/ssrf.go:35-60` and `internal/mcp/ssrf.go:52-`, and both copies have
the gap. Karakeep blocks 6to4, Teredo and the reserved ranges. Neither project blocks NAT64. The
probe tables are in §Measurements. The realistic risk is low. The fix landed the same day as
one shared classifier (K1, "Landed the same day").

**The design rules.**
- Use the labels of a document's nearest reviewed neighbours as the dumb baseline for
  `docfields` Phases 2 and 3 (K4).
- Give the asset queue a priority inside each identity (K6).
- Do not trust MCP tool annotations if Karakeep is ever mounted (K8).
- Give the planned document workflows a cycle guard, which Karakeep's rule engine lacks (K9).

**The rest is not worth taking.** That covers Karakeep's browser extensions, mobile apps,
highlights, public lists, full-page archive, yt-dlp, importers and the queue and search stacks.
Aura either has an equivalent, or nothing has been measured that needs it.

**Karakeep's AI is unevaluated.** Auto-tagging is on by default
(`docs/docs/03-configuration/01-environment-variables.md:122`) and summaries are off by default
(`:123`). Neither has an evaluation anywhere in the repository. A grep for precision, recall,
golden and eval sets over `apps/`, `packages/` and `docs/docs/` finds none. So Karakeep adds no
evidence to D6 (an LLM summary per file), and BASELINE OR DROP stands unchanged for any learned
piece taken from it.

## K1. The outbound URL guard

**What it is** (`apps/workers/network.ts`).
- **A denylist of address classes**, matched on the parsed address (`:16-33`, `:78-91`). It covers:
  - unspecified, broadcast, multicast, link-local, loopback, private, reserved and CGNAT;
  - for IPv6: unique-local, 6to4, Teredo, benchmarking, deprecated and discard-only.

  An IPv4-mapped IPv6 address is unwrapped first (`:83-89`).
- **Every resolved record is checked**, and one forbidden record refuses the whole host
  (`:216-223`).
- **The connection is pinned** to the validated addresses through a custom socket lookup, so
  nothing is resolved twice between the check and the dial (`:234-274`).
- **Redirects are followed by hand**, with `redirect: manual` and at most five hops. Each hop is
  validated again, and a 303 turns the method into GET (`:432`, `:476-551`).
- **A hostname allowlist for internal access** takes dot-prefixed wildcards. A bare `.` allows
  every host (`:106-138`).
- **In the crawler's browser**, every sub-request is validated (`apps/workers/workers/crawlerWorker.ts:1510-1548`).
  A Chrome DevTools Protocol `Fetch` guard also validates every redirect the browser would follow
  (`workers/utils/redirectGuard.ts:26-123`).
- **The weak points Karakeep states itself:**
  - Under a proxy, the DNS check is skipped entirely (`network.ts:185-189`).
  - yt-dlp makes its own requests, unguarded (`workers/videoWorker.ts:81-83`).
  - The security page says the checks "mitigate only some of the risks" and recommends an egress
    proxy outside the network (`docs/docs/06-administration/01-security-considerations.md`).

**For Aura.**
- **What exists, and is at least equal.** Aura's guard is stricter on several points:
  - it pins DNS per conversation (`internal/web/dnspin.go:38-63`);
  - it dials only the pinned IP and checks that IP again in `net.Dialer.Control`
    (`internal/web/transport.go:90-120`);
  - it re-validates every redirect up to five hops (`internal/web/fetcher.go:17,192`);
  - it fails closed when any resolved record is blocked (`ssrf.go:85-107`);
  - it blocks cloud-metadata hostnames before DNS (`ssrf.go:22-28`), and Karakeep has no such list;
  - it never skips the check, proxy or not.

  The browser box sits behind a network-layer egress floor
  (`internal/sandbox/usersandbox/egress.go:31-35,137-150`), not only behind a URL check.
- **What is missing, measured.** `classify` does not block the following (§Measurements):
  - **IPv4 embedded in IPv6 transition addresses:** NAT64 `64:ff9b::/96` (RFC 6052) and 6to4
    `2002::/16` (RFC 3056). For example, `64:ff9b::a9fe:a9fe` carries 169.254.169.254 and is
    allowed.
  - **Teredo:** `2001::/32`.
  - **The IPv4 reserved and special ranges:** `240.0.0.0/4`, `198.18.0.0/15`,
    `255.255.255.255`, `192.0.0.0/24`.
  - **Two more IPv6 ranges:** discard-only `100::/64` and the deprecated site-local `fec0::/10`.

  `internal/mcp/ssrf.go` is a stated "faithful copy" of the same classifier (`:9-18`), and its
  probe gives the same table. Karakeep blocks 6to4, Teredo, the reserved ranges, broadcast and
  discard. It does not block NAT64 or `fec0::/10`, which ipaddr.js 2.2.0 files as `rfc6052` and
  plain `unicast`.
- **The egress floor, read, not measured.** It is an `ip` table only, so it does nothing for
  IPv6. It drops the metadata address as a `/32`, not the whole of `169.254.0.0/16`. It does not
  drop CGNAT `100.64.0.0/10`.
  - This matters only on a host that routes those ranges, for example through a Tailscale
    tailnet. Aura's tree never mentions Tailscale.
  - Whether the compose networks enable IPv6 on the lab VM was not checked.
- **Realistic path, honestly.**
  - A conforming NAT64 refuses the well-known prefix for non-global IPv4 (RFC 6052 §3.1).
  - A 6to4 packet is decapsulated at a relay, not at the appliance.

  So the gap is defence in depth, not an open door.
- **Where it lands.** `classify` in `internal/web/ssrf.go`:
  - unwrap the embedded IPv4 of `64:ff9b::/96` and `2002::/16`, then classify that address;
  - block Teredo and the ranges above.

  Both copies need the change. Better still, extract the one guard that the comment at
  `internal/mcp/ssrf.go:12-14` defers to QUAL-03, so the next gap is fixed once.
- **Gate.** The comment at `ssrf.go:30-34` called `classify` the mutation-gate target, but
  `scripts/critical_mutation_gate.py` had no such scope, so the claim was false.
  - One row per new class goes into the classifier's table test.
  - The mutation gate must pass on the changed file.
  - No live claim is made without a lab-VM probe through a host that has IPv6.

**Landed the same day.**
- **One classifier, `internal/netguard/netguard.go`.** `internal/web`, `internal/mcp` and
  `publicBrowserURL` call it, and the two copies are gone. The browser check was a third,
  narrower variant. Probed the same way, it accepted the CGNAT, `0.1.2.3`, reserved, NAT64
  and 6to4 literals.
- **The rule comes from the IANA registries** read that day:
  - a carried IPv4 (mapped, `64:ff9b::/96`, `2002::/16`) is judged as itself;
  - every block marked "Globally Reachable: False" is refused, including all of IPv6 outside
    `2000::/3`;
  - `192.0.0.0/24` and `2001::/23` are refused whole, Teredo included.

  `IsMetadata` sees carried forms, so the MCP dev profile refuses them too. A metadata
  hostname written with the root dot is refused.
- **Evidence.**
  - The table and property tests in `internal/netguard` cover 100% of statements.
  - The new `ssrf` scope of the critical mutation gate killed 30 of 30 mutants in 14 s. The
    first run left one equivalent mutant, `b[2:6]` → `b[2:7]` in a slice-to-array conversion.
    The redundant upper bound was dropped, which removed that mutant.
  - `scripts/ssrf_smoke.sh` gives 6/6 `blocked_url` with the real binary. The binary built
    from `209e8d0` passed both new targets to the dial (`http_error`).
- **One test fixture changed, with its reason.** `internal/web/fetcher_test.go` had used
  TEST-NET-3 as its "public" stand-in. A documentation block is exactly what the rule
  refuses, so the fixture is now a routable address that the test's dialer never contacts.

## K2. A saved link becomes a document

**What it is.**
- **The bookmark's link is the identity.**
  - Saving the same URL twice returns the existing bookmark with `alreadyExists`. It restores the
    bookmark from the archive and moves it to the top (`packages/trpc/routers/bookmarks.ts:277-330`).
  - Automated sources (`rss`, `import`) are exempt from that bump. For them, a re-save is a no-op,
    so that bulk flows stay idempotent (`:209-215`).
- **Two definitions of "the same URL".**
  - The create path compares the raw string per user (`attemptToDedupLink`, `:131-146`).
  - The `checkUrl` endpoint drops the fragment and trailing slash before comparing
    (`:1236-1272`).

  Neither strips tracking parameters. This inconsistency is a defect to avoid, not a behaviour.
- **Content-type routing before the crawl.** A plain GET probe routes the URL by its declared
  type (`crawlerWorker.ts:545-555`, `:577-`). The probe waits at most 5 s for the headers, buffers
  at most 2 MB of body, and runs for at most 30 s.
  - A PDF or image URL becomes an asset bookmark and goes through asset preprocessing
    (`:423-432`, `:822-870`).
  - HTML goes to the browser render. The probe's metadata is written early, fill-only, as
    `COALESCE(NULLIF(existing,''), new)`, so that a late write from a failed attempt never
    overwrites the retry's result (`:714-741`).
- **Metadata.** A metascraper chain extracts title, description, image, logo, author, publisher,
  and the published and modified dates, with site-specific plugins
  (`apps/workers/scripts/parseHtmlSubprocess.ts:43-70`).
  - When the browser render looks blocked, the probe's metadata wins field by field over the
    render's (`workers/utils/metadataResolver.ts:111-135`).
  - The parse runs in a subprocess with a bounded heap (`workers/utils/parseSubprocess.ts:1-3,36-52`).
- **Re-capture policy.** The SingleFile upload takes `ifexists=skip|overwrite|overwrite-recrawl|append|append-recrawl`
  (`docs/docs/05-integrations/05-singlefile.md`). It is an explicit vocabulary for what a second
  save of the same page does.
- **Capture from the user's own browser.** SingleFile and the extension's client-side crawl
  upload the page as the user sees it. That gets past paywalls, logins and bot walls without a
  server crawler.

**For Aura.**
- **What exists.**
  - `web_fetch` (`internal/agent/tools/web_fetch.go:41-71`) returns
    `{title,url,content_md,links,warning}`.
  - Readability and html-to-markdown run on bytes that the hardened transport has already fetched
    (`internal/web/html.go:45-85`). PDF and text have their own paths (`fetcher_pdf.go`,
    `fetcher_text.go`).
  - **Nothing persists the result.**
    - Memory facts have no URL field.
    - The ingest reads only the identity's bucket (`services/ingest/source.py:1-11`).
    - A file the agent delivers is deliberately never indexed
      (`internal/assets/ingest_agent.go:18-22`).
  - The only URL normalization is for citations: it lowercases scheme and host and drops the
    fragment and trailing slash (`internal/agent/display/sources.go:98-118`). The fetch cache is
    keyed by the raw URL.
  - A link sent on Telegram is plain text for an ordinary turn
    (`internal/channels/telegram/bot_dispatch.go:150-154`).
- **What exists and is unused (INVENTORY BEFORE INVENTION).** The readability library Aura
  already depends on, `codeberg.org/readeck/go-readability/v2` v2.1.3, exposes these methods on
  `Article`:
  - `Byline`, `Excerpt` and `SiteName`;
  - `ImageURL`, `Favicon` and `Language`;
  - `PublishedTime` and `ModifiedTime`.

  Aura keeps the title only, by decision D-18 (`internal/web/html.go:49-50`). That decision is
  right for a fetch the model reads once. A saved page is where the date and author pay off, in
  answers to "when" and "who wrote it". No new extractor is needed.
- **What is new.**
  - **An explicit save.** The operator says "salva questo link", or uses a tool flag on
    `web_fetch`. The readable markdown is then written, with a small metadata header (canonical
    URL, title, author, site, published date, fetched_at), as one `.md` object into the
    identity's bucket under a stable key derived from the canonical URL. CocoIndex indexes it like
    any other document, so the card, the fields, `document_search` and the citations come for
    free.
    - The save stays explicit. That keeps the rule `ingest_agent.go` states: a produced file must
      never silently become searchable.
  - **One canonical-URL function**, shared by the save key, the dedup check and the citations.
    Karakeep's two definitions are the warning.
  - **A re-save policy**, `skip` by default or `overwrite`, borrowed from the `ifexists`
    vocabulary. `append` is versioning, which the operator dropped on 2026-10-17.
- **What to measure first (PRD-first).** Over the operator's real conversations on the lab VM:
  - how many URLs the operator sends per week, on Telegram and in the cockpit;
  - how many `web_fetch` calls hit the same URL from two or more distinct conversations (the cost
    of not saving).

  If both are near zero, K2 is not built. If it is built, the acceptance runs on the live stack:
  save three real pages, then a week later ask about each one in a new conversation. The answer
  must come from `document_search`, with the page's citation and its published date.
- **Risk.**
  - Saved web text is untrusted, and now it is durable: an injected instruction persists in the
    library. Retrieval already treats documents as untrusted data (prd.md §12).
  - A page that is bot-gated or behind a login saves junk. That is K3.

## K3. Junk pages are refused before they are kept

**What it is.**
- **Challenge pages served with HTTP 200** (`workers/utils/metadataResolver.ts:8-101`). They are
  detected in two ways, preferring false negatives:
  - an exact, case-folded match on known interstitial titles (Cloudflare, PerimeterX, Akamai,
    Imperva, Vercel);
  - high-confidence body markers, searched only in pages under 100 KB, so that an article
    *about* bot walls is not misclassified.

  A self-clearing challenge is waited out for up to `CRAWLER_CHALLENGE_WAIT_SEC` (default 15), by
  polling every 500 ms (`workers/utils/botChallenge.ts:17-94`). DataDome's hard captcha is not
  waited on (`metadataResolver.ts:87`).
- **The retry rule.** 403, 429 and 5xx are retried (`workers/utils/crawlerUtils.ts:37-42`). On
  the last attempt the crawl proceeds anyway (`crawlerWorker.ts:979-987`).
- **A reader-view assessment** (`workers/utils/readerViewAssessment.ts`). It is a deterministic
  0–100 score with named reasons and a classifier version:
  - Article metadata (og:type, schema.org Article in JSON-LD) adds to the score; Product and
    SearchResultsPage subtract.
  - So do text length, the count of paragraphs of 80 or more characters, link density, the
    density of form controls, search-URL signals and sentence density, plus Mozilla's
    `isProbablyReaderable`.
  - A score of 70 or more is readable and 35 or more is uncertain. A root URL with no article
    metadata is capped at 60 (`:87-95`, `:234-239`).
- **The tagging prompt abstains on junk.** It is told not to tag error, challenge, consent or
  login pages, and to return an empty list instead (`packages/shared/prompts.ts:36-40,75-79`).

**For Aura.**
- **What exists.**
  - A challenge is recognised only from the `Cf-Mitigated: challenge` header on a non-2xx
    response (`internal/web/fetcher.go:217-222`).
  - Thin pages get `low_content`, and an empty JavaScript shell gets `javascript_required`
    (`html.go:53-59`, `html_jsshell.go`).
  - A 200-status interstitial with a little text passes as content with at most a warning.
- **What is new, and only with K2.** A save refuses or flags:
  - a page whose title is a known interstitial, or whose small body carries a known marker;
  - a page whose reader score says it is a hub or a search result, not a document.

  The reasons are kept, so a refusal explains itself. For plain `web_fetch` the existing
  warnings are enough, because the model reads the page once and the operator sees the answer.
- **What to measure.** A labelled set of about 40 URLs (real articles, homepages, search pages,
  link hubs, and pages behind Cloudflare or DataDome), fetched through `web_fetch` on the lab VM.
  Count the junk returned as content with no warning. Build the gate only if that count is not
  zero.
- **Risk.** Title lists rot as vendors change their pages. Karakeep's preference for false
  negatives is the right default.

## K4. The labels of a document's neighbours, as the dumb baseline

**What it is** (`apps/workers/workers/inference/tagging.ts`).
- **Before the model tags a bookmark,** the ten nearest bookmarks of the same user in the vector
  store are fetched, with a score of 0.75 or more (`:538`, `:550-594`). Their tags are collected,
  shortest first, up to 1,000 characters (`:40`, `:596-626`).
- **Those tags go into the prompt** as "similar bookmarks were tagged with…, reuse if possible"
  (`packages/shared/utils/tag.ts:39-46`).
- **A curated list** replaces the hint with a closed vocabulary (`tag.ts:32-37`,
  `tagging.ts:666-692`).
- **Provenance has two values, `ai` and `human`.**
  - A re-tag replaces only the AI-attached tags (`tagging.ts:477-500`).
  - A tag that a rule attaches is recorded as `human` (`packages/trpc/lib/ruleEngine.ts:275`), so
    rule-made and person-made tags cannot be told apart.

**For Aura.**
- **What exists.**
  - Per-value provenance is already richer: `operator`, `extractor`, `copied`, `classifier`,
    `llm`, or `rule:<label id>` (`internal/docfields/values.go:21-24,265-272`).
  - Phases 2 (classifier) and 3 (LLM) are gated behind BASELINE OR DROP
    (`docs/superpowers/specs/2026-10-18-document-fields-design.md:403-416`).
  - Every document has a card-description vector (`2026-10-16-card-description-vector-design.md`).
  - `vector.neighbors` takes a filter (CLAUDE.md, inventory rule).
- **What is new.** Karakeep uses the neighbours' labels as a hint for an LLM. For Aura they are
  better used as **the baseline itself**: suggest a document's type or counterparty from a
  majority vote of its k nearest *reviewed* neighbours in card-vector space. The vote needs no
  training, no model call and no new dependency, because the vectors already exist. Phase 2's
  classifier then has to beat this vote, not only the rules.
- **What to measure.** Precision, recall and abstention rate per field. Run out of sample on the
  operator's reviewed documents on the lab VM: rules alone, then rules plus the kNN vote at a few
  values of k and of the similarity floor.
- **Risk.** A small library per identity gives few neighbours, so the vote must be able to
  abstain. Labels that cut across topics, such as a counterparty that sends many document types,
  may not cluster at all. That is the measurement's job to say.

## K5. Normalised label names

**What it is.** A tag's uniqueness key folds case and strips spaces, hyphens and underscores
(`tagging.ts:82-91`, which must stay in sync with a generated column). So "Machine Learning",
"machine-learning" and "machine_learning" are one tag.

**For Aura.**
- **What exists.** `NameKey` is lowercase plus trim (`internal/docfields/labels.go:39`), so
  "Gialli Import" and "Gialli-Import" are two names.
- **What to measure.** Count near-duplicates in the identities' lists and proposals on the lab VM.
- **What it would change.** Folding hyphens and spaces in `NameKey`, only if that count is not
  zero.
- **Risk.** Distinct legal names that differ only by a hyphen would merge. That is rare, but it
  is real for counterparties.

## K6. Priority inside an identity's queue

**What it is.**
- **The queue contract** (`packages/shared/queueing.ts`) is a priority, an idempotency key, a
  delay and a `groupId`.
- **The restate dispatcher** serves the best priority first, then the least recently served group
  (`packages/plugins/queue-restate/src/semaphore.ts:152-200`). So one user's 10,000-item import
  does not starve another user's single save.
- **A user who creates more than 30 bookmarks in 5 minutes** is moved to the low-priority queues
  (`routers/bookmarks.ts:203-231`, `:487`).
- **A rate-limited job raises `QueueRetryAfterError`.** It is retried after the window plus up to
  40% jitter, and does not consume an attempt (`queueing.ts`, `crawlerWorker.ts:506-541`).
- **Imports go through a staging table.**
  - Claims are bounded by an in-flight capacity (`workers/importWorker.ts:176-205`, `:683-700`).
  - Batches are picked fairly across sessions (`:324-350`).
  - A row left in processing for an hour goes back to pending (`:110`, `:704-711`).

**For Aura.**
- **What exists.** One `IngestionJobWorker` per identity claims only that identity's rows
  (`internal/documents/jobs_worker.go:84-108`), so fairness between identities holds by
  construction. Leases and generations fence late writes more strongly than Karakeep's fill-only
  write (`internal/db/queries/ingestion_jobs.sql:27-80`).
- **What is missing.** Within one identity the claim order is `next_attempt_at, created_at`
  (`ingestion_jobs.sql:49`). There is no priority, so a bulk upload queues ahead of a single file
  sent on Telegram.
- **What to measure.** On the lab VM, the time until a Telegram document becomes searchable,
  idle and then while 200 files of the same identity are queued. Add a priority column only if
  the second number is one the operator would notice.

## K7. Feed watching

**What it is** (`workers/feedWorker.ts`, `workers/utils/feedParser.ts`).
- **An hourly cron enqueues every enabled feed** at a minute offset hashed from the feed's id, so
  feeds spread across the hour. The idempotency key is per feed and hour (`feedWorker.ts:26-35,60-112`).
- **An item's identity is `guid`**, then `id`, then `link` (`feedParser.ts:27-30`). Items are
  deduplicated per (feed, entry id) in an import table, and an item without a link is skipped
  (`feedWorker.ts:270-290`).
- **Each fetch records its status and time.** There is no conditional GET (no ETag or
  If-Modified-Since). Category import as tags is opt-in.

**For Aura.**
- **What exists.**
  - No feed parser is in `go.mod`.
  - A scheduled `agent_job` can already call `web_fetch` (`internal/agent/tools/task.go:131`). The
    lab VM ran scheduled `web_fetch` jobs (prd.md, "Every scheduled `web_fetch` failed", fixed in
    `82a49761c`).
- **What would be new.** A per-job "seen" set keyed as Karakeep keys items, so that a feed watch
  reports only what is new.
- **Gate.** The operator asking for one. Not before.

## K8. Karakeep as a mounted MCP server, not absorbed

**What it is** (`apps/mcp/src/`). Twenty-nine tools wrap Karakeep's REST API with a bearer key,
over bookmarks, lists, tags, highlights and assets. Only eight of the 29 tools carry annotations:
- `readOnlyHint: true` on five reads (`bookmarks.ts:170`, `highlights.ts:53,94,127`,
  `lists.ts:323`);
- `destructiveHint: false` on two highlight writes (`highlights.ts:186,232`);
- `destructiveHint: true` on `delete-highlight` alone (`highlights.ts:268-270`).

`delete-bookmark`, `delete-list` and `delete-tag` carry no annotation. So do `search-bookmarks`
and `get-bookmark`. Under the MCP defaults, an unannotated tool is not read-only and is
destructive.

**For Aura.** If the operator runs Karakeep, Aura mounts its MCP server through the registry and
gains the whole product for free. The per-identity tool policy (consolidation item 1) must then
set `ask` or `deny` per tool name, because the hints cannot be trusted in either direction.
Karakeep also ships an agent skill that drives its CLI (`skills/SKILL.md`). An identity could
install it at runtime like any third-party skill. It is never vendored into Aura's tree.
- **Gate.** The operator choosing to run Karakeep.

## K9. A warning for document workflows: rules need a cycle guard

**What it is.**
- **A rule is an event, a condition tree and a list of actions**
  (`packages/shared/types/rules.ts:6-220`):
  - events: added, tag added or removed, added to or removed from a list, favourited, archived;
  - conditions: and/or over URL or title contains, feed, type, source, has tag, favourited,
    archived;
  - actions: add or remove a tag, add to or remove from a list, archive the full page, favourite,
    archive.
- **Before enqueueing,** a cheap pre-check asks whether any enabled rule listens to the event
  (`lib/ruleEngine.ts:104-156`).
- **Rule actions emit events of their own.** A list action calls `addBookmark` or
  `removeBookmark`, which trigger the rule engine again (`packages/trpc/models/lists.ts:1184,1234`).
  There is no depth or visited guard.
  - Two rules, one saying "added to list X → remove from X" and the other "removed from X → add
    to X", would ping-pong forever.
  - Read, not run.

**For Aura.** Document workflows are increment D, optional and not built
(`docs/superpowers/specs/2026-10-21-paperless-document-workspace-design.md:184-185`). If they
are built, they take three things from this read:
- the cheap pre-check;
- a depth limit on actions triggered by actions;
- provenance that names the rule, which Aura already has as `rule:<id>`.

## Already in Aura, no action

- **SSRF with DNS pinning and per-hop redirect checks.** Aura is stricter, apart from the ranges
  in K1.
- **A browser behind a network egress floor**, with takeover by the operator (consolidation
  item 2). That covers the case Karakeep's SingleFile path covers: a page that needs a human or a
  login.
- **Per-value provenance and Paperless-style deterministic rules** (`internal/docfields/rules/`).
  Both are richer than Karakeep's `urlContains`/`titleContains` conditions and its `ai`/`human`
  split.
- **Saved views** (`internal/docfields/views.go`), the counterpart of Karakeep's smart lists.
- **OCR by a vision model**, with the chat model's vision first (`2026-10-25-media-vision-primary-design.md`),
  where Karakeep defaults to Tesseract with a confidence floor of 50. Speech-to-text already runs
  on Telegram voice and ingest audio.
- **Leases with generation fencing on the job queue**, retries with jitter on the scheduler
  (`internal/cron/transient_retry.go:22`), and per-kind maximum durations.
- **Nightly backups of Postgres and objects, plus ArcadeDB's own** (`docs/BACKUP-RESTORE.md`),
  and conversation and owner exports.
- **Token-budgeted prompt truncation.** Karakeep's `prompts.server.ts:27-80` is the generic
  version.

## Not taken, with the reason

- **Browser extensions, mobile apps and a PWA share target.** Every phone's share sheet already
  sends a link to the Telegram bot.
- **Highlights, reading progress, collaborative lists, public lists, RSS out and offline
  reading.** These are read-it-later product surfaces outside prd.md §1.
- **Full-page archive (monolith), PDF snapshots and full-page screenshots.** Each costs storage
  per page, and no link rot has been measured. Karakeep itself defaults all three off
  (`01-environment-variables.md:145-149`) and caps archive size because "media-rich pages can
  produce 1GB+ files".
- **yt-dlp video archiving.** It is unguarded against SSRF by Karakeep's own admission
  (`videoWorker.ts:81-83`), and it raises the sites' terms of service. Aura indexes video by
  metadata. A transcript through the existing speech-to-text would be the useful form, and
  nothing asks for it.
- **Default-on auto-tagging, and LLM summaries.** Neither is evaluated (§Verdict). D6 stays "not
  measured" on Aura's own sets.
- **Importers** (Netscape HTML, Pocket, Omnivore, Linkwarden, Readwise and others,
  `packages/shared/import-export/parsers.ts:663-705`). There is no saved-link store to import
  into, and no demand.
- **A per-user storage quota with a `QuotaApproved` token** (`packages/shared-server/src/services/quotaService.ts:94-123`,
  `packages/shared-server/src/assetdb.ts:40-55`). Aura has per-modality caps and a disk-pressure
  monitor, and no member has been measured filling the disk. If it is ever needed: Karakeep's
  token proves that a check ran, but the check and the write are not atomic, so concurrent
  uploads can exceed the quota.
- **Outbound webhooks** (bearer token, no HMAC signature, `workers/webhookWorker.ts:128-170`).
  Aura delivers through its channels, and no consumer exists.
- **A search query language.** The fields spec rules one out (`2026-10-18-document-fields-design.md:59`).
  If one ever lands, Karakeep's degradation is the behaviour to keep: an invalid query becomes
  plain text, and a partial one keeps the parsed part and treats the rest as text
  (`packages/shared/searchQueryParser.ts:435-473`).
- **Autoconsent, the adblocker, the bot-challenge wait and the parser subprocess.** These belong
  to a headless server crawler written in Node. Aura's browser is the agent's own box, which the
  operator can take over, and its fetch is Go with a 5 MB body cap.
- **The Meilisearch, liteque/restate, Drizzle/SQLite and Puppeteer/Playwright stack.** Aura's
  stack differs, and only behaviours transfer.

## Measurements run for this read

**Aura's `classify`, both copies.** Run on 2026-10-26 with `go1.27.3`, through a `go test
-overlay` probe, so no file was added to the tree (`git status` clean afterwards). The probe ran
against `internal/web` and `internal/mcp` at `baa95bb`, and both gave the same table:

| Address | What it is | Aura blocks |
|---|---|---|
| `127.0.0.1`, `169.254.169.254`, `10.0.0.1`, `100.64.0.1` | loopback, metadata, private, CGNAT | yes |
| `::ffff:169.254.169.254` | IPv4-mapped metadata | yes |
| `64:ff9b::a9fe:a9fe`, `64:ff9b::7f00:1` | NAT64 embedding metadata and loopback | **no** |
| `2002:a9fe:a9fe::1`, `2002:7f00:1::1` | 6to4 embedding metadata and loopback | **no** |
| `2001:0:4136:e378:8000:63bf:3fff:fdd2` | Teredo | **no** |
| `198.18.0.1`, `240.0.0.1`, `192.0.0.1` | benchmarking, reserved, IETF assignments | **no** |
| `255.255.255.255` | broadcast | **no** |
| `100::1`, `fec0::1` | IPv6 discard-only, deprecated site-local | **no** |
| `8.8.8.8` | public | no (correct) |

**Karakeep's denylist.** Its set from `network.ts:16-33` was run through ipaddr.js 2.2.0 (the
version range `apps/workers/package.json` declares). NAT64 comes back as `rfc6052` and is
**allowed**. `fec0::1` comes back as `unicast` and is **allowed**. 6to4, Teredo, the reserved
ranges, broadcast and discard-only are blocked.

**What these measurements do not prove.**
- No request was made through a NAT64 gateway or a 6to4 relay, so no exploit is shown.
- Whether the lab VM's networks carry IPv6 at all was not checked.
- The egress-floor observations (IPv4-only, a metadata `/32`, no CGNAT) are read from
  `egress.go`, not probed in a running box.
- Nothing in K2 to K9 was run. Every "what to measure" above is still to run.
