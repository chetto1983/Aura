#!/usr/bin/env bash
# Aura RAG answer-quality eval (RAGAS) runner. Provisions an isolated Python 3.12 venv via
# uv (the system Python is 3.14 -> no ragas wheels) with the PINNED langchain 0.3.x stack
# (langchain 1.x breaks ragas 0.2.15's langchain_community.chat_models.vertexai import).
# Judge = OpenRouter (paid, bounded ~35 calls); embeddings = the free local granite sidecar.
#
# Usage (WSL, stack up):
#   scripts/eval/ragas/run.sh            # live scoring (paid) — the gate
#   scripts/eval/ragas/run.sh --dry-run  # validate dataset + wiring, no model calls (free)
set -uo pipefail
set +H
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
[ -f "$HOME/aura.env" ] && { set -a; source "$HOME/aura.env"; set +a; }

VENV="$HOME/.aura-ragas-eval/.venv"
if [ ! -d "$VENV" ]; then
  mkdir -p "$HOME/.aura-ragas-eval"
  uv venv --python 3.12 "$VENV"
  # shellcheck disable=SC1091
  source "$VENV/bin/activate"
  uv pip install 'ragas==0.2.15' 'langchain>=0.3,<0.4' 'langchain-core>=0.3,<0.4' \
    'langchain-community>=0.3,<0.4' 'langchain-openai>=0.2,<0.3'
else
  # shellcheck disable=SC1091
  source "$VENV/bin/activate"
fi

export AURA_EMBED_BASE_URL="${AURA_EMBED_BASE_URL:-http://127.0.0.1:8081}"
HERE="$(cd "$(dirname "$0")" && pwd)"
python "$HERE/rag_answer_eval.py" "$@"
echo "RUN_EXIT=$?"
