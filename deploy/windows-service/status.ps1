[CmdletBinding()]
param()

. (Join-Path $PSScriptRoot 'service-common.ps1')

$service = Get-AxonHubService
if (-not $service) {
  Write-Warn "Service '$($script:AxonHubServiceName)' is not installed."
  exit 1
}

Write-Host "Name:        $($service.Name)"
Write-Host "DisplayName: $($service.DisplayName)"
Write-Host "Status:      $($service.Status)"
Write-Host "StartType:   $($service.StartType)"
Write-Host "Config:      $script:AxonHubConfigFile"
Write-Host "Logs:        $script:AxonHubLogDir"

if (Test-Path -LiteralPath $script:AxonHubConfigFile) {
  try {
    $config = Get-Content -Raw -LiteralPath $script:AxonHubConfigFile | ConvertFrom-Json
    Write-Host "Backend:     $($config.backend)"
    if ($config.frontend) { Write-Host "Frontend:    $($config.frontend)" }
  } catch {
    Write-Warn "Could not parse service config: $($_.Exception.Message)"
  }
}
