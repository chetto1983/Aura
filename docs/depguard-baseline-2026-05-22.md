# Depguard Baseline 2026-05-22

## Scope

Phase-MODERNIZE US-MOD-INFRA-01 adds `depguard` rules for the architecture
dependency boundaries in `prd.md` section 9.

The enforced forbidden directions are:

- `agent -> channels/*`
- `agent -> api`
- `agent -> telegram`
- `agent -> concrete qdrant/sqlite/source parser details`
- `memory -> channels/*`
- `storage -> agent`
- `rag -> channels/*`
- `tools -> chat`
- `learning -> channels/*`

## Audit Result

Command:

```powershell
& $HOME\go\bin\golangci-lint.exe run --enable-only=depguard ./...
```

Result as of `fb56c797`:

- Grandfathered depguard violations: `0`
- `//nolint:depguard` comments added: `0`

No production import currently violates the configured boundary rules, so the
baseline is intentionally empty. Future violations should fail the CI depguard
step unless a reviewed exception is added here with the exact file, import,
reason, and planned fix.
