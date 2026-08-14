$ErrorActionPreference = 'Stop'

$script:AxonHubServiceName = 'AxonHub'
$script:AxonHubProjectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$script:AxonHubStateRoot = Join-Path $script:AxonHubProjectRoot '.agent\windows-service'
$script:AxonHubServiceExe = Join-Path $script:AxonHubStateRoot 'axonhub-service.exe'
$script:AxonHubBackendExe = Join-Path $script:AxonHubStateRoot 'axonhub.exe'
$script:AxonHubConfigFile = Join-Path $script:AxonHubStateRoot 'axonhub-service.json'
$script:AxonHubLogDir = Join-Path $script:AxonHubStateRoot 'logs'

function Write-Info([string]$Message) { Write-Host "[AxonHub] $Message" -ForegroundColor Cyan }
function Write-Success([string]$Message) { Write-Host "[AxonHub] $Message" -ForegroundColor Green }
function Write-Warn([string]$Message) { Write-Host "[AxonHub] $Message" -ForegroundColor Yellow }
function Write-ErrorMessage([string]$Message) { Write-Host "[AxonHub] ERROR: $Message" -ForegroundColor Red }

function Test-IsAdministrator {
  $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
  $principal = [Security.Principal.WindowsPrincipal]::new($identity)
  return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Ensure-Administrator([string]$Arguments) {
  if (Test-IsAdministrator) { return }

  Write-Info 'Administrator permission is required; requesting elevation...'
  $elevated = Start-Process -FilePath 'powershell.exe' -Verb RunAs -ArgumentList $Arguments -Wait -PassThru
  exit $elevated.ExitCode
}

function Ensure-ServiceDirectories {
  New-Item -ItemType Directory -Force -Path $script:AxonHubStateRoot | Out-Null
  New-Item -ItemType Directory -Force -Path $script:AxonHubLogDir | Out-Null
}

function Get-AxonHubService {
  return Get-Service -Name $script:AxonHubServiceName -ErrorAction SilentlyContinue
}

function Wait-ServiceState([string]$DesiredState, [int]$TimeoutSeconds = 30) {
  $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
  do {
    $service = Get-AxonHubService
    if ($service -and $service.Status.ToString() -eq $DesiredState) { return $true }
    Start-Sleep -Milliseconds 500
  } while ((Get-Date) -lt $deadline)
  return $false
}

function Invoke-Sc([string[]]$Arguments) {
  & sc.exe @Arguments
  if ($LASTEXITCODE -ne 0) {
    throw "sc.exe failed with exit code ${LASTEXITCODE}: $($Arguments -join ' ')"
  }
}
