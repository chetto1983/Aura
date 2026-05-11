# test-tool-search-removal.ps1
# Phase 2 ROADMAP success criterion #6: tool_search removed. This guard
# fails CI if any reference to the deleted tool or its associated symbols
# surfaces in production code.
#
# Plan 02-06b deletes internal/tools/tool_search.go. Once both the 02-06b
# worktree and this plan's worktree merge, this script will exit 0 on the
# clean tree. Until then, tool_search.go still exists in this worktree but
# the script is written to reflect the POST-MERGE steady state.
#
# Exclusions:
#   .planning/ — historical planning artifacts reference these names
#   test-tool-search-removal.ps1 (this file) — contains the patterns as strings
#   .git/ — binary/pack objects

$ErrorActionPreference = "Stop"
$repoRoot = Resolve-Path "$PSScriptRoot/.."
Set-Location $repoRoot

$patterns = @(
    "tool_search",
    "ToolSearchTool",
    "tierSearch",
    "toolNamesFromToolSearchResult"
)

$excludeDirs = @(
    [regex]::Escape(".git"),
    [regex]::Escape("node_modules"),
    [regex]::Escape(".planning"),
    [regex]::Escape("dist"),
    [regex]::Escape("data")
)

$matched = @()
foreach ($p in $patterns) {
    $hits = Get-ChildItem -Path . -Recurse -File -Include "*.go","*.ts","*.tsx","*.ps1" |
        Where-Object {
            $path = $_.FullName
            $skip = $false
            foreach ($ex in $excludeDirs) {
                if ($path -match $ex) { $skip = $true; break }
            }
            # This script itself contains the patterns as literal strings for
            # the grep loop — exclude it to avoid false self-referential hits.
            if ($_.Name -eq "test-tool-search-removal.ps1") { $skip = $true }
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
    Write-Error "tool_search references found in production code (Phase 2 ROADMAP #6 regression):`n  - $($matched -join "`n  - ")"
    exit 1
}

Write-Host "OK: no tool_search references found"
exit 0
