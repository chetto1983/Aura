"""Pipeline configuration: YAML defaults overlaid by AURA_FT_* env vars.

Mirrors Aura's own config precedence (built-in default < file < env). Kept in
one small dataclass so every stage (synth, harvest, build, train, eval) reads
the same paths and ids.
"""

from __future__ import annotations

import os
from dataclasses import dataclass, field, fields
from pathlib import Path
from typing import Any

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent  # the finetune/ directory


def _default_config_path() -> Path:
    return ROOT / "config" / "pipeline.yaml"


@dataclass
class Config:
    # --- paths (relative to finetune/ unless absolute) ---
    tools_json: str = "data/aura_tools.json"
    seeds_path: str = "config/seeds.jsonl"
    raw_dir: str = "data/raw"          # harvested + synthetic traces land here
    dataset_dir: str = "data/dataset"  # train/val/test JSONL
    out_dir: str = "data/out"          # adapters, merged model, gguf, reports

    # --- base model ---
    base_model: str = "unsloth/functiongemma-270m-it"
    max_seq_len: int = 4096

    # --- teacher (synthetic distillation) — OpenAI-compatible endpoint ---
    teacher_base_url: str = "https://openrouter.ai/api/v1"
    teacher_model: str = "deepseek/deepseek-v4-flash:nitro"
    teacher_api_key_env: str = "OPENROUTER_API_KEY"
    synth_samples_per_seed: int = 6
    synth_max_turns: int = 8
    synth_temperature: float = 0.8

    # --- real-trace harvest — Postgres (read-only) ---
    pg_dsn_env: str = "AURA_DB_URL"
    harvest_limit: int = 2000  # max conversations to scan

    # --- dataset split ---
    train_frac: float = 0.85
    val_frac: float = 0.10
    # test_frac is the remainder
    seed: int = 1983

    # --- LoRA / SFT ---
    lora_r: int = 32
    lora_alpha: int = 64
    lora_dropout: float = 0.0
    learning_rate: float = 2e-4
    epochs: int = 3
    batch_size: int = 8
    grad_accum: int = 2
    warmup_ratio: float = 0.05
    weight_decay: float = 0.01

    # Gemma turn markers for train_on_responses_only (loss on model turns only).
    instruction_part: str = "<start_of_turn>user\n"
    response_part: str = "<start_of_turn>model\n"

    def path(self, attr: str) -> Path:
        """Resolve a path field against the finetune/ root."""
        raw = Path(getattr(self, attr))
        return raw if raw.is_absolute() else (ROOT / raw)


def _coerce(value: str, typ: type) -> Any:
    if typ is bool:
        return value.strip().lower() in {"1", "true", "yes", "on"}
    return typ(value)


def load(config_path: str | os.PathLike[str] | None = None) -> Config:
    """Load YAML defaults then apply AURA_FT_<UPPER_FIELD> env overrides."""
    cfg = Config()

    path = Path(config_path) if config_path else _default_config_path()
    if path.exists():
        try:
            import yaml  # type: ignore
        except ModuleNotFoundError as exc:  # pragma: no cover - dependency guard
            raise SystemExit(
                "pyyaml is required to read config/pipeline.yaml — `pip install -r requirements.txt`"
            ) from exc
        data = yaml.safe_load(path.read_text()) or {}
        known = {f.name: f.type for f in fields(Config)}
        for key, val in data.items():
            if key in known:
                setattr(cfg, key, val)

    for f in fields(Config):
        env_key = f"AURA_FT_{f.name.upper()}"
        if env_key in os.environ:
            setattr(cfg, f.name, _coerce(os.environ[env_key], type(getattr(cfg, f.name))))

    return cfg
