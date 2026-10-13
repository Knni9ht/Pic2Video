$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$binaryPath = Join-Path $projectRoot 'bin/Pic2Video.exe'
if (-not (Test-Path -LiteralPath $binaryPath)) {
    throw '请先执行 powershell -ExecutionPolicy Bypass -File scripts/build.ps1'
}
Push-Location $projectRoot
try {
    & $binaryPath
    if ($LASTEXITCODE -ne 0) { throw '服务启动失败，请检查配置和日志。' }
} finally {
    Pop-Location
}
