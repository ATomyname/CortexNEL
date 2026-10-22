# 架构说明

> 本文是 `README.md` 里被省略的实现细节：模块划分、依赖方向、关键机制与已知边界。
> 只想用的话不必读这里。

## 分层与依赖方向

依赖只允许单向向下，禁止反向 import：

```
main.go / internal/app      组装 + Wails 绑定，只转发
  ↓
internal/service            用例编排：登录、进服一条龙、镜像启动
  ↓
internal/session            领域：会话状态机
internal/account            领域：协议账号（认证、EntityID、token）
internal/profile            领域：档案（客户端路径 / JVM 参数 / mods 快照）
internal/namegen            领域：角色随机命名与同音字变形
  ↓
internal/relay              基础设施：帧管线引擎（双 leg、编解码、注册、派发）
internal/netease            基础设施：网易协议（init/join/签名/登录链/角色 UUID）
internal/launcher           基础设施：启动命令装配（目录 / Java / 参数 / 沙盒）
internal/clientcontrol      基础设施：启动器 ↔ 本机客户端的环回控制通道
internal/clientdata         基础设施：客户端数据目录布局与选址
internal/install            基础设施：客户端本体与服务器组件安装
internal/archive            基础设施：压缩包下载 / 校验 / 安全解压
internal/crypto             基础设施：ChaCha8 / AES-CFB8 / AES-ECB / Skip32 / 网关签名
internal/captchaocr         基础设施：登录验证码 CNN 识别
internal/store              基础设施：版本化配置落盘 + DPAPI 凭据保护
internal/telemetry          基础设施：结构化 JSONL 日志 + 事件总线
```

## 模块地图

```
main.go                    进程入口：只做 Wails 装配与注册
internal/app               绑定层：参数校验 + DTO 映射 + 转发，无业务决策
internal/service           用例编排
  ├─ join_game.go          进服一条龙：起代理 → 启镜像 → 等 LIVE
  ├─ mirror_launch.go      镜像启动编排：开控制通道 → 装配参数 → 起进程
  ├─ login.go              网易登录用例
  └─ launch.go             镜像客户端进程生命周期
internal/session           状态机 IDLE→ATTACH→TAP→AUTH→MIRROR→LIVE→CLOSED
internal/relay             帧管线引擎
  ├─ packet.go             Packet 契约与注册键
  ├─ conn.go               单连接双 leg 状态 + 收包派发
  ├─ stage/                framer / cipher / compressor
  └─ packet/
     ├─ login/             握手、LoginStart、EncReq/Resp、压缩、Configuration
     ├─ heypixel/          服务器适配（GameID 作用域）
     └─ ecpack/            资源包推送改写
internal/netease           网易协议
  ├─ gateway.go            网关客户端核心与端点常量
  ├─ yggdrasil.go          init / join 帧构造与流程
  ├─ roleuuid.go           角色 UUID 派生（单一来源）
  ├─ com4399.go            4399com 密码链与验证码
  ├─ mpay.go               mPay 设备注册与 x19 邮箱/手机号登录
  └─ httpcrypt.go          HTTP AES-CBC 加密传输
internal/launcher          启动命令装配（只装配，不起进程）
  ├─ launch.go             补齐缺省 → 准备沙盒 → 装配命令
  ├─ sandbox.go            按服隔离运行沙盒与同步报告
  └─ javaruntime.go        Java 探测（JAVA_HOME → PATH → 已知安装位置）
internal/clientcontrol     控制通道：帧格式、操作码、JoinTarget 推送
frontend/                  Vue 3 + TS + Vite + Pinia，产物由 go:embed 内嵌
```

## 三个关键机制

**环回代理即会话持有者。** 客户端启动参数里的 `--server` 恒为本机 `127.0.0.1:<port>`，真实上游由代理自己拨号。早期曾按"官方命令行形状"把 `--server` 写成服务器公开域名，实测导致客户端绕开代理直连、白端进服全部超时，该方案已移除。

**加密代答。** 上游的 EncryptionRequest 被吞掉不透传，客户端 leg 保持明文，代理成为加密会话持有者；共享密钥只存在于会话内存，结束时清零。

**惰性解析。** 管线命中处理器才重序列化，未命中的包按原始字节转发，零字段解析。处理器顺序在会话启动时冻结，相同输入产生相同输出。

## 引擎协议中立

服务器域名、brand 字符串、GameID 字面量只允许出现在 `internal/relay/packet/<server>/` 下。`scripts/check.ps1` 用 grep 门禁守住这条：引擎文件（`internal/relay/*.go`、`stage/`、`packet/login/`）里出现这些字面量即判失败。

这样服务器适配是注册式扩展，与引擎隔离——将来要拆成插件，整目录迁出即可，引擎零改动。

## 客户端隔离

按服生成独立运行沙盒：基础 gameDir 播种 + 服务器组件叠加，`config` / `defaultconfigs` / `options.txt` 分层写入。会话期不写原客户端目录，不向进程注入任何 DLL。

## 已知边界

- 只实现 Java 线。基岩版（PE）服务器在列表里可见，但不提供进服实现。
- 目标服务器必须与客户端协议版本匹配，跨大版本转译不在本仓库范围。
- 登录链依赖网易线上接口，接口变形会让对应登录方式失效，属于预期内的维护面。
- 影子端（第二客户端观战）依赖宿主机上另一份客户端实例，未做端到端真机验收。
