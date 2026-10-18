param(
  [Parameter(Mandatory=$true)][string]$InstallDirectory
)

$ErrorActionPreference = "Stop"
$directory = (Resolve-Path $InstallDirectory).Path
$status = Get-MpComputerStatus -ErrorAction SilentlyContinue
$detections = @(Get-MpThreatDetection -ErrorAction SilentlyContinue | Where-Object {
  ($_.Resources -join " ") -like "*$directory*"
} | Select-Object -First 20 InitialDetectionTime,ThreatID,ActionSuccess,Resources)

[ordered]@{
  checked_at = (Get-Date).ToUniversalTime().ToString("o")
  install_directory = $directory
  defender_available = [bool]$status
  antivirus_enabled = if ($status) { $status.AntivirusEnabled } else { $null }
  real_time_protection_enabled = if ($status) { $status.RealTimeProtectionEnabled } else { $null }
  signature_last_updated = if ($status) { $status.AntivirusSignatureLastUpdated } else { $null }
  matching_detections = $detections
  note = "Diagnostic only. This script does not create antivirus exclusions or restore quarantined files."
} | ConvertTo-Json -Depth 8
