[CmdletBinding()]
param()

. (Join-Path $PSScriptRoot 'service-common.ps1')
Ensure-Administrator "-NoProfile -ExecutionPolicy Bypass -File `"$PSCommandPath`""

$service = Get-AxonHubService
if (-not $service) {
  Write-Warn "Service '$($script:AxonHubServiceName)' is not installed."
  exit 0
}
if ($service.Status -eq 'Stopped') {
  Write-Info "Service '$($script:AxonHubServiceName)' is already stopped."
  exit 0
}

Write-Info "Stopping service '$($script:AxonHubServiceName)'..."
Stop-Service -Name $script:AxonHubServiceName -Force
if (-not (Wait-ServiceState 'Stopped' 30)) {
  throw "Service did not stop within 30 seconds. Check $script:AxonHubLogDir\service.log"
}
Write-Success "Service '$($script:AxonHubServiceName)' is stopped."
