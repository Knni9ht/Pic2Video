$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location $projectRoot
$previousCGO = $env:CGO_ENABLED
try {
    Push-Location (Join-Path $projectRoot 'web')
    try {
        & npm.cmd ci --no-audit --no-fund
        if ($LASTEXITCODE -ne 0) { throw '前端依赖安装失败。' }
        & npm.cmd run build
        if ($LASTEXITCODE -ne 0) { throw '前端构建失败。' }
    } finally {
        Pop-Location
    }
    $env:CGO_ENABLED = '0'
    & go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go 测试失败。' }
    New-Item -ItemType Directory -Force -Path bin | Out-Null
    & go build -trimpath -o bin/Pic2Video.exe ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw 'Go 构建失败。' }
    Write-Host '构建完成：bin/Pic2Video.exe'
} finally {
    $env:CGO_ENABLED = $previousCGO
    Pop-Location
}
