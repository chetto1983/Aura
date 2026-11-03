# How soon a mail matters, learned per identity: the centroid in shadow, on a helper shared with documents and retrieval

**Status:** approved by the operator on 2026-11-03 ("ok"). Asked the same day, "allora procedi con il centroide" and "proviamo e vediamo",
after the classification spike of 2026-11-02, and built on one helper for every domain that learns from the person,
as the operator asked the same day. It is step 6 of `2026-10-31-mail-calendar-watch-design.md` §8,
corrections by similarity, and it is the first learned piece, so it runs in that spec's shadow (§5.5). It changes §5.4
of that spec in one point: a correction's vector is stored in the identity's ArcadeDB database.

## Why

The watch decides a mail's tier with rules and provider facts alone. Measured on the pilot's 200 invented mails
(`docs/superpowers/verification/2026-11-02-mail-classification-spike.md`), the ladder alone scores 37.0% right and
finds 2 of 23 `now`. Over the same month, on the 87 mails decided after a shadow half-month:

| Arm | Macro F1 | `now` found of 11 | Gain over the per-sender baseline, 95% |
|---|---:|---:|---|
| Per-sender memory, §7 G2b's baseline | 28.4 | 1 | |
| Centroid, sender and subject plus six header facts | 58.8 | 5 | +29.9 [+17.0, +42.8] |
| Centroid, sender and subject plus the four facts `Signals` carries | 57.5 | 5 | +28.7 [+16.0, +41.6] |
| **The same, as it ships: `semindex`'s math, seeds under `.example`** | **56.7** | **8** | **+28.0 [+13.8, +41.6]** |
| Centroid, sender and subject alone | 49.7 | 5 | +21.0 [+8.1, +34.1] |

The last three rows were measured on 2026-11-03 for this spec (`D:\tmp\mail-class\run6.py`, `run7.py`). The bold row
is what this spec builds: `semindex.Classifier`'s math, which gave the same 57.5 as the spike's on the same seeds, and
seeds whose sender domains were moved under `.example` while the mail being decided kept its own. The rest of the
spike decided the shape:

- **No network.** A small network, Paperless's shape and a global-plus-delta model did not clear the baseline.
- **No body.** Sender and subject do as well as the whole mail.
- **Shadow first.** A model that decides from day one hides mail filed `later`, the person stops correcting, and the
  feed dries: in the first version of the bench the feed stalled after 0 to 3 of 4 personas, and from an empty start
  one correction taught "everything is `later`".

`internal/semindex` already holds this classifier, written for the reasoning router: `Classifier.RankVecs` normalises
each exemplar, scores a vector against each label's mean, and returns the label, its cosine and the top-2 margin.

## What it does

For every mail the ladder decides, the watch also asks the centroid. The answer never changes a verdict and never
reaches the person's phone. It is written beside the verdict with what the scoring needs later, and the decisions view
shows where it differs from the ladder. That is all this spec builds.

The centroid of each tier is built from two banks:

1. **Group seeds,** shipped with Aura: the pilot's 200 invented mails, four personas, their frozen tiers.
2. **The identity's corrections,** each the vector of a mail the person moved to another tier, with that tier.

A correction weighs 6 and a seed 1, as measured. The person's corrections pull their own centroid, and the seeds keep
it from starting empty.

## One helper for mail, documents and retrieval

Operator, 2026-11-03: "io costruirei un helper comune per email, documenti e rag così non duplichiamo funzioni". Mail is
the first consumer, and nothing in the helper may know it is mail.

What exists: `semindex.Classifier` does the math in memory, for the reasoning router, whose seeds are written in Go.
`internal/arcadedb/embedding_space.go` stamps a vector's space and gates reads by it, for memory and documents. What no
domain has is a place for **what the person confirmed**, kept as examples and fed back. Each domain would write its
own, so the helper is that place, built once:

- **A store of confirmed examples,** in the identity's ArcadeDB database, one vertex type for every domain. An example
  has a domain, a ref that names the thing it was made from, an owner whose removal forgets it, a label, the vector, its
  space and when it was made. One example per domain and ref: a new confirmation of the same thing replaces the old.
  Vectors follow `embedding_space.go`'s stamping. No text, ever.
