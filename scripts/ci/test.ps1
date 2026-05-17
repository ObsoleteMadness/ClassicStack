$ErrorActionPreference = 'Stop'

$tagSets = @(
    ''
    'afp'
    'afp macgarden'
    'afp macip'
    'afp macgarden macip'
    'afp sqlite_cnid'
    'all'
    'ipx netbeui smb'
)

foreach ($tags in $tagSets) {
    Write-Host "=== go test -tags `"$tags`" ==="
    $packages = & go list -tags $tags ./... | Where-Object { $_ -notmatch '(^|/)(dist|icon|icons)($|/)' }
    if (-not $packages) {
        throw "No packages found to test for tags=`"$tags`""
    }
    & go test -tags $tags $packages
    if ($LASTEXITCODE -ne 0) {
        throw "go test failed for tags=`"$tags`""
    }
}
