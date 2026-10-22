# check.ps1 — 确定性门禁（L0/L1 + 仓库不变量），CI 与本地共用同一份逻辑。
#
# 用法:
#   pwsh scripts/check.ps1            全部检查
#   pwsh scripts/check.ps1 -SkipTests 跳过 go test（本地快速迭代用）
#
# 检查项:
#   L0 静态        gofmt / go vet
#   L1 单元        go test ./internal/...
#   行数禁线        >800 失败 / >500 告警
#   P1 引擎中立    引擎文件无服务器字面量
#   P2 知识隔离    字面量只出现在适配包
#   D3 包名        禁 core/common/utils/misc 兜底名
#   命名后缀       禁 _new/_old/_final/_v2/_copy/_backup/_tmp
#   S2/S3 凭据     跟踪文件无 key 样式明文
#
# 退出码: 0 = 全部通过; 1 = 有失败项（告警不失败）。
[CmdletBinding()]
param(
    [switch]$SkipTests
)

$ErrorActionPreference = 'Continue'
$repo = Split-Path -Parent $PSScriptRoot
Set-Location $repo

$script:failures = [System.Collections.Generic.List[string]]::new()
$script:warnings = [System.Collections.Generic.List[string]]::new()

function Write-Section([string]$title) {
    Write-Host ''
    Write-Host "=== $title ===" -ForegroundColor Cyan
}

function Fail([string]$message) { $script:failures.Add($message); Write-Host "FAIL  $message" -ForegroundColor Red }
function Warn([string]$message) { $script:warnings.Add($message); Write-Host "WARN  $message" -ForegroundColor Yellow }
function Pass([string]$message) { Write-Host "ok    $message" -ForegroundColor Green }

# 行数统计：按换行符计数，末行无换行符也计入。
function Get-FileLineCount([string]$path) {
    $text = [System.IO.File]::ReadAllText($path)
    $count = 0
    foreach ($ch in $text.ToCharArray()) { if ($ch -eq "`n") { $count++ } }
    if (-not $text.EndsWith("`n")) { $count++ }
    return $count
}

function Get-SourceFiles {
    Get-ChildItem -Recurse -File -Include '*.go', '*.vue', '*.ts' internal, frontend\src -ErrorAction SilentlyContinue |
        Where-Object { $_.FullName -notmatch '\\node_modules\\|\\wailsjs\\|\\dist\\' }
}

# ---------------------------------------------------------------- L0 / L1
Write-Section 'L0 静态检查'

$unformatted = gofmt -l internal 2>$null
if ($unformatted) { Fail "gofmt 未格式化: $($unformatted -join ', ')" } else { Pass 'gofmt 干净' }

$vetOutput = go vet ./... 2>&1
if ($LASTEXITCODE -ne 0) { Fail "go vet 失败:`n$vetOutput" } else { Pass 'go vet 通过' }

if (-not $SkipTests) {
    Write-Section 'L1 单元测试'
    $testOutput = go test ./internal/... 2>&1
    if ($LASTEXITCODE -ne 0) { Fail "go test 失败:`n$testOutput" } else { Pass 'go test ./internal/... 通过' }
}

# ---------------------------------------------------------------- 行数禁线
Write-Section '规范 §4 行数禁线（>800 禁止，>500 需在交付说明中给理由）'

