<#
.SYNOPSIS
  Measures build times and output size of one phase 0 UI spike on Windows.

.DESCRIPTION
  Runs, in order:
    1. Removes dependency caches (npm, Cargo registry, Go module cache, Electron downloads)
       and previous build outputs.
    2. Fetches dependencies.                       -> depsSec
    3. Release build from that clean state.        -> cleanBuildSec
    4. Measures the output.                        -> sizeBytes, zipBytes
    5. Changes BENCH_REVISION in the backend, rebuilds. -> backendIncrSec
    6. Changes BENCH_REVISION in the frontend, rebuilds. -> frontendIncrSec
  The "incremental frontend" number is a release rebuild, not dev-server hot reload.
  The working tree is modified (BENCH_REVISION); run it on a throwaway checkout such as CI.

.EXAMPLE
  pwsh scripts/phase0/bench.ps1 -Candidate tauri -Run 1 -OutFile bench-tauri-1.json
#>
param(
  [Parameter(Mandatory)][ValidateSet("tauri", "electron", "wails")][string]$Candidate,
  [int]$Run = 1,
  [Parameter(Mandatory)][string]$OutFile,
  # Where to copy the built app (for the memory measurement). Optional.
  [string]$ArtifactDir
)

$ErrorActionPreference = "Stop"
$PSNativeCommandUseErrorActionPreference = $true

$repo = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
$spike = Join-Path $repo "spikes/$Candidate"

function Invoke-Timed([string]$Name, [scriptblock]$Block) {
  Write-Host "::group::$Candidate run $Run - $Name"
  $sw = [Diagnostics.Stopwatch]::StartNew()
  try {
    & $Block
  } finally {
    $sw.Stop()
    Write-Host "::endgroup::"
  }
  $sec = [math]::Round($sw.Elapsed.TotalSeconds, 2)
  Write-Host "$Name : $sec s"
  return $sec
}

function Remove-IfExists([string]$Path) {
  if (Test-Path $Path) { Remove-Item -Recurse -Force $Path }
}

function Set-BenchRevision([string]$File, [string]$Pattern, [string]$Replacement) {
  $text = Get-Content -Raw $File
  $updated = [regex]::Replace($text, $Pattern, $Replacement)
  if ($updated -eq $text) { throw "BENCH_REVISION marker not found in $File" }
  Set-Content -NoNewline -Path $File -Value $updated
}

# --- per-candidate definitions --------------------------------------------

switch ($Candidate) {
  "electron" {
    $deps = { Push-Location $spike; try { npm ci } finally { Pop-Location } }
    $build = { Push-Location $spike; try { npm run dist } finally { Pop-Location } }
    $outputs = @(Join-Path $spike "release/win-unpacked")
    $backendFile = Join-Path $spike "src/main/core.ts"
    $backendPattern = "BENCH_REVISION = \d+"
    $frontendFile = Join-Path $spike "src/renderer/src/main.ts"
    $clean = @("node_modules", "dist", "release") | ForEach-Object { Join-Path $spike $_ }
  }
  "tauri" {
    $deps = {
      Push-Location $spike
      try {
        npm ci
        Push-Location src-tauri; try { cargo fetch } finally { Pop-Location }
      } finally { Pop-Location }
    }
    $build = { Push-Location $spike; try { npx tauri build --no-bundle } finally { Pop-Location } }
    $outputs = @(Join-Path $spike "src-tauri/target/release/spike-tauri.exe")
    $backendFile = Join-Path $spike "src-tauri/src/core.rs"
    $backendPattern = "BENCH_REVISION: u32 = \d+"
    $frontendFile = Join-Path $spike "src/main.ts"
    $clean = @("node_modules", "dist", "src-tauri/target") | ForEach-Object { Join-Path $spike $_ }
  }
  "wails" {
    $deps = {
      Push-Location $spike
      try {
        go mod tidy
        go mod download
        Push-Location frontend; try { npm ci } finally { Pop-Location }
      } finally { Pop-Location }
    }
    $build = {
      Push-Location $spike
      try {
        Push-Location frontend; try { npm run build } finally { Pop-Location }
        go build -tags production -trimpath -buildvcs=false -ldflags="-w -s -H windowsgui" -o bin/SpikeWails.exe .
      } finally { Pop-Location }
    }
    $outputs = @(Join-Path $spike "bin/SpikeWails.exe")
    $backendFile = Join-Path $spike "core.go"
    $backendPattern = "BenchRevision = \d+"
    $frontendFile = Join-Path $spike "frontend/src/main.ts"
    $clean = @("frontend/node_modules", "frontend/dist", "bin") | ForEach-Object { Join-Path $spike $_ }
  }
}

# --- 1. clean state -------------------------------------------------------

Write-Host "Cleaning dependency caches and build outputs"
$clean | ForEach-Object { Remove-IfExists $_ }
npm cache clean --force
Remove-IfExists (Join-Path $env:LOCALAPPDATA "electron/Cache")
Remove-IfExists (Join-Path $env:LOCALAPPDATA "electron-builder/Cache")
$cargoHome = if ($env:CARGO_HOME) { $env:CARGO_HOME } else { Join-Path $env:USERPROFILE ".cargo" }
Remove-IfExists (Join-Path $cargoHome "registry")
Remove-IfExists (Join-Path $cargoHome "git")
if (Get-Command go -ErrorAction SilentlyContinue) {
  go clean -modcache
  go clean -cache
}

# --- 2-6. measure ---------------------------------------------------------

$depsSec = Invoke-Timed "deps" $deps
$cleanBuildSec = Invoke-Timed "clean build" $build

$files = $outputs | ForEach-Object { Get-ChildItem -Recurse -File $_ }
$sizeBytes = ($files | Measure-Object -Sum Length).Sum
$zip = Join-Path ([IO.Path]::GetTempPath()) "spike-$Candidate-$Run.zip"
Remove-IfExists $zip
Compress-Archive -Path $outputs -DestinationPath $zip -CompressionLevel Optimal
$zipBytes = (Get-Item $zip).Length
Remove-Item $zip

if ($ArtifactDir) {
  New-Item -ItemType Directory -Force $ArtifactDir | Out-Null
  $outputs | ForEach-Object { Copy-Item -Recurse -Force $_ $ArtifactDir }
}

Set-BenchRevision $backendFile $backendPattern ($backendPattern -replace '\\d\+', '1')
$backendIncrSec = Invoke-Timed "incremental build (backend)" $build

Set-BenchRevision $frontendFile "BENCH_REVISION = \d+" "BENCH_REVISION = 1"
$frontendIncrSec = Invoke-Timed "incremental build (frontend)" $build

$versions = [ordered]@{
  node = (node --version)
  npm = (npm --version)
}
if ($Candidate -eq "tauri") { $versions.rustc = (rustc --version); $versions.cargo = (cargo --version) }
if ($Candidate -eq "wails") { $versions.go = (go version) }

$result = [ordered]@{
  candidate = $Candidate
  run = $Run
  runner = "$env:RUNNER_OS $env:ImageOS $env:ImageVersion".Trim()
  cpu = (Get-CimInstance Win32_Processor | Select-Object -First 1).Name
  logicalCpus = [Environment]::ProcessorCount
  depsSec = $depsSec
  cleanBuildSec = $cleanBuildSec
  backendIncrSec = $backendIncrSec
  frontendIncrSec = $frontendIncrSec
  sizeBytes = $sizeBytes
  zipBytes = $zipBytes
  fileCount = @($files).Count
  versions = $versions
}
$result | ConvertTo-Json -Depth 4 | Set-Content -Path $OutFile
Get-Content $OutFile
