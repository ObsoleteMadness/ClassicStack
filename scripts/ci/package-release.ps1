$ErrorActionPreference = 'Stop'

$releaseTag = if ($env:RELEASE_TAG) { $env:RELEASE_TAG } else { 'dev-local' }
$stage = "release/omnitalk-$releaseTag-windows-amd64"
$archiveName = "omnitalk-$releaseTag-windows-amd64.zip"

New-Item -ItemType Directory -Path $stage -Force | Out-Null
Copy-Item out/omnitalk.exe "$stage/omnitalk.exe"
Copy-Item README.md,server.toml.example,extmap.conf $stage
Get-ChildItem -Path dist -Force | Copy-Item -Destination $stage -Recurse -Force
Compress-Archive -Path $stage -DestinationPath $archiveName -Force

$archiveName
