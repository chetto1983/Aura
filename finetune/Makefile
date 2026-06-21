# Aura FunctionGemma fine-tuning spike.
# Run targets from the finetune/ directory: `make <target>`.
# The Go exporter is invoked from the repo root so it sees the module.

PY ?= python
REPO_ROOT := $(shell cd .. && pwd)
CONFIG ?= config/pipeline.yaml

.PHONY: help tools harvest synth dataset train eval-base eval-finetuned all clean

help:
	@echo "Targets:"
	@echo "  tools          export Aura's real tool specs -> data/aura_tools.json (Go)"
	@echo "  harvest        pull real traces from Postgres   (needs AURA_DB_URL)"
	@echo "  synth          generate synthetic traces        (needs OPENROUTER_API_KEY)"
	@echo "  dataset        merge + dedup + split -> data/dataset/{train,val,test}.jsonl"
	@echo "  train          LoRA SFT of FunctionGemma-270M    (GPU recommended)"
	@echo "  eval-base      tool-call equivalence, base model (the 'before')"
	@echo "  eval-finetuned tool-call equivalence, fine-tuned (the 'after')"
	@echo "  all            tools -> synth -> dataset (data prep only; harvest opt-in)"
	@echo "  clean          remove data/"

tools:
	cd $(REPO_ROOT) && go run ./finetune/exporter -out finetune/data/aura_tools.json

harvest:
	$(PY) -m aura_finetune.harvest --config $(CONFIG)

synth: tools
	$(PY) -m aura_finetune.synth --config $(CONFIG)

dataset:
	$(PY) -m aura_finetune.build_dataset --config $(CONFIG)

train:
	$(PY) -m aura_finetune.train --config $(CONFIG)

eval-base:
	$(PY) -m aura_finetune.evaluate --config $(CONFIG) --base

eval-finetuned:
	$(PY) -m aura_finetune.evaluate --config $(CONFIG) --adapter data/out/lora

all: tools synth dataset
	@echo "data prep complete. Run 'make harvest' to add real traces, then 'make dataset' again."

clean:
	rm -rf data/
