$ErrorActionPreference = "Stop"
$ProjectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$Source = (Resolve-Path (Join-Path $ProjectRoot "web\dist")).Path
$Target = (Resolve-Path (Join-Path $ProjectRoot "server\web\dist")).Path
$ExpectedTarget = [System.IO.Path]::GetFullPath((Join-Path $ProjectRoot "server\web\dist"))
if (-not [string]::Equals($Target, $ExpectedTarget, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Refusing to synchronize unexpected web target: $Target"
}

$wanted = @{}
Get-ChildItem -LiteralPath $Source -Recurse -File | ForEach-Object {
    $relative = $_.FullName.Substring($Source.Length).TrimStart('\', '/')
    $wanted[$relative] = $true
}

Get-ChildItem -LiteralPath $Target -Recurse -File | ForEach-Object {
    $relative = $_.FullName.Substring($Target.Length).TrimStart('\', '/')
    if (-not $wanted.ContainsKey($relative)) {
        Remove-Item -LiteralPath $_.FullName -Force
    }
}

Get-ChildItem -LiteralPath $Target -Recurse -Directory |
    Sort-Object { $_.FullName.Length } -Descending |
    ForEach-Object {
        if (-not (Get-ChildItem -LiteralPath $_.FullName -Force | Select-Object -First 1)) {
            Remove-Item -LiteralPath $_.FullName -Force
        }
    }

Copy-Item -Path (Join-Path $Source '*') -Destination $Target -Recurse -Force
Write-Host "Embedded web distribution synchronized: $Target"
