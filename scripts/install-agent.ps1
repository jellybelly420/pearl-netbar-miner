param(
  [Parameter(Mandatory=$true)][string]$AgentExe,
  [Parameter(Mandatory=$true)][string]$ConfigPath
)

$ErrorActionPreference = "Stop"
$agent = (Resolve-Path $AgentExe).Path
$config = (Resolve-Path $ConfigPath).Path
$workingDirectory = Split-Path -Parent $agent
$taskName = "PearlNetbarAgent"
$action = New-ScheduledTaskAction -Execute $agent -Argument "-config `"$config`"" -WorkingDirectory $workingDirectory
$trigger = New-ScheduledTaskTrigger -AtLogOn
$settings = New-ScheduledTaskSettingsSet -RestartCount 5 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit (New-TimeSpan -Days 3650)

Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Settings $settings -Description "Owner-authorized Pearl net-cafe mining agent" -Force | Out-Null
Start-ScheduledTask -TaskName $taskName
Write-Host "Installed and started $taskName"

