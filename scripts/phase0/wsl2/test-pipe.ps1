<#
.SYNOPSIS
  Sends raw HTTP requests to the Docker Engine API over the relay's named pipe.
  Checks the relay without a Windows docker CLI.
#>
param(
  [string]$PipeName = "phase0-engine-docker",
  [string[]]$Path = @("/_ping", "/version")
)
$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Core

foreach ($p in $Path) {
  $sw = [Diagnostics.Stopwatch]::StartNew()
  $client = New-Object System.IO.Pipes.NamedPipeClientStream(".", $PipeName, [System.IO.Pipes.PipeDirection]::InOut)
  $client.Connect(10000)
  # HTTP/1.0 so that the engine closes the connection after the response.
  $request = [Text.Encoding]::ASCII.GetBytes("GET $p HTTP/1.0`r`nHost: docker`r`n`r`n")
  $client.Write($request, 0, $request.Length)
  $client.Flush()
  $reader = New-Object System.IO.StreamReader($client)
  $response = $reader.ReadToEnd()
  $client.Dispose()
  $status = ($response -split "`r`n")[0]
  $body = ($response -split "`r`n`r`n", 2)[1]
  if ($body.Length -gt 160) { $body = $body.Substring(0, 160) + "..." }
  "{0,-10} {1} ({2:N0} ms) {3}" -f $p, $status, $sw.Elapsed.TotalMilliseconds, $body
}
