# Quick dev build — suppresses the console window, skips installer steps.
# Output: pycalendar.exe in the repo root.
# Usage: .\scripts\build-dev.ps1
param(
    [string]$Version = "dev"
)

$ErrorActionPreference = "Stop"
$Root = Split-Path $PSScriptRoot -Parent
Set-Location $Root

Write-Host "Building pycalendar.exe (dev, $Version)..."
$env:GOARCH = "amd64"
$env:GOOS   = "windows"
go build `
    -ldflags="-H windowsgui -X main.version=$Version" `
    -o "$Root\pycalendar.exe" `
    .
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Write-Host "  -> pycalendar.exe"
