[CmdletBinding()]
param(
    [switch]$StaticOnly
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot

function Assert-Observability {
    param(
        [Parameter(Mandatory)] [bool]$Condition,
        [Parameter(Mandatory)] [string]$Message
    )
    if (-not $Condition) {
        throw "observability verification failed: $Message"
    }
}

$dashboardContracts = [ordered]@{
    'aura-overview.json'       = 'aura-overview'
    'aura-agents.json'         = 'aura-agents'
    'aura-tools-mcp.json'      = 'aura-tools-mcp'
    'aura-data-retention.json' = 'aura-data-retention'
}

$dashboardRoot = Join-Path $repoRoot 'observability/grafana/dashboards'
$seenUids = @{}
foreach ($entry in $dashboardContracts.GetEnumerator()) {
    $path = Join-Path $dashboardRoot $entry.Key
    Assert-Observability (Test-Path -LiteralPath $path -PathType Leaf) "missing dashboard $($entry.Key)"
    $dashboard = Get-Content -LiteralPath $path -Raw | ConvertFrom-Json
    Assert-Observability ($dashboard.uid -ceq $entry.Value) "dashboard $($entry.Key) must use UID $($entry.Value)"
    Assert-Observability (-not $seenUids.ContainsKey($dashboard.uid)) "duplicate dashboard UID $($dashboard.uid)"
    $seenUids[$dashboard.uid] = $true
    $panelIds = @($dashboard.panels | ForEach-Object { [int]$_.id })
    Assert-Observability ($panelIds.Count -gt 0) "dashboard $($entry.Key) has no panels"
    Assert-Observability (($panelIds | Select-Object -Unique).Count -eq $panelIds.Count) "dashboard $($entry.Key) has duplicate panel IDs"
}

$datasourcePath = Join-Path $repoRoot 'observability/grafana/provisioning/datasources/aura.yml'
$providerPath = Join-Path $repoRoot 'observability/grafana/provisioning/dashboards/aura.yml'
$tempoPath = Join-Path $repoRoot 'observability/tempo/tempo.yml'
Assert-Observability (Test-Path -LiteralPath $datasourcePath -PathType Leaf) 'missing Grafana datasource provisioning'
Assert-Observability (Test-Path -LiteralPath $providerPath -PathType Leaf) 'missing Grafana dashboard provider'
Assert-Observability (Test-Path -LiteralPath $tempoPath -PathType Leaf) 'missing Tempo configuration'

$datasourceText = Get-Content -LiteralPath $datasourcePath -Raw
$providerText = Get-Content -LiteralPath $providerPath -Raw
$tempoText = Get-Content -LiteralPath $tempoPath -Raw
Assert-Observability ($datasourceText -cmatch 'uid:\s+aura-prometheus') 'Prometheus datasource UID is not stable'
Assert-Observability ($datasourceText -cmatch 'uid:\s+aura-tempo') 'Tempo datasource UID is not stable'
Assert-Observability ($providerText -cmatch 'allowUiUpdates:\s+false') 'provisioned dashboards must be read-only'
Assert-Observability ($providerText -cmatch 'path:\s+/var/lib/grafana/dashboards') 'dashboard provider path does not match the container mount'
Assert-Observability ($tempoText -cmatch 'block_retention:\s+336h') 'Tempo must own a bounded 14-day retention window'

Write-Host "observability static verification passed: $($dashboardContracts.Count) dashboards, $($seenUids.Count) stable UIDs"

if (-not $StaticOnly) {
    Write-Host 'runtime smoke verification is added by Task 3'
}
