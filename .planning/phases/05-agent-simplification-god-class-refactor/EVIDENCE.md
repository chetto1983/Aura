# Evidence Appendix

Appendice di contesto per la semplificazione dell'agente Aura e il refactor delle god class. Fonti: log locali, lettura read-only di `D:\Aura\data\aura.db`, codice Aura, clone locale Picobot in `D:\tmp\picobot`, clone locale Hermes in `D:\tmp\hermes-agent`.

## Sintomi Dai Log E Dal DB Conversazioni

- `D:\Aura\data\aura.db`, tabella `conversations`: 380 righe tra `2026-05-06 11:43:55 +0000 UTC` e `2026-05-08 07:04:46`, lette con SQLite `mode=ro&immutable=1`.
- Richiesta semplice di recall: user turn `272`, contenuto `di costa stavavamo parlando?`; assistant turn `289` finisce con `Mi sono fermato prima di completare una risposta finale affidabile`, dopo `llm_calls=8`, `tool_calls_count=8`, `elapsed_ms=32670`, `tokens_in=83067`.
- Dopo il feedback `migliorati`, assistant turn `296` usa ancora strumenti non centrati sul problema (`llm_calls=2`, `tool_calls_count=2`, `tokens_in=128012`) e ammette di aver scavato troppo nell'archivio.
- Bloat estremo del contesto: assistant turn `232` arriva a `tokens_in=618495`; turn `228` a `586933`; turn `222` a `537279`; turn `218` a `503668`; turn `338` a `429752`.
- Deadlock da profilo/preflight: assistant turn `216` dice che il sistema chiede di leggere una skill per poter scrivere ma non espone il tool giusto; turn `222` conferma che anche `propose_skill_change` e' bloccato dalla stessa guardia.
- Confusione approval skill: assistant turn `338` conclude che le skill approvate sono inesistenti come file e che l'approvazione dashboard non crea `SKILL.md`; il codice conferma che l'approve delle proposte skill non applica install/update/delete locale.
- Duplicazione tool web: `D:\Aura\logs\aura-2026-05-06.log` righe 68-83 mostra `web_search` avviato a coppie ripetute e conversazione conclusa con `elapsed_ms=171422`, `llm_calls=4`, `tool_calls=7`.
- Scheduler fragile: `D:\Aura\logs\aura-2026-05-06.log` riga 43 mostra job fallito per modello stale `e2e-test-1778053565905` non trovato.
- Job ripetitivi: `D:\Aura\logs\aura-2026-05-05.log` righe 15-30 mostra quattro `web_search` paralleli identici e job completato con `llm_calls=4`, `tool_calls=7`, `tokens_total=25836`; righe 45-63 ripetono il pattern con `tool_calls=9`, `tokens_total=68271`.

## Hotspot Del Codice

- `D:\Aura\internal\telegram\conversation.go`: 1474 righe; concentra routing conversazione, prompt, loop LLM/tool, esecuzione tool, terminal swarm, fallback, snapshot, archive e formatting.
- `D:\Aura\internal\telegram\conversation.go:498`: `runToolCallingLoop` contiene il loop principale invece di stare in un package agentico riusabile.
- `D:\Aura\internal\telegram\conversation.go:522`: branch terminale che auto-lancia `run_aurabot_swarm` quando il profilo e' `swarm_research`.
- `D:\Aura\internal\telegram\conversation.go:621`: regola speciale post-swarm che tratta `run_aurabot_swarm` come terminal tool.
- `D:\Aura\internal\telegram\conversation.go:661`: fallback testuale generico `Mi sono fermato...`, invece di una finalizzazione no-tool sui risultati raccolti.
- `D:\Aura\internal\telegram\conversation.go:759`: `executeToolCalls` duplica policy runtime, esecuzione parallela, error handling, preflight e statistiche.
- `D:\Aura\internal\telegram\conversation.go:839`: blocco live su `skillPreflightDecision(...).Required && !Satisfied`.
- `D:\Aura\internal\telegram\conversation.go:1187`: `skillPreflightDecision` parte da `SkillPreflightRequired` se non trova config valida.
- `D:\Aura\internal\telegram\conversation.go:1526`: `userFacingFatalToolResult` maschera errori interni dopo che il loop ha gia' fatalizzato il turno.
- `D:\Aura\internal\orchestration\orchestration.go`: 525 righe; profili, profile cards, allowlist, denied tools, prompt modules e loop policy vivono nello stesso file.
- `D:\Aura\internal\orchestration\orchestration.go:21`: `ProfileSwarmResearch` e' un profilo runtime, non solo una strategia.
- `D:\Aura\internal\orchestration\skill_policy.go`: 208 righe dedicate a required/advisory/off, capability inference e matching skill; e' policy rituale, non safety di confine.
- `D:\Aura\internal\orchestration\skill_policy.go:10`: `SkillPreflightRequired` ancora presente come modalita' valida.
- `D:\Aura\internal\orchestration\skill_policy.go:192`: valori non riconosciuti degradano a `SkillPreflightRequired`, scelta pericolosa.
- `D:\Aura\internal\orchestration\hooks.go:10`: `ErrHiddenTool` rende fatale una chiamata a tool non esposto, invece di restituire errore recuperabile al modello.
- `D:\Aura\internal\api\summaries.go`: l'approvazione applica mutazioni wiki ma non installa proposte skill locali.
- `D:\Aura\internal\api\types.go`: documenta che approve di summary generico rivede la bozza ma non scrive `SKILL.md`; UX e aspettative agente restano ambigue.