- **Seeds per domain,** shipped as files, embedded once per process and per space and kept in memory, never stored as
  the person's.
- **A per-identity classifier** built from a domain's seeds and the identity's examples in the live space, with the
  domain's weights, answering through `semindex.Classifier.RankVecs`: label, cosine, margin. Owners that no longer exist
  are left out when the bank loads.
- **Forgetting** by owner, and by domain when a domain is dropped.

Each domain keeps what is its own: what text becomes the vector, which facts are added, when an example is written,
the weights, and how its shadow is scored.

| Domain | An example is written when | The vector | Status |
|---|---|---|---|
| Mail tier | the person corrects a tier (`Service.Correct`) | sender and subject, plus four facts | this spec |
| Document type, tag, counterparty | the person sets one in the cockpit (`docfields.ApplyDiff` with `SetByOperator`) | the document's card | later, after its own measure: d1 already names the type of 34 of 34 synthetic documents with no examples (`spikes/d1-document-labels/FINDINGS.md`), so the helper's place there is the person's own tags |
| Retrieval | there is no signal yet: nothing in Aura records that the person found a passage useful | | when such a signal exists |

The general side, a decision model naming what anyone would name (d1 on labels, document types and passages, in the
night spikes of 2026-11-02), is a second shared piece with no Go client yet. It is not this spec.

## Decisions

1. **The vector.** EmbeddingGemma over `"<from>\n<subject>"`, where `from` is the bare address the PIM gives in
   `Change.From`, with no prefix and no body. The embedding is normalised, then four facts are appended, each 0.3 when
   true: `WrittenTo` (true only when the provider says so), `ProviderImportant`, `AuthFailed`, `Bulk`. This is the bold
   row. A different text or different facts is a different arm and is measured first.
2. **The route.** Aura's embedding route, the one documents use, through `embeddings.Route`. Its `Space()` names the
   space. Seeds and corrections from another space never enter the bank.
3. **The classifier.** The helper's, so `semindex.Classifier.RankVecs` as it is. The six-to-one weighting is the
   builder's to express, by repeating a correction's row or by a weighted add in `semindex`.
4. **Seeds.** The mail-tier domain's seed file, with each seed's sender, subject, the four facts and the tier, as the pilot
   wrote them, except that every sender's domain is moved under `.example` (RFC 2606): `<local>@pec.<office>.it`
   becomes `<local>@<office>.example`, dropping the TLD and the `www`, `mail` and `pec` labels. That keeps real
   domains and look-alikes of real brands out of a public repository, and it cost 0.8 points, inside the noise. The seeds' `WrittenTo` is the pilot's "known correspondent",
   which the watch spec's §7.1 calls inflated: it stays as measured. Seeds are embedded once per process and per space,
   kept in memory, and never written to ArcadeDB. A seed whose embedding fails leaves the bank smaller, and the run
   counts it.
5. **Corrections.** Examples in the helper's store (`TenantClients.For`), domain mail tier, ref the account and the item,
   owner the account, label the tier: a second correction of the same mail replaces the first. One is written when
   `Service.Correct` changes a tier. The sender and subject are read live from
   the PIM by id at that moment, embedded and dropped, and the four facts come from the verdict row (decision 7). No
   text is stored. A label-only correction writes nothing here. If the PIM or the embedder fails, the correction still
   stands in Postgres as today, with no vector, and the failure is counted.
6. **Forgetting.** `RemoveAccount` forgets the account's examples through the helper, by owner. Because that call runs
   after the sidecar has dropped the account and only logs a failure, the bank also leaves out owners the identity no
   longer watches, and the watch's existing sweep forgets them. A correction from an old embedding space cannot be
   re-embedded, because its text is gone, so it leaves the bank. The sender rules it may have taught stay.
7. **What a verdict row keeps,** on `aura.pim_watch_verdicts`, so the gate can be scored from the rows alone. A
   correction overwrites `tier` today, so the ladder's own answer has to be kept apart:
   - the ladder's tier, written once and never changed by a correction;
   - the tier the centroid would give, and its margin, empty when it could not answer;
   - the per-sender memory's tier at the same tick: the sender's last corrected tier, else the ladder's;
   - the four facts of decision 1;
   - when a sent digest covered the verdict.

   No sender and no text. The migration takes the next free number at landing (`ls internal/db/migrations/ | tail
   -1`; today 0151 is the last).
