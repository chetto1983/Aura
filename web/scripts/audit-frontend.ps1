Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$WebRoot = Split-Path -Parent $PSScriptRoot
$RepoRoot = Split-Path -Parent $WebRoot
$EnvPath = Join-Path $RepoRoot ".env"

function Read-DotEnv {
  param([string]$Path)
  $values = @{}
  if (-not (Test-Path -LiteralPath $Path)) {
    return $values
  }
  Get-Content -LiteralPath $Path | ForEach-Object {
    if ($_ -match '^\s*([^#][^=]+)=(.*)$') {
      $values[$matches[1].Trim()] = $matches[2]
    }
  }
  return $values
}

function Dashboard-Url {
  param([hashtable]$EnvValues)
  $listen = "127.0.0.1:8080"
  if ($EnvValues.ContainsKey("HTTP_PORT") -and -not [string]::IsNullOrWhiteSpace($EnvValues["HTTP_PORT"])) {
    $listen = $EnvValues["HTTP_PORT"]
  }
  $port = ($listen -split ":")[-1]
  return "http://127.0.0.1:$port"
}

Push-Location $WebRoot
try {
  npm run i18n:check
  npm run lint
  npm run build

  $envValues = Read-DotEnv $EnvPath
  if (-not $envValues.ContainsKey("AURA_E2E_TOKEN") -or [string]::IsNullOrWhiteSpace($envValues["AURA_E2E_TOKEN"])) {
    throw "AURA_E2E_TOKEN is not set in .env. Mint a dashboard token before running the frontend audit."
  }

  $dashboardUrl = Dashboard-Url $envValues
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
    $env:AURA_E2E_TOKEN = $envValues["AURA_E2E_TOKEN"]
    if ($envValues.ContainsKey("AURA_E2E_CHAT_ID") -and -not [string]::IsNullOrWhiteSpace($envValues["AURA_E2E_CHAT_ID"])) {
      $env:AURA_E2E_CHAT_ID = $envValues["AURA_E2E_CHAT_ID"]
    } else {
      Remove-Item Env:AURA_E2E_CHAT_ID -ErrorAction SilentlyContinue
    }

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
