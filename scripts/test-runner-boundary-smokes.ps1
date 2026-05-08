param(
    [int]$MaxElapsedMS = 30000
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

Invoke-Smoke -Name "status prompt" -ToolArgs @(
    "-no-validate",
    "-prompt", "fammi il punto sintetico sullo stato di Aura e non creare file",
    "-expect-toolset", "default",
    "-expect-loop-steps-max", "2",
    "-expect-llm-calls-max", "2",
    "-expect-tool-calls-max", "2",
    "-max-elapsed-ms", "$MaxElapsedMS",
    "-expect-workspace-root", "/workspace"
)

Invoke-Smoke -Name "memory prompt" -ToolArgs @(
    "-no-validate",
    "-prompt", "cosa ricordi della memoria e del grafo di Aura? dammi un riassunto breve",
    "-expect-toolset", "default",
    "-expect-tools", "search_memory",
    "-expect-loop-steps-max", "2",
    "-expect-llm-calls-max", "2",
    "-expect-tool-calls-max", "2",
    "-max-elapsed-ms", "$MaxElapsedMS",
    "-expect-workspace-root", "/workspace"
)

Invoke-Smoke -Name "document prompt" -Env @{ AURA_TOOLSET_MODE = "document" } -ToolArgs @(
    "-no-validate",
    "-prompt", "crea un documento Word breve con titolo Stato Aura e un paragrafo: Runtime aggiornato, container healthy, memoria compatta sincronizzata. Non cercare memoria.",
    "-expect-toolset", "document",
    "-expect-tools", "create_docx",
    "-expect-terminal-tool", "create_docx",
    "-expect-loop-steps-max", "2",
    "-expect-llm-calls-max", "2",
    "-expect-tool-calls-max", "2",
    "-max-elapsed-ms", "$MaxElapsedMS",
    "-expect-workspace-root", "/workspace"
)

Invoke-Smoke -Name "admin daily briefing prompt" -Env @{ AURA_TOOLSET_MODE = "admin" } -ToolArgs @(
    "-no-validate",
    "-prompt", "fammi un daily briefing breve di Aura per oggi",
    "-expect-toolset", "admin",
    "-expect-tools", "daily_briefing",
    "-expect-loop-steps-max", "2",
    "-expect-llm-calls-max", "2",
    "-expect-tool-calls-max", "1",
    "-max-elapsed-ms", "$MaxElapsedMS",
    "-expect-workspace-root", "/workspace"
)
