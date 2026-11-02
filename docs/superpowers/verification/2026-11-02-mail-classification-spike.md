# Mail classification: centroid, small network, ArcadeDB's graph and an LLM fallback, 2026-11-02

**Measured offline on the pilot's 200 synthetic Italian mails:** four invented personas, frozen tiers
(23 `now`, 72 `today`, 105 `later`), cached EmbeddingGemma vectors, and cached answers standing in for
the LLM. No model was called and nothing was spent. No product code changed. The bench lives in
`D:\tmp\mail-class`, outside the repository.

The graph arms ran on a disposable engine, one database per persona, on `26.10.2` and on
`26.11.1-SNAPSHOT` (build `d6e16a35e`, `sha256:2852f6fb00d2`). All 16 rows came out identical to the
digit on both versions.

**Result:** one arm clears BASELINE OR DROP, a class centroid over group seeds, and it needs neither
the mail's body nor a network. Self-training gained nothing measurable. This corpus cannot say what
happens past 15 corrections.

## Protocol

- **Per persona, in date order.** Group seeds are the other three personas, weight 1. A correction
  weighs 6.
- **Shadow first, spec §5.5.** To day 15 the ladder decides and nothing is hidden, and the person
  corrects the ladder's mistakes. From day 16 the model decides.
- **Scored as it happened.** A late correction does not undo an alert, and a mail filed `later` is
  never seen again, so it is never corrected.
- **The baseline** is §7 G2b's: the ladder plus the last correction per sender.

The first version of the bench let the model decide from day one, and it could not run the curve. The
model files mail, the person stops seeing it, and the feed dries: 5 corrections were reached in 0 to 3
of the 4 personas, and from an empty start one correction taught "everything is `later`". This is the
mechanism of the classifier that stalled at 0 of 13 overrides, so shadow is not optional.

## A month, days 16 on

87 mails. The interval is the 95% paired bootstrap of the macro F1 gain over the baseline.

| Arm | Macro F1 | `now` found of 11 | Alerts | `now` filed `later` | `later` filed of 48 | Gain over baseline |
|---|---:|---:|---:|---:|---:|---|
| Per-sender memory, the baseline | 28.4 | 1 | 6 | 8 | 11 | |
| **Centroid, group seeds** | **59.0** | 7 | 20 | 1 | 34 | **+30.3 [+16.8, +43.1]** |
| **Centroid, group seeds, sender and subject only** | **58.8** | 5 | 14 | 1 | 34 | **+29.9 [+17.0, +42.8]** |
| Centroid, no seeds | 45.6 | 5 | 13 | 6 | 45 | +16.8 [+2.7, +31.3] |
| Small network, 32 units, group seeds | 44.5 | 3 | 6 | 6 | 46 | +15.6 [-1.3, +32.1] |
| kNN 5, group seeds | 42.3 | 3 | 8 | 7 | 47 | +13.5 [-3.4, +29.0] |
| Global model plus per-person delta | 38.5 | 3 | 3 | 8 | 48 | +9.7 [-8.2, +26.0] |
| Softmax, group seeds | 34.1 | 1 | 2 | 8 | 48 | +5.5 [-9.5, +20.6] |
| Paperless's shape, a network on words | 31.2 | 0 | 1 | 9 | 45 | +2.9 [-9.9, +14.6] |

With half of the visible mistakes corrected, five draws, the centroid holds at 59.5 and the baseline
is 32.0.

The centroid is what `vector.avg` and `vector.cosinesimilarity` answer in one query, and it is a few
lines in Go. The global-plus-delta shape is Gmail Priority Inbox's (Aberdeen, Pacovsky, Slater, 2010).
That system trains on every mail the user could see, on implicit actions and at a volume this corpus
does not have, so this row says nothing against it there.

## By corrections, on 100 held-out mails

Macro F1 on the second half of the month, which nothing was trained on. One persona offered 13
corrections, so its column 15 stops there.

| Arm | 0 | 5 | 10 | 15 |
|---|---:|---:|---:|---:|
| Per-sender memory | 21.2 | 33.3 | 37.6 | 34.7 |
| Centroid | 57.2 | 59.0 | 57.5 | 59.6 |
| Small network, 32 units | 49.0 | 51.9 | 46.2 | 46.9 |
| Softmax | 48.1 | 44.1 | 38.9 | 35.4 |
| PPR, sender, domain and similarity graph | 60.4 | 45.9 | 38.5 | 35.2 |
| Louvain community vote | 51.1 | 46.9 | 46.1 | 45.8 |

