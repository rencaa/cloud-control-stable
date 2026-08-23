param(
	[switch]$Edge,
	[switch]$Tls,
	[switch]$ExternalMqtt,
	[switch]$RequireDocker,
	[string]$EnvironmentFile = ""
)

$ErrorActionPreference = "Stop"
$ProjectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
if (-not $EnvironmentFile) {
    $EnvironmentFile = Join-Path $ProjectRoot ".env"
}

function Read-DotEnv([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "Missing .env. Copy .env.example to .env and replace every secret."
    }
    $values = @{}
    foreach ($line in Get-Content -LiteralPath $Path -Encoding UTF8) {
        $trimmed = $line.Trim()
        if (-not $trimmed -or $trimmed.StartsWith("#")) { continue }
        $separator = $trimmed.IndexOf("=")
        if ($separator -lt 1) { continue }
        $values[$trimmed.Substring(0, $separator).Trim()] = $trimmed.Substring($separator + 1).Trim()
    }
    return $values
}

if ((Split-Path $ProjectRoot -Leaf) -notin @("cloud-control-stable", "cloud-control-longonline-v2026.08.24", "cloud-control-optimized-v2026.08.24")) {
    throw "Preflight must run from an isolated cloud-control release directory."
}

if ($Edge -and $ExternalMqtt) {
    throw "Edge mode uses the embedded MQTT broker and cannot enable ExternalMqtt."
}

$environment = Read-DotEnv $EnvironmentFile
$requiredValues = @("CLOUD_JWT_SECRET", "CLOUD_ADMIN_PASSWORD", "CLOUD_CORS_ORIGINS")
if (-not $Edge) {
    $requiredValues += @("MYSQL_DATABASE", "MYSQL_USER", "MYSQL_PASSWORD", "MYSQL_ROOT_PASSWORD")
}
foreach ($required in $requiredValues) {
    $value = $environment[$required]
    if (-not $value -or $value -like "replace-with-*") {
        throw "Set a real value for $required in $EnvironmentFile."
    }
}
if ($environment["CLOUD_JWT_SECRET"].Length -lt 32) {
    throw "CLOUD_JWT_SECRET must contain at least 32 characters."
}

if ($Tls) {
    foreach ($certificate in @("fullchain.pem", "privkey.pem")) {
        $certificatePath = Join-Path $ProjectRoot "deploy\tls\$certificate"
        if (-not (Test-Path -LiteralPath $certificatePath -PathType Leaf)) {
            throw "TLS file is missing: $certificatePath"
        }
    }
}

if ($ExternalMqtt) {
    foreach ($required in @("CLOUD_MQTT_BROKER_URL", "CLOUD_MQTT_USERNAME", "CLOUD_MQTT_PASSWORD")) {
        if (-not $environment[$required]) { throw "External MQTT requires $required." }
    }
    if ($environment["EXTERNAL_MQTT_PORT"] -eq $environment["EMBEDDED_MQTT_PORT"]) {
        throw "EXTERNAL_MQTT_PORT and EMBEDDED_MQTT_PORT cannot be the same host port."
    }
    & (Join-Path $PSScriptRoot "render-emqx-config.ps1") -EnvironmentFile $EnvironmentFile
}

Push-Location $ProjectRoot
try {
    Push-Location "server"
    try {
        & go test ./...
        if ($LASTEXITCODE -ne 0) { throw "Go tests failed." }
        & go vet ./...
        if ($LASTEXITCODE -ne 0) { throw "Go vet failed." }
    } finally {
        Pop-Location
    }

    if (Get-Command node -ErrorAction SilentlyContinue) {
        & node --check "client/easyclick/src/js/main.js"
        if ($LASTEXITCODE -ne 0) { throw "EasyClick main.js syntax check failed." }
        & node --check "client/easyclick/src/slib/mqtt_transport.js"
        if ($LASTEXITCODE -ne 0) { throw "MQTT transport syntax check failed." }
    } else {
        Write-Warning "Node.js is unavailable; EasyClick JavaScript syntax check was skipped."
    }

    if (Get-Command docker -ErrorAction SilentlyContinue) {
        $composeFile = if ($Edge) { "docker-compose.edge.yml" } else { "docker-compose.yml" }
        $composeArgs = @("compose", "--project-name", "cloud-control-stable", "-f", $composeFile)
        if ($Tls) { $composeArgs += @("-f", "docker-compose.tls.yml") }
        if ($ExternalMqtt) {
            $composeArgs += @("-f", "docker-compose.external-mqtt.yml", "--profile", "external-mqtt")
        }
        $composeArgs += @("config", "--quiet")
        & docker @composeArgs
        if ($LASTEXITCODE -ne 0) { throw "Docker Compose validation failed." }
    } elseif ($RequireDocker) {
        throw "Docker is required but is not installed or not on PATH."
    } else {
        Write-Warning "Docker is unavailable; Compose runtime validation was skipped."
    }
} finally {
    Pop-Location
}

Write-Host "Preflight passed for isolated stable build."
