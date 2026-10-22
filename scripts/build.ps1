# CortexNEL 构建脚本（npm run build → wails build，顺序固定）。
# 用法：
#   pwsh scripts/build.ps1                # 常规构建
#   pwsh scripts/build.ps1 -Obfuscate     # 发布混淆构建（garble）
[CmdletBinding()]
param(
    # 发布混淆构建：wails build -obfuscated，经 garble 重命名 + 字面量加密。
    [switch]$Obfuscate,
    # garble 固定 seed（base64，恰好 8 字节）：使混淆产物可复现。
    # 缺省使用内置固定种子 'bmV4dXNfdjI='。传 'random' 则使用随机种子。
    [string]$GarbleSeed = 'bmV4dXNfdjI='
)

$ErrorActionPreference = 'Stop'

# Windows x64 一等目标；显式覆盖本机 GOARCH 环境差异。
$env:GOARCH = 'amd64'

$repoRoot = Split-Path -Parent $PSScriptRoot

if ($Obfuscate) {
    # garble 版本钉死并与仓库 Go 工具链匹配——v0.17.0 要求 Go ≥ 1.26，
    # 但对 Go 1.27+ 的链接器补丁尚未发布，混淆构建需 Go 1.26.x 工具链；
    # 升级 Go 前先查 garble 支持矩阵。
    $garbleVersion = 'v0.17.0'

    # Go 1.26 起 go install 可能按 $GOOS_$GOARCH 子目录落盘，也可能仍是扁平 bin/
    # （实测本机为扁平落盘）；两处都探测。wails 按名字调用 garble，需补进会话 PATH。
    $goBin = if ($env:GOBIN) { $env:GOBIN } else { Join-Path (go env GOPATH) 'bin' }
    $garbleDirs = @((Join-Path $goBin "$(go env GOOS)_$env:GOARCH"), $goBin)
    # 钉版校验：已安装的 garble 版本必须与钉版一致，否则重装（防止旧二进制架空钉版）。
    $garbleExe = $garbleDirs | ForEach-Object { Join-Path $_ 'garble.exe' } |
        Where-Object { Test-Path $_ } | Select-Object -First 1
    $haveVersion = if ($garbleExe) { (& $garbleExe version 2>$null | Select-Object -First 1) } else { '' }
    if ($haveVersion -notmatch [regex]::Escape($garbleVersion)) {
        Write-Host "==> go install mvdan.cc/garble@$garbleVersion（当前：$(if ($haveVersion) { $haveVersion } else { '未安装' })）"
        go install "mvdan.cc/garble@$garbleVersion"
        if ($LASTEXITCODE -ne 0) { throw "garble 安装失败（exit $LASTEXITCODE）" }
    }
    if (-not (Get-Command garble -ErrorAction SilentlyContinue)) {
        if (-not $garbleExe) { throw "未找到 garble.exe（探测位置：$($garbleDirs -join '、')）" }
        $env:Path = "$(Split-Path -Parent $garbleExe);$env:Path"
    }
    Write-Host "==> garble $(garble version 2>$null | Select-Object -First 1)"
}

Write-Host '==> npm install（frontend）'
Push-Location (Join-Path $repoRoot 'frontend')
try {
    npm install --no-audit --no-fund
    if ($LASTEXITCODE -ne 0) { throw "npm install 失败（exit $LASTEXITCODE）" }

    Write-Host '==> npm run build'
    npm run build
    if ($LASTEXITCODE -ne 0) { throw "npm run build 失败（exit $LASTEXITCODE）" }
} finally {
    Pop-Location
}

Write-Host '==> wails build -platform windows/amd64'
Push-Location $repoRoot
try {
    $wailsArgs = @('build', '-platform', 'windows/amd64')
    if ($Obfuscate) {
        # -obfuscated 会强制重新生成绑定（Wails 按混淆后的方法名生成 wailsjs）。
        $seedArg = if ($GarbleSeed -eq 'random') { '-seed=random' } elseif ($GarbleSeed) { "-seed=$GarbleSeed" } else { '-seed=bmV4dXNfdjI=' }
        Write-Host "==> garble 混淆参数: -literals -tiny $seedArg"
        $wailsArgs += @('-obfuscated', '-garbleargs', "-literals -tiny $seedArg")
    }
    wails @wailsArgs
    if ($LASTEXITCODE -ne 0) { throw "wails build 失败（exit $LASTEXITCODE）" }
} finally {
    Pop-Location
}

Write-Host '==> 完成。产物：build/bin/CortexNEL.exe'
Write-Host '提示：提交前请还原 frontend/dist/index.html 占位：git restore frontend/dist/index.html'
