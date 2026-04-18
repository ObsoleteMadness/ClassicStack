$ErrorActionPreference = 'Stop'

$packages = go list ./... | Where-Object { $_ -notmatch '(^|/)(dist|icon|icons)($|/)' }
if (-not $packages) {
    throw 'No packages found to test'
}

go test $packages
