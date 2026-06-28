#!/usr/bin/env bash
# Spike 076 live runner (WSL). Provisions an isolated Python 3.12 venv via uv (system
# python is 3.14 = no ragas wheels), installs ragas + langchain-openai, and runs the
# construct-validity probe. Judge = OpenRouter (paid, bounded ~50 calls); embeddings =
# local granite. OPENROUTER_API_KEY comes from ~/aura.env.
set -uo pipefail
set +H
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
# shellcheck disable=SC1090
set -a; source "$HOME/aura.env"; set +a

VENV="$HOME/spike076/.venv"
if [ ! -d "$VENV" ]; then
  mkdir -p "$HOME/spike076"
  uv venv --python 3.12 "$VENV"
  # shellcheck disable=SC1091
  source "$VENV/bin/activate"
  # RAGAS 0.2.15 predates the langchain 1.x split — uv otherwise resolves langchain
  # 1.3 / community 0.4 and ragas dies importing langchain_community.chat_models.vertexai
  # (removed in 0.4). Pin the whole stack to the 0.3.x era ragas 0.2.15 was built against.
  uv pip install 'ragas==0.2.15' 'langchain>=0.3,<0.4' 'langchain-core>=0.3,<0.4' \
    'langchain-community>=0.3,<0.4' 'langchain-openai>=0.2,<0.3'
else
  # shellcheck disable=SC1091
  source "$VENV/bin/activate"
fi

export AURA_EMBED_BASE_URL="${AURA_EMBED_BASE_URL:-http://127.0.0.1:8081}"
python /mnt/d/Aura/.planning/spikes/076-ragas-faithfulness-discriminates/ragas_probe.py
echo "RUN_EXIT=$?"
