# 参与开发

## 提交前必须跑

```powershell
pwsh scripts/check.ps1         # L0 静态 + L1 单元 + 仓库不变量
pwsh scripts/check-slop.ps1    # 反 AI 垃圾层
cd frontend; npm run typecheck
```

`check.ps1` 覆盖：gofmt、go vet、go test、行数禁线（>800 失败 / >500 告警）、引擎协议中立 grep、包名与命名后缀禁令、凭据样式明文扫描。

`check-slop.ps1` 覆盖：界面非受控 Emoji、脱离设计 Token 的硬编码颜色、中文套话、占位标记、显式预留但未实现的 seam。命中只表示"此处命中某模式"，需人工分流；确认为有意为之可在行尾加 `slop-ignore: <tell-id>`。

## 提交信息

Conventional Commits：`<type>(<scope>): <description>`，type 限 `feat / fix / docs / refactor / test / chore / ci / perf`。一个提交只含一个逻辑变化。

## 代码约定

- 一个文件一个可说明的职责。>500 行需在说明里给出保留理由，>800 行禁止。
- 绑定层（`internal/app`）只做参数校验 + DTO 映射 + 转发，业务决策放 `internal/service` 或领域包。
- 包名不用 `core / common / utils / misc / helpers` 这类兜底名。
- 文件名禁 `_new / _old / _final / _v2 / _copy / _backup / _tmp` 后缀。
- 测试文件与被测对象同名：`session_keepalive_test.go` 配 `session_keepalive.go`。
- 时序测试用注入 clock / deadline，禁止靠 sleep。
- 凭据只经 `internal/store` 落盘；敏感值不进日志与遥测事件。

## 测试阶梯

| 档 | 内容 | 时机 |
|---|---|---|
| L0 静态 | gofmt / go vet / vue-tsc | 每次提交 |
| L1 单元 | 领域包离线单测 | 每次提交 |
| L2 集成 | loopback 管线端到端 | 涉及协议与管线改动 |
| L3 真机 | 对真实服务器冒烟 | 维护者在环时 |

## 改动协议相关代码时

协议常量取自 `internal/netease/constants.go`，每条都注明来源。改协议行为前先确认对应的端点与帧格式，不要凭猜测改常量。

新增服务器适配时，处理器放 `internal/relay/packet/<server>/`，域名与 GameID 字面量只能出现在该目录内——这是 `check.ps1` 会拦的硬约束。
