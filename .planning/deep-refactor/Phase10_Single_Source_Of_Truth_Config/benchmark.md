# Phase10 Benchmark

| Check | Command / Method | Threshold | Actual Result | Status |
| --- | --- | --- | --- | --- |
| Fresh install boots without `.env` | rm `.env` + `rm aura.db` + run aura | wizard reachable at HTTP_PORT default; no panic | not run | planned |
| Wizard persistence | submit token + LLM config | SQLite `secrets` table has rows; no `.env` written | not run | planned |
| Existing install migration | run aura with old `.env` present + empty `secrets` table | secrets imported to SQLite; log lists imported keys | not run | planned |
| Bootstrap env override | set `DB_PATH=/tmp/foo.db` and boot | aura opens `/tmp/foo.db`, not `./aura.db` | not run | planned |
| Compose without env_file | docker compose up after dropping `env_file:` | container boots; first-run wizard reachable on host:8080 | not run | planned |
| Secrets table privacy | tail logs during boot | no secret values logged (keys only) | not run | planned |
| Full compile/vet/test | `go build ./...`; `go vet ./...`; `go test ./...` | green | not run | planned |
