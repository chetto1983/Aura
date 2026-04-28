# PRD: Aura — Personal AI Agent with Compounding Memory

## Introduction

Aura is a local-first personal AI agent that accumulates knowledge over time through a structured wiki maintained by the model. Unlike traditional chatbots, Aura is built as a deterministic, observable, and secure system with strong emphasis on runtime reliability, cost control, execution safety, and incremental memory evolution.

This PRD covers all four phases of Aura's development: core bot + wiki, deterministic memory, skill sandbox, and optional multi-agent capabilities. Version 3.0 simplifies the architecture from earlier versions, eliminating premature complexity and prioritizing determinism and testability.

## Goals

- Provide a reliable personal AI agent accessible via Telegram
- Accumulate and retrieve knowledge through a structured wiki that compounds over time
- Ensure deterministic behavior (temperature=0, versioned prompts and schemas)
- Maintain strict cost control with token tracking and budget enforcement
- Enable safe execution of skills with trust levels and resource constraints
- Keep the system predictable, controllable, and debuggable at all times

## User Stories

### US-001: Start a conversation via Telegram
**Description:** As a user, I want to send a message to Aura on Telegram so that I can interact with my personal AI agent.

**Acceptance Criteria:**
- [ ] Telegram bot receives messages from allowlisted users
- [ ] Bot ignores messages from non-allowlisted users
- [ ] Each conversation runs in its own goroutine
- [ ] Bot sends a confirmation when a conversation starts
- [ ] Typecheck/lint passes

### US-002: Exchange messages with the LLM
**Description:** As a user, I want to send messages and receive AI responses so that I can get answers and assistance.

**Acceptance Criteria:**
- [ ] Messages are forwarded to the LLM client and responses returned to the user
- [ ] Streaming responses are supported via token channel
- [ ] Retry with exponential backoff on LLM failure (max 5 retries)
- [ ] Context window managed with rolling summarization at 80% threshold
- [ ] Typecheck/lint passes

### US-003: Store and retrieve wiki knowledge
**Description:** As a user, I want Aura to remember information across conversations so that knowledge compounds over time.

**Acceptance Criteria:**
- [ ] LLM output is parsed as YAML and validated against SCHEMA.md before writing
- [ ] Invalid wiki writes trigger a retry with schema error feedback
- [ ] Wiki files are stored under /wiki with atomic write (temp + rename)
- [ ] File-level mutex prevents concurrent write corruption
- [ ] Wiki changes are tracked via Git
- [ ] Typecheck/lint passes

### US-004: Search wiki with vector search
**Description:** As a user, I want Aura to search my wiki semantically so that relevant knowledge is surfaced in conversations.

**Acceptance Criteria:**
- [ ] Vector search returns relevant wiki pages based on query
- [ ] chromem-go is the primary search engine
- [ ] pgvector serves as scalable fallback when configured
- [ ] Search results are injected into conversation context
- [ ] Typecheck/lint passes

### US-005: Manage conversation context
**Description:** As the system, I need to manage conversation context so that token limits are respected and costs stay within budget.

**Acceptance Criteria:**
- [ ] Active context, rolling summary, and transcript are maintained per conversation
- [ ] MAX_CONTEXT_TOKENS limit is enforced
- [ ] Summarization triggers at 80% of context window
- [ ] Token usage tracked per conversation and globally
- [ ] Soft and hard budget limits are enforced
- [ ] Typecheck/lint passes

### US-006: Predict and control costs
**Description:** As a user, I want cost controls so that I don't exceed my budget on LLM calls.

**Acceptance Criteria:**
- [ ] Cost prediction calculated as context + expected_output before each call
- [ ] Soft budget triggers a warning notification
- [ ] Hard budget halts LLM calls and notifies user
- [ ] Token usage visible via /status endpoint
- [ ] Typecheck/lint passes

### US-007: Execute skills in a sandbox
**Description:** As a user, I want Aura to execute skills/tools safely so that untrusted code cannot compromise my system.

**Acceptance Criteria:**
- [ ] Skills are classified by trust level: local, verified, untrusted
- [ ] Untrusted skills run with no network access by default
- [ ] All skills have timeout, CPU limit, and memory limit constraints
- [ ] Skill execution uses os/exec (containerd optional for advanced isolation)
- [ ] Typecheck/lint passes

### US-008: Validate wiki schema deterministically
**Description:** As the system, I want deterministic wiki writes so that memory evolution is predictable and debuggable.

**Acceptance Criteria:**
- [ ] LLM runs with temperature=0 for wiki operations
- [ ] Prompt versioning tracks which prompt generated each wiki page
- [ ] Schema versioning tracks wiki page format versions
- [ ] Each wiki page includes metadata: schema_version and prompt_version
- [ ] Snapshot tests validate deterministic output for fixed inputs
- [ ] Typecheck/lint passes

### US-009: Fall back to alternative LLM providers
**Description:** As a user, I want Aura to work even if my primary LLM provider is unavailable.

**Acceptance Criteria:**
- [ ] MCP Client is the primary LLM interface
- [ ] OpenAI-compatible HTTP client serves as fallback
- [ ] Ollama client enables offline mode
- [ ] Failover between providers is automatic and transparent
- [ ] Typecheck/lint passes

