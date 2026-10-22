# 从源码构建

> 只想用的话直接下 Releases 里的 exe，不需要看这里。

## 环境要求

| 项 | 要求 |
|---|---|
| 系统 | Windows 10/11 x64（唯一支持平台，非 amd64 启动即退出） |
| Go | 1.25+，版本以 `go.mod` 为准 |
| Node.js | 20+ |
| Wails | v2 CLI，见下 |
| 游戏 | 本机已安装网易版 Minecraft 客户端 |

装 Wails CLI，并确认 `$(go env GOPATH)\bin` 在 `PATH` 里：

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

## 构建

构建脚本是唯一受支持的入口，顺序固定为 `npm install` → `npm run build` → `wails build`，并强制 `GOARCH=amd64`：

```powershell
git clone https://github.com/ATomyname/CortexNEL.git
cd CortexNEL
pwsh scripts/build.ps1
```

产物在 `build/bin/CortexNEL.exe`。

发布用混淆构建（garble，固定种子，产物可复现）：

```powershell
pwsh scripts/build.ps1 -Obfuscate
```

> 不要手动拼 `wails build`。脚本里的环境覆盖与 garble 参数是构建契约的一部分，绕过会让产物形态偏离。

开发调试用热更：

```powershell
wails dev
```

## 提交前校验

```powershell
pwsh scripts/check.ps1         # gofmt / go vet / go test + 行数禁线 + 引擎中立 + 命名禁令 + 凭据扫描
pwsh scripts/check-slop.ps1    # 界面 Emoji / 硬编码颜色 / 套话 / 占位标记 / 预留 seam
cd frontend; npm run typecheck # vue-tsc --noEmit
```

CI 跑的就是这几条，判据单源在脚本里，workflow 只装配环境。

## 常见构建问题

**`wails` 不在 PATH**：把 Go 的 bin 目录加进去再重跑。

```powershell
$env:Path = 'C:\Users\<you>\go\bin;' + $env:Path
```

**构建后 `frontend/dist/index.html` 变了**：`npm run build` 覆盖了提交用的占位文件，提交前还原。

```powershell
git restore frontend/dist/index.html
```

**`go build` 报 `pattern all:frontend/dist: no matching files`**：`frontend/dist` 被清空了。保留或恢复 `frontend/dist/index.html` 即可，`go:embed` 只要求目录非空。

**CI 上 gofmt 全仓失败**：行尾符问题。仓库用 `.gitattributes` 强制 LF，Windows 本地若 `core.autocrlf=true`，检出会变 CRLF 导致 gofmt 判未格式化。跑一次 `git config core.autocrlf input` 再重新检出。

**杀软报毒**：混淆构建的产物会被部分引擎误判。用未混淆构建（`pwsh scripts/build.ps1`）或自行从源码编译。