## Mappa God Class

- `D:\Aura\internal\telegram\conversation.go`: 1474 righe; target primario, da ridurre a trasporto Telegram + chiamata agent loop.
- `D:\Aura\internal\orchestration\orchestration.go`: 525 righe; target secondario, da ridurre a selezione toolset/prompt hint.
- `D:\Aura\internal\tools\files.go`: 599 righe; grande ma meno urgente, relativo a generazione file.
- `D:\Aura\internal\wiki\memory_hygiene.go`: 706 righe; grande ma non causa primaria della confusione del loop.
- `D:\Aura\web\src\components\SourceInbox.tsx`: 748 righe; frontend god component differibile.
- `D:\Aura\web\src\components\TasksPanel.tsx`: 623 righe; frontend god component differibile.
- `D:\Aura\web\src\components\SettingsPanel.tsx`: 494 righe; grande ma non blocca il refactor agente.

## Confronto Picobot

- `D:\tmp\picobot\internal\agent\loop.go`: 333 righe; agente compatto con registry tool, workspace root e loop diretto.
- `D:\tmp\picobot\internal\agent\loop.go:83`: usa `os.OpenRoot(workspace)` per confinare filesystem invece di profili rituali.
- `D:\tmp\picobot\internal\agent\loop.go:235`: prende `a.tools.Definitions()` direttamente dal registry per il turno.
- `D:\tmp\picobot\internal\agent\tools\registry.go`: 78 righe; `Register`, `Definitions`, `Execute`, senza profile maze.
- `D:\tmp\picobot\internal\agent\context.go`: 103 righe; compone contesto e skills in modo semplice.
- Le safety utili stanno al confine tool/workspace; l'agente non viene bloccato da un preflight mentale.

## Confronto Hermes

- `D:\tmp\hermes-agent\run_agent.py`: grande, ma i guardrail principali sono igiene del loop, non rituali di preuso.
- `D:\tmp\hermes-agent\run_agent.py:5271`: `_cap_delegate_task_calls` limita il numero di subagenti concorrenti.
- `D:\tmp\hermes-agent\run_agent.py:5302`: `_deduplicate_tool_calls` elimina chiamate tool identiche nello stesso turno.
- `D:\tmp\hermes-agent\run_agent.py:13205`: applica cap delegazione e dedupe prima dell'esecuzione tool.
- `D:\tmp\hermes-agent\tools\delegate_tool.py:42`: blocca ai subagenti tool pericolosi/ricorsivi (`delegate_task`, `clarify`, `memory`, `send_message`, `execute_code`), guardrail sensato per isolamento figli.
- `D:\tmp\hermes-agent\tools\delegate_tool.py:1885`: ignora `max_iterations` fornito dal modello e usa config autorevole.
- `D:\tmp\hermes-agent\toolsets.py`: toolset per capacita', non profili conversazionali che nascondono memoria/wiki dopo una route.
- Da copiare: dedupe, budget, recovery tool-call/JSON, limiti subagenti, finalizzazione no-tool. Da non copiare in Aura: complessita' monolitica.

## Guardrail Da Eliminare O Ridurre

- Eliminare `SkillPreflightRequired` dal runtime: `read_skill` puo' essere suggerito, mai richiesto per sbloccare un tool.
- Rimuovere capability future/non presenti dal runtime live: browser/docker/security/release/MCP non devono pesare sul loop quotidiano.
- Collassare profili in pochi toolset: `default`, `compute`, `document`, `admin`.
- Rimuovere `swarm_research` come gabbia read-only; tenere `run_aurabot_swarm` come tool normale.
- Rimuovere hidden-tool fatal: tool non disponibile deve tornare come tool result recuperabile.
- Togliere terminal swarm auto-run e regola "dopo swarm niente altro"; il modello deve poter usare altri tool safe dopo un risultato swarm.
- Rimuovere fallback generico "Mi sono fermato"; sostituire con finalizzazione no-tool basata sugli ultimi risultati compatti.
- Ridurre snapshot/prompt telemetry reinserita nel modello; tenere i log strutturati, non il bloat conversazionale.
- Evitare skill proposal come soluzione comportamentale per ogni errore: prima fix del loop e dei tool, poi skill quando un workflow si ripete davvero.

## Guardrail Da Tenere

- Auth Telegram, allowlist utenti, pending users e bearer token dashboard.
- Secret redaction e divieto di committare `.env`, DB, binari, raw wiki/OCR generati.
- Admin gate per `install_skill`, `delete_skill`, settings mutation, token dashboard e azioni distruttive future.
- Regola Docker-first sul DB: non mutare `D:\Aura\data\aura.db` da host mentre il servizio Compose `aura` e' attivo; backup via snapshot SQLite.
- Path containment per futuri workspace file tools, preferibilmente con root unica e denylist per `.env`, `.git`, `data/aura.db`, WAL/SHM, binari e raw OCR.
- Timeouts, max iteration budget, max tool result chars, dedupe tool-call identiche.
- Isolamento subagenti: figli senza memoria condivisa scrivibile, senza messaggi utente diretti e senza delegazione ricorsiva salvo ruolo esplicito.
- Validazione argomenti tool e errori recuperabili al modello.
- Audit log compatto e strutturato per debug, senza riversare tutto nel contesto LLM.
