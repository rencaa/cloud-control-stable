param(
    [Parameter(Mandatory = $true)]
    [string]$Server,

    [string]$Domain = "",
    [string]$Email = "",
    [string]$HostName = "",
    [switch]$Staging,
    [switch]$EnableRegistration
)

$ErrorActionPreference = "Stop"
$ProjectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$ArchivePath = Join-Path $ProjectRoot "release\cloud-control-stable-edge.zip"

if ($Server -notmatch '^[A-Za-z0-9._-]+@[A-Za-z0-9._:-]+$') {
    throw "Server must look like ubuntu@203.0.113.10 or ubuntu@example.com."
}
if (-not (Get-Command scp -ErrorAction SilentlyContinue) -or -not (Get-Command ssh -ErrorAction SilentlyContinue)) {
    throw "Windows OpenSSH scp and ssh are required."
}

if ($Staging) {
    if (-not $HostName) {
        $HostName = ($Server -split '@', 2)[1]
    }
    if ($HostName -notmatch '^[A-Za-z0-9][A-Za-z0-9.-]*$') {
        throw "HostName contains unsupported characters."
    }
} else {
    if ($Domain -notmatch '^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)+$') {
        throw "A valid -Domain is required for production installation."
    }
    if ($Email -notmatch '^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$') {
        throw "A valid -Email is required for production installation."
    }
}

Write-Host "Creating the isolated edge deployment package..."
& powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot "package-edge.ps1") -OutputPath $ArchivePath
if ($LASTEXITCODE -ne 0) { throw "Packaging failed." }

Write-Host "Uploading package to $Server..."
& scp $ArchivePath "${Server}:/tmp/cloud-control-stable-edge.zip"
if ($LASTEXITCODE -ne 0) { throw "Package upload failed." }

$installerArguments = if ($Staging) {
    "--staging --host '$HostName'"
} else {
    "--production --domain '$($Domain.ToLowerInvariant())' --email '$Email'"
}
if ($EnableRegistration) {
    $installerArguments += " --enable-registration"
}

$remoteCommand = @"
set -e
sudo apt-get update
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y unzip
sudo mkdir -p /opt
sudo unzip -o /tmp/cloud-control-stable-edge.zip -d /opt
sudo bash /opt/cloud-control-stable/deploy/install-edge.sh $installerArguments
"@

Write-Host "Starting the Ubuntu one-click installer..."
& ssh -t $Server $remoteCommand
if ($LASTEXITCODE -ne 0) { throw "Remote installation failed. Review the output above; existing services were not stopped by the installer." }

Write-Host "Remote installation completed."
