Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$WebRoot = Split-Path -Parent $PSScriptRoot
$RepoRoot = Split-Path -Parent $WebRoot

function Dashboard-Url {
  $listen = $env:HTTP_PORT
  if ([string]::IsNullOrWhiteSpace($listen)) {
    $listen = "127.0.0.1:8080"
  }
  $port = ($listen -split ":")[-1]
  return "http://127.0.0.1:$port"
}

Push-Location $WebRoot
try {
  npm run i18n:check
  npm run lint
  npm run build

  if ([string]::IsNullOrWhiteSpace($env:AURA_E2E_TOKEN)) {
    throw "AURA_E2E_TOKEN is not set in the process environment. Run cmd/seed_e2e_env (eval its output) before running the frontend audit."
  }

  $dashboardUrl = Dashboard-Url
  $uri = [Uri]$dashboardUrl
  $existing = Get-NetTCPConnection -LocalPort $uri.Port -ErrorAction SilentlyContinue |
    Where-Object { $_.State -eq "Listen" }
  if ($existing) {
    throw "Port $($uri.Port) is already in use. Stop the running Aura process before running the frontend audit."
  }

  $tmpDir = Join-Path $RepoRoot ".tmp"
  New-Item -ItemType Directory -Force -Path $tmpDir | Out-Null
  $stdout = Join-Path $tmpDir "audit-frontend-aura.out.log"
  $stderr = Join-Path $tmpDir "audit-frontend-aura.err.log"
  $proc = Start-Process -FilePath "go" `
    -ArgumentList @("run", "./cmd/aura") `
    -WorkingDirectory $RepoRoot `
    -WindowStyle Hidden `
    -RedirectStandardOutput $stdout `
    -RedirectStandardError $stderr `
    -PassThru

  try {
    $alive = $false
    for ($i = 0; $i -lt 30; $i++) {
      try {
        $health = Invoke-RestMethod -Uri "$dashboardUrl/health" -TimeoutSec 2
        if ($health.status -eq "alive") {
          $alive = $true
          break
        }
      } catch {
        Start-Sleep -Seconds 1
      }
    }
    if (-not $alive) {
      Get-Content -LiteralPath $stdout -ErrorAction SilentlyContinue | Select-Object -Last 80
      Get-Content -LiteralPath $stderr -ErrorAction SilentlyContinue | Select-Object -Last 80
      throw "Aura did not become healthy at $dashboardUrl."
    }

    $env:AURA_DASHBOARD_URL = $dashboardUrl
    # AURA_E2E_TOKEN + AURA_E2E_CHAT_ID inherit from the caller's process env.

    npm run e2e:pages
  } finally {
    if ($proc -and -not $proc.HasExited) {
      Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
    }
    Get-NetTCPConnection -LocalPort $uri.Port -ErrorAction SilentlyContinue |
      Where-Object { $_.State -eq "Listen" } |
      ForEach-Object { Stop-Process -Id $_.OwningProcess -Force -ErrorAction SilentlyContinue }
  }
} finally {
  Pop-Location
}
