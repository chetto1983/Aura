# A label for every mail the person's rules leave unsorted: the decision model, picked like the embeddings

**Status:** draft, 2026-11-03, for the operator's approval. Gate G1 was measured the same day.

**Asked** the same day, after the urgency centroid shipped in shadow: "se devo fare tutto manualmente tanto vale che apro
le mail e me le leggo".

**The operator approved:**
- the task ("va bene");
- the per-label keep or discard ("io metterei una regola per etichette se scartare o tenere");
- a model label that counts for real ("si va bene poi l'utente decide ed il modello apprende");
- reading the mail's text ("si perchè le etichette l'utente può cambiarle in base alle sue esigenze e le sue regole,
  questa cosa funzionava abbastanza bene in pmsync");
- the picker ("io metterei la scelta come per gli embedding"; "l'utente sceglie se usare openrouter o locale");
- descriptions in the person's own words ("le descrizioni le scrive l'utente");
- default labels that arrive described, in Italian and English ("la a ma con le traduzioni inglese e italiano");
- discard as out of sight and counted, and the paid gate G3 ("ok va bene", with the key to use).

**Where it fits.** It is the local model of step 5 of `2026-10-31-mail-calendar-watch-design.md` §8, for labels. That
spec's §4 already foresaw it: "A local decision model reads mail on the appliance, and a cloud one is an opt-in per
account". Laya's detection of `now`, the other half of step 5, is not part of this spec. What it changes in the watch
spec is listed below.

## Why

The watch decides a mail's tier with the person's rules and the provider's facts. It gives a label only when a rule
names one. On the lab VM on 2026-11-03, most of the day's mail sat under «Da smistare», so the person sorted by hand,
and that is the work the watch was meant to remove.

PMSync, which the operator says worked well enough, did this with a model. Its behaviour, read from
`chetto1983/PMSync` (`backend/src/modules/ai/services/email-analysis.service.ts`; behaviour only, no code comes over):
- Every label the person defines carries an instruction in their own words (`aiPrompt`). The model gets the list of
  labels and those instructions.
- The model reads the sender, the subject and the start of the body.
- The person's rules decide first. For the sender in front of it, the model also gets the person's last few corrections
  as short lines, and a hint such as "past corrections suggest this label".
- The answer is normalised to the person's labels. An answer outside them is dropped.

## Measured before this spec

On 2026-11-02, LiquidAI's d1 decision models, on 200 synthetic Italian mails with eight labels and gold labels set
before any prediction. All runs used llama.cpp `server-cuda-b11960` and its native `/v1/systemone` endpoint
(`docs/superpowers/verification/2026-11-02-d1-italian-email-labeling.md` and
`docs/superpowers/verification/2026-11-02-d1-email-accuracy-improvement.md`):

| Model | Correct | At probability ≥ 0.9 | Latency, GPU |
|---|---|---|---|
| d1-omni-600M Q8_0, English question | 130/200, 65% | 42 accepted, 38 right | 39 ms |
| d1-3B Q4_K_M, the same question | 191/200, 95.5% | 148 accepted, 146 right (74% coverage, 98.65%) | 202 ms |
| d1-3B, 120 mails written after the choice | 120/120 | | |

- Italian instructions scored 15.5 points below English ones on the 600M model.
- Eight examples in the prompt did not help the 600M model.

On 2026-11-03, d1-3B on CPU only, 4 threads on the development PC (an i7-11850H, `-ngl 0`), on the same frozen sets
and with the 2026-11-02 question (eight labels, English definitions): **81 of 81 right**, the 40 confirmation mails and
the first 41 of the 80 held out, at **p50 5.0 s and p95 6.4 s per mail**
(`spikes/d1-email-labeling/labels5/cpu-3b-4threads/`). The run was stopped there to free the CPU for G1 on the GPU. A
smaller appliance CPU is slower, and a box like a Ryzen AI Max+ 395 is faster. Neither was measured.

On 2026-11-03, gate G1 (`spikes/d1-email-labeling/labels5/`): d1-3B on the GPU, the operator's five labels plus
«Nessuna» ("none of these"). Each label had a one-line description as a person would write it, the frame was in English,
and the body was cut at 255 characters. A fixed mapping froze the gold before any run: Amministrazione, Spedizioni and
Notifiche became «Nessuna». The threshold was chosen on the 200 mails by a rule written down before the 120 held-out
mails were run (`results/threshold-choice.json`). The rule takes the value that maximises right labels minus c times
wrong ones. That value is 0.5 for c = 5 and c = 10, and 0.4 and 0.5 tie for c = 3.

