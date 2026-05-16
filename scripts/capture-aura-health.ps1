param(
    [string]$OutputRoot = "reports/health",
    [string]$ApiBase = "http://127.0.0.1:18080",
    [string]$DashboardToken = "",
    [string]$TelegramBotToken = "",
    [int]$LogTail = 400,
    [int]$ConversationLimit = 80,
    [switch]$FetchTelegramUpdates,
    [switch]$RunTelegramSmoke,
    [string]$SmokePrompt = "ciao Aura, rispondi con uno stato sintetico"
)

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $scriptDir
Set-Location $repoRoot

if ([string]::IsNullOrWhiteSpace($DashboardToken)) {
    $DashboardToken = $env:AURA_DASHBOARD_TOKEN
}
if ([string]::IsNullOrWhiteSpace($DashboardToken)) {
    $DashboardToken = $env:DASHBOARD_TOKEN
}
if ([string]::IsNullOrWhiteSpace($TelegramBotToken)) {
    $TelegramBotToken = $env:TELEGRAM_BOT_TOKEN
}

$stamp = Get-Date -Format "yyyyMMdd-HHmmss"
$outDir = Join-Path $repoRoot (Join-Path $OutputRoot $stamp)
New-Item -ItemType Directory -Force -Path $outDir | Out-Null

$secrets = @()
foreach ($value in @($DashboardToken, $TelegramBotToken, $env:LLM_API_KEY, $env:EMBEDDING_API_KEY)) {
    if (-not [string]::IsNullOrWhiteSpace($value)) {
        $secrets += $value
    }
}

function Redact-Text {
    param([AllowNull()][string]$Text)
    if ($null -eq $Text) { return "" }
    $redacted = $Text
    foreach ($secret in $script:secrets) {
        if (-not [string]::IsNullOrWhiteSpace($secret)) {
            $redacted = $redacted.Replace($secret, "[REDACTED]")
        }
    }
    $redacted = $redacted -replace 'Bearer\s+[A-Za-z0-9._~+/=-]+', 'Bearer [REDACTED]'
    $redacted = $redacted -replace '([A-Z0-9_]*TOKEN[A-Z0-9_]*=)[^\s]+', '$1[REDACTED]'
    $redacted = $redacted -replace '([A-Z0-9_]*API_KEY[A-Z0-9_]*=)[^\s]+', '$1[REDACTED]'
    return $redacted
}

function Save-Text {
    param(
        [string]$Name,
        [AllowNull()][string]$Text
    )
    $path = Join-Path $outDir $Name
    $parent = Split-Path -Parent $path
    if (-not [string]::IsNullOrWhiteSpace($parent)) {
        New-Item -ItemType Directory -Force -Path $parent | Out-Null
    }
    Redact-Text $Text | Set-Content -Path $path -Encoding UTF8
}

function Invoke-Capture {
    param(
        [string]$Name,
        [scriptblock]$Command
    )
    $global:LASTEXITCODE = 0
    try {
        $output = & $Command 2>&1 | Out-String
        $exitCode = $global:LASTEXITCODE
    } catch {
        $output = $_ | Out-String
        $exitCode = 1
    }
    Save-Text $Name ("exit_code=$exitCode`n`n$output")
    return $exitCode
}

function Invoke-JsonGet {
    param(
        [string]$Name,
        [string]$Url,
        [hashtable]$Headers = @{}
    )
    try {
        $response = Invoke-WebRequest -UseBasicParsing -Uri $Url -Headers $Headers -TimeoutSec 20
        Save-Text $Name $response.Content
        return $true
    } catch {
        Save-Text $Name ($_ | Out-String)
        return $false
    }
}

$metadata = [ordered]@{
    captured_at = (Get-Date).ToUniversalTime().ToString("o")
    repo_root = $repoRoot
    api_base = $ApiBase
    log_tail = $LogTail
    conversation_limit = $ConversationLimit
    dashboard_token_present = -not [string]::IsNullOrWhiteSpace($DashboardToken)
    telegram_token_present = -not [string]::IsNullOrWhiteSpace($TelegramBotToken)
    fetch_telegram_updates = [bool]$FetchTelegramUpdates
    run_telegram_smoke = [bool]$RunTelegramSmoke
}
Save-Text "metadata.json" ($metadata | ConvertTo-Json -Depth 5)

