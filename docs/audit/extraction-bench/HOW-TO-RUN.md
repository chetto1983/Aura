# Extraction bench

The harness behind D-3 and D-4 in [`../consolidated-fix-plan-2026-07-20.md`](../consolidated-fix-plan-2026-07-20.md).
Kept because those two decisions are still open: the numbers should be re-runnable, not
just quoted.

Fixtures are real — three signal sentences, three verbatim user turns pulled from
`aura.conversation_turns`, the ilmeteo.net weather-header row that the LLM extractor
actually turned into 13 "entities", and `"ciao"`. Labels and threshold are the sidecar's
own (`extraction/factory.py:43-60`, `config/settings.py:217`), so the bench measures the
configuration that would ship, not a flattering one.

```sh
docker run -d --name gliner-probe -v "//d/Aura/docs/audit/extraction-bench:/t" \
  -w /t python:3.11-slim sleep 7200

docker exec gliner-probe sh -c '
  pip install -q torch --index-url https://download.pytorch.org/whl/cpu
  pip install -q gliner onnxruntime spacy
  for m in en_core_web_sm it_core_news_sm it_core_news_lg xx_ent_wiki_sm; do
    python -m spacy download $m
  done'

docker exec gliner-probe python /t/gliner_test.py     # recall + false positives + latency
docker exec gliner-probe python /t/spacy_test.py      # the four spaCy candidates
docker exec gliner-probe python /t/type_compare.py    # type correctness — the one that decides

docker rm -f gliner-probe
```

`gliner_test.py` takes `GLINER_REPO`, `GLINER_ONNX` (`1`/`0`) and `GLINER_ONNX_FILE`, which
is how the four builds in D-3's table were produced.

**Two traps this bench exists to document.** `onnx/model_quantized.onnx` loads cleanly, runs
5× faster and returns **nothing** — benchmark that file and you will conclude the model is
useless on Italian. And recall alone is the wrong metric: the entity MERGE key is
`{name, type, deduplication_scope}`, so a wrong *type* creates a duplicate node rather than
a mislabelled one. `type_compare.py` is the one to trust.
