<#
.SYNOPSIS
  Relays a Windows named pipe to the Docker Engine socket in the engine distro (phase 0 prototype).

.DESCRIPTION
  Listens on \\.\pipe\<PipeName>. For each client connection it starts
    wsl.exe -d <Distro> -u root --exec socat STDIO UNIX-CONNECT:/var/run/docker.sock
  and copies bytes in both directions. Nothing listens on TCP.
  The pipe only accepts the current Windows user (and SYSTEM).

  Use it from the host docker CLI with:
    docker context create phase0-engine --docker host=npipe:////./pipe/<PipeName>

  This is a prototype to check the approach. The app will implement the relay in Go.

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File pipe-relay.ps1
#>
param(
  [string]$Distro = "phase0-engine",
  [string]$PipeName = "phase0-engine-docker",
  # Stop after this many connections (0 = run until Ctrl+C). Used by tests.
  [int]$MaxConnections = 0
)

$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Core

function New-PipeSecurity {
  $security = New-Object System.IO.Pipes.PipeSecurity
  $me = [System.Security.Principal.WindowsIdentity]::GetCurrent().User
  $system = New-Object System.Security.Principal.SecurityIdentifier "S-1-5-18"
  foreach ($sid in $me, $system) {
    $rule = New-Object System.IO.Pipes.PipeAccessRule($sid, "FullControl", "Allow")
    $security.AddAccessRule($rule)
  }
  return $security
}

# Copies one direction and flushes after every read. Stream.CopyToAsync does not flush,
# so a request can sit in the stdin buffer of wsl.exe and never reach the engine.
$pumpScript = {
  param($Source, $Destination)
  $buffer = New-Object byte[] 65536
  try {
    while (($n = $Source.Read($buffer, 0, $buffer.Length)) -gt 0) {
      $Destination.Write($buffer, 0, $n)
      $Destination.Flush()
    }
  } catch {
    # The other side went away; ending the pump is enough.
  }
}

function Start-Pump($Source, $Destination) {
  $ps = [powershell]::Create()
  [void]$ps.AddScript($pumpScript).AddArgument($Source).AddArgument($Destination)
  return [pscustomobject]@{ PowerShell = $ps; Handle = $ps.BeginInvoke() }
}

function Start-Relay([System.IO.Pipes.NamedPipeServerStream]$Pipe) {
  $psi = New-Object System.Diagnostics.ProcessStartInfo
  $psi.FileName = "wsl.exe"
  $psi.Arguments = "-d $Distro -u root --exec socat STDIO UNIX-CONNECT:/var/run/docker.sock"
  $psi.UseShellExecute = $false
  $psi.RedirectStandardInput = $true
  $psi.RedirectStandardOutput = $true
  $psi.CreateNoWindow = $true
  $proc = [System.Diagnostics.Process]::Start($psi)

  return [pscustomobject]@{
    Pipe = $Pipe
    Process = $proc
    ToEngine = Start-Pump $Pipe $proc.StandardInput.BaseStream
    ToClient = Start-Pump $proc.StandardOutput.BaseStream $Pipe
    StdinClosed = $false
  }
}

function Update-Relays([System.Collections.ArrayList]$Active) {
  # A relay ends when the engine closes the connection (socat exits, stdout hits EOF)
  # or when the client closes the pipe (then the engine side gets EOF on stdin).
  foreach ($relay in @($Active)) {
    if ($relay.ToEngine.Handle.IsCompleted -and -not $relay.StdinClosed) {
      try { $relay.Process.StandardInput.Close() } catch {}
      $relay.StdinClosed = $true
    }
    if ($relay.ToClient.Handle.IsCompleted) {
      try { $relay.Pipe.WaitForPipeDrain() } catch {}
      try { $relay.Pipe.Dispose() } catch {}
      try { if (-not $relay.Process.HasExited) { $relay.Process.Kill() } } catch {}
      foreach ($pump in $relay.ToEngine, $relay.ToClient) { try { $pump.PowerShell.Dispose() } catch {} }
      $Active.Remove($relay)
    }
  }
}

$security = New-PipeSecurity
$active = New-Object System.Collections.ArrayList
$served = 0
Write-Host "Relaying \\.\pipe\$PipeName -> ${Distro}:/var/run/docker.sock (Ctrl+C to stop)"

try {
  while ($MaxConnections -eq 0 -or $served -lt $MaxConnections) {
    $pipe = New-Object System.IO.Pipes.NamedPipeServerStream(
      $PipeName, [System.IO.Pipes.PipeDirection]::InOut,
      [System.IO.Pipes.NamedPipeServerStream]::MaxAllowedServerInstances,
      [System.IO.Pipes.PipeTransmissionMode]::Byte, [System.IO.Pipes.PipeOptions]::Asynchronous,
      65536, 65536, $security)
    $connect = $pipe.WaitForConnectionAsync()
    # Keep finishing old relays while waiting for the next client.
    while (-not $connect.Wait(100)) { Update-Relays $active }
    $served++
    [void]$active.Add((Start-Relay $pipe))
    Update-Relays $active
  }
  $deadline = (Get-Date).AddSeconds(30)
  while ($active.Count -gt 0 -and (Get-Date) -lt $deadline) {
    Start-Sleep -Milliseconds 100
    Update-Relays $active
  }
} finally {
  foreach ($relay in @($active)) {
    try { $relay.Pipe.Dispose() } catch {}
    try { if (-not $relay.Process.HasExited) { $relay.Process.Kill() } } catch {}
  }
}
