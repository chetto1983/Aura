# CLI reference

`aura serve` is the long-lived runtime; every other subcommand is an operator surface
over the same runtime, stores and tools. `aura <cmd> --help` prints the flags.

```text
aura serve                    run the long-lived agent runtime (channels, cockpit, scheduler)
aura shell | chat <sub>       interactive REPL / chat conversations against the agent loop
aura doctor | config <sub>    environment diagnostics / effective configuration
aura agent dry-run            drive a mock LoopAgent through the Budget tree
aura tools                    print the tool manifest
aura task <sub>               operator parity with the model-facing `task` tool:
                              schedule | list | cancel | run_now | pause | resume | approve | runs | doctor
aura mcp <sub>                managed MCP servers: install | add | list | doctor | tools | enable | disable | remove
aura memory <sub>             ArcadeDB memory administration
aura identity <sub>           identities, capability grants, operator break-glass recovery
aura gateway grants <sub>     AG-UI gateway approval grants
aura paused-states <sub>      HITL pauses
aura skills <sub> | pack <sub> skill lifecycle · packs: list | show | install | trust
aura retention <plan|apply>   retention sweep
aura db <sub>                 Postgres lifecycle: migrate | ping | status | reset
aura objectstore <sub>        Garage object-store administration
aura web <doctor|tool ...>    web tools (search/fetch) from the CLI
aura docs <sub>               document ingestion
aura version                  build metadata
```
