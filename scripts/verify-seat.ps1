param(
  [Parameter(Mandatory=$true)][string]$AgentExe,
  [Parameter(Mandatory=$true)][string]$ConfigPath
)

$ErrorActionPreference = "Stop"
$agent = (Resolve-Path $AgentExe).Path
$configFile = (Resolve-Path $ConfigPath).Path
$config = Get-Content -Raw -Path $configFile | ConvertFrom-Json
$poolParts = $config.mining.pool -split ":"
$poolHost = ($poolParts[0..($poolParts.Length - 2)] -join ":")
$poolPort = [int]$poolParts[-1]

$result = [ordered]@{
  checked_at = (Get-Date).ToUniversalTime().ToString("o")
  computer = $env:COMPUTERNAME
  agent_exists = (Test-Path $agent)
  config_exists = (Test-Path $configFile)
  scheduled_task = [bool](Get-ScheduledTask -TaskName "PearlNetbarAgent" -ErrorAction SilentlyContinue)
  pool = $config.mining.pool
  pool_tcp_reachable = $false
  gpus = @()
  nvidia_smi_available = [bool](Get-Command nvidia-smi -ErrorAction SilentlyContinue)
  dry_run_ok = $false
  dry_run_output = ""
}

$result.gpus = @(Get-CimInstance Win32_VideoController | ForEach-Object {
  [ordered]@{ name = $_.Name; driver_version = $_.DriverVersion; status = $_.Status }
})

try {
  $result.pool_tcp_reachable = (Test-NetConnection -ComputerName $poolHost -Port $poolPort -InformationLevel Quiet -WarningAction SilentlyContinue)
} catch {
  $result.pool_tcp_reachable = $false
}

$dryRun = & $agent -config $configFile -dry-run -once 2>&1 | Out-String
$result.dry_run_output = $dryRun.Trim()
$result.dry_run_ok = ($LASTEXITCODE -eq 0 -and $dryRun -match '"config_valid": true')

$result | ConvertTo-Json -Depth 6
if (-not $result.dry_run_ok -or -not $result.pool_tcp_reachable) {
  exit 1
}
