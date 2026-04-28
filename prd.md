# Aura — Personal AI Agent with Compounding Memory

**Product Requirements Document — Version 3.0 (Production-Ready)**
**Date:** 2026-04-28

---

# 1. Executive Summary

Aura è un agente AI personale, local-first, progettato per accumulare conoscenza nel tempo attraverso una wiki strutturata mantenuta dal modello. A differenza dei chatbot tradizionali, Aura è costruito come un sistema deterministico, osservabile e sicuro, con forte enfasi su:

* Affidabilità runtime
* Controllo dei costi
* Sicurezza delle esecuzioni
* Evoluzione incrementale della memoria

Questa versione (v3.0) introduce un'architettura semplificata ma robusta, eliminando complessità premature e privilegiando determinismo e testabilità.

---

# 2. Design Principles

1. **Determinismo > Creatività**
2. **Semplicità > Astrazione prematura**
3. **File system > sistemi distribuiti**
4. **Controllo esplicito > automazione opaca**
5. **Fallback sempre disponibili**

---

# 3. System Architecture (Simplified)

## 3.1 Core Model

```
User → Telegram → Orchestrator → LLM → Tools → Wiki
```

## 3.2 Execution Model

* 1 goroutine = 1 conversation
* Nessun DAG engine
* Nessuna orchestrazione distribuita
* Loop sequenziale deterministico

---

# 4. Core Components

## 4.1 Orchestrator

Responsabilità:

* Gestione conversazioni
* Loop LLM
* Retry semplice
* Budget enforcement

### Scelta package

* **Nessun framework orchestrator esterno**
* Implementazione custom

Motivo:

* Riduzione complessità
* Eliminazione rischio dependency

---

## 4.2 LLM Client Layer

### Interfaccia

```go
type LLMClient interface {
    Send(ctx context.Context, req Request) (Response, error)
    Stream(ctx context.Context, req Request) (<-chan Token, error)
}
```

### Implementazioni

* MCP Client (primary)
* OpenAI-compatible HTTP client (fallback)
* Ollama client (offline mode)

### Package consigliati

* net/http (native)
* encoding/json

Motivo: zero vendor lock-in

---

## 4.3 Wiki System

### Storage

* File system + Git

### Structure

```
/wiki
  /raw
  /wiki
  SCHEMA.md
```

### Package

* go-git (github.com/go-git/go-git/v5)

### Write Safety

* File-level mutex
* Atomic write (temp + rename)

---

## 4.4 Schema Validation Layer (NEW)

### Package

* go-playground/validator
* gopkg.in/yaml.v3

### Flow

```
LLM output → Parse YAML → Validate → Write or Retry
```

---

## 4.5 Vector Search

### Primary

* chromem-go

### Scalable fallback

* pgvector (PostgreSQL)

---

## 4.6 Database

### Primary

* PostgreSQL

### Driver

* pgx (github.com/jackc/pgx/v5)

### Migrations

* golang-migrate

---

## 4.7 Telegram Interface

### Package

* telebot.v4

### Motivazione

* API stabile
* Middleware support
* Inline keyboard

---

## 4.8 HTTP Server

### Package

* chi router

---

## 4.9 Logging

### Package

* zap

---

## 4.10 Config

### Package

* envconfig

---

## 4.11 Observability

### Package

* OpenTelemetry

---

# 5. Memory Model

## 5.1 Layers

* Raw
* Wiki
* Schema

## 5.2 Deterministic Mode (MANDATORY)

* temperature = 0
* prompt versioning
* schema versioning

### Page metadata

```yaml
schema_version: 1
prompt_version: ingest_v1
```

---

# 6. Conversation Model

```
conversation/
  active_context
  rolling_summary
  transcript
```

---

# 7. Context Management

* MAX_CONTEXT_TOKENS
* Summarization threshold: 80%

---

# 8. Skill System (Hardened)

## 8.1 Security Model

### Trust Levels

* local
* verified
* untrusted

## 8.2 Execution Constraints

* No network (default)
* Timeout
* CPU limit
* Memory limit

## 8.3 Package

* os/exec
* containerd (optional advanced)

---

# 9. Cost Control

## 9.1 Token Tracking

* Per conversation
* Global

## 9.2 Cost Prediction (NEW)

```
estimated = context + expected_output
```

## 9.3 Budget Levels

* Soft
* Hard

---

# 10. Retry Model

* Exponential backoff
* Max 5 retries

---

# 11. Persistence

## 11.1 Conversations

* JSON or PostgreSQL

## 11.2 Wiki

* File system + Git

---

# 12. Testing Strategy

## 12.1 Unit

* validator
* workspace safety

## 12.2 Integration

* DB
* wiki

## 12.3 Snapshot Testing (CRITICAL)

* deterministic outputs

---

# 13. Deployment

## 13.1 Docker

* Multi-stage build
* Alpine

## 13.2 Target

* Linux only

---

# 14. Security

* Telegram allowlist
* Workspace isolation
* No secrets in logs

---

# 15. Observability

* /status
* structured logs

---

# 16. Roadmap

## Phase 1

* Core bot
* Wiki base

## Phase 2

* Deterministic memory

## Phase 3

* Skill sandbox

## Phase 4

* Multi-agent (optional)

---

# 17. Final Notes

Aura non deve essere un sistema complesso.

Deve essere:

* prevedibile
* controllabile
* debuggabile

Tutto il resto è secondario.

---

**End of PRD v3.0**
