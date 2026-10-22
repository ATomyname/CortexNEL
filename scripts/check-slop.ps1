# check-slop.ps1 — 反 AI 垃圾检查层（评估方案 Layer 2）。
#
# 设计遵循 GitHub 反 slop 项目的一致方法论（hallmark / no-ai-slop / kill-ai-slop /
# AI-SLOP-Detector / sloppylint）:
#   1. tell 可枚举、可命名、有 ID —— 不靠"感觉像 AI"
#   2. 确定性核心，无语义判断（语义层由 Jev 承担，见评估方案 Layer 1）
#   3. 先分流后修复 —— 本脚本只出报告，不改任何文件
#   4. 证据优于指控 —— 命中只报告"此处命中 tell X"，不断言"这是 AI 写的"
#   5. 机器可读输出 + 抑制指令（slop-ignore）
#
# 用法:
#   pwsh scripts/check-slop.ps1              人类可读报告
#   pwsh scripts/check-slop.ps1 -Json        机器可读（供 Jev 分流层消费）
#   pwsh scripts/check-slop.ps1 -Tell ui-emoji   只跑指定 tell
#
# 抑制: 在命中行尾加注释 `slop-ignore: <tell-id>`（.go/.ts）或
#       `<!-- slop-ignore: <tell-id> -->`（.md/.vue），确认是有意为之时使用。
#
# 退出码: 0 = 无失败项; 1 = 有失败项（告警不失败）。
[CmdletBinding()]
param(
    [switch]$Json,
    [string]$Tell
)

$ErrorActionPreference = 'Continue'
$repo = Split-Path -Parent $PSScriptRoot
Set-Location $repo

# ------------------------------------------------------------------ tell 注册表
# severity: fail = 规范明令禁止（§11 清单）; warn = 需人工分流
$tells = [ordered]@{
    'ui-emoji' = @{
        Severity = 'fail'
        Title    = '界面非受控 Emoji / 符号'
        Clause   = '规范 U1 / §11.12'
        Scope    = 'frontend/src/**/*.vue 的 <template> 区块'
    }
    'ui-hardcoded-color' = @{
        Severity = 'warn'
        Title    = '脱离设计 Token 的硬编码颜色/渐变'
        Clause   = '规范 U2 / §11.13'
        Scope    = 'frontend/src 下除 tokens.css 外的 .vue/.ts'
    }
    'zh-slop-phrase' = @{
        Severity = 'warn'
        Title    = '中文 AI 味套话'
        Clause   = '评估方案 Layer 2 写作层（模式库参照 no-ai-slop 中文化）'
        Scope    = 'internal/**/*.go 注释 + frontend/src'
    }
    'code-placeholder' = @{
        Severity = 'warn'
        Title    = '占位标记（TODO/FIXME/HACK/未实现）'
        Clause   = '评估方案 Layer 2 代码层（参照 sloppylint Lies 轴）'
        Scope    = 'internal/**/*.go'
    }
    'declared-seam' = @{
        Severity = 'warn'
        Title    = '显式预留但未实现的 seam'
        Clause   = '评估方案 Layer 2 架构层（seam 必须诚实可追踪）'
        Scope    = 'internal/**/*.go'
    }
}

# 中文 AI 味套话模式库（只收高置信、低误报的；"既…又…"等通用句式故意不收）
$zhSlopPatterns = @(
    '总而言之', '总的来说', '综上所述', '值得一提', '需要注意的是', '值得注意的是',
    '随着.{2,24}的发展', '在.{2,24}的过程中', '标志着', '奠定了', '具有重要意义',
    '为广大用户', '赋能', '抓手', '让我们一起', '希望对你有所帮助', '不难发现'
)

# 占位标记（代码层）。故意不收裸 XXX：-Dxxx=true、c.Logger().Xxx、测试数据里的
# xxxxxx 都是合法用法，误报率太高（参照 kill-ai-slop 的 strictness controls）。
$placeholderPatterns = @('TODO', 'FIXME', 'HACK', '未实现', '暂不支持', '待实现')

# 界面符号判定：按码位逐字符判定，比正则区间更可控。
# 命中：U+1F300-1FAFF（emoji）/ U+2600-27BF（符号与装饰符，含 ✔✖）/ U+FE0F（变体选择符）
#      / U+2B00-2BFF（杂项符号与箭头，含 ⬛⬜⭐）
# 排除：U+2190-21FF（排版箭头 ←↑→↓，属正常排版而非表情符号）
function Test-EmojiOrSymbol([int]$code) {
    if ($code -ge 0x2190 -and $code -le 0x21FF) { return $false }
    if ($code -ge 0x1F300 -and $code -le 0x1FAFF) { return $true }
    if ($code -ge 0x2600 -and $code -le 0x27BF) { return $true }
    if ($code -eq 0xFE0F) { return $true }
    if ($code -ge 0x2B00 -and $code -le 0x2BFF) { return $true }
    return $false
}

if ($Tell -and -not $tells.Contains($Tell)) {
    Write-Host "未知 tell: $Tell（可用: $($tells.Keys -join ', ')" -ForegroundColor Red
    exit 1
}

