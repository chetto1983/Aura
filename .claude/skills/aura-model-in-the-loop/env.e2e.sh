# Source this before every `aura` command of a model-in-the-loop run:
#   HAND=<scratch dir> source .claude/skills/aura-model-in-the-loop/env.e2e.sh
# Every value is a CI dummy (ci.yml uses the same passwords); nothing here is a real secret.
: "${HAND:?set HAND to the scratch directory of the run}"
export POSTGRES_PASSWORD=ci-test-password
export AURA_DB_URL="postgres://aura_app:ci-test-password@127.0.0.1:5432/aura_e2e?sslmode=disable"
export AURA_DB_MIGRATE_URL="postgres://aura_migrate:ci-test-password@127.0.0.1:5432/aura_e2e?sslmode=disable"
# Strict on purpose: dev and local_trusted turn the gateway into a no-op, so nothing headless is measured.
export AURA_PROFILE=single_user_hardened
export AURA_AUTHULA_SECRET=00000000000000000000000000000000000000000000000000000000000000a1
export AURA_RUN_DIR=/tmp/aura-e2e-run AURA_SKILL_EXPORT_DIR=/tmp/aura-e2e-skills
# The model is you: the hand endpoint parks every completion until you write its answer.
export AURA_LLM_PROVIDER=llamacpp AURA_LLM_MODEL=hand-model AURA_LLM_BASE_URL=http://127.0.0.1:18484/v1
export AURA_LLM_STREAM_IDLE_TIMEOUT_SEC=3600 AURA_LLM_TOTAL_TIMEOUT_SEC=3600 AURA_AGENT_JOB_MAX_DURATION_SEC=7200
export AURA_TIMEZONE=Europe/Rome
export AURA_EMBED_BASE_URL=http://127.0.0.1:8081 AURA_EMBED_DIMENSIONS=768
export AURA_EMBED_REVISION=0f741b5a6585bd53aeb15cd1372c56f2a0f65e12
export AURA_EMBED_FINGERPRINT=b5ce9d77a3fc4b3b39ccb5643c36777911cc4eb46a66962eadfa3f5f60490d63
export AURA_EMBED_MODEL_DIR=/tmp/aura-ci-cache/llama.cpp/llama.cpp
export AURA_OBJECTSTORE_BACKEND=filesystem-dev
export AURA_OBJECTSTORE_ACCESS_KEY=GKe2e0000000000000000000ab
export AURA_OBJECTSTORE_SECRET_KEY=e2e-not-a-sample-secret-0000000000000000000000000000000000000000
export GARAGE_RPC_SECRET=e2e00000000000000000000000000000000000000000000000000000000000d4
export AURA_ARCADEDB_TENANT_SECRET=0000000000000000000000000000000000000000000000000000000000000d1
export ARCADEDB_URL=http://127.0.0.1:2480 ARCADEDB_PASSWORD=ci-test-password ARCADEDB_APP_PASSWORD=ci-test-password
# Without the admin pair the daemon refuses to boot ("bootstrap service not configured").
export ARCADEDB_ADMIN_USER=root ARCADEDB_ADMIN_PASSWORD=ci-test-password
export AURA_PIM_MCP_URL=http://127.0.0.1:8093 AURA_PIM_MCP_PORT=8093

# The memory sidecar (cmd/arcadedb-mcp), run natively: these are read only by that process.
export ARCADEDB_DATABASE=aura_memory ARCADEDB_USER=aura_memory
export AURA_ARCADEDB_MCP_HOST=127.0.0.1 AURA_ARCADEDB_MCP_PORT=8096
export MCP_OAUTH_ISSUER=http://127.0.0.1:9080 MCP_OAUTH_JWKS_URL=http://127.0.0.1:9080/oauth/jwks
export MCP_OAUTH_RESOURCE=http://127.0.0.1:8096/mcp,http://127.0.0.1:8096/mcp/ MCP_OAUTH_SCOPE=mcp:tools
