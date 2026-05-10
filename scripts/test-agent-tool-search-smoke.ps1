param(
    [int]$MaxElapsedMS = 60000
)

$ErrorActionPreference = "Stop"

function Invoke-Smoke {
    param(
        [string]$Name,
        [string[]]$ToolArgs,
        [hashtable]$Env = @{}
    )

    Write-Host "== $Name =="
    $old = @{}
    foreach ($key in $Env.Keys) {
        $old[$key] = [Environment]::GetEnvironmentVariable($key, "Process")
        [Environment]::SetEnvironmentVariable($key, [string]$Env[$key], "Process")
    }
    try {
        & go run ./cmd/debug_telegram_sandbox @ToolArgs
        if ($LASTEXITCODE -ne 0) {
            throw "$Name failed with exit code $LASTEXITCODE"
        }
    } finally {
        foreach ($key in $Env.Keys) {
            [Environment]::SetEnvironmentVariable($key, $old[$key], "Process")
        }
    }
}

Write-Host "Aura tool-search / programmatic execution smokes"

Invoke-Smoke -Name "tool_search discovery" -ToolArgs @(
    "-no-validate",
    "-prompt", "Use tool_search to discover what Python/code execution tool is available, then report back what you found. Do not execute anything and do not look for shell tools.",
    "-expect-tools", "tool_search",
    "-forbid-tools", "execute_shell",
    "-expect-loop-steps-max", "3",
    "-expect-llm-calls-max", "3",
    "-expect-tool-calls-max", "3",
    "-expect-visible-tools-max", "10",
    "-max-elapsed-ms", "$MaxElapsedMS",
    "-expect-workspace-root", "/workspace"
)

Invoke-Smoke -Name "tool_search execute_code orchestration" -ToolArgs @(
    "-no-validate",
    "-prompt", "Use tool_search to find the execute_code tool, then use execute_code to compute sum(range(1, 101)) with Python. Report the result.",
    "-expect-tools", "tool_search,execute_code",
    "-expect-llm-calls-max", "3",
    "-expect-tool-calls-max", "3",
    "-expect-execute-code-calls-min", "1",
    "-expect-tool-search-calls-max", "1",
    "-expect-visible-tools-max", "10",
    "-max-elapsed-ms", "$MaxElapsedMS",
    "-expect-workspace-root", "/workspace"
)

Invoke-Smoke -Name "container diagnostic with python and shell" -ToolArgs @(
    "-no-validate",
    "-prompt", "Esegui una diagnostica del container Aura: usa tool_search per trovare execute_code e execute_shell, poi con un solo script Python in execute_code scrivi un file di diagnostica in /tmp/aura_out/diag.txt con data, hostname (via socket.gethostname), e versione Python. Non usare execute_shell. Poi dimmi che il file e' stato creato.",
    "-expect-tools", "tool_search,execute_code",
    "-expect-llm-calls-max", "4",
    "-expect-tool-calls-max", "4",
    "-expect-execute-code-calls-min", "1",
    "-expect-tool-search-calls-max", "1",
    "-expect-visible-tools-max", "10",
    "-max-elapsed-ms", "$MaxElapsedMS",
    "-expect-workspace-root", "/workspace"
)

Write-Host ""
Write-Host "PASS: all tool-search / programmatic execution smokes passed"
