# TokenJuice archive (shipped 2026-05-19)

5 docs covering TokenJuice (Layer-1 deterministic rule compactor): algorithm
spec, Aura integration plan, baseline measurements, rules catalog, and test
strategy. Phase-TJ closed 2026-05-19 (8/8 stories US-TJ01..08). The package
lives at `internal/tokenjuice/` and runs at executor.go:92-94 BEFORE the
Phase-CTX payload_summarizer (layer-2 LLM fallback).

Moved here 2026-05-24 to keep `docs/` lean for active phases.
