"""LoRA SFT of FunctionGemma-270M with Unsloth + TRL.

Follows the distil-labs / Unsloth recipe: load the IT base, attach a LoRA
adapter, render each trace through the model's own chat template, and train on
*response tokens only* (`train_on_responses_only` with Gemma's turn markers) so
the loss rewards correct tool calls, not the reproduction of the prompt.

The 270M base fits comfortably; LoRA on a 4GB GPU (Amendment #59 constraint) is
fine, and CPU works for a small dataset if no GPU is present.

Outputs to <out_dir>: the LoRA adapter, optionally a merged 16-bit model and a
GGUF for local inference.
"""

from __future__ import annotations

from pathlib import Path

from . import config as cfgmod
from .format import render_text


def _load_split(path: Path) -> list[dict]:
    import json

    return [json.loads(l) for l in path.read_text().splitlines() if l.strip()]


def train(cfg: cfgmod.Config, *, export_gguf: bool = False, merge_16bit: bool = False) -> str:
    from datasets import Dataset  # type: ignore
    from trl import SFTConfig, SFTTrainer  # type: ignore
    from trl import train_on_responses_only  # type: ignore
    from unsloth import FastLanguageModel  # type: ignore
    from unsloth.chat_templates import train_on_responses_only as _u_torr  # type: ignore  # noqa: F401

    model, tokenizer = FastLanguageModel.from_pretrained(
        model_name=cfg.base_model,
        max_seq_length=cfg.max_seq_len,
        load_in_4bit=False,
        load_in_16bit=True,
    )
    model = FastLanguageModel.get_peft_model(
        model,
        r=cfg.lora_r,
        lora_alpha=cfg.lora_alpha,
        lora_dropout=cfg.lora_dropout,
        target_modules=[
            "q_proj", "k_proj", "v_proj", "o_proj",
            "gate_proj", "up_proj", "down_proj",
        ],
        use_gradient_checkpointing="unsloth",
        random_state=cfg.seed,
    )

    def to_text(ex: dict) -> dict:
        from .format import to_chat_messages

        messages = to_chat_messages(ex["messages"])
        return {"text": render_text(messages, ex.get("tools", []), tokenizer)}

    train_rows = _load_split(cfg.path("dataset_dir") / "train.jsonl")
    val_rows = _load_split(cfg.path("dataset_dir") / "val.jsonl")
    if not train_rows:
        raise SystemExit(
            f"train split empty at {cfg.path('dataset_dir') / 'train.jsonl'} — "
            "run `make synth` (+ optional `make harvest`) then `make dataset` first."
        )
    _train_raw = Dataset.from_list(train_rows)
    _val_raw = Dataset.from_list(val_rows) if val_rows else None
    # Drop the normal-form columns (messages/tools/source) so TRL's SFTTrainer trains
    # on the rendered `text` field. If they survive, SFTTrainer takes its conversational
    # branch and silently empties the split → `num_samples=0` at the DataLoader sampler.
    train_ds = _train_raw.map(to_text, remove_columns=_train_raw.column_names)
    val_ds = _val_raw.map(to_text, remove_columns=_val_raw.column_names) if _val_raw else None

    out_dir = cfg.path("out_dir")
    out_dir.mkdir(parents=True, exist_ok=True)

    trainer = SFTTrainer(
        model=model,
        tokenizer=tokenizer,
        train_dataset=train_ds,
        eval_dataset=val_ds,
        args=SFTConfig(
            dataset_text_field="text",
            max_seq_length=cfg.max_seq_len,
            per_device_train_batch_size=cfg.batch_size,
            gradient_accumulation_steps=cfg.grad_accum,
            num_train_epochs=cfg.epochs,
            learning_rate=cfg.learning_rate,
            warmup_ratio=cfg.warmup_ratio,
            weight_decay=cfg.weight_decay,
            logging_steps=10,
            optim="adamw_8bit",
            seed=cfg.seed,
            output_dir=str(out_dir / "trainer"),
            report_to="none",
        ),
    )

    # Loss on model turns only — the heart of tool-call SFT.
    trainer = train_on_responses_only(
        trainer,
        instruction_part=cfg.instruction_part,
        response_part=cfg.response_part,
    )

    trainer.train()

    adapter_dir = out_dir / "lora"
    model.save_pretrained(str(adapter_dir))
    tokenizer.save_pretrained(str(adapter_dir))
    print(f"saved LoRA adapter -> {adapter_dir}")

    if merge_16bit:
        merged = out_dir / "merged-16bit"
        model.save_pretrained_merged(str(merged), tokenizer, save_method="merged_16bit")
        print(f"saved merged 16-bit model -> {merged}")

    if export_gguf:
        gguf = out_dir / "gguf"
        model.save_pretrained_gguf(str(gguf), tokenizer, quantization_method="q4_k_m")
        print(f"saved GGUF (q4_k_m) -> {gguf}")

    return str(adapter_dir)


def main(argv: list[str] | None = None) -> int:
    import argparse

    ap = argparse.ArgumentParser(description="LoRA-fine-tune FunctionGemma on the Aura dataset.")
    ap.add_argument("--config", default=None)
    ap.add_argument("--gguf", action="store_true", help="also export a q4_k_m GGUF")
    ap.add_argument("--merge", action="store_true", help="also save a merged 16-bit model")
    args = ap.parse_args(argv)

    cfg = cfgmod.load(args.config)
    train(cfg, export_gguf=args.gguf, merge_16bit=args.merge)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
