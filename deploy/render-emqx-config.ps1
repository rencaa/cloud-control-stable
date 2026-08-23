param(
    [string]$EnvironmentFile = (Join-Path $PSScriptRoot "..\.env"),
    [string]$TemplateFile = (Join-Path $PSScriptRoot "emqx-mysql-access.hocon.example"),
    [string]$OutputFile = (Join-Path $PSScriptRoot "generated\emqx-mysql-access.hocon")
)

$ErrorActionPreference = "Stop"

function Read-DotEnv([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "Environment file not found: $Path"
    }
    $values = @{}
    foreach ($line in Get-Content -LiteralPath $Path -Encoding UTF8) {
        $trimmed = $line.Trim()
        if (-not $trimmed -or $trimmed.StartsWith("#")) { continue }
        $separator = $trimmed.IndexOf("=")
        if ($separator -lt 1) { continue }
        $name = $trimmed.Substring(0, $separator).Trim()
        $value = $trimmed.Substring($separator + 1).Trim()
        $values[$name] = $value
    }
    return $values
}

function Escape-HoconString([string]$Value) {
    return $Value.Replace("\", "\\").Replace('"', '\"').Replace("`r", "").Replace("`n", "\n")
}

$environment = Read-DotEnv $EnvironmentFile
foreach ($required in @("MYSQL_DATABASE", "MYSQL_USER", "MYSQL_PASSWORD")) {
    if (-not $environment.ContainsKey($required) -or -not $environment[$required] -or $environment[$required] -like "replace-with-*") {
        throw "Set a real value for $required in $EnvironmentFile before rendering EMQX configuration."
    }
}

$rendered = Get-Content -LiteralPath $TemplateFile -Raw -Encoding UTF8
$rendered = $rendered.Replace("CHANGE_ME_DATABASE", (Escape-HoconString $environment["MYSQL_DATABASE"]))
$rendered = $rendered.Replace("CHANGE_ME_USER", (Escape-HoconString $environment["MYSQL_USER"]))
$rendered = $rendered.Replace("CHANGE_ME_PASSWORD", (Escape-HoconString $environment["MYSQL_PASSWORD"]))

$outputDirectory = Split-Path -Parent $OutputFile
New-Item -ItemType Directory -Path $outputDirectory -Force | Out-Null
[System.IO.File]::WriteAllText($OutputFile, $rendered, [System.Text.UTF8Encoding]::new($false))
Write-Host "Rendered EMQX configuration: $OutputFile"
