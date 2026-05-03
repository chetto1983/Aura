# Nanobot Swarm — Design

Data: 2026-05-03

## Obiettivo

Parallelizzare le operazioni wiki (ingest, ricerca, scrittura, lint) tramite nanobot autonomi che si coordinano in uno swarm, con ricorsione libera e massima autonomia.

## Riferimenti

- Claude Code agent teams: `https://code.claude.com/docs/it/agent-teams`
- Picobot spawn: `d:\tmp\picobot\internal\agent\tools\spawn.go`
- llm-wiki pattern: `docs/llm-wiki.md`

## Architettura

```
swarm {id}
  ├─ coordinatore (loop principale o nanobot padre)
  │   └─ spawn_nanobot tool call
  ├─ nanobot-1 (goroutine)
  │   ├─ conversation.Context isolato
  │   ├─ mini tool-loop (max 5 iterazioni, timeout 60s)
  │   ├─ tutti i tool del coordinatore (allowlist opzionale)
  │   ├─ puo' spawnare nanobot figli (ricorsione libera)
  │   └─ restituisce risultato via channel
  ├─ nanobot-2 (goroutine, parallelo a nanobot-1)
  └─ nanobot-N ...
```

Ogni nanobot ha piena liberta': scrivere/eseguire codice nella sandbox, invocare MCP server, creare skill al volo, leggere/scrivere file. Nessuna limitazione predefinita.

## Contenimento

- Contatore atomico globale `activeNanobots int32`
- Limite hard configurabile (`NANOBOT_MAX_ACTIVE`, default 20)
- Timeout per nanobot (`NANOBOT_TIMEOUT_SEC`, default 60s)
- Recover da panic nella goroutine

## Task list condivisa

- Directory: `~/.aura/swarms/{swarm-id}/tasks/`
- File JSON per task: `{id, subject, status, owner, blockedBy}`
- File locking (flock) per prevenire race condition sui claim
- Dipendenze: un task `blockedBy: ["1"]` non e' claimable finche' il task 1 e' `completed`

## Messaggistica

- Tool `send_message`: invia messaggio diretto a un nanobot per nome
- Risultati risalgono la catena via channel Go
- Il parent riceve notifica quando un figlio completa

## Tool principali

### spawn_nanobot
```
name:    string   — nome univoco
task:    string   — prompt completo
tools:   []string — allowlist (default: tutti i tool)
```

### send_message
```
to:      string   — nome nanobot destinatario
message: string   — corpo messaggio
```

## Loop condiviso

`internal/nanobot/nanobot.go` estrae il loop da `runToolCallingLoop`, usato sia dal loop principale (max 10 iterazioni) che dai nanobot (max 5 iterazioni).

## Configurazione

```env
NANOBOT_MAX_ACTIVE=20
NANOBOT_TIMEOUT_SEC=60
NANOBOT_MAX_ITERATIONS=5
```

## File system

```
~/.aura/
  swarms/
    {swarm-id}/
      tasks/       — task list JSON
      sandbox/     — working directory nanobot
      output/      — risultati completati
```

## Rollout

| Slice | Cosa | Effetto |
|-------|------|---------|
| 1 | Estrai `nanobot.Loop` condiviso | Refactor, nessun comportamento nuovo |
| 2 | `spawn_nanobot` + `SwarmManager` + task list | Nanobot attivabili |
| 3 | `send_message` tool | Comunicazione inter-nanobot |
| 4 | Integrazione `Bot` struct + test E2E | Pronto per uso in produzione |
