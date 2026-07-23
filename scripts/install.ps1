[CmdletBinding()]
param(
    [string]$Version,
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA "dsmctl\bin"),
    [switch]$AddToPath
)

$ErrorActionPreference = "Stop"
$repository = "derekvery666/dsmctl"
$asset = "dsmctl-windows-amd64.zip"

$architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
if ($architecture -ne "X64") {
    throw "This preview installer currently supports Windows amd64 only; detected $architecture."
}

if ([string]::IsNullOrWhiteSpace($Version)) {
    $headers = @{ Accept = "application/vnd.github+json"; "User-Agent" = "dsmctl-installer" }
    $release = Invoke-RestMethod -Headers $headers -Uri "https://api.github.com/repos/$repository/releases/latest"
    $tag = [string]$release.tag_name
    if (-not $tag.StartsWith("dsmctl-v", [System.StringComparison]::Ordinal)) {
        throw "Unable to resolve the latest stable dsmctl release. Pass -Version for a prerelease."
    }
    $Version = $tag.Substring("dsmctl-v".Length)
}
else {
    $Version = $Version -replace '^dsmctl-v', '' -replace '^v', ''
    if ($Version -notmatch '^[0-9]+\.[0-9]+\.[0-9]+-[1-9][0-9]*$') {
        throw "Invalid version: $Version"
    }
    $tag = "dsmctl-v$Version"
}

$downloadBase = "https://github.com/$repository/releases/download/$tag"
$systemTemp = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$work = Join-Path $systemTemp ("dsmctl-install-" + [Guid]::NewGuid().ToString("N"))
$work = [IO.Path]::GetFullPath($work)
if (-not $work.StartsWith($systemTemp, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Refusing temporary path outside the system temporary directory: $work"
}

New-Item -ItemType Directory -Path $work | Out-Null
try {
    $archive = Join-Path $work $asset
    $checksums = Join-Path $work "SHA256SUMS"
    Invoke-WebRequest -UseBasicParsing -Uri "$downloadBase/$asset" -OutFile $archive
    Invoke-WebRequest -UseBasicParsing -Uri "$downloadBase/SHA256SUMS" -OutFile $checksums

    $checksumText = Get-Content -LiteralPath $checksums -Raw
    $escapedAsset = [Regex]::Escape($asset)
    $match = [Regex]::Match($checksumText, "(?im)^([0-9a-f]{64})\s+\*?$escapedAsset\s*$")
    if (-not $match.Success) {
        throw "No checksum found for $asset"
    }
    $expected = $match.Groups[1].Value.ToLowerInvariant()
    $actual = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        throw "Checksum mismatch for $asset"
    }

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [IO.Compression.ZipFile]::OpenRead($archive)
    try {
        $entries = @($zip.Entries | ForEach-Object { $_.FullName } | Sort-Object)
    }
    finally {
        $zip.Dispose()
    }
    $expectedEntries = @("LICENSE", "README.txt", "dsmctl.exe")
    if (@(Compare-Object -ReferenceObject $expectedEntries -DifferenceObject $entries).Count -ne 0) {
        throw "Release archive contains unexpected files; refusing extraction."
    }

    $expanded = Join-Path $work "archive"
    Expand-Archive -LiteralPath $archive -DestinationPath $expanded
    $cli = Join-Path $expanded "dsmctl.exe"
    if (-not (Test-Path -LiteralPath $cli -PathType Leaf)) {
        throw "Release archive is missing dsmctl.exe"
    }

    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    Copy-Item -LiteralPath $cli -Destination (Join-Path $InstallDir "dsmctl.exe") -Force
    $installed = @((Join-Path $InstallDir "dsmctl.exe"))

    if ($AddToPath) {
        $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
        $entries = @($userPath -split ';' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
        if (-not ($entries | Where-Object { $_.TrimEnd('\') -ieq $InstallDir.TrimEnd('\') })) {
            $newPath = (@($entries) + $InstallDir) -join ';'
            [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
            Write-Host "Added $InstallDir to the user PATH. Open a new terminal to use it."
        }
    }

    Write-Host "Installed checksum-verified dsmctl $Version`:"
    $installed | ForEach-Object { Write-Host "  $_" }
    if (-not $AddToPath) {
        Write-Host "Use -AddToPath to add the install directory to your user PATH."
    }
}
finally {
    if (Test-Path -LiteralPath $work) {
        Remove-Item -LiteralPath $work -Recurse -Force
    }
}