| Descriptions | Set | Right, no threshold | Labels at ≥ 0.5 | Of which right | Labelable mail labelled at ≥ 0.5 | Labelled at ≥ 0.9 |
|---|---|---|---|---|---|---|
| Italian | 200 | 170 | 94 | 91 | 73% | 8% |
| Italian | 120 held out | 104 | 58 | 58 | 77% | 16% |
| English | 200 | 176 | 101 | 96 | 77% | 16% |
| English | 120 held out | 109 | 66 | 65 | 87% | 21% |
| Italian, `liquid/d1.1` on OpenRouter (G3) | 200 | 195 | 124 | 121 | 97% | 61% |
| Italian, `liquid/d1.1` on OpenRouter (G3) | 120 held out | 116 | 75 | 72 | 96% | 68% |

- **Wrong labels at 0.5 (d1-3B, Italian, 200 mails): three.** Two mails were labelled Lavoro (one a customer's, one personal),
  and one mail that has no label was labelled Cliente.
- No mail was wrongly labelled Pubblicità, at any threshold, in either set.
- **The draft's 0.9 labels almost nothing.** The «Nessuna» option and the short instructions spread the probability.
  The 2026-11-02 spike had eight long English definitions and no «Nessuna».
- **The sender hint (d1-3B, Italian, at 0.5).**
  - A right hint: 90 of 93 right on the 200, and 59 of 59 held out. Without a hint: 91 of 94, and 58 of 58.
  - A wrong hint: 67 of 72 and 49 of 54, with five labels on mail that has none in each set.
- **The misses (d1-3B, Italian, 200 mails).** Most are Personale (12 of 25) and Opportunità (9 of 25).
- **A label with no description** (d1-3B read the five names only, with the same «Nessuna» option):
  - on the 200, 47 labels at 0.5, of which 31 were right, and 12 fell on mail that has none;
  - held out, 29 labels, of which 23 were right.
  - Measured after the operator's "le descrizioni le scrive l'utente" (`results-names/`, `results-names-heldout/`).
- **CLM's architecture, without its trained heads.** The operator liked it for the cached option vectors and its 131K
  context. It encodes the mail and each option apart, and picks the nearest. It was run with the EmbeddingGemma sidecar
  Aura already runs (`embed_arm.py`, `results-embed/`):
  - by argmax, 127 of 200 right and 73 of 120 held out, against d1's 170 and 104;
  - no temperature and threshold reached 90% right labels.

  The only published models with trained heads are `Contrastive-LM/CLM-v0.1-8B` and `jayavibhav/clm-qwen3.5-2b`. Both
  cards say English only. The 2B card gives 33.9% zero-shot on typed decisions, below the 47.0% of always answering the
  commonest label. The 8B lost to seeds and BM25 in Aura on 2026-10-07 (`spikes/clm-decision/`).
- **Latency of d1-3B on the GPU:** p50 133 to 152 ms per mail.
- No body in either set is longer than 255 characters, so the preview cut was never exercised.

**G3** was run on 2026-11-03 with the operator's go and their key, through
`spikes/d1-document-labels/openrouter_shim.py` (`results-d1.1/`, `results-d1.1-heldout/`).
- OpenRouter served `liquid/d1.1-20261027` from Liquid.
- The run took 75,019 input tokens, about $0.0015 at $0.02 per million.
- Latency was p50 345 to 352 ms per mail from the development PC.
- At 0.5 its wrong labels were three in each set, all on mail that has none. None was a wrong Pubblicità.

## What it does

1. **One model call per mail that has no label yet.** It runs after the person's rules, on the mails no rule gave a label
   to.
2. **The model chooses one of the person's described labels, or none.** Each label has three settings:
   - **tier:** it exists today;
   - **keep or discard:** new;
   - **description:** new, written by the person, for example «Cliente: chi mi chiede preventivi, ordini o assistenza».

   A label with no description is not offered to the model. With the names alone, a third of the model's labels were
   wrong (see Measured).
3. **The label counts.** A label the model is sure of brings that label's tier, and with it any alert, within the floor
   of decision 1. Discard keeps the mail out of alerts, out of the digest and out of the decisions list, under a
   «Scartate» filter (decision 5). The digest closes with one line that counts the discarded mail, with no list.
4. **The person decides last.** A wrong label, tier or discard is corrected with one click in the decisions view, as
   today. The correction stands for that mail. From the second correction it becomes a sender rule, which outranks the
   model (decision 7).
5. **The model is picked in the cockpit like the embeddings.**
   - **Locale:** a llama.cpp sidecar with a pinned d1 GGUF.
   - **OpenRouter:** a model from the list OpenRouter serves with `output_modalities=decisions`.
   - On OpenRouter the mail leaves the appliance, so the model reads an account's mail there only once that account is
     switched on (decision 9).

## Decisions

1. **Order, and the people floor.** The model fills a label where the ladder has none. What that label then does
   depends on what gave the mail its tier (`DecideMail`, watch spec §5.3):
   - A correction on the mail outranks everything, as today.
   - A rule that gives a label is final, and the model is not called.
   - A rule that gives only a tier keeps its tier. The model fills the label, but the label's tier and its discard do not
     apply.
   - **Failed authentication.** The model fills the label. The tier stays `today`, and the mail is never discarded.
   - **Someone the account has written to, or a sender the provider cannot place.** This is `writtenTo` true and not
     bulk, or `writtenTo` unknown. The model fills the label. The label's tier may raise the mail but never lower it,
     and the mail is never discarded.
   - **Every other mail** (not written to, or bulk) takes the label's tier when the label has one, and its discard.
   - When the model is not sure, the mail keeps the ladder's verdict and stays «Da smistare».

   *Engineer's call*, the whole floor. It keeps watch spec §5.7's "the people floor still holds" true, and losing a
   person's mail is the error the operator named.
2. **What the model reads: the subject and the snippet.**
   - The PIM fork already writes a `snippet` on every mail for Google, Graph and IMAP (`MailSnippet`, at `931a385`). It
     is the first 400 characters of the body as plain text, with no links, no markup and no text hidden from the reader.
   - The fork does not use the providers' own previews, because those keep hidden text.
   - Aura's `Change` does not decode it yet (`internal/pimwatch/client.go`). No fork change is needed.
   - The state is `Oggetto: <subject>`, a blank line, then the snippet: the shape G1 measured.
   - *Engineer's call:* not the sender's address or name. No measurement included them, and the sender already acts
     through the rules and the people step.
   - The model also reads the person's described labels.
   - Aura never stores the snippet. It is read, judged and dropped, like the mail itself.
3. **The question.**
   - One `choice` question per mail. The options are the identity's labels that have a description, plus «Nessuna».
     Each option carries its description.
   - The frame is in English, because that measured 15.5 points better. The label names and descriptions stay in the
     person's words.
   - G1 measured the cost of Italian descriptions at 0.5. They labelled less mail than English ones: 73% against 77% of
     the labelable mail on the 200, and 77% against 87% held out. The share of right labels was the same.
   - Aura sends the descriptions as the person wrote them and does not translate them. The operator, 2026-11-03: "le
     descrizioni le scrive l'utente".
   - When no label has a description, the model is not called.
4. **Sure means a threshold of 0.5.**
   - A label is applied only when its probability is at least **0.5**, that is, more than all the other options
     together. Below it, the mail keeps the ladder's verdict and no label.
   - The value was chosen on G1's 200 mails by a rule written down first, and confirmed on the 120 mails held out (see
     Measured). It replaces the draft's 0.9, which labelled 8% of the labelable mail.
   - The verdict keeps the probability (decision 11), so G4 can recompute what any other threshold would have done on
     real mail.
   - *Engineer's call:* it is a constant, not a setting.
