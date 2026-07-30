$ErrorActionPreference = "Stop"

$ProjectRoot = Split-Path -Parent $PSScriptRoot
Set-Location $ProjectRoot

$App = "Atoms"
$AssetsDir = Join-Path $ProjectRoot "build\assets"
$SysoPath = Join-Path $ProjectRoot "src\resource_windows_amd64.syso"
$IcoPath = Join-Path $AssetsDir "icon.ico"
$Entry = ".\src"
$OutExe = Join-Path $ProjectRoot "$App.exe"

$GoBin = (& go env GOBIN).Trim()
if ([string]::IsNullOrEmpty($GoBin)) {
    $GoPathRaw = (& go env GOPATH).Trim()
    $GoPath = ($GoPathRaw -split ';')[0].TrimEnd('\', '/', ';')
    $GoBin = Join-Path $GoPath "bin"
}

$Rsrc = Join-Path $GoBin "rsrc.exe"
if (-not (Test-Path $Rsrc)) {
    Write-Host ">> rsrc not found at $Rsrc, installing github.com/akavel/rsrc ..."
    & go install github.com/akavel/rsrc@latest | Out-Null
    if (-not (Test-Path $Rsrc)) {
        throw "rsrc install failed, check go env GOBIN=$GoBin"
    }
}

Write-Host ">> Generating icon resources ..."
& powershell -ExecutionPolicy Bypass -File (Join-Path $AssetsDir "generate_icon.ps1")

Write-Host ">> Compiling Windows icon resource ..."
& $Rsrc -ico $IcoPath -o $SysoPath

Write-Host ">> Building $App.exe ..."
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go build -o $OutExe $Entry

Write-Host ">> Done: $OutExe"
Get-Item $OutExe | Select-Object Name, @{Name="Size(MB)";Expression={[math]::Round($_.Length/1MB,2)}}, LastWriteTime
