$ErrorActionPreference = 'Stop'

$releaseTag = if ($env:RELEASE_TAG) { $env:RELEASE_TAG } else { 'dev-local' }
$buildVariant = if ($env:BUILD_VARIANT) { $env:BUILD_VARIANT } else { 'all' }

if ($buildVariant -eq 'all') {
    $variantSlug = ''
    $exeName = 'classicstack.exe'
} else {
    $variantSlug = "-$buildVariant"
    $exeName = "classicstack-$buildVariant.exe"
}

$stage = "release/classicstack$variantSlug-$releaseTag-windows-amd64"
$archiveName = "classicstack$variantSlug-$releaseTag-windows-amd64.zip"

New-Item -ItemType Directory -Path $stage -Force | Out-Null
Copy-Item "out/$exeName" "$stage/$exeName"
# Ship the Windows service wrapper alongside the main binary when built.
if (Test-Path 'out/classicstack-svc.exe') {
    Copy-Item 'out/classicstack-svc.exe' "$stage/classicstack-svc.exe"
}
Copy-Item README.md,server.toml.example,extmap.conf $stage
Get-ChildItem -Path dist -Force | Copy-Item -Destination $stage -Recurse -Force
Compress-Archive -Path $stage -DestinationPath $archiveName -Force

$archiveName
