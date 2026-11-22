<#
.SYNOPSIS
  Creates a dedicated WSL2 distro that runs Docker Engine (phase 0 prototype).

.DESCRIPTION
  1. Downloads the Alpine minirootfs and checks its SHA-256.
  2. Imports it as a WSL2 distro (wsl --import).
  3. Runs provision.sh inside it: proxy and CA, packages, Docker Engine static binaries,
     Docker Compose, and /etc/wsl.conf with a [boot] command that starts dockerd.
  4. Restarts the distro and waits until the engine answers.

  The proxy and the CA certificates come from the parameters, or from the host settings
  with -UseHostProxy / -ImportHostRootCerts. The proxy value is passed through WSLENV
  (not the command line) and is never printed.

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File new-engine-distro.ps1
.EXAMPLE
  powershell -ExecutionPolicy Bypass -File new-engine-distro.ps1 -UseHostProxy -ImportHostRootCerts -Force
#>
param(
  [string]$Name = "phase0-engine",
  [string]$InstallRoot = (Join-Path $env:LOCALAPPDATA "phase0-engine"),
  [string]$AlpineVersion = "3.24.4",
  [string]$AlpineSha256 = "cdaa38cb8de3537967ee5b6c9b6cd00cc962da2b018e6eea48696bb274923012",
  [string]$DockerVersion = "29.10.0",
  [string]$ComposeVersion = "v5.8.0",
  [string]$Proxy,
  [string]$NoProxy,
  [switch]$UseHostProxy,
  [string[]]$CaFile = @(),
  [switch]$ImportHostRootCerts,
  # Replace an existing distro with the same name. Its disk and images are deleted.
  [switch]$Force
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version 3
. (Join-Path $PSScriptRoot "host-network.ps1")

function Invoke-Wsl {
  # wsl.exe writes UTF-16 for its own messages; run commands and fail on a non-zero exit.
  & wsl.exe @args
  if ($LASTEXITCODE -ne 0) { throw "wsl.exe $($args -join ' ') failed with exit code $LASTEXITCODE" }
}

function Copy-ToDistro([string]$DistroName, [string]$Source, [string]$Target) {
  # Sends a file through wsl.exe arguments (base64), so that it works without /mnt/c
  # (automount can be disabled by policy) and without a shared folder.
  $b64 = [Convert]::ToBase64String([IO.File]::ReadAllBytes($Source))
  $dir = $Target.Substring(0, $Target.LastIndexOf("/"))
  Invoke-Wsl -d $DistroName -u root --exec mkdir -p $dir
  Invoke-Wsl -d $DistroName -u root --exec sh -c ': > $0.b64' $Target
  for ($offset = 0; $offset -lt $b64.Length; $offset += 24000) {
    $chunk = $b64.Substring($offset, [Math]::Min(24000, $b64.Length - $offset))
    # base64 has no spaces or glob characters, so $0 can stay unquoted.
    Invoke-Wsl -d $DistroName -u root --exec sh -c 'printf %s $0 >> $1.b64' $chunk $Target
  }
  Invoke-Wsl -d $DistroName -u root --exec sh -c 'base64 -d $0.b64 > $0 && rm $0.b64' $Target
}

function Test-DistroExists([string]$DistroName) {
  # WSL_UTF8 makes wsl.exe print UTF-8 instead of UTF-16; strip NULs in case it is ignored.
  $oldUtf8 = $env:WSL_UTF8
  $oldEncoding = [Console]::OutputEncoding
  try {
    $env:WSL_UTF8 = "1"
    [Console]::OutputEncoding = [Text.Encoding]::UTF8
    $names = & wsl.exe --list --quiet | ForEach-Object { ($_ -replace "`0", "").Trim() }
  } finally {
    $env:WSL_UTF8 = $oldUtf8
    [Console]::OutputEncoding = $oldEncoding
  }
  return [bool]($names | Where-Object { $_ -eq $DistroName })
}

$sw = [Diagnostics.Stopwatch]::StartNew()
function Step([string]$Message) { Write-Host ("[{0,6:N1}s] {1}" -f $sw.Elapsed.TotalSeconds, $Message) }

# --- 1. rootfs ----------------------------------------------------------------
$cache = Join-Path $InstallRoot "cache"
$setup = Join-Path $InstallRoot "setup"
$disk = Join-Path $InstallRoot "disk"
New-Item -ItemType Directory -Force $cache, $setup | Out-Null

$minor = ($AlpineVersion -split "\.")[0..1] -join "."
$rootfsName = "alpine-minirootfs-$AlpineVersion-x86_64.tar.gz"
$rootfs = Join-Path $cache $rootfsName
if (-not (Test-Path $rootfs)) {
  Step "downloading $rootfsName"
  $url = "https://dl-cdn.alpinelinux.org/alpine/v$minor/releases/x86_64/$rootfsName"
  $ProgressPreference = "SilentlyContinue"
  Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $rootfs
}
$actual = (Get-FileHash -Algorithm SHA256 $rootfs).Hash.ToLowerInvariant()
if ($actual -ne $AlpineSha256) {
  Remove-Item $rootfs
  throw "SHA-256 mismatch for $rootfsName (expected $AlpineSha256, got $actual)"
}

# --- 2. import ------------------------------------------------------------------
if (Test-DistroExists $Name) {
  if (-not $Force) { throw "WSL distro '$Name' already exists. Use -Force to replace it." }
  Step "unregistering existing distro $Name"
  Invoke-Wsl --unregister $Name
}
if (Test-Path $disk) { Remove-Item -Recurse -Force $disk }
Step "importing $Name"
Invoke-Wsl --import $Name $disk $rootfs --version 2

# --- 3. provision -------------------------------------------------------------
Get-ChildItem $setup | Remove-Item -Recurse -Force
Copy-Item (Join-Path $PSScriptRoot "*.sh") $setup
$caDir = Join-Path $setup "ca"
New-Item -ItemType Directory -Force $caDir | Out-Null
$i = 0
foreach ($file in $CaFile) {
  $i++
  Copy-Item $file (Join-Path $caDir ("extra-{0:D2}.crt" -f $i))
}
if ($ImportHostRootCerts) {
  $count = Export-HostRootCertificates -Destination $caDir
  Step "exported $count host root certificate(s)"
}

$network = if ($UseHostProxy) { Get-HostProxySettings } else { [pscustomobject]@{ Proxy = $null; NoProxy = $null; Note = $null } }
if ($Proxy) { $network.Proxy = $Proxy }
if ($NoProxy) { $network.NoProxy = $NoProxy }
if ($network.Note) { Write-Warning $network.Note }

$env:ENGINE_NAME = $Name
$env:DOCKER_VERSION = $DockerVersion
$env:COMPOSE_VERSION = $ComposeVersion
$env:ENGINE_PROXY = if ($network.Proxy) { $network.Proxy } else { "" }
$env:ENGINE_NO_PROXY = if ($network.NoProxy) { $network.NoProxy } else { "" }
$env:ENGINE_CA_DIR = "/opt/engine-setup/ca"
$env:WSLENV = "ENGINE_NAME:DOCKER_VERSION:COMPOSE_VERSION:ENGINE_PROXY:ENGINE_NO_PROXY:ENGINE_CA_DIR"

Step "copying setup files into $Name"
Get-ChildItem $setup -File | ForEach-Object { Copy-ToDistro $Name $_.FullName "/opt/engine-setup/$($_.Name)" }
Invoke-Wsl -d $Name -u root --exec mkdir -p /opt/engine-setup/ca
Get-ChildItem $caDir -File | ForEach-Object { Copy-ToDistro $Name $_.FullName "/opt/engine-setup/ca/$($_.Name)" }

Step "provisioning (packages, Docker Engine $DockerVersion, Compose $ComposeVersion)"
Invoke-Wsl -d $Name -u root --exec sh /opt/engine-setup/provision.sh
Remove-Item Env:ENGINE_PROXY -ErrorAction SilentlyContinue

# --- 4. restart and wait ------------------------------------------------------
Step "restarting $Name so that the [boot] command starts the engine"
Invoke-Wsl --terminate $Name
Invoke-Wsl -d $Name -u root --exec true
$deadline = (Get-Date).AddSeconds(60)
$ready = $false
# Windows PowerShell turns native stderr into errors; relax that while polling.
$ErrorActionPreference = "Continue"
do {
  & wsl.exe -d $Name -u root --exec docker version --format "{{.Server.Version}}" 2>&1 | Out-Null
  if ($LASTEXITCODE -eq 0) { $ready = $true; break }
  Start-Sleep -Milliseconds 500
} while ((Get-Date) -lt $deadline)
$ErrorActionPreference = "Stop"
if (-not $ready) { throw "Docker Engine did not respond within 60 s. See /var/log/dockerd.log in $Name." }
Step "engine answered"

$server = (& wsl.exe -d $Name -u root --exec docker version --format "{{.Server.Version}}").Trim()
Step "Docker Engine $server is running in $Name"
Write-Host "Socket for other WSL distros: unix:///mnt/wsl/$Name/docker.sock"