### US-010: Monitor system health and observability
**Description:** As a user, I want to monitor Aura's health and resource usage so that I can debug issues.

**Acceptance Criteria:**
- [ ] /status endpoint returns system health
- [ ] Structured logs via zap
- [ ] OpenTelemetry traces for LLM calls and tool executions
- [ ] No secrets appear in logs
- [ ] Typecheck/lint passes

## Functional Requirements

- FR-1: Telegram bot interface using telebot.v4 with allowlist-based access control
- FR-2: Orchestrator manages conversations sequentially (1 goroutine = 1 conversation, no DAG engine)
- FR-3: LLM client layer with interface supporting Send and Stream operations
- FR-4: Three LLM implementations: MCP Client (primary), OpenAI-compatible HTTP (fallback), Ollama (offline)
- FR-5: Wiki system with file system storage, Git versioning, atomic writes, and file-level mutex
- FR-6: Wiki schema validation: LLM output → Parse YAML → Validate → Write or Retry
- FR-7: Deterministic mode for wiki operations: temperature=0, prompt versioning, schema versioning
- FR-8: Vector search via chromem-go (primary) with pgvector fallback
- FR-9: PostgreSQL database using pgx driver with golang-migrate for migrations
- FR-10: Conversation context management with active context, rolling summary, and transcript
- FR-11: Context window enforcement: MAX_CONTEXT_TOKENS with 80% summarization threshold
- FR-12: Token tracking per conversation and globally with cost prediction before each call
- FR-13: Budget enforcement with soft (warning) and hard (halt) levels
- FR-14: Skill system with three trust levels: local, verified, untrusted
- FR-15: Skill execution constraints: no network (default), timeout, CPU limit, memory limit
- FR-16: HTTP server using chi router for /status and observability endpoints
- FR-17: Structured logging via zap with no secrets in log output
- FR-18: OpenTelemetry integration for distributed tracing
- FR-19: Configuration via envconfig
- FR-20: Docker deployment with multi-stage Alpine build, Linux-only target

## Non-Goals

- No distributed orchestration or DAG engine
- No automatic priority assignment or notifications for wiki pages
- No multi-agent coordination (Phase 4 is optional and out of scope for initial delivery)
- No mobile or web UI — Telegram is the sole interface
- No real-time collaboration features
- No cloud-hosted wiki — local-first, file system only
- No custom LLM training or fine-tuning

## Design Considerations

- **Telegram-first interface:** All user interaction flows through Telegram using telebot.v4 with inline keyboard support
- **Local-first architecture:** Wiki stored on file system with Git versioning; no cloud dependency for core functionality
- **Deterministic memory:** Wiki writes use temperature=0 with versioned prompts and schemas to ensure reproducibility
- **Graceful degradation:** Multiple LLM providers allow fallback; offline mode via Ollama
- **Security by default:** Skills run with no network access unless explicitly trusted; Telegram allowlist gates access

## Technical Considerations

- **Language:** Go — chosen for concurrency model (goroutines per conversation) and single-binary deployment
- **No framework dependencies for orchestration:** Custom orchestrator eliminates vendor lock-in and complexity
- **Database:** PostgreSQL with pgx driver; migrations via golang-migrate
- **Vector search:** chromem-go for local-first; pgvector for scalable deployment
- **Wiki storage:** File system + Git (go-git/go-git/v5) with atomic writes (temp + rename pattern)
- **Schema validation:** go-playground/validator + gopkg.in/yaml.v3
- **Containerization:** Multi-stage Docker build targeting Alpine Linux
- **Retry strategy:** Exponential backoff, max 5 retries for LLM calls
- **Config:** envconfig for environment-based configuration

## Success Metrics

- Wiki writes succeed with valid schema on >99% of attempts (schema validation + retry)
- LLM response latency under 5 seconds for 95th percentile
- Zero data loss on wiki writes (atomic write + Git versioning)
- Cost stays within hard budget limit 100% of the time
- Deterministic wiki outputs produce identical results for identical inputs (snapshot tests pass)
- Single Docker container deploys and starts serving in under 10 seconds

## Open Questions

- Should the wiki support concurrent read access from multiple conversations?
- What is the expected wiki page volume and how does it affect vector search performance?
- How should the skill trust level be assigned — manual configuration or automated verification?
- What metrics should trigger automatic failover between LLM providers?
- Should Phase 4 (multi-agent) use a message-passing model or shared state?

## Roadmap

### Phase 1: Core Bot + Wiki
- Telegram bot with allowlist
- Orchestrator with sequential conversation loop
- LLM client layer (MCP + fallback)
- Wiki system with file storage, Git versioning, atomic writes
- Basic vector search (chromem-go)

### Phase 2: Deterministic Memory
- Schema validation layer
- Deterministic mode (temperature=0, prompt/schema versioning)
- Context management with rolling summarization
- Cost prediction and budget enforcement
- Snapshot testing

### Phase 3: Skill Sandbox
- Trust level classification
- Execution constraints (network, CPU, memory, timeout)
- os/exec-based sandbox
- Optional containerd isolation

### Phase 4: Multi-Agent (Optional)
- Agent-to-agent communication protocol
- Shared workspace isolation
- Coordinated task execution

---

**End of PRD v3.0 — Restructured**