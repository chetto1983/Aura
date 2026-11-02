# The digest the model writes

**Status:** approved by the operator on 2026-11-02, on a mock of the text: "va bene, a questo serve, se non è solo una
lista sterile". It changes `2026-10-31-mail-calendar-watch-design.md` §4 and §5.2, which said the digest ran with no model.

## Why

The first digest on the lab VM fired on time and in the person's language, and the operator's verdict was "così serve a
nulla" and then "qui serve LLM una volta al giorno". Measured on that digest, 29 lines decided in one day, 20 shown:

| What was wrong | Measure |
|---|---|
| It repeated what the phone already said | 10 of the 12 "Urgenti" had `notified = true` |
| It told a meeting's whole history | 8 of the 20 lines were 3 meetings, all three already cancelled |
| It printed lines with nothing in them | 2 of the 20 read "(non più disponibile)" |
| It split one conversation | 2 lines were the same thread |

A list cannot fix the second and the fourth without reading natural language, and rules on natural language are what
this project refuses. So this is the one place the watch pays for a model, once a day. The ladder stays as it is: it
decides alone, with no model on the path, every five minutes.

§5.2 repeated `now` deliberately, "with no delivery state to keep". That state exists now: `notified` has been on every
verdict since the alert recovery landed, so the reason is gone.

## What the person reads

```
Riepilogo di posta e calendario
Da fare
1. Martina SNC: risposta sul tagliando Corolla, 2 mail
2. PierMarSync: richiesta preventivo
Da guardare
3. GitHub: CI Skills fallita su master
Da sapere
4. PierMarSync: tre riunioni annullate, niente in agenda
Altre 10 voci non chiedono nulla.
```

## Decisions

**One call a day, per identity, at the digest hour.** It runs inside `pim_digest`, with no tools, on the model that
identity chats with: `IdentityLLMResolver.SnapshotFor`, so an identity's own credential pays for its own digest and a
deployment on a local model stays local. There is no setting and no second model. A window with nothing new makes no call.

**The model reads headers, never a body.** Each item is a number, whether it was already told, tier, label, account, mail
or event, sender name, subject, the event's time and change word. No body is fetched: one mail body measured 62k
characters (§3 of the watch spec). The window is read up to 100 items (`maxDigestRead`), twice the PIM's batch, so the
cost is bounded whatever the day was. What lies beyond is counted and said.

**The model answers with numbers, Aura writes the digest.** The reply is JSON,
`{"lines":[{"section":"do|look|know","items":[3,7],"text":"..."}]}`, read strictly. Aura prepends the sender it read from
the PIM to each line, keeps the mark of a sender that failed authentication, numbers the lines, and keeps each line's
addresses in the record, so "apri la 1" still works and opens both mails. A number that does not exist, a section
outside the three, an empty `text` or an unparsable reply refuses the whole answer. At most 20 lines stand
(`maxDigestLines`), and what a cut leaves out is counted with the rest.

**Sender and subject are attacker text, here too.** They reach the model inside the untrusted frame, the call has no
tools, and the model's text goes in the record's frame like the subjects did. A hostile subject can at worst bend a
sentence. It cannot invent a mail, change who a line says it comes from, or hide what the next rule protects.

**The model cannot lose what nobody was told.** An item at `now` with `notified = false`, held back by the daily cap or
raised by a correction, stands in "Da guardare" in the fixed form if the model left it out.

**A told item is context, not a line.** It goes to the model marked as told, because a mail decided `today` may be the
cancellation of a meeting the phone announced. Unless the model cites it, it appears nowhere and is not counted.

**A gone item leaves the digest,** from the model's input and from the list: the line said nothing a person could use.

**The model failing costs nothing but the prose.** A timeout, a refused credential or a refused answer sends the fixed
list, minus what was told and what is gone, and the window moves as it always did. The digest is the only delivery for
everything below `now`, so it never waits for a model. A run's ledger summary says which of the two was sent and how
many tokens the call took.

## What is out

- Dropping advertising before it reaches the digest, and sorting mail by similarity in ArcadeDB. They belong to the
  classification work, which follows this one.
- A thread id on the verdict. The model merges a thread by its subject. If that proves weak, the fork already carries
  `ThreadId`, and a column would bring it here.
- A switch to turn the model off. The list is the fallback, and BASELINE OR DROP below is how it would go.
- A budget check. One call a day, measured in the ledger, is not what a budget gate is for.

## Baseline

CLAUDE.md names the briefing among the components that must beat their dumb baseline or be dropped. The baseline is the
fixed list, minus what was told and what is gone. The gate is the operator's reading over five working days, with what
each digest left out available in the account's day in the cockpit: an important item the model hid and the list would
have shown counts against it. If it hides more than it saves, the wiring comes out and the list stays.

## What this does not prove

The token figure in the design is an estimate until a live run writes it in the ledger. A mock written by hand from the
operator's own paste shows the shape of the text, not what any model writes. Quality on a small local model is unmeasured.
