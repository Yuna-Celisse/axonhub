[CmdletBinding()]
param(
  [switch]$BackendOnly,
  [switch]$SkipBuild,
  [switch]$Start
)

. (Join-Path $PSScriptRoot 'service-common.ps1')

$elevationArgs = "-NoProfile -ExecutionPolicy Bypass -File `"$PSCommandPath`""
if ($BackendOnly) { $elevationArgs += ' -BackendOnly' }
if ($SkipBuild) { $elevationArgs += ' -SkipBuild' }
if ($Start) { $elevationArgs += ' -Start' }
Ensure-Administrator $elevationArgs
Ensure-ServiceDirectories

if (-not $SkipBuild) {
  $go = Get-Command go.exe -ErrorAction SilentlyContinue
  if (-not $go) { throw 'Go was not found in PATH. Install Go or use -SkipBuild with existing binaries.' }

  Write-Info 'Building the AxonHub backend and Windows service host...'
  Push-Location $script:AxonHubProjectRoot
  try {
    & $go.Source build -o $script:AxonHubBackendExe ./cmd/axonhub
    if ($LASTEXITCODE -ne 0) { throw "backend build failed with exit code $LASTEXITCODE" }
    & $go.Source build -o $script:AxonHubServiceExe ./cmd/axonhub-service
    if ($LASTEXITCODE -ne 0) { throw "service host build failed with exit code $LASTEXITCODE" }
  } finally {
    Pop-Location
  }
}

if (-not (Test-Path -LiteralPath $script:AxonHubBackendExe -PathType Leaf)) {
  throw "Backend executable not found: $script:AxonHubBackendExe"
}
if (-not (Test-Path -LiteralPath $script:AxonHubServiceExe -PathType Leaf)) {
  throw "Service host executable not found: $script:AxonHubServiceExe"
}

$frontendRoot = Join-Path $script:AxonHubProjectRoot 'frontend'
$frontendCommand = $null
$frontendEnvironment = [ordered]@{}
if (-not $BackendOnly -and (Test-Path (Join-Path $frontendRoot 'package.json'))) {
  $pnpm = Get-Command pnpm.cmd -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($pnpm) {
    $frontendCommand = $pnpm.Source
    $frontendArgs = @('dev', '--host', '127.0.0.1')

    $node = Get-Command node.exe -ErrorAction SilentlyContinue | Select-Object -First 1
    $pathParts = @()
    if ($node) { $pathParts += [IO.Path]::GetDirectoryName($node.Source) }
    if ($frontendCommand) { $pathParts += [IO.Path]::GetDirectoryName($frontendCommand) }
    $pathParts += $env:PATH
    $frontendEnvironment['PATH'] = ($pathParts -join ';')
    $frontendEnvironment['VITE_PORT'] = if ($env:VITE_PORT) { $env:VITE_PORT } else { '5173' }
  } else {
    Write-Warn 'pnpm.cmd was not found; installing backend-only service.'
    $frontendArgs = @()
  }
} else {
  $frontendArgs = @()
}

$serviceConfig = [ordered]@{
  backend         = $script:AxonHubBackendExe
  backendArgs     = @()
  workDir         = $script:AxonHubProjectRoot
  logDir          = $script:AxonHubLogDir
  frontend        = $frontendCommand
  frontendArgs    = $frontendArgs
  frontendDir     = $frontendRoot
  environment     = $frontendEnvironment
  restartDelaySec = 5
}
$json = $serviceConfig | ConvertTo-Json -Depth 5
[IO.File]::WriteAllText($script:AxonHubConfigFile, $json, [Text.UTF8Encoding]::new($false))

$existing = Get-AxonHubService
if ($existing) {
  Write-Info "Removing existing service definition '$($script:AxonHubServiceName)'..."
  if ($existing.Status -ne 'Stopped') {
    Stop-Service -Name $script:AxonHubServiceName -Force
    [void](Wait-ServiceState 'Stopped')
  }
  Invoke-Sc @('delete', $script:AxonHubServiceName)
  Start-Sleep -Seconds 1
}

$binPath = "`"$script:AxonHubServiceExe`" -config `"$script:AxonHubConfigFile`""
Write-Info "Creating Windows service '$($script:AxonHubServiceName)'..."
Invoke-Sc @(
  'create', $script:AxonHubServiceName,
  "binPath= $binPath",
  'start= delayed-auto',
  'obj= LocalSystem',
  'DisplayName= AxonHub AI Gateway'
)
Invoke-Sc @('description', $script:AxonHubServiceName, 'AxonHub AI gateway backend and local frontend service')
Invoke-Sc @('failure', $script:AxonHubServiceName, 'reset= 86400', 'actions= restart/5000/restart/10000/restart/30000')
Invoke-Sc @('failureflag', $script:AxonHubServiceName, '1')

Write-Success "Service '$($script:AxonHubServiceName)' installed."
Write-Info "Config: $script:AxonHubConfigFile"
Write-Info "Logs:  $script:AxonHubLogDir"
if ($frontendCommand) {
  Write-Info 'Frontend mode: pnpm dev on http://localhost:5173'
} else {
  Write-Info 'Frontend mode: disabled (backend-only)'
}

if ($Start) {
  & (Join-Path $PSScriptRoot 'start.ps1')
}
