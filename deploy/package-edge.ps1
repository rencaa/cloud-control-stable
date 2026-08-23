param(
    [string]$OutputPath = ""
)

$ErrorActionPreference = "Stop"
$ProjectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
if (-not $OutputPath) {
    $OutputPath = Join-Path $ProjectRoot "release\cloud-control-stable-edge.zip"
}
$OutputPath = [System.IO.Path]::GetFullPath($OutputPath)
$OutputDirectory = Split-Path $OutputPath -Parent
if (-not (Test-Path -LiteralPath $OutputDirectory -PathType Container)) {
    New-Item -ItemType Directory -Path $OutputDirectory | Out-Null
}
$TempBase = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath())
$TempRoot = Join-Path $TempBase ("cloud-control-edge-package-" + [guid]::NewGuid().ToString("N"))
$PackageRoot = Join-Path $TempRoot "cloud-control-stable"

try {
    New-Item -ItemType Directory -Path $PackageRoot | Out-Null
    foreach ($file in @(
        ".env.edge.example",
        "docker-compose.edge.yml",
        "docker-compose.tls.yml",
        "README.md",
        "OPEN_SOURCE.md",
        "LICENSE"
    )) {
        Copy-Item -LiteralPath (Join-Path $ProjectRoot $file) -Destination $PackageRoot
    }
    Copy-Item -LiteralPath (Join-Path $ProjectRoot "deploy") -Destination $PackageRoot -Recurse
    Copy-Item -LiteralPath (Join-Path $ProjectRoot "docs") -Destination $PackageRoot -Recurse
    Get-ChildItem -LiteralPath (Join-Path $PackageRoot "deploy\tls") -Filter "*.pem" -File -ErrorAction SilentlyContinue |
        ForEach-Object { [System.IO.File]::Delete($_.FullName) }
    $GeneratedEmqx = Join-Path $PackageRoot "deploy\generated\emqx-mysql-access.hocon"
    if (Test-Path -LiteralPath $GeneratedEmqx -PathType Leaf) {
        [System.IO.File]::Delete($GeneratedEmqx)
    }

    $ServerTarget = Join-Path $PackageRoot "server"
    New-Item -ItemType Directory -Path $ServerTarget | Out-Null
    foreach ($file in @(".dockerignore", "Dockerfile", "go.mod", "go.sum", "main.go")) {
        Copy-Item -LiteralPath (Join-Path $ProjectRoot "server\$file") -Destination $ServerTarget
    }
    foreach ($directory in @("config", "handlers", "middleware", "models", "utils")) {
        Copy-Item -LiteralPath (Join-Path $ProjectRoot "server\$directory") -Destination $ServerTarget -Recurse
    }
    $WebTarget = Join-Path $ServerTarget "web"
    New-Item -ItemType Directory -Path $WebTarget | Out-Null
    Copy-Item -LiteralPath (Join-Path $ProjectRoot "server\web\dist") -Destination $WebTarget -Recurse
    $NestedDist = Join-Path $WebTarget "dist\dist"
    if (Test-Path -LiteralPath $NestedDist -PathType Container) {
        $ResolvedNested = (Resolve-Path -LiteralPath $NestedDist).Path
        if (-not $ResolvedNested.StartsWith($TempRoot, [System.StringComparison]::OrdinalIgnoreCase)) {
            throw "Refusing to remove an unexpected path: $ResolvedNested"
        }
        Remove-Item -LiteralPath $ResolvedNested -Recurse -Force
    }

    Compress-Archive -LiteralPath $PackageRoot -DestinationPath $OutputPath -CompressionLevel Optimal -Force
    $archive = Get-Item -LiteralPath $OutputPath
    Write-Host "Edge deployment package: $($archive.FullName) ($([math]::Round($archive.Length / 1MB, 1)) MiB)"
} finally {
    if (Test-Path -LiteralPath $TempRoot) {
        $ResolvedTemp = (Resolve-Path -LiteralPath $TempRoot).Path
        if (-not $ResolvedTemp.StartsWith($TempBase, [System.StringComparison]::OrdinalIgnoreCase)) {
            throw "Refusing to remove an unexpected temporary path: $ResolvedTemp"
        }
        Remove-Item -LiteralPath $ResolvedTemp -Recurse -Force
    }
}
