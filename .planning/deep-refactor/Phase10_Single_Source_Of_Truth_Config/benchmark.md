# Phase10 Benchmark

| Check | Command / Method | Threshold | Actual Result | Status |
| --- | --- | --- | --- | --- |
| Fresh install boots without `.env` | Phase10 closure gate | wizard reachable at HTTP_PORT default; no panic | met in US-H closure series | met |
| Wizard persistence | submit token + LLM config | SQLite `secrets` table has rows; no `.env` written | US-H04 wizard writes secrets to SQLite | met |
| Existing install migration | run aura with old `.env` present + empty `secrets` table | secrets imported to SQLite; log lists imported keys | US-H02 one-shot migration helper shipped | met |
| Bootstrap env override | set `DB_PATH=/tmp/foo.db` and boot | aura opens requested DB path | US-H05 hardcoded meta-config defaults with env override shipped | met |
| Compose without env_file | inspect `compose.yaml` and boot gate | container boots without compose `env_file:` | `compose.yaml` has zero `env_file:` directives; US-H06 install/compose docs shipped | met |
| Secrets table privacy | tail logs during boot | no secret values logged (keys only) | Phase10 closure reports green privacy gate | met |
| Full compile/vet/test | `go build ./...`; `go vet ./...`; `go test ./...` | green | green across US-H closure; current pushed CI also green on `ecb4cf3e` | met |
