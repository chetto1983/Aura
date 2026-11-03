# Urgency centroid in shadow, first live run: 2026-11-03

Scope: lab VM .158, updated from the cockpit to Aura `91ca9e571`, one person, the Gmail account. Every workflow on that SHA was green: CI, CodeQL, Skills, the edge image, the ArcadeDB MCP and create-aura. The person sent a test mail from another address, looked at the decisions view and corrected one tier. The figures come from the verdict rows, the run ledger, the container log and the identity's ArcadeDB database. All of them are aggregates or column values, and no mail text was read.

Spec `docs/superpowers/specs/2026-11-03-urgency-centroid-design.md`, plan `docs/superpowers/plans/2026-11-03-urgency-centroid-shadow.md`.

## What was measured

| Step | Result |
|---|---|
| Update from the cockpit | Aura `91ca9e571` running and healthy at 11:11:59 UTC, migration 152 applied and clean |
| The shadow is wired | From 11:16 every tick summary has a `shadow:` part, and none had one before the update |
| Empty ticks | 0.2 s with nothing new, 1.1 to 1.2 s with a PIM read and no mail |
| Cold start | The first tick with mail (11:16:49) read 2 mails. Both shadows were empty and counted, the tick took 3.0 s, and both verdicts were written as the ladder decided them |
| Seeds | 200 seeds embedded once, in the background, in 6.54 s (`example bank seeds embedded`) |
| First answer | Tick at 11:22:19: `1 answered, 0 agreed, 0 empty`, 2.8 s. The row: ladder `now` by `rule`, centroid `later` at margin 0.015, sender memory `now`, facts written-to and provider-important. The tier stayed `now` |
| The cockpit's mark | "Da mail simili avrebbe detto Dopo" under "L'ha deciso una tua regola", seen by the person. The two cold rows have no mark |
| A correction teaches | The person moved the mail to Dopo. The row reads `later` by `person`, and its ladder answer (`now` by `rule`) and the centroid's are kept. The identity's memory database holds exactly 1 `ConfirmedExample`, domain `mail_tier`, label `later`. Another identity's database holds none |
| The report | `aura mail-watch shadow` printed on the VM: strong truth 1 row, centroid 1 of 1, ladder 0, overrules by rung `rule 1` |
| Failure logs | 0 of each of the four new warnings: sender memory not read, a correction taught nothing, an orphan's examples stayed, the shadow is off |

Before the push, on the same SHA: `make quality` green, coverage 87.8% (52,639 of 59,931) with the package policy passing, and `internal/examplebank` at 95.4%.

## The VM's route is not the one the spike measured

The VM's documents embed through OpenRouter with `perplexity/pplx-embed-v1-4b` at 768 dimensions. A settings row sets it, and the space id `es1-b604719dc5fc0ce4` recomputes to exactly that route. The spike measured its 56.7 macro F1 on EmbeddingGemma vectors. The shadow follows the documents' route, as the operator ruled on 2026-11-03. So on this VM it scores a pplx-embed centroid, and each mail's sender and subject go to OpenRouter.

## What this does not establish

- **That the centroid is any good on real mail.** One mail was answered and one corrected. The promotion spec reads the report after weeks of corrections and settled mail.
- **That the spike's 56.7 holds on pplx-embed.** The VM's route is a different arm (spec decision 1). Re-running the spike's 87 mails on pplx-embed would answer it. That is a paid run, about 300 short texts, and it is not done without the operator's go.
- **The bound on a hung embedder, live.** `TestAFailingCentroidIsAskedOncePerTick` and `TestASlowEmbedderCostsAtMostTheTimeout` pin it with fakes. Nothing on the VM hung.
- **Forgetting an account, live.** Removing an account was not run on the VM. `TestRemovingAnAccountForgetsItsExamples` and the bank's orphan tests pin it.
- **Mutation at 70% on `bank.go`, `centroid.go` and `shadow_report.go`.** CI's critical-mutation gate scopes ten fixed files, and none of these is one of them. Adding them is open.
- **The cost on a cloud route over a day.** Neither the seeds' tokens nor the per-mail tokens were read from OpenRouter's billing.
