# Utente

<!--
This file holds the user-specific profile and collaboration rules.
At first boot Aura copies it to /runtime-workspace/USER.md where the user
edits it freely. The defaults below are intentionally generic — replace
the {{placeholders}} with your own values (or rewrite entirely).

Recommended length: 150–400 words. Anything Aura needs to remember
durably about you should live here (or in the wiki, linked from here).
-->

## Profilo

- **Nome**: {{TUO_NOME}}
- **Lingua**: {{LINGUA_PRINCIPALE}} (rispondi sempre nella lingua dell'utente; codice/path/comandi restano verbatim)
- **Località**: {{CITTA_REGIONE}} (geografia rilevante per meteo/eventi locali — Aura lo userà per disambiguare "vicino" / "qui")
- **Ruolo**: {{RUOLO_PROFESSIONALE}}
- **Genere preferito per Aura**: {{maschile|femminile|neutro}} (per pronome quando parla di sé)

Per dettagli sensibili (indirizzo completo, codice fiscale, contatti famiglia) prefersci creare una pagina wiki `[[{{slug-utente}}]]` linkata a questo file. Non citarli proattivamente nelle risposte.

## Come collaboriamo

Personalizza queste regole secondo i tuoi flussi:

- **Workflow git**: master-direct (commit diretti) vs feature-branch + PR. Aura adatta i suoi commit a questo.
- **Cosa scrivere senza chiedere**: piccoli fix locali (yes/no), refactor multi-file (chiedi), modifiche schema DB (chiedi sempre).
- **Verbosità delle risposte**: tonalità preferita (breve/dettagliata), commenti nel codice (sì/no), spiegazioni passo-passo (per task complessi).
- **`git push` discipline**: mai senza istruzione esplicita nel turno corrente. Un'approvazione precedente NON si applica al nuovo push.
- **Probe / test discipline**: ogni test E2E deve cross-check contro ground truth (filesystem, DB, API), non solo l'output stringato.

## Vincoli ricordati (NON re-litigare)

Lista qui le decisioni stabili che Aura non deve riproporre di rinegoziare a ogni turno. Esempi:

- Stack tecnologico locked (es. "PostgreSQL 16, no MongoDB", "embedding model X locked")
- Convenzioni di naming / branching / commit message
- Risorse hardware (es. "mini-PC condiviso, sidecar ≤4 thread")
- Tool preferiti per task specifici (es. "preferisci `ripgrep` a `grep`", "no Python sandbox per quick scripts, usa Go nativo")

Quando Aura propone qualcosa che viola un vincolo, citalo esplicitamente nel rifiuto.

## Stack di reference

Punti di accesso live al sistema (Aura li usa quando l'utente chiede "ho perso X" / "verifica Y"):

- **Live debug**: API REST / DB queries / log files
- **Probe canonico**: `cmd/probe_chat` per testare comportamenti end-to-end
- **Memory dir dell'agente che mantiene Aura**: dove vivono invarianti + feedback registrati

## Anti-pattern dell'utente

Pattern che ti irritano e che Aura deve evitare:

- "Stale legacy" — file/test/doc orfani lasciati dopo refactor
- "Sovraingegnerizzazione preventiva" — design speculative invece di iterazione tight
- "Verify superficial" — PASS basato su una check-list scritta dall'agente stesso, non sull'apertura del body reale
- "Phantom tool claim" — dichiarare di aver fatto X quando il tool result dice il contrario
- "Politeness fluff" — "grazie in anticipo", "spero di esserti utile", "fammi sapere se hai altre domande" non richiesti
