[CmdletBinding()]
param()

. (Join-Path $PSScriptRoot 'service-common.ps1')
Ensure-Administrator "-NoProfile -ExecutionPolicy Bypass -File `"$PSCommandPath`""

$service = Get-AxonHubService
if (-not $service) {
  throw "Service '$($script:AxonHubServiceName)' is not installed. Run install.bat first."
}
if ($service.Status -eq 'Running') {
  Write-Warn "Service '$($script:AxonHubServiceName)' is already running."
  exit 0
}

Write-Info "Starting service '$($script:AxonHubServiceName)'..."
Start-Service -Name $script:AxonHubServiceName
if (-not (Wait-ServiceState 'Running' 30)) {
  throw "Service did not reach Running state. Check $script:AxonHubLogDir\service.log"
}
Write-Success "Service '$($script:AxonHubServiceName)' is running."
