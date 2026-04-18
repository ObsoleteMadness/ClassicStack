$ErrorActionPreference = 'Stop'

$releaseTag = if ($env:RELEASE_TAG) { $env:RELEASE_TAG } else { 'dev-local' }
$stage = "release/omnitalk-$releaseTag-windows-amd64"
$archiveName = "omnitalk-$releaseTag-windows-amd64.zip"

New-Item -ItemType Directory -Path $stage -Force | Out-Null
Copy-Item out/omnitalk.exe "$stage/omnitalk.exe"
Copy-Item README.md,server.ini.example,extmap.conf $stage
Copy-Item dist "$stage/dist" -Recurse
Compress-Archive -Path $stage -DestinationPath $archiveName -Force

$archiveName
