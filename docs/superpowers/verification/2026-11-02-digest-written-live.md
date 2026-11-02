# The digest written by the model, first live run, 2026-11-02

**Measured on lab VM .158, revision `0f83a97e1`, every workflow green on that SHA:** the 18:00 digest
of a real identity with three watched accounts (Google, Microsoft, IMAP). It was written by the
identity's own model, reached the person on Telegram, and the operator closed the run.

The run is real but small: the window held two items, because the previous digest had gone out two
hours earlier, at 16:00 local.

| | |
|---|---|
| Started, ended | 17:00:02, 17:00:07 UTC |
| Ledger | `2 in the window, 1 written by the model, 503+38 tokens` |
| Errors, fallback to the list | none |

## What the window held

From the ledger, counts only.

| Account | Mail decided | `today` | `later` |
|---|---:|---:|---:|
| Google | 4 | 2 | 2 |
| Microsoft | 1 | 0 | 1 |
| IMAP | 0 | 0 | 0 |

The digest takes `now` and `today`, so five decisions became two items, both from one account, and the
three `later` stayed out by rule. The operator saw one account and asked why. That is the reason, as
far as the ledger can show. All three mail cursors were `ok` and advancing.

## What it does not prove

- **Quality on a full day.** Two items cannot show a person's day reduced to what matters. The first
  digest of the same day, a plain list of 29, is what the design answers, and this run never met that
  load.
- **BASELINE OR DROP is still open.** The spec's gate is the operator's reading over five working days
  against the fixed list. One small run closes the wiring, not that gate.
- **Whether both items got a line.** The ledger counts one written line. It may cite both items, or
  the second may be in the closing count. I did not read the text.
- **The token cost of a real day.** 503+38 is two items. The design's estimate stays an estimate.
- **The fallback, live.** The model neither failed nor was refused here. Only the tests cover that path.
- **That nothing was missed.** The ledger shows what the watch saw, not what the mailboxes received.
  The PIM answers only with an OAuth token issued by Aura, and I did not work around it to count. IMAP
  has been watched only since 15:16 UTC, has decided nothing yet, and stays unmeasured on a real mailbox.
- **What an account keeps across a re-link.** Microsoft's earlier verdicts and corrections were gone
  after the operator re-linked it at 16:02 UTC, which is why it shows one decision today. The code
  that deletes them is the path for an account that left. Open, and not caused by this change.
