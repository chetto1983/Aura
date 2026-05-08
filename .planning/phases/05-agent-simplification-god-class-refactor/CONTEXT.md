# Context Handoff: Agent Simplification And God Class Refactor

## Intento attuale

L'utente vuole semplificare Aura prima di aggiungere nuovi strumenti potenti. La diagnosi condivisa e': l'agente e' confuso per troppi guardrail rituali, profili rigidi, skill preflight, tool nascosti e routing terminale. Il target e' uno stile piu' vicino a Codex/Picobot/Hermes: pochi tool chiari, loop compatto, errori recuperabili, safety reale ai confini dei tool.

Il lavoro richiesto adesso e' salvare il contesto per continuare la refactor senza perdere il filo. Nessuna modifica di codice e' partita; esistono solo documenti di planning in questa fase.

## Vincoli progetto da AGENTS.md

- Workspace: `D:\Aura`.
- Aura e' un assistant Telegram Go con dashboard React embedded.
- Main binary: `./cmd/aura`; smoke utility: `./cmd/debug_llm`, `./cmd/debug_tools`, `./cmd/debug_ingest`.
- Comandi principali: `go fmt ./...`, `go test ./...`, `go build ./...`, `go run ./cmd/aura`; `make all` esegue test e build.
- Preservare sempre modifiche utente nel working tree.
- Non committare `.env`, database, binari o wiki raw data generati.
- Usare `Body` per contenuto wiki; link wiki in forma `[[slug]]`.
- `LLM_API_KEY` e' solo per chat model; web search usa `WEB_SEARCH_PROVIDER`; embeddings usano le variabili Mistral dedicate.
- In runtime Docker-first, `data/aura.db` e' live/container-owned: non mutarlo da host mentre il servizio Compose `aura` gira. Backup solo via snapshot SQLite, non copia raw DB/WAL/SHM live.

## Cosa e' stato auditato

- Log conversazioni e archivio SQLite in sola lettura: trovati loop inutili, ripetizioni di `search_memory`, max-loop fallback, token input enormi, confusione su skill approvate ma non installate.
- `internal/telegram/conversation.go`: god class principale, circa 1565 righe, contiene routing, prompt, loop tool, esecuzione tool, terminal handling, archive, telemetry, fallback e snapshot.
- `internal/orchestration/orchestration.go`: policy god class secondaria con profili, allowlist/denylist, loop policy e prompt profile-specific.
- `internal/orchestration/skill_policy.go` e `capabilities.go`: skill preflight e capability taxonomy troppo rigidi; il preflight required ha causato deadlock reali.
- `internal/orchestration/hooks.go`: hidden-tool fatal duplicato rispetto al filtro tool.
- `internal/api/summaries.go`, `internal/api/types.go`, `internal/tools/skill_proposal.go`, `internal/skills/admin.go`: approval skill ambiguo; approvare una proposal puo' marcarla come approvata senza installare `SKILL.md`.
- `D:\tmp\picobot`: loop semplice con registry tool diretto, workspace-bounded tools e meno policy rituale.
- `D:\tmp\hermes-agent`: guardrail utili concentrati su loop hygiene, dedupe, JSON/tool recovery, delegation bounds e final no-tool summary, non su preflight rituali.

## Piano esistente

Il piano operativo e' in:

`.planning/phases/05-agent-simplification-god-class-refactor/PLAN.md`

Quel piano e' la fonte principale per esecuzione, file target, test e ordine commit.

## Ordine di esecuzione proposto

1. Baseline e branch: controllare working tree, creare `codex/simplify-agent-god-classes`, eseguire test baseline.
2. Rimuovere skill preflight required: nessun tool deve essere bloccato da mancato `read_skill`.
3. Collassare profili in toolset semplici: `default`, `compute`, `document`, `admin`.
4. Estrarre `internal/agentloop`: loop LLM/tool indipendente da Telegram.
5. Rendere tool non disponibili errori recuperabili, non fatal user-facing.
6. Rimuovere terminal swarm routing: swarm resta un tool normale, non una gabbia.
7. Spezzare `conversation.go` in archive/snapshot/format/context piu' loop esterno.
8. Sistemare semantica approval skill: approve installa davvero oppure diventa review esplicita.
9. Solo dopo aggiungere bounded workspace file tools: `list_files`, `read_file`, `search_files`, `write_file`, `apply_patch`.

## Decisioni gia' prese

- Prima si cancella il superfluo; i filesystem/workspace tools arrivano dopo.
- Safety da tenere: auth, secret redaction, admin gates, path containment, deny live DB/WAL/SHM, timeouts, loop budgets e logging compatto.
- Safety da togliere o declassare: skill preflight required, profili-gabbia, hidden-tool fatal, terminal swarm obbligato, fallback generico "mi sono fermato".
- `conversation.go` va ridotto a trasporto Telegram + chiamata loop + archive, non deve piu' contenere tutta l'orchestrazione.
- Hermes e' riferimento per loop hygiene; Picobot e' riferimento per semplicita' del registry/tool loop.

## Primo passo per il prossimo agente

1. Usare `aura-implementation` prima di eseguire il piano.
2. Leggere `PLAN.md` e questo `CONTEXT.md`.
3. Eseguire `git status --short -uall` e preservare ogni modifica utente.
4. Creare o confermare il branch `codex/simplify-agent-god-classes`.
5. Eseguire baseline test prima di toccare codice:

```powershell
go test ./internal/orchestration ./internal/telegram ./internal/api ./internal/settings ./internal/config
go test ./...
```

6. Iniziare da Task 1 del piano: rimuovere il runtime block del required skill preflight.

## Stato modifiche

Nessun codice e' stato modificato. La fase contiene planning docs; questo file e' solo handoff di contesto. Preservare tutte le modifiche utente esistenti.