8. **Every mail.** The shadow answers for every mail `DecideMail` decides, rules included, so the counts can say where
   it would have overruled each rung. Calendar changes are out.
9. **Failure is silent to the person, never to the ledger.** An embedding, ArcadeDB or PIM failure leaves the verdict
   exactly as today and the shadow empty. The tick never waits on a retry and never fails because of the shadow. The
   tick's ledger summary adds the shadow's count: answered, agreed with the ladder, empty.
10. **What the person sees.** The decisions view marks a mail where the centroid's tier differs from the ladder's, with
    the tier it would have given. No toggle exists yet, because going live is not built.
11. **The scoring.** A read-only report on the appliance prints the gate's counts from the verdict rows, and nothing
    else: no sender, no subject, no id. A CLI subcommand is enough.
12. **Cost.** One embedding call per page, batching that page's mail. No model call. **Changed on 2026-11-03** by the
    operator, after the final review asked: the shadow runs on the documents' embedding route whatever it is, as the plan's
    decision 8 says. On the local route it costs no tokens. On a cloud route each mail's sender and subject go to that
    route, about 4,000 tokens for the seeds once per process and space, then one short text per mail.

## What is out

- **Going live.** The tier on the verdict stays the ladder's. Watch spec §5.5 sets seven days of shadow and the
  person's switch, and the promotion gate below must pass first. That is its own spec.
- **A doubt threshold.** Without the body, sending doubts to the digest made the centroid worse (49.1 against 57.5), so
  no threshold is set here. Over the 200 seeds the margin's 10th, 50th and 90th percentiles are 0.007, 0.036 and 0.074.
  The margins written in shadow are what will choose one.
- **The alert's text.** The operator found the alerts sterile on 2026-11-03. That is a separate spec, after this one.
- **Labels, d1, TabPFN, a network, ArcadeDB's graph algorithms.** Measured and not chosen, or a separate question. The
  graph arms are re-run in the spike once the fixes for #11144-#11146 reach an image.
- **Re-linking.** Removing and re-adding an account forgets its corrections today
  (`project_relink_wipes_watch_history`). This spec stores more per correction, so that loss grows. Fixing the cockpit's
  re-link is separate, and this spec only says so.

## Baseline

BASELINE OR DROP, on the operator's real mail. After seven days of shadow, the report scores the centroid's tier, the
ladder's and the per-sender memory's against what the person settled:

- **Strong truth:** a corrected mail's new tier.
- **Weak truth:** an uncorrected mail older than 48 hours whose verdict reached the person, through a delivered alert
  or a sent digest that covered it. Its tier stands. The digest shows at most 20 lines and the model may leave one out,
  so this truth is weak and is reported apart.

The centroid must beat both the ladder and the per-sender memory, on the strong truth first, or it is dropped and its
vectors deleted. The counts stay on the appliance, as §5.5 requires, and no developer reads mail to make them.

## What this does not prove

- **That the spike's number holds on real mail.** One author wrote all four personas, and the seeds and the test mails
  share that author. Seeds written by us may transfer worse to a real inbox. Shadow exists to find this out.
- **That seeds in Italian help an identity that writes in another language.** EmbeddingGemma is multilingual, but
  this was not measured.
- **Anything past 15 corrections.**
- **That people correct enough to feed it.** The lab VM's 17 corrections are all from test runs. If shadow shows no
  feed, the centroid runs on seeds alone, and the baseline decides whether that is worth keeping.
- **The tick's added latency on the appliance.** One local embedding batch per tick is expected to be small. It is
  measured on the lab VM, not assumed.

## Left to the builder

- Where the shadow code sits, under 600 lines a file; `watcher.go` is at 541 already.
- The helper's package boundary (the math stays in `semindex`, the store beside the other ArcadeDB types), its names,
  and the example type's DDL, following `internal/arcadedb`'s schema code and `embedding_space.go`.
- How the bank is cached between ticks and when a new correction invalidates it.
- How the per-sender memory's tier is read at the tick without one query per mail.
- The seeds file's format and where it lives.
- The decisions view's mark, in the cockpit's existing style.
- The report's exact form.