$findings = [System.Collections.Generic.List[object]]::new()

function Add-Finding {
    param([string]$Id, [string]$File, [int]$Line, [string]$Evidence)
    $findings.Add([pscustomobject]@{
        tell     = $Id
        severity = $tells[$Id].Severity
        file     = $File.Replace("$repo\", '')
        line     = $Line
        evidence = $evidence
    })
}

# 命中行是否带抑制指令
function Test-Suppressed([string]$lineText, [string]$tellId) {
    return ($lineText -match "slop-ignore:\s*$tellId") -or ($lineText -match "slop-ignore:\s*all")
}

# 取 .vue 的根 <template> 区块行范围（U1 只查界面文本，不查 CSS/脚本注释）。
# 必须按嵌套计数：<template #slot> 插槽的 </template> 不是根区块的结束，
# 用"第一个 </template>"会提前收尾，把大部分界面文本漏掉（2026-09-21 实测教训）。
function Get-TemplateLineRange([string]$path) {
    $lines = [System.IO.File]::ReadAllLines($path)
    $start = -1
    for ($i = 0; $i -lt $lines.Count; $i++) {
        if ($lines[$i] -match '^\s*<template\b') { $start = $i; break }
    }
    if ($start -lt 0) { return $null }
    $depth = 0
    for ($i = $start; $i -lt $lines.Count; $i++) {
        $opens = ([regex]::Matches($lines[$i], '<template\b')).Count
        $closes = ([regex]::Matches($lines[$i], '</template>')).Count
        $depth += $opens - $closes
        if ($depth -le 0) {
            return @{ Start = $start; End = $i; Lines = $lines }
        }
    }
    return @{ Start = $start; End = $lines.Count - 1; Lines = $lines }
}

# ------------------------------------------------------------------ ui-emoji
if ((-not $Tell -or $Tell -eq 'ui-emoji')) {
    foreach ($f in (Get-ChildItem -Recurse -File frontend\src -Filter '*.vue' | Where-Object { $_.FullName -notmatch 'node_modules|wailsjs' })) {
        $range = Get-TemplateLineRange $f.FullName
        if ($null -eq $range) { continue }
        for ($i = $range.Start; $i -le $range.End; $i++) {
            $line = $range.Lines[$i]
            $hit = $null
            foreach ($ch in $line.ToCharArray()) {
                if (Test-EmojiOrSymbol ([int]$ch)) { $hit = $ch; break }
            }
            if ($null -ne $hit) {
                if (Test-Suppressed $line 'ui-emoji') { continue }
                $text = "[U+{0:X4} '{1}'] {2}" -f [int]$hit, $hit, ($line.Trim().Substring(0, [Math]::Min(70, $line.Trim().Length)))
                Add-Finding 'ui-emoji' $f.FullName ($i + 1) $text
            }
        }
    }
}

# ------------------------------------------------------------------ ui-hardcoded-color
# antdTheme.ts 是调色板源头（Apple 系统色 → antd token 的映射），字面量按设计存在，排除。
#
# 判定严格按规范 U2 原文：「禁止私自引入脱离全局体系的高斯模糊彩色光晕大球、
# 非标重度渐变投影或硬编码背景色」——只查 **background 系属性**上的字面颜色。
# 半透明 border / box-shadow / color 上的 rgba 不在禁令范围内（tokens.css 自身
# 也大量使用 rgba 分隔线与阴影），不查，避免把规范没禁的也报出来。
if ((-not $Tell -or $Tell -eq 'ui-hardcoded-color')) {
    foreach ($f in (Get-ChildItem -Recurse -File frontend\src -Include '*.vue', '*.ts' | Where-Object { $_.FullName -notmatch 'node_modules|wailsjs|\\styles\\tokens\.css$|\\lib\\antdTheme\.ts$' })) {
        $lines = [System.IO.File]::ReadAllLines($f.FullName)
        for ($i = 0; $i -lt $lines.Count; $i++) {
            $line = $lines[$i]
            if ($line -notmatch '(?i)(background|background-color)\s*:') { continue }
            # 剥掉合法的 Token 引用（含 var(--n-x, #hex) 兜底值）
            $stripped = $line -replace 'var\(--n-[a-z0-9-]+,\s*#[0-9a-fA-F]{3,8}\)', ''
            $stripped = $stripped -replace 'var\(--[a-z0-9-]+,\s*#[0-9a-fA-F]{3,8}\)', ''
            $stripped = $stripped -replace 'var\(--n-[a-z0-9-]+\)', ''
            $stripped = $stripped -replace 'var\(--[a-z0-9-]+\)', ''
            # 剩余部分若仍含字面颜色（hex / rgb / rgba / 英文色名）即判脱离体系
            $hasLiteralColor = $stripped -match '#[0-9a-fA-F]{3,8}\b' -or
                $stripped -match '(?i)\brgba?\(' -or
                $stripped -match '(?i):\s*(white|black|red|blue|green|gray|grey|orange|purple)\b'
            if ($hasLiteralColor) {
                if (Test-Suppressed $line 'ui-hardcoded-color') { continue }
                Add-Finding 'ui-hardcoded-color' $f.FullName ($i + 1) ($line.Trim().Substring(0, [Math]::Min(80, $line.Trim().Length)))
            }
        }
    }
}

# ------------------------------------------------------------------ zh-slop-phrase
if ((-not $Tell -or $Tell -eq 'zh-slop-phrase')) {
    $scanFiles = [System.Collections.Generic.List[System.IO.FileInfo]]::new()
    foreach ($d in (Get-ChildItem -Recurse -File internal -Filter '*.go')) { $scanFiles.Add($d) }
    foreach ($d in (Get-ChildItem -Recurse -File frontend\src -Include '*.vue', '*.ts' | Where-Object { $_.FullName -notmatch 'node_modules|wailsjs' })) { $scanFiles.Add($d) }
    foreach ($f in $scanFiles) {
        $lines = [System.IO.File]::ReadAllLines($f.FullName)
        for ($i = 0; $i -lt $lines.Count; $i++) {
            $line = $lines[$i]
            foreach ($p in $zhSlopPatterns) {
                if ($line -match $p) {
                    if (Test-Suppressed $line 'zh-slop-phrase') { continue }
                    Add-Finding 'zh-slop-phrase' $f.FullName ($i + 1) "[$p] $($line.Trim().Substring(0, [Math]::Min(70, $line.Trim().Length)))"
                    break
                }
            }
        }
    }
}

# ------------------------------------------------------------------ code-placeholder
if ((-not $Tell -or $Tell -eq 'code-placeholder')) {
    foreach ($f in (Get-ChildItem -Recurse -File internal -Filter '*.go')) {
        $lines = [System.IO.File]::ReadAllLines($f.FullName)
        for ($i = 0; $i -lt $lines.Count; $i++) {
            $line = $lines[$i]
            foreach ($p in $placeholderPatterns) {
                if ($line -match $p) {
                    if (Test-Suppressed $line 'code-placeholder') { continue }
                    Add-Finding 'code-placeholder' $f.FullName ($i + 1) "[$p] $($line.Trim().Substring(0, [Math]::Min(70, $line.Trim().Length)))"
                    break
                }
            }
        }
    }
}

# ------------------------------------------------------------------ declared-seam
if ((-not $Tell -or $Tell -eq 'declared-seam')) {
    foreach ($f in (Get-ChildItem -Recurse -File internal -Filter '*.go')) {
        $lines = [System.IO.File]::ReadAllLines($f.FullName)
        for ($i = 0; $i -lt $lines.Count; $i++) {
            $line = $lines[$i]
            if ($line -match '预留' -and $line -match '未实现|尚未实现|待实现') {
                if (Test-Suppressed $line 'declared-seam') { continue }
                Add-Finding 'declared-seam' $f.FullName ($i + 1) ($line.Trim().Substring(0, [Math]::Min(80, $line.Trim().Length)))
            }
        }
    }
}

# ------------------------------------------------------------------ 输出
$byTell = $findings | Group-Object tell | Sort-Object Name
$failCount = @($findings | Where-Object { $_.severity -eq 'fail' }).Count
$warnCount = @($findings | Where-Object { $_.severity -eq 'warn' }).Count

if ($Json) {
    [pscustomobject]@{
        generatedAt = (Get-Date -Format 'o')
        fail        = $failCount
        warn        = $warnCount
        findings    = $findings
    } | ConvertTo-Json -Depth 5
} else {
    Write-Host ''
    Write-Host '=== 反 AI 垃圾检查（评估方案 Layer 2）===' -ForegroundColor Cyan
    foreach ($g in $byTell) {
        $meta = $tells[$g.Name]
        $color = if ($meta.Severity -eq 'fail') { 'Red' } else { 'Yellow' }
        $tag = if ($meta.Severity -eq 'fail') { 'FAIL' } else { 'WARN' }
        Write-Host ''
        Write-Host "[$tag] $($meta.Title)  ($($g.Count) 处)" -ForegroundColor $color
        Write-Host "      依据: $($meta.Clause) | 范围: $($meta.Scope)"
        foreach ($x in ($g.Group | Select-Object -First 12)) {
            Write-Host "      $($x.file):$($x.line)  $($x.evidence)"
        }
        if ($g.Count -gt 12) { Write-Host "      ... 其余 $($g.Count - 12) 处略" }
    }
    if (-not $findings) { Write-Host '未发现命中' -ForegroundColor Green }
    Write-Host ''
    Write-Host "合计: 失败 $failCount 项 / 告警 $warnCount 项" -ForegroundColor $(if ($failCount -gt 0) { 'Red' } else { 'Green' })
    Write-Host '说明: 命中只表示"此处命中某模式"，需人工分流后处置；确认为有意为之可在行尾加 slop-ignore: <tell-id>' -ForegroundColor DarkGray
}

if ($failCount -gt 0) { exit 1 }
exit 0
