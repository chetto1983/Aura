# Claude Messages wire probe

Measures the request shapes the `anthropic` route sends before any adapter code is written
(plan `docs/superpowers/plans/2026-11-27-anthropic-provider.md`, Task 1). Results:
`docs/superpowers/verification/2026-11-27-anthropic-wire-probe.md`.

```bash
go run ./spikes/anthropic-provider/probe -keyfile <file holding a Claude Console key> \
  -model claude-sonnet-5-5 -case all \
  -pdf scripts/fixtures/document_retrieval_eval/complex_corpus/scheda_sicurezza_detergente_DX45.pdf
```

Cases: `headers`, `sampling`, `tool-choice`, `loop`, `loop-omitted`, `steady`, `foreign-history`,
`pdf-in-tool-result`. Every call is billed to the key's Console credit; keep the key outside the repository
and revoke it after the run.
