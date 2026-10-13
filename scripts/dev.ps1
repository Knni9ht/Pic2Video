$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location $projectRoot
try {
    if (-not (Test-Path -LiteralPath 'web/dist/index.html')) {
        Push-Location (Join-Path $projectRoot 'web')
        try {
            & npm.cmd ci --no-audit --no-fund
            if ($LASTEXITCODE -ne 0) { throw '前端依赖安装失败。' }
            & npm.cmd run build
            if ($LASTEXITCODE -ne 0) { throw '前端构建失败。' }
        } finally {
            Pop-Location
        }
    }
    Write-Host '启动 Go 开发服务；前端热更新请在另一个终端执行 npm --prefix web run dev'
    & go run ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw '开发服务退出，请检查配置和日志。' }
} finally {
    Pop-Location
}
