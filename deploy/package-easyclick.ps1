param(
    [string]$OutputPath = ""
)

$ErrorActionPreference = "Stop"
$ProjectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$ClientRoot = Join-Path $ProjectRoot "client\easyclick"
if (-not $OutputPath) {
    $OutputPath = Join-Path $ProjectRoot "release\cloud-control-easyclick-lan-v93.zip"
}
$OutputPath = [System.IO.Path]::GetFullPath($OutputPath)
$OutputDirectory = Split-Path $OutputPath -Parent
if (-not (Test-Path -LiteralPath $OutputDirectory -PathType Container)) {
    New-Item -ItemType Directory -Path $OutputDirectory | Out-Null
}

$TempBase = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath())
$TempRoot = Join-Path $TempBase ("cloud-control-easyclick-package-" + [guid]::NewGuid().ToString("N"))
$PackageRoot = Join-Path $TempRoot "cloud-control-easyclick-lan-v93"

try {
    New-Item -ItemType Directory -Path $PackageRoot -Force | Out-Null
    Get-ChildItem -LiteralPath $ClientRoot -Force | Where-Object {
        $_.Name -notin @("build", "dist", ".git", "node_modules")
    } | ForEach-Object {
        Copy-Item -LiteralPath $_.FullName -Destination $PackageRoot -Recurse -Force
    }
    Copy-Item -LiteralPath (Join-Path $ProjectRoot "OPEN_SOURCE.md") -Destination (Join-Path $PackageRoot "OPEN_SOURCE.md")
    Copy-Item -LiteralPath (Join-Path $ProjectRoot "LICENSE") -Destination (Join-Path $PackageRoot "LICENSE")

    foreach ($requiredFile in @("pkgsetting.json", "src\js\main.js", "src\layout\main.xml", "src\layout\ui.js", "OPEN_SOURCE.md", "LICENSE")) {
        if (-not (Test-Path -LiteralPath (Join-Path $PackageRoot $requiredFile) -PathType Leaf)) {
            throw "EasyClick package is missing required file: $requiredFile"
        }
    }
    if (Test-Path -LiteralPath $OutputPath) {
        [System.IO.File]::Delete($OutputPath)
    }
    Compress-Archive -Path $PackageRoot -DestinationPath $OutputPath -CompressionLevel Optimal
    $archive = Get-Item -LiteralPath $OutputPath
	$archiveHash = (Get-FileHash -LiteralPath $archive.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
	[System.IO.File]::WriteAllText(
		$archive.FullName + ".sha256",
		"$archiveHash  $($archive.Name)`n",
		[System.Text.Encoding]::ASCII
	)
    Write-Host "EasyClick LAN package: $($archive.FullName) ($([math]::Round($archive.Length / 1KB)) KiB)"
} finally {
    if (Test-Path -LiteralPath $TempRoot) {
        $resolvedTemp = (Resolve-Path -LiteralPath $TempRoot).Path
        if (-not $resolvedTemp.StartsWith($TempBase, [System.StringComparison]::OrdinalIgnoreCase)) {
            throw "Refusing to remove an unexpected temporary path: $resolvedTemp"
        }
        Remove-Item -LiteralPath $resolvedTemp -Recurse -Force
    }
}
