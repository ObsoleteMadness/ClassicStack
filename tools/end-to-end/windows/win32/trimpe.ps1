# Trim the trailing zero padding the MSVC 1.2 LINK32 appends to a PE image.
#
# This early NT linker writes the whole in-memory image size to the file,
# padding with zeros far past the last section's raw data (a ~40 KB tool comes
# out as a 2 MB file). That padding is not referenced by any PE structure, so
# the file can be safely truncated to the end of the last section's raw data
# (rounded up to FileAlignment) — which is what this script does, so the .EXE
# fits the 1.44 MB floppy flopgen builds.
#
# Usage:  powershell -ExecutionPolicy Bypass -File trimpe.ps1 SMBE2E.EXE
param([Parameter(Mandatory=$true)][string]$Path)

$b = [System.IO.File]::ReadAllBytes($Path)
$pe = [BitConverter]::ToInt32($b, 0x3C)
if ($b[$pe] -ne 0x50 -or $b[$pe+1] -ne 0x45) { throw "not a PE: $Path" }

$nsec  = [BitConverter]::ToUInt16($b, $pe + 6)
$optsz = [BitConverter]::ToUInt16($b, $pe + 20)
$opt   = $pe + 24
$filealign = [BitConverter]::ToInt32($b, $opt + 36)
$sectbl = $opt + $optsz

$maxEnd = 0
for ($i = 0; $i -lt $nsec; $i++) {
    $e = $sectbl + $i * 40
    $rawsize = [BitConverter]::ToInt32($b, $e + 16)
    $rawptr  = [BitConverter]::ToInt32($b, $e + 20)
    $end = $rawptr + $rawsize
    if ($end -gt $maxEnd) { $maxEnd = $end }
}

if ($filealign -lt 1) { $filealign = 512 }
$cut = [int](([math]::Ceiling($maxEnd / $filealign)) * $filealign)

if ($cut -ge $b.Length) {
    Write-Output "trimpe: $Path already $($b.Length) bytes (<= $cut); no trim"
    exit 0
}

$fs = [System.IO.File]::OpenWrite($Path)
$fs.SetLength($cut)
$fs.Close()
Write-Output "trimpe: $Path trimmed $($b.Length) -> $cut bytes"
