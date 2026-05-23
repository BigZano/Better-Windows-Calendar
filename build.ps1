# Root-level build shortcut. Always produces a windowsgui binary (no CMD window).
# Usage: .\build.ps1 [-Version "1.2.3"]
param([string]$Version = "dev")
& "$PSScriptRoot\scripts\build-dev.ps1" -Version $Version