Invoke-Capture "git-status.txt" { git status --short } | Out-Null
Invoke-Capture "docker-compose-ps.txt" { docker compose ps } | Out-Null
Invoke-Capture "docker-compose-config.txt" { docker compose config } | Out-Null
Invoke-Capture "docker-logs-aura.txt" { docker compose logs --tail $LogTail aura } | Out-Null
Invoke-Capture "docker-logs-runtime.txt" { docker compose logs --tail $LogTail qdrant searxng garage } | Out-Null
Invoke-Capture "container-health.txt" {
    docker compose exec -T aura sh -lc 'date; echo "--- status"; wget -qO- http://127.0.0.1:8080/status || true; echo; echo "--- workspace"; find /workspace -maxdepth 2 -type f 2>/dev/null | head -120; echo "--- disk"; du -sh /workspace /data 2>/dev/null || true'
} | Out-Null

Invoke-JsonGet "status.json" "$ApiBase/status" | Out-Null

if (-not [string]::IsNullOrWhiteSpace($DashboardToken)) {
    $headers = @{ Authorization = "Bearer $DashboardToken" }
    Invoke-JsonGet "conversations-stats.json" "$ApiBase/conversations/stats" $headers | Out-Null
    Invoke-JsonGet "conversations-recent.json" "$ApiBase/conversations?limit=$ConversationLimit" $headers | Out-Null
    Invoke-JsonGet "auth-whoami.json" "$ApiBase/auth/whoami" $headers | Out-Null
} else {
    Save-Text "conversations-skipped.txt" "Dashboard token missing. Set AURA_DASHBOARD_TOKEN or pass -DashboardToken to capture /conversations safely through Aura's API."
}

if (-not [string]::IsNullOrWhiteSpace($TelegramBotToken)) {
    $telegramBase = "https://api.telegram.org/bot$TelegramBotToken"
    Invoke-JsonGet "telegram-getMe.json" "$telegramBase/getMe" | Out-Null
    Invoke-JsonGet "telegram-webhookInfo.json" "$telegramBase/getWebhookInfo" | Out-Null
    if ($FetchTelegramUpdates) {
        Invoke-JsonGet "telegram-updates.json" "$telegramBase/getUpdates?limit=20&timeout=0" | Out-Null
    } else {
        Save-Text "telegram-updates-skipped.txt" "Skipped getUpdates because the live bot owns long polling. Pass -FetchTelegramUpdates only when Aura polling is stopped or you explicitly want this diagnostic."
    }
} else {
    Save-Text "telegram-skipped.txt" "Telegram token missing. Set TELEGRAM_BOT_TOKEN or pass -TelegramBotToken to capture getMe/webhookInfo."
}

if ($RunTelegramSmoke) {
    $previousWikiPath = $env:WIKI_PATH
    try {
        if ([string]::IsNullOrWhiteSpace($env:WIKI_PATH) -or $env:WIKI_PATH -eq ".\wiki" -or $env:WIKI_PATH -eq "./wiki") {
            $env:WIKI_PATH = "runtime-workspace\wiki"
        }
        Invoke-Capture "telegram-smoke.txt" {
            go run ./cmd/debug_telegram_sandbox -timeout 90s -no-validate -prompt $SmokePrompt
        } | Out-Null
    } finally {
        $env:WIKI_PATH = $previousWikiPath
    }
}

$summary = @"
Aura health snapshot saved.

Directory: $outDir

Core files:
- metadata.json
- docker-compose-ps.txt
- docker-logs-aura.txt
- container-health.txt
- status.json
- conversations-recent.json, if a dashboard token was provided
- telegram-getMe.json, if a Telegram bot token was provided
"@
Save-Text "SUMMARY.txt" $summary
Write-Host $summary
