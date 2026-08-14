[CmdletBinding()]
param()

. (Join-Path $PSScriptRoot 'service-common.ps1')
Ensure-Administrator "-NoProfile -ExecutionPolicy Bypass -File `"$PSCommandPath`""

& (Join-Path $PSScriptRoot 'stop.ps1')
& (Join-Path $PSScriptRoot 'start.ps1')
