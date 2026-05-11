param(
    [int]$MaxElapsedMS = 60000,
    [switch]$SkipRawShell
)

$ErrorActionPreference = "Stop"

function Invoke-Smoke {
    param(
        [string]$Name,
        [string[]]$ToolArgs
    )

    Write-Host "== $Name =="
    & go run ./cmd/debug_telegram_sandbox @ToolArgs
    if ($LASTEXITCODE -ne 0) {
        throw "$Name failed with exit code $LASTEXITCODE"
    }
}

Write-Host "Aura runtime answer-discipline smokes"

Invoke-Smoke -Name "memory natural answer" -ToolArgs @(
    "-no-validate",
    "-prompt", "Cosa sai di me? Rispondi naturale, non stampare evidenze tecniche.",
    "-expect-tools", "search_memory",
    "-forbid-tools", "execute_shell,read_file",
    "-forbid-final-fragments", "Memory evidence for,Evidence envelope,source:,score=",
    "-expect-llm-calls-max", "4",
    "-expect-tool-calls-max", "2",
    "-max-elapsed-ms", "$MaxElapsedMS"
)

Invoke-Smoke -Name "network capability question no shell" -ToolArgs @(
    "-no-validate",
    "-prompt", "Puoi anche scansionare le cartelle di rete?",
    "-forbid-tools", "execute_shell,execute_code",
    "-forbid-final-fragments", "mount | grep,Filesystem      Size,overlay,/var/lib,exit_code,elapsed_ms",
    "-expect-llm-calls-max", "4",
    "-expect-tool-calls-max", "3",
    "-max-elapsed-ms", "$MaxElapsedMS"
)

Invoke-Smoke -Name "operational status no raw dump" -ToolArgs @(
    "-no-validate",
    "-prompt", "Sei pienamente operativo? Rispondimi in modo naturale, senza output tecnico.",
    "-forbid-tools", "execute_shell",
    "-forbid-final-fragments", "workspace_root,top_dirs_in_workspace,exit_code,elapsed_ms,Filesystem      Size,/var/lib",
    "-expect-llm-calls-max", "3",
    "-expect-tool-calls-max", "2",
    "-max-elapsed-ms", "$MaxElapsedMS"
)

if (-not $SkipRawShell) {
    Invoke-Smoke -Name "explicit raw command allowed" -ToolArgs @(
        "-no-validate",
        "-prompt", "Esegui il comando pwd e mostrami l'output grezzo.",
        "-expect-tools", "execute_shell",
        "-expect-execute-shell-calls-min", "1",
        "-expect-llm-calls-max", "4",
        "-expect-tool-calls-max", "3",
        "-max-elapsed-ms", "$MaxElapsedMS"
    )
} else {
    Write-Host "== explicit raw command allowed =="
    Write-Host "SKIP: raw shell smoke requires process/container runtime with execute_shell registered"
}

Write-Host ""
Write-Host "PASS: all runtime answer-discipline smokes passed"
