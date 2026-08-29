# Builds every ClassicStack Windows binary into ..\bin (the same convention
# scripts/build-local.sh uses on other platforms), then compiles the Inno
# Setup installer against them.
#
#   pwsh packaging/windows/build.ps1                    # build + compile, version 0.0.0-dev
#   pwsh packaging/windows/build.ps1 -Version 1.2.3      # stamp a real version
#   pwsh packaging/windows/build.ps1 -SkipInstaller      # just populate .\bin
#
# Requires: Go (with GOOS=windows support — cross-compiles fine from any
# host), bash (for scripts/ci/spa.sh, same as scripts/ci/build.ps1 already
# assumes), and, unless -SkipInstaller, ISCC.exe (Inno Setup 6) on PATH.
param(
    [string]$Version = "0.0.0-dev",
    [switch]$SkipInstaller
)
$ErrorActionPreference = 'Stop'

$root = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
Push-Location $root
try {
    $binDir = Join-Path $root 'bin'
    New-Item -Path $binDir -ItemType Directory -Force | Out-Null

    $buildCommit = try { (git rev-parse --short=12 HEAD).Trim() } catch { 'unknown' }
    $buildDate = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
    $ldflags = "-s -w -X main.BuildVersion=$Version -X main.BuildCommit=$buildCommit -X main.BuildDate=$buildDate"

    # `all` embeds the Vite SPA (adapter/control/http's web-admin UI); build it
    # if it isn't already there, same guard scripts/build-local.sh uses.
    $spaDir = Join-Path $root 'adapter\control\http\spa\assets'
    if (-not (Test-Path $spaDir) -or (Get-ChildItem $spaDir -ErrorAction SilentlyContinue).Count -eq 0) {
        Write-Host "build.ps1: building the SPA (make spa)"
        bash scripts/ci/spa.sh
    }

    # csmount mounts via WinFsp on Windows (no `fuse` tag — that's macFUSE/libfuse
    # only). classicstack-tray is Windows-only once its own build tags land; try
    # it and warn (not fail) if that hasn't happened yet.
    $targets = @('classicstack', 'classicstack-svc', 'csmount', 'csclient', 'csecho', `
                 'csgetzones', 'csipxping', 'csnbp', 'csncpinfo', 'csnetsend', 'csnetview')

    foreach ($target in $targets) {
        $out = Join-Path $binDir "$target.exe"
        Write-Host "build.ps1: building $target -> bin\$target.exe"
        go build -trimpath -tags all -ldflags $ldflags -o $out "./cmd/$target"
        if ($LASTEXITCODE -ne 0) { throw "go build ./cmd/$target failed" }
    }

    # classicstack-tray.exe is optional in the installer (#ifexist-guarded in
    # ClassicStack.iss) — build it too, but don't fail the whole build if a
    # future change temporarily breaks its Windows port; just ship without it.
    $trayOut = Join-Path $binDir 'classicstack-tray.exe'
    Write-Host "build.ps1: building classicstack-tray -> bin\classicstack-tray.exe"
    go build -trimpath -tags all -ldflags $ldflags -o $trayOut ./cmd/classicstack-tray 2>$null
    if ($LASTEXITCODE -ne 0) {
        Write-Warning "classicstack-tray failed to build for windows/amd64 — installer will ship without the tray task."
        Remove-Item $trayOut -ErrorAction SilentlyContinue
    }
} finally {
    Pop-Location
}

if ($SkipInstaller) { return }

$iscc = Get-Command ISCC.exe -ErrorAction SilentlyContinue
if (-not $iscc) {
    Write-Warning "ISCC.exe not found on PATH — install Inno Setup 6 (https://jrsoftware.org/isinfo.php) or rerun with -SkipInstaller."
    return
}

$issPath = Join-Path $PSScriptRoot 'ClassicStack.iss'
Write-Host "build.ps1: compiling installer (version $Version)"
& $iscc.Source "/DMyAppVersion=$Version" $issPath
if ($LASTEXITCODE -ne 0) { throw "ISCC compile failed" }
