$ErrorActionPreference = 'Stop'

$buildVersion = if ($env:BUILD_VERSION) { $env:BUILD_VERSION } else { '0.0.0-dev' }
$buildCommit = if ($env:BUILD_COMMIT) { $env:BUILD_COMMIT } else { (git rev-parse --short=12 HEAD).Trim() }
$buildDate = if ($env:BUILD_DATE) { $env:BUILD_DATE } else { [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ') }
$buildVariant = if ($env:BUILD_VARIANT) { $env:BUILD_VARIANT } else { 'all' }

switch ($buildVariant) {
    'all'    { $tags = 'all' }
    'router' { $tags = '' }
    default  { throw "Unsupported BUILD_VARIANT: $buildVariant (expected: all|router)" }
}

if ($env:OUTPUT) {
    $output = $env:OUTPUT
} elseif ($buildVariant -eq 'all') {
    $output = 'out/omnitalk.exe'
} else {
    $output = "out/omnitalk-$buildVariant.exe"
}

$versionForRc = '0.0.0.0'
if ($buildVersion -match '^([0-9]+)\.([0-9]+)\.([0-9]+)(?:[-+].*)?$') {
    $versionForRc = "$($Matches[1]).$($Matches[2]).$($Matches[3]).0"
}
$parts = $versionForRc.Split('.')
$major = [int]$parts[0]
$minor = [int]$parts[1]
$patch = [int]$parts[2]
$build = [int]$parts[3]

$exeName = Split-Path -Leaf $output
$descriptionSuffix = if ($buildVariant -eq 'all') { '' } else { " ($buildVariant)" }

@"
{
  "StringFileInfo": {
    "Comments": "OmniTalk",
    "CompanyName": "ObsoleteMadness",
    "FileDescription": "OmniTalk AppleTalk Router$descriptionSuffix",
    "FileVersion": "$buildVersion",
    "InternalName": "omnitalk",
    "LegalCopyright": "GPL-3.0",
    "OriginalFilename": "$exeName",
    "ProductName": "OmniTalk",
    "ProductVersion": "$buildVersion"
  },
  "FixedFileInfo": {
    "FileVersion": {
      "Major": $major,
      "Minor": $minor,
      "Patch": $patch,
      "Build": $build
    },
    "ProductVersion": {
      "Major": $major,
      "Minor": $minor,
      "Patch": $patch,
      "Build": $build
    },
    "FileFlagsMask": "3f",
    "FileFlags": "00",
    "FileOS": "040004",
    "FileType": "01",
    "FileSubType": "00"
  },
  "IconPath": "../../icons/omnitalk.ico"
}
"@ | Set-Content -Path cmd/omnitalk/versioninfo.json -NoNewline

if (-not (Get-Command goversioninfo -ErrorAction SilentlyContinue)) {
    go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
}
Push-Location cmd/omnitalk
goversioninfo -64
Pop-Location

$parent = Split-Path -Parent $output
if ($parent) {
    New-Item -Path $parent -ItemType Directory -Force | Out-Null
}

$ldflags = "-s -w -X main.BuildVersion=$buildVersion -X main.BuildCommit=$buildCommit -X main.BuildDate=$buildDate"

if ($tags) {
    go build -trimpath -tags $tags -ldflags $ldflags -o $output ./cmd/omnitalk
} else {
    go build -trimpath -ldflags $ldflags -o $output ./cmd/omnitalk
}
