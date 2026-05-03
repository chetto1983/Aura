# AuraBot Swarm — Design

Data: 2026-05-03

## Obiettivo

Parallelizzare le operazioni wiki (ingest, ricerca, scrittura, lint) tramite AuraBot autonomi che si coordinano in uno swarm, con ricorsione libera e massima autonomia.

## Riferimenti

- Claude Code agent teams: `https://code.claude.com/docs/it/agent-teams`
- Picobot spawn: `d:\tmp\picobot\internal\agent\tools\spawn.go`
- llm-wiki pattern: `docs/llm-wiki.md`

## Architettura

```
swarm {id}
  ├─ coordinatore (loop principale o AuraBot padre)
  │   └─ spawn_AuraBot tool call
  ├─ AuraBot-1 (goroutine)
  │   ├─ conversation.Context isolato
  │   ├─ mini tool-loop (max 5 iterazioni, timeout 60s)
  │   ├─ tutti i tool del coordinatore (allowlist opzionale)
  │   ├─ puo' spawnare AuraBot figli (ricorsione libera)
  │   └─ restituisce risultato via channel
  ├─ AuraBot-2 (goroutine, parallelo a AuraBot-1)
  └─ AuraBot-N ...
```

Ogni AuraBot ha piena liberta': scrivere/eseguire codice nella sandbox, invocare MCP server, creare skill al volo, leggere/scrivere file. Nessuna limitazione predefinita.

## Contenimento

- Contatore atomico globale `activeAuraBots int32`
- Limite hard configurabile (`AuraBot_MAX_ACTIVE`, default 20)
- Timeout per AuraBot (`AuraBot_TIMEOUT_SEC`, default 60s)
- Recover da panic nella goroutine

## Task list condivisa

- Directory: `~/.aura/swarms/{swarm-id}/tasks/`
- File JSON per task: `{id, subject, status, owner, blockedBy}`
- File locking (flock) per prevenire race condition sui claim
- Dipendenze: un task `blockedBy: ["1"]` non e' claimable finche' il task 1 e' `completed`

## Messaggistica

- Tool `send_message`: invia messaggio diretto a un AuraBot per nome
- Risultati risalgono la catena via channel Go
- Il parent riceve notifica quando un figlio completa

## Tool principali

### spawn_AuraBot
```
name:    string   — nome univoco
task:    string   — prompt completo
tools:   []string — allowlist (default: tutti i tool)
```

### send_message
```
to:      string   — nome AuraBot destinatario
message: string   — corpo messaggio
```

## Loop condiviso

`internal/AuraBot/AuraBot.go` estrae il loop da `runToolCallingLoop`, usato sia dal loop principale (max 10 iterazioni) che dai AuraBot (max 5 iterazioni).

## Configurazione

```env
AuraBot_MAX_ACTIVE=20
AuraBot_TIMEOUT_SEC=60
AuraBot_MAX_ITERATIONS=5
```

## File system

```
~/.aura/
  swarms/
    {swarm-id}/
      tasks/       — task list JSON
      sandbox/     — working directory AuraBot
      output/      — risultati completati
```

## Rollout

| Slice | Cosa | Effetto |
|-------|------|---------|
| 1 | Estrai `AuraBot.Loop` condiviso | Refactor, nessun comportamento nuovo |
| 2 | `spawn_AuraBot` + `SwarmManager` + task list | AuraBot attivabili |
| 3 | `send_message` tool | Comunicazione inter-AuraBot |
| 4 | Integrazione `Bot` struct + test E2E | Pronto per uso in produzione |