$oversized = Get-SourceFiles | ForEach-Object {
    [pscustomobject]@{ Lines = (Get-FileLineCount $_.FullName); Path = $_.FullName.Replace("$repo\", '') }
} | Where-Object { $_.Lines -gt 500 } | Sort-Object Lines -Descending

foreach ($f in ($oversized | Where-Object { $_.Lines -gt 800 })) {
    Fail "$($f.Path) $($f.Lines) 行 > 800 禁线"
}
foreach ($f in ($oversized | Where-Object { $_.Lines -le 800 })) {
    Warn "$($f.Path) $($f.Lines) 行（>500，需在交付说明中给出保留理由）"
}
if (-not $oversized) { Pass '无超过 500 行的源文件' }

# ---------------------------------------------------------------- P1 / P2
Write-Section '规范 P1/P2 引擎协议中立'

# 引擎文件集 = internal/relay 顶层 *.go + codec/ + stage/ + packet/login/（规范 P1 括号内定义）。
$engineFiles = [System.Collections.Generic.List[System.IO.FileInfo]]::new()
foreach ($f in (Get-ChildItem (Join-Path $repo 'internal\relay') -File -Filter '*.go')) { $engineFiles.Add($f) }
foreach ($sub in @('codec', 'stage', 'packet\login')) {
    $dir = Join-Path $repo "internal\relay\$sub"
    if (Test-Path $dir) {
        foreach ($f in (Get-ChildItem $dir -File -Filter '*.go' -Recurse)) { $engineFiles.Add($f) }
    }
}

# 服务器知识字面量：域名、brand、GameID。取值来自 docs/facts/NETEASE-PROTOCOL.md §3.4。
$serverLiterals = @('4661334467366178884', 'heypixel', 'HeyPixel', 'bjdmc.net', 'mcbjd.net')
$engineDirty = $false
foreach ($f in $engineFiles) {
    $hits = Select-String -Path $f.FullName -Pattern ($serverLiterals -join '|') -AllMatches -ErrorAction SilentlyContinue
    if ($hits) {
        $engineDirty = $true
        $found = ($hits | ForEach-Object { $_.Matches.Value } | Sort-Object -Unique) -join ', '
        Fail "引擎文件含服务器字面量: $($f.FullName.Replace("$repo\", '')) -> $found"
    }
}
if (-not $engineDirty) { Pass "引擎文件集（$($engineFiles.Count) 个）无服务器字面量" }

# P2 反向断言：上述字面量只应出现在适配包 packet/<server>/ 下。
$adapterRoot = Join-Path $repo 'internal\relay\packet'
$outsideAdapter = Get-ChildItem -Recurse -File -Filter '*.go' (Join-Path $repo 'internal\relay') -ErrorAction SilentlyContinue |
    Where-Object { $_.FullName -notlike "$adapterRoot\*" -and $_.FullName -notmatch '\\shadow\\' } |
    ForEach-Object {
        $hits = Select-String -Path $_.FullName -Pattern ($serverLiterals -join '|') -AllMatches -ErrorAction SilentlyContinue
        if ($hits) { $_.FullName.Replace("$repo\", '') }
    }
if ($outsideAdapter) {
    Warn "服务器字面量出现在适配包之外（规范 P1 未枚举的目录，需人工确认）: $($outsideAdapter -join ', ')"
}

# ---------------------------------------------------------------- 命名禁令
Write-Section '规范 §3 D3 / §4 命名后缀'

$bannedPackages = @('core', 'common', 'utils', 'misc', 'helpers', 'base', 'util')
$badPackages = Get-ChildItem -Recurse -Directory internal -ErrorAction SilentlyContinue |
    Where-Object { $_.Name -in $bannedPackages } |
    ForEach-Object { $_.FullName.Replace("$repo\", '') }
if ($badPackages) { Fail "兜底包名: $($badPackages -join ', ')" } else { Pass '无兜底包名' }

$badSuffix = Get-SourceFiles |
    Where-Object { $_.BaseName -match '_(new|old|final|v2|copy|backup|tmp)$' } |
    ForEach-Object { $_.FullName.Replace("$repo\", '') }
if ($badSuffix) { Fail "命名后缀禁令: $($badSuffix -join ', ')" } else { Pass '无禁用命名后缀' }

# ---------------------------------------------------------------- 凭据扫描
Write-Section 'S2/S3 凭据泄漏扫描'

$tracked = git ls-files
$credHits = [System.Collections.Generic.List[string]]::new()
foreach ($file in $tracked) {
    if (-not (Test-Path -LiteralPath $file)) { continue }
    $hits = Select-String -Path $file -Pattern 'apikey_[A-Za-z0-9_]{8,}|sk-[A-Za-z0-9]{20,}|ghp_[A-Za-z0-9]{20,}|AKIA[0-9A-Z]{16}|BEGIN [A-Z ]*PRIVATE KEY' -AllMatches -ErrorAction SilentlyContinue
    if ($hits) { $credHits.Add($file) }
}
if ($credHits.Count -gt 0) { Fail "疑似凭据明文: $($credHits -join ', ')" } else { Pass '跟踪文件无凭据样式明文' }

# ---------------------------------------------------------------- 汇总
Write-Section '检查结果'
if ($script:warnings.Count -gt 0) {
    Write-Host "告警 $($script:warnings.Count) 项（不阻断）:" -ForegroundColor Yellow
    foreach ($w in $script:warnings) { Write-Host "  - $w" -ForegroundColor Yellow }
}
if ($script:failures.Count -gt 0) {
    Write-Host "失败 $($script:failures.Count) 项:" -ForegroundColor Red
    foreach ($f in $script:failures) { Write-Host "  - $f" -ForegroundColor Red }
    exit 1
}
Write-Host '全部检查通过' -ForegroundColor Green
exit 0
