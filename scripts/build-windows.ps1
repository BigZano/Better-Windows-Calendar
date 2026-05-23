# Build PyCalendar for Windows and (optionally) produce an Inno Setup installer.
# Usage: .\scripts\build-windows.ps1 [-Version "1.2.3"] [-Installer]
param(
    [string]$Version = "0.1.0",
    [switch]$Installer
)

$ErrorActionPreference = "Stop"
$Root = Split-Path $PSScriptRoot -Parent

Set-Location $Root

# Ensure dist/ exists
New-Item -ItemType Directory -Force -Path "$Root\dist" | Out-Null

Write-Host "Building pycalendar.exe v$Version..."
$env:GOARCH = "amd64"
$env:GOOS   = "windows"
go build `
    -ldflags="-H windowsgui -s -w -X main.version=$Version" `
    -o "$Root\dist\pycalendar.exe" `
    .

if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Write-Host "  -> dist\pycalendar.exe"

if ($Installer) {
    # Patch the version in setup.iss and compile
    $issPath = "$Root\installer\windows\setup.iss"
    $issContent = (Get-Content $issPath -Raw) -replace '#define MyAppVersion ".*"', "#define MyAppVersion `"$Version`""
    $issContent | Set-Content $issPath -Encoding UTF8

    Write-Host "Compiling installer with Inno Setup..."
    $iscc = "C:\Program Files (x86)\Inno Setup 6\ISCC.exe"
    if (-not (Test-Path $iscc)) {
        Write-Error "Inno Setup not found at '$iscc'. Install it from https://jrsoftware.org/isinfo.php"
    }
    & $iscc $issPath
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    Write-Host "  -> dist\PyCalendarSetup-v$Version.exe"
}

Write-Host "Done."
