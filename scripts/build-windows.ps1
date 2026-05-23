# Build PyCalendar for Windows and (optionally) produce an Inno Setup installer.
# Usage: .\scripts\build-windows.ps1 [-Version "1.2.3"] [-Installer]
param(
    [string]$Version = "0.1.0",
    [switch]$Installer
)

$ErrorActionPreference = "Stop"
$Root = Split-Path $PSScriptRoot -Parent
Set-Location $Root
New-Item -ItemType Directory -Force -Path "$Root\dist" | Out-Null

# Ensure go-winres is available
if (-not (Get-Command go-winres -ErrorAction SilentlyContinue)) {
    Write-Host "Installing go-winres..."
    go install github.com/tc-hib/go-winres@latest
}

# Patch version into winres.json (quad format required)
$quad = "$Version.0"
$j = Get-Content "$Root\winres\winres.json" -Raw
$j = $j -replace '"file_version": ".*"',    "`"file_version`": `"$quad`""
$j = $j -replace '"product_version": ".*"', "`"product_version`": `"$quad`""
$j = $j -replace '"FileVersion": ".*"',     "`"FileVersion`": `"$Version`""
$j = $j -replace '"ProductVersion": ".*"',  "`"ProductVersion`": `"$Version`""
Set-Content "$Root\winres\winres.json" $j -Encoding UTF8

Write-Host "Generating Windows resources..."
go-winres make --in "$Root\winres\winres.json"

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
    $issPath = "$Root\installer\windows\setup.iss"
    $iss = (Get-Content $issPath -Raw) -replace '#define MyAppVersion ".*"', "#define MyAppVersion `"$Version`""
    Set-Content $issPath $iss -Encoding UTF8

    $iscc = "C:\Program Files (x86)\Inno Setup 6\ISCC.exe"
    if (-not (Test-Path $iscc)) {
        Write-Error "Inno Setup not found. Install: winget install JRSoftware.InnoSetup"
    }
    Write-Host "Compiling installer..."
    & $iscc $issPath
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    Write-Host "  -> dist\PyCalendarSetup-v$Version.exe"
}

Write-Host "Done."
