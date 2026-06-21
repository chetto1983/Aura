"""Aura FunctionGemma fine-tuning spike.

A self-contained pipeline that teaches Google's FunctionGemma-270M to emit
Aura's exact tool-calling format, reproducing the distil-labs result
(10-39% -> 90-97% multi-turn tool-call equivalence) against Aura's real tools.

Modules are import-light by design: heavy/optional dependencies (torch,
unsloth, psycopg, openai) are imported lazily inside the functions that need
them, so `format` and `tools` work with nothing but the standard library.
"""

__all__ = [
    "config",
    "tools",
    "format",
    "harvest",
    "synth",
    "build_dataset",
    "train",
    "evaluate",
]
