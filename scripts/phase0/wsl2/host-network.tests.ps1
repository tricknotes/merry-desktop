# Checks ConvertFrom-WinInetProxy with typical WinINET values. Run:
#   powershell -ExecutionPolicy Bypass -File host-network.tests.ps1
$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "host-network.ps1")

$cases = @(
  @{ Server = "proxy.example.com:8080"; Override = $null;
     Proxy = "http://proxy.example.com:8080"; NoProxy = "localhost,127.0.0.1,::1" },
  @{ Server = "http=p1.example.com:8080;https=p2.example.com:8443;ftp=f.example.com:21"; Override = "";
     Proxy = "http://p2.example.com:8443"; NoProxy = "localhost,127.0.0.1,::1" },
  @{ Server = "http=p1.example.com:8080"; Override = "<local>";
     Proxy = "http://p1.example.com:8080"; NoProxy = "localhost,127.0.0.1,::1" },
  @{ Server = "proxy.corp:3128"; Override = "<local>;*.corp.example;10.*;192.168.*.*;intranet";
     Proxy = "http://proxy.corp:3128"; NoProxy = "localhost,127.0.0.1,::1,.corp.example,10.0.0.0/8,192.168.0.0/16,intranet" },
  @{ Server = "socks=s.example.com:1080"; Override = $null;
     Proxy = $null; NoProxy = "localhost,127.0.0.1,::1" }
)

$failed = 0
foreach ($case in $cases) {
  $actual = ConvertFrom-WinInetProxy -ProxyServer $case.Server -ProxyOverride $case.Override
  $ok = ($actual.Proxy -eq $case.Proxy) -and ($actual.NoProxy -eq $case.NoProxy)
  if (-not $ok) { $failed++ }
  "{0}  {1} | {2}" -f ($(if ($ok) { "PASS" } else { "FAIL" })), $case.Server, $case.Override
  if (-not $ok) { "      got:  $($actual.Proxy) / $($actual.NoProxy)"; "      want: $($case.Proxy) / $($case.NoProxy)" }
}
"host proxy (read-only): " + ((Get-HostProxySettings | Select-Object @{n="Proxy";e={ if ($_.Proxy) { "(set)" } else { "(none)" } }}, Note) | Out-String).Trim()
if ($failed) { throw "$failed case(s) failed" }
