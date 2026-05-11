# test-heuristic-removal.ps1
# Phase 2 ROADMAP success criterion #1: no heuristic text-parsing for wiki
# writes remains on the agent-loop path. This guard fails CI if any
# heuristic marker resurfaces in production code.
#
# Patterns are the canonical heuristic identifiers that Phase 2 explicitly
# forbids on the agent-loop path. Adding a pattern here documents the
# regression contract; removing one weakens it.

$ErrorActionPreference = "Stop"
$repoRoot = Resolve-Path "$PSScriptRoot/.."
Set-Location $repoRoot

$patterns = @(
    "looksLikeWikiYAML",
    "isLikelyWikiPage",
    "detectWikiFromText",
    "parseWikiFromAssistant"
)

# Directories and file patterns to exclude from scanning.
# .planning/ is excluded so historical planning artifacts that reference
# these names (e.g., RESEARCH.md, PLAN.md) do not false-trigger.
$excludeDirs = @(
    [regex]::Escape(".git"),
    [regex]::Escape("node_modules"),
    [regex]::Escape(".planning"),
    [regex]::Escape("dist"),
    [regex]::Escape("data")
)

$matched = @()
foreach ($p in $patterns) {
    $hits = Get-ChildItem -Path . -Recurse -File -Include "*.go","*.ts","*.tsx" |
        Where-Object {
            $path = $_.FullName
            $skip = $false
            foreach ($ex in $excludeDirs) {
                if ($path -match $ex) { $skip = $true; break }
            }
            # Exclude test files — heuristic identifiers may appear in tests
            # that verify the heuristics are absent (e.g., this guard's own
            # regression test). Production code must be heuristic-free.
            if ($_.Name -match "_test\.go$|\.test\.tsx?$") { $skip = $true }
            -not $skip
        } |
        Select-String -Pattern $p -SimpleMatch
    if ($hits) {
        foreach ($h in $hits) {
            $matched += "$($h.Filename):$($h.LineNumber): $($h.Line.Trim())"
        }
    }
}

if ($matched.Count -gt 0) {
    Write-Error "Heuristic markers found in production code (Phase 2 ROADMAP #1 regression):`n  - $($matched -join "`n  - ")"
    exit 1
}

Write-Host "OK: no heuristic markers found"
exit 0
