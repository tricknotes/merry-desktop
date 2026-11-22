<#
.SYNOPSIS
  Measures the resident memory of a phase 0 spike after it has been idle.

.DESCRIPTION
  Starts the app, waits for the idle period, then sums memory over the app process
  and all of its descendants (Electron helpers, msedgewebview2.exe, ...).
  Reports both Working Set (what Task Manager shows by default) and Private Bytes.
  The settings window is not opened unless -OpenWindowPrompt is given, in which case
  the script waits for you to open it from the tray menu before the idle period starts.

.EXAMPLE
  pwsh scripts/phase0/measure-memory.ps1 -Exe .\SpikeTauri\spike-tauri.exe -Label tauri
#>
param(
  [Parameter(Mandatory)][string]$Exe,
  [Parameter(Mandatory)][string]$Label,
  [int]$IdleSeconds = 300,
  [int]$SampleCount = 5,
  [switch]$OpenWindowPrompt,
  [string]$OutFile
)

$ErrorActionPreference = "Stop"

function Get-ProcessTree([int]$RootId) {
  $all = Get-CimInstance Win32_Process | Select-Object ProcessId, ParentProcessId, Name
  $ids = [System.Collections.Generic.HashSet[int]]::new()
  [void]$ids.Add($RootId)
  do {
    $added = $false
    foreach ($p in $all) {
      if ($ids.Contains([int]$p.ParentProcessId) -and $ids.Add([int]$p.ProcessId)) { $added = $true }
    }
  } while ($added)
  return $all | Where-Object { $ids.Contains([int]$_.ProcessId) }
}

function Measure-Tree([int]$RootId) {
  $ws = 0L; $private = 0L; $names = @()
  foreach ($p in Get-ProcessTree $RootId) {
    $proc = Get-Process -Id $p.ProcessId -ErrorAction SilentlyContinue
    if (-not $proc) { continue }
    $ws += $proc.WorkingSet64
    $private += $proc.PrivateMemorySize64
    $names += $proc.ProcessName
  }
  return [pscustomobject]@{ WorkingSet = $ws; Private = $private; Processes = $names }
}

$exePath = (Resolve-Path $Exe).Path
$proc = Start-Process -FilePath $exePath -PassThru
Write-Host "Started $Label (pid $($proc.Id))"

try {
  if ($OpenWindowPrompt) {
    Read-Host "Open the settings window from the tray menu, then press Enter"
  }
  Write-Host "Idling for $IdleSeconds s"
  Start-Sleep -Seconds $IdleSeconds
  if ($proc.HasExited) { throw "$Label exited early with code $($proc.ExitCode)" }

  # Take a few samples a second apart and keep the median, to smooth out GC spikes.
  $samples = 1..$SampleCount | ForEach-Object { Measure-Tree $proc.Id; Start-Sleep -Seconds 1 }
  $median = { param($values) ($values | Sort-Object)[[math]::Floor($values.Count / 2)] }
  $result = [ordered]@{
    label = $Label
    windowOpen = [bool]$OpenWindowPrompt
    idleSeconds = $IdleSeconds
    workingSetMB = [math]::Round((& $median ($samples.WorkingSet)) / 1MB, 1)
    privateMB = [math]::Round((& $median ($samples.Private)) / 1MB, 1)
    processCount = $samples[-1].Processes.Count
    processes = ($samples[-1].Processes | Group-Object | ForEach-Object { "$($_.Name) x$($_.Count)" })
    os = (Get-CimInstance Win32_OperatingSystem).Caption
    osBuild = [Environment]::OSVersion.Version.ToString()
  }
  $json = $result | ConvertTo-Json -Depth 3
  if ($OutFile) { Set-Content -Path $OutFile -Value $json }
  $json
} finally {
  # Stop the whole tree; helpers do not always exit with the root process.
  Get-ProcessTree $proc.Id | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
}
