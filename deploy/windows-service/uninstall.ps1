[CmdletBinding()]
param()

. (Join-Path $PSScriptRoot 'service-common.ps1')
Ensure-Administrator "-NoProfile -ExecutionPolicy Bypass -File `"$PSCommandPath`""

$service = Get-AxonHubService
if (-not $service) {
  Write-Warn "Service '$($script:AxonHubServiceName)' is not installed."
  exit 0
}
if ($service.Status -ne 'Stopped') {
  Write-Info "Stopping service '$($script:AxonHubServiceName)'..."
  Stop-Service -Name $script:AxonHubServiceName -Force
  [void](Wait-ServiceState 'Stopped')
}

Invoke-Sc @('delete', $script:AxonHubServiceName)
Write-Success "Service '$($script:AxonHubServiceName)' was removed."
Write-Info "Binaries, config and logs were kept under $script:AxonHubStateRoot."
