$ErrorActionPreference = "Stop"
$taskName = "PearlNetbarAgent"

if (Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue) {
  Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
  Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
  Write-Host "Removed $taskName"
} else {
  Write-Host "$taskName was not installed"
}

