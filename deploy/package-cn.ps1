param(
    [string]$OutputPath = ""
)

$ErrorActionPreference = "Stop"
$ProjectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
if (-not $OutputPath) {
    $OutputPath = Join-Path $ProjectRoot "release\cloud-control-stable-cn-fast.tar.gz"
}
$OutputPath = [System.IO.Path]::GetFullPath($OutputPath)
$OutputDirectory = Split-Path $OutputPath -Parent
if (-not (Test-Path -LiteralPath $OutputDirectory -PathType Container)) {
    New-Item -ItemType Directory -Path $OutputDirectory | Out-Null
}

$TempBase = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath())
$TempRoot = Join-Path $TempBase ("cloud-control-cn-package-" + [guid]::NewGuid().ToString("N"))
$PackageRoot = Join-Path $TempRoot "cloud-control-cn"
$BinRoot = Join-Path $PackageRoot "bin"
$ServerRoot = Join-Path $ProjectRoot "server"

& (Join-Path $PSScriptRoot "sync-web-dist.ps1")

try {
    New-Item -ItemType Directory -Path $BinRoot -Force | Out-Null

    $previousCgo = $env:CGO_ENABLED
    $previousGoos = $env:GOOS
    $previousGoarch = $env:GOARCH
    try {
        $env:CGO_ENABLED = "0"
        $env:GOOS = "linux"
        foreach ($architecture in @("amd64", "arm64")) {
            $env:GOARCH = $architecture
            $outputBinary = Join-Path $BinRoot "cloud-control-stable-linux-$architecture"
            Push-Location $ServerRoot
            try {
                & go build -trimpath -ldflags="-s -w" -o $outputBinary .
                if ($LASTEXITCODE -ne 0) { throw "Linux $architecture build failed." }
            } finally {
                Pop-Location
            }
        }
    } finally {
        $env:CGO_ENABLED = $previousCgo
        $env:GOOS = $previousGoos
        $env:GOARCH = $previousGoarch
    }

    Copy-Item -LiteralPath (Join-Path $ProjectRoot "deploy\install-cn.sh") -Destination (Join-Path $PackageRoot "install-cn.sh")
    # Keep this source path ASCII-only so Windows PowerShell 5.1 can parse the
    # script correctly even when the file is saved as UTF-8 without a BOM.
    Copy-Item -LiteralPath (Join-Path $ProjectRoot "README.md") -Destination (Join-Path $PackageRoot "README.md")
    Copy-Item -LiteralPath (Join-Path $ProjectRoot "OPEN_SOURCE.md") -Destination (Join-Path $PackageRoot "OPEN_SOURCE.md")
    Copy-Item -LiteralPath (Join-Path $ProjectRoot "LICENSE") -Destination (Join-Path $PackageRoot "LICENSE")

    $checksumLines = foreach ($binary in Get-ChildItem -LiteralPath $BinRoot -File | Sort-Object Name) {
        $hash = (Get-FileHash -LiteralPath $binary.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        "$hash  bin/$($binary.Name)"
    }
    # Linux sha256sum requires LF-only paths; WriteAllLines would emit CRLF on Windows.
    $checksumText = ([string]::Join("`n", $checksumLines)) + "`n"
    [System.IO.File]::WriteAllText((Join-Path $PackageRoot "SHA256SUMS"), $checksumText, [System.Text.Encoding]::ASCII)

    foreach ($requiredFile in @("install-cn.sh", "README.md", "OPEN_SOURCE.md", "LICENSE", "SHA256SUMS")) {
        $requiredPath = Join-Path $PackageRoot $requiredFile
        if (-not (Test-Path -LiteralPath $requiredPath -PathType Leaf)) {
            throw "Package is missing required file: $requiredFile"
        }
    }

    if (Test-Path -LiteralPath $OutputPath) {
        [System.IO.File]::Delete($OutputPath)
    }
    & tar -czf $OutputPath -C $TempRoot "cloud-control-cn"
    if ($LASTEXITCODE -ne 0) { throw "Creating the tar.gz package failed." }

    $archive = Get-Item -LiteralPath $OutputPath
    $archiveHash = (Get-FileHash -LiteralPath $archive.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
    $checksumSidecar = $archive.FullName + ".sha256"
    [System.IO.File]::WriteAllText(
        $checksumSidecar,
        "$archiveHash  $($archive.Name)`n",
        [System.Text.Encoding]::ASCII
    )
    Write-Host "China-fast deployment package: $($archive.FullName) ($([math]::Round($archive.Length / 1MB, 1)) MiB)"
    Write-Host "SHA-256 sidecar: $checksumSidecar"
} finally {
    if (Test-Path -LiteralPath $TempRoot) {
        $resolvedTemp = (Resolve-Path -LiteralPath $TempRoot).Path
        if (-not $resolvedTemp.StartsWith($TempBase, [System.StringComparison]::OrdinalIgnoreCase)) {
            throw "Refusing to remove an unexpected temporary path: $resolvedTemp"
        }
        Remove-Item -LiteralPath $resolvedTemp -Recurse -Force
    }
}