Fifteen corrections do not move the centroid beyond the noise, and they make softmax and PPR worse.
Corrections are the ladder's mistakes, mostly `later`, so weighting them pulls everything there.
Counting an uncorrected digest mail as a weak `today` stopped most of the fall: softmax 46.4 and the
small network 55.0 at 15.

## The LLM fallback

Below a threshold, the mail is a doubt: it stays in the digest, and the stand-in answers for it there
once a day. It can never raise an alert. Centroid, threshold 0.5, 87 mails:

| Variant | Macro F1 | `now` found of 11 | `now` filed `later` | Mails asked |
|---|---:|---:|---:|---:|
| No threshold | 59.0 | 7 | 1 | 0 |
| Doubts stay in the digest, no LLM | 62.6 | 6 | 0 | 31 |
| The stand-in answers at the digest | 63.4 | 6 | 0 | 31 |
| Its answers also teach, as weak labels | 63.9 | 6 | 0 | 32 |
| Taught, behind a gate on held-back corrections | 55.2 | 2 | 2 | 15 |

- **Abstaining does more than asking:** 3.6 points and no urgent mail filed, against 0.8 for a third
  of the mail sent to the model.
- **Teaching changed nothing measurable,** on any arm.
- **The gate hurt the best arm,** 13 refusals on a held-back set of a few corrections. It raised the
  weak arms by 4 to 18 points, mostly because it holds back a third of the corrections that were
  hurting them.
- **Other thresholds are lower.** At 0.6 the centroid doubts 55 of 87 mails.
- **Showing a share of `later` anyway** moved softmax from 34.1 to 35.3 at 20%.

## What ArcadeDB offers here

Enumerated from the installed jar and read in the source at the tag.

| Function | Verdict |
|---|---|
| `vector.avg`, `vector.cosinesimilarity` | The winning arm. |
| `vector.neighbors` | Same order as the exact computation. kNN does not earn its place. |
| `vector.neighbors` with `maxDistance` 0.30 | 37.6 with group seeds. Below the plain kNN. |
| `vector.recommend`, `vector.discover`, `vector.boost` | Retrieval and rerank, not classification. Not run. |
| `algo.personalizedPageRank` | Works with edges written both ways. Starts level with the centroid and falls with corrections. |
| `algo.fastrp`, `algo.graphsage` | Read no node property, so they never see the text. No gain as extra features. |
| `algo.hashgnn` | All-zero vectors with its defaults, #11144. |
| `algo.labelpropagation` | Takes no seeds, whatever its use-case line says, #11145. |
| `algo.louvain` community vote | Below the centroid. |
| `algo.knn` | Jaccard on graph neighbours, not vectors. Not run. |

Upstream, opened 2026-11-02 at the maintainers' request before the release: ArcadeData/arcadedb#11144,
#11145 and #11146. None of them blocks the winning arm.

## What this does not prove

- **It is not real mail.** One author wrote all four personas, so group seeds may transfer better here
  than seeds written by us will transfer to a real inbox. This is the largest risk to the centroid's
  number.
- **The sample is small:** 87 and 100 mails, about ±13 points of macro F1. Differences inside that are
  noise.
- **The stand-in is not the digest's model.** It is Jev 1.14's cached answers, macro F1 46.1 alone and
  16 of 105 `later`. "The LLM adds little" holds for this stand-in only.
- **The threshold is chosen after the fact.** 0.5 is the best row of a table, so its numbers are
  optimistic. They are not a calibration.
- **Past 15 corrections nothing is known.** The Paperless shape and the delta may need that volume.
- **Corrections are simulated,** and no implicit signal is tested. The corpus has no read state, and
  the PIM fills `IsRead` for Gmail, IMAP and Graph while Aura does not read it.
- **The live feed proves nothing yet.** The lab VM holds 29 verdicts and 17 corrections, all from
  today's test runs.
- **CPU and scale are not measured.** PPR took 6 to 7 ms per tier on about 200 nodes, and that is all.
  Nothing ran on the appliance or under the ≤4 thread budget as a measurement.