5. **Discard: out of sight, and counted.** The operator approved this on 2026-11-03 ("ok va bene").
   - A label whose tier is `later` already keeps a mail out of alerts and out of the digest
     (`ListPIMWatchDigestVerdicts` takes only `now` and `today`).
   - Discard adds two things `later` does not do:
     - the mail leaves the decisions view's list, and shows only under a «Scartate» filter;
     - the digest ends with one line that counts the discarded mail, with no list.
   - A discarded mail is decided as `later`, whatever the label's tier.
   - It is kept, never deleted. Under «Scartate» the person rescues it by changing its label or its tier, and that is a
     correction like any other.
   - It applies whoever gave the label: a rule, the person or the model.
   - A label the model gave discards only where decision 1 lets it: never on failed authentication, never for someone
     the account has written to or cannot place, and never under a rule's tier.
6. **The tick never waits long.**
   - The model's calls on a tick share one budget, *engineer's call* **30 s**. On the measured CPU that is about five
     mails, and a normal tick has fewer.
   - When the budget runs out, the remaining mails take the ladder's verdict and no label, and the summary counts them.
   - `PageReserve` holds the budget. Spec decision 9's lesson (2026-11-03 review): a page starts only with room for the
     PIM's read, the shadow, the model and the commit.
   - The alert waits at most the budget. The poll interval is already 5 minutes.
