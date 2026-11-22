# Reads proxy and root CA settings from the Windows host (phase 0 prototype).
# Dot-source this file. Nothing here writes to the host.

function ConvertFrom-WinInetProxy {
  <#
    Converts WinINET values to a proxy URL and a NO_PROXY list.
      ProxyServer:   "host:port" or "http=host:port;https=host:port;..."
      ProxyOverride: "<local>;*.example.com;10.*"
  #>
  param([Parameter(Mandatory)][string]$ProxyServer, [string]$ProxyOverride)
  $server = $ProxyServer.Trim()
  if ($server -match "=") {
    $map = @{}
    foreach ($part in $server -split ";") {
      $kv = $part -split "=", 2
      if ($kv.Count -eq 2) { $map[$kv[0].Trim().ToLowerInvariant()] = $kv[1].Trim() }
    }
    $server = @($map["https"], $map["http"]) | Where-Object { $_ } | Select-Object -First 1
  }
  $proxy = $null
  if ($server) { $proxy = if ($server -match "^[a-z]+://") { $server } else { "http://$server" } }

  $noProxy = @("localhost", "127.0.0.1", "::1")
  if ($ProxyOverride) {
    foreach ($entry in $ProxyOverride -split ";") {
      $entry = $entry.Trim()
      if (-not $entry -or $entry -eq "<local>") { continue }   # <local>: plain host names; covered by localhost only
      if ($entry -match "^\*\.") { $noProxy += $entry.Substring(1); continue }   # *.example.com -> .example.com
      if ($entry -match "^(\d+)\.(\d+|\*)\.(\d+|\*)\.\*$") {
        # 10.* style wildcards -> CIDR, which Docker and most tools understand.
        $octets = $entry -split "\."
        $fixed = @($octets | Where-Object { $_ -ne "*" })
        $noProxy += ("{0}/{1}" -f (($fixed + @("0") * (4 - $fixed.Count)) -join "."), (8 * $fixed.Count))
        continue
      }
      if ($entry -match "^(\d+)\.\*$") { $noProxy += "$($Matches[1]).0.0.0/8"; continue }
      $noProxy += $entry
    }
  }
  return [pscustomobject]@{ Proxy = $proxy; NoProxy = (($noProxy | Select-Object -Unique) -join ",") }
}

function Get-HostProxySettings {
  <#
    Returns the user's WinINET proxy (Settings > Network > Proxy) as
    [pscustomobject]@{ Proxy; NoProxy; Note }. Environment variables win if set.
    PAC files (AutoConfigURL) are not evaluated; a note is returned instead.
  #>
  $result = [pscustomobject]@{ Proxy = $null; NoProxy = $null; Note = $null }

  $envProxy = @($env:HTTPS_PROXY, $env:https_proxy, $env:HTTP_PROXY, $env:http_proxy) | Where-Object { $_ } | Select-Object -First 1
  if ($envProxy) {
    $result.Proxy = $envProxy
    $result.NoProxy = @($env:NO_PROXY, $env:no_proxy) | Where-Object { $_ } | Select-Object -First 1
    return $result
  }

  $key = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings"
  $settings = Get-ItemProperty -Path $key -ErrorAction SilentlyContinue
  if (-not $settings) { return $result }

  if ($settings.PSObject.Properties["AutoConfigURL"] -and $settings.AutoConfigURL) {
    $result.Note = "A proxy auto-config (PAC) script is configured; it is not evaluated. Pass -Proxy explicitly."
  }
  if ($settings.PSObject.Properties["ProxyEnable"] -and $settings.ProxyEnable -eq 1 -and $settings.ProxyServer) {
    $override = if ($settings.PSObject.Properties["ProxyOverride"]) { $settings.ProxyOverride } else { $null }
    $converted = ConvertFrom-WinInetProxy -ProxyServer $settings.ProxyServer -ProxyOverride $override
    $result.Proxy = $converted.Proxy
    $result.NoProxy = $converted.NoProxy
  }
  return $result
}

function ConvertTo-Pem([System.Security.Cryptography.X509Certificates.X509Certificate2]$Certificate) {
  $base64 = [Convert]::ToBase64String($Certificate.RawData, "InsertLineBreaks")
  return "-----BEGIN CERTIFICATE-----`n$($base64 -replace "`r`n", "`n")`n-----END CERTIFICATE-----`n"
}

function Export-HostRootCertificates {
  <#
    Writes the trusted root certificates of the current user and the machine as PEM
    files (*.crt) into -Destination, and returns how many were written.
    This includes the public roots that Alpine already trusts; duplicates are harmless.
  #>
  param([Parameter(Mandatory)][string]$Destination)
  New-Item -ItemType Directory -Force $Destination | Out-Null
  $seen = @{}
  $count = 0
  foreach ($store in "Cert:\LocalMachine\Root", "Cert:\CurrentUser\Root") {
    foreach ($cert in Get-ChildItem $store) {
      if ($seen.ContainsKey($cert.Thumbprint)) { continue }
      if ($cert.NotAfter -lt (Get-Date)) { continue }
      $seen[$cert.Thumbprint] = $true
      $path = Join-Path $Destination ("host-{0}.crt" -f $cert.Thumbprint.ToLowerInvariant())
      [IO.File]::WriteAllText($path, (ConvertTo-Pem $cert), [Text.UTF8Encoding]::new($false))
      $count++
    }
  }
  return $count
}