7. **Learning from corrections.**
   - A label correction is written as today (`pim_watch_corrections.label_id`). Two corrections outside the 15-minute
     window already make a sender rule (watch spec §5.4), and a rule outranks the model (decision 1).
   - **No sender hint.** The draft put the label of the sender's last corrected mail into the model's state. G1 measured
     it at the shipping threshold: a right hint changed nothing, and a wrong one, which a sender who writes about
     different things gives, lowered the labels' accuracy (see Measured). It is dropped, as the draft said it would be.
   - *Engineer's call:* no example bank, kNN or prompt examples for labels. Eight examples hurt the 600M model, and rules
     already learn from the second correction.
8. **The route: one knob, read live.**
   - Two routes, the ones the operator named: the local sidecar and OpenRouter. *Engineer's call:* no manual endpoint.
     The two request shapes differ (decision 10), and nobody asked for a third.
   - One knob, like `config.ResolveEmbedRoute`. An empty model means the local sidecar at `AURA_DECIDE_BASE_URL`, with
     no key. A model id in `AURA_DECIDE_MODEL` means OpenRouter.
   - **Read live.** The watch reads the route on every tick, and a change in the cockpit applies with no restart. A
     decision model leaves nothing stored that a change would invalidate. The embeddings do leave something, so
     `EmbeddingBackendControl` restarts the daemon. This control copies its look, not its restart.
   - The cockpit lists OpenRouter's decision models through `llm.FetchOutputModalityCatalog` with `decisions`.
   - The local route is the default wherever its sidecar runs. Without a route, the model step is off and the watch
     behaves as today.
   - The 0.5 threshold applies to every model. Probabilities differ from model to model, so G4's baseline is counted per
     route and model.
9. **A cloud route reads an account's mail only with that account's consent.** Watch spec §4 says "a cloud one is an
   opt-in per account".
   - Each account has a switch, off by default: «Lascia leggere questo account al modello su OpenRouter».
   - When the switch is off and the route is OpenRouter, that account's mail gets no model call, and the watch behaves
     as today.
   - On the local route the switch does not apply.
10. **One helper, not a mail feature.** The decision call lives in a domain-free package, beside `internal/examplebank`.
    It takes a state, a question and options, and answers a choice with its probabilities. Document labels can use it
    later, and nothing in it knows mail. *Engineer's call:* it speaks llama.cpp's `/v1/systemone` and OpenRouter's
    decisions API, the two request shapes the spikes measured (`spikes/d1-email-labeling/benchmark.py`,
    `spikes/d1-document-labels/openrouter_shim.py`). The builder reads both APIs' documentation first.
11. **The record.** The verdict row keeps what the model said:
    - the label it chose, or none;
    - its probability;
    - whether it decided (`decided_by` gains `model`);
    - whether the mail was discarded.

    The centroid's shadow still compares against the rules' own answer (`ladder_tier`). The model's answer is a separate
    column, so the shadow report can score both later.
12. **The default labels arrive described, in Italian and English.** The operator, 2026-11-03: "la a ma con le
    traduzioni inglese e italiano".
    - Each default label in `DefaultLabels` (`internal/pimwatch/text.go`) carries a description in the identity's
      language, beside its name, and the person can change it.
    - The texts are G1's (`spikes/d1-email-labeling/labels5/g1.py`, `ITALIAN` and `ENGLISH`), because those are the
      ones measured.
    - A label the person adds starts with no description, so the model leaves it out until they write one (decision 3).
    - **Identities that already have their labels** (the lab VM's among them) get the default description once. That
      applies to a label whose name still equals a default name and whose description is empty.
    - A person who later clears a description does not get it back, just as `labels_seeded` keeps a removed label
      removed.

## What this changes in the watch spec

The builder records these in `2026-10-31-mail-calendar-watch-design.md` and prd.md when the code lands.
- **§5.5, shadow.** The model's label is live from the first day, with no shadow week. That is the operator's decision
  ("si va bene poi l'utente decide"). As with Laya's exception, live counts are the gate (G4 and the baseline). The
  mistake §5.5 kept the local head in shadow for was filing a person's mail out of the digest. Decision 1's floor
  forbids it.
- **§5.7.** "The people floor still holds" stays true, by decision 1.
- **§5.8, "Labels need examples".** This becomes "labels need the person's description". A label without one is not
  offered to the model (see Measured), and no label carries seeds from a corpus.
- **§8, step 5.** Its local model is this one, for labels. Laya's detection of `now` stays as that step describes it.

## What is out

- Moving, labelling or deleting anything in the mailbox. Aura's label stays in Aura (watch spec §4).
- Drafts, replies, summaries and starting an agent run. That is the parked agent-activation idea, after the PII work.
- Changing a tier by the model other than through a label's tier.
- An example bank for labels.
- A CLM-shaped model. See Measured: without trained heads it is too weak, and the trained ones are English only.
- A GPU image for AMD boxes. llama.cpp's Vulkan and ROCm builds are not measured in Aura.
- PII tokenization before a cloud model. That is the next spec, with `hoophq/alcatraz` and
  `Rizzo-AI-Academy/rizzo-pii`. Until then, decision 9's switch is the only guard on a cloud route.

## Gates, before and after

- **G1, done on 2026-11-03** (see Measured). What ships:
  - the person's own descriptions, in their language;
  - only the labels that have one;
  - no sender hint;
  - a threshold of 0.5.
- **G2, done on the development PC** (see Measured), with the 2026-11-02 question, not G1's. On the appliance mini PC
  it is still owed, if the operator makes the mini PC reachable.
- **G3, done on 2026-11-03** (see Measured). `liquid/d1.1` holds the 0.5 threshold. At 0.5 it labels 96 to 97% of the
  labelable mail, against 73 to 77% for the local d1-3B, with the same share of right labels.
- **G4, live, on the lab VM.** Two weeks with the model on:
  - how many model labels the person corrects;
  - how many alerts the model started, and how many of those were corrected;
  - how many mails were discarded, and how many the person rescued.

  Each count is set against the same two weeks' mail that only the rules decided.

## Baseline

BASELINE OR DROP: the stupid baseline is no label, which is today's «Da smistare». In G4, on the mail the rules left
without a label, the model must reach both numbers below. Otherwise it is switched off by default and the cockpit says
why.
- **At least 90% of its labels left standing.**
- **At least 60% of the labelled mail labelled by the model.** That is its labels left standing, divided by those plus
  the labels the person adds by hand to mail the model left without one.

Both numbers are *engineer's calls*, written before G4. G1 measured 97 to 100% and 73 to 77% on synthetic mail, and that
does not replace them.

## What this does not prove

- That synthetic mail stands for a person's real inbox. The corpus has one author and balanced classes.
- That a real person's descriptions read as well as G1's. Those were written by the engineer, in a person's style.
- English default labels as they ship. G1's English arm had English descriptions under the Italian label names, and
  Italian mail.
- The speed on any appliance but the one measured, or with G1's question on a CPU.
- What `liquid/d1.1` is. OpenRouter does not say its size, and it is not the local d1-3B, so the two routes do not
  label alike.
- That the threshold holds on real mail. The held-out sets show it was not fitted to the 200 mails, but they come from
  the same author.
- Real snippets. No G1 body was longer than 255 characters, while the fork's snippet runs to 400 and real mail carries
  greetings, signatures and quoted replies that G1's bodies do not.
- That a label left standing is right. The person may not have looked.

## Left to the builder

- Whether the existing embedding sidecar can host the decision model too (llama.cpp's router mode at the pinned build),
  or a second sidecar is needed. Measure the appliance's memory either way.
- Decoding the fork's `snippet` in `Change`. It is already in the contract fixture, `testdata/pim_contract.json`.
- The migration: label `description` and `discard`, the verdict's model columns, and `model` in `decided_by`'s check.
- Where the per-account switch of decision 9 lives. Today no watch table is per account.
- Which stored key the OpenRouter route uses.
- The cockpit:
  - the labels panel's two new fields;
  - the decisions view's reason ("L'ha deciso il modello") and its discarded mark;
  - the per-account switch;
  - the picker.
- The installer and payload manifest for a pinned d1 GGUF, with size and SHA-256, like the embedding model.
