// Package launcher 把档案与会话参数翻译成白端客户端的可执行启动命令。
//
// 职责边界：
//   - 本包只装配命令（Exe / Args / WorkDir），不起进程 —— 进程生命周期属
//     internal/service（MirrorLauncher）。
//   - 本包不做网络请求：组件清单与基础客户端由用户或网易启动器预先落盘，
//     本包只探测既有目录（参考实现 Fantnel 的 InstallerService 负责下载，
//     nexus 不复制该职责，见 docs/design/ARCHITECTURE.md §4）。
//   - 协议派生（角色 UUID / Skip32）属 internal/netease，本包只调用。
//
// 平台：仅 Windows x64 为一等目标（规范 §2），非 Windows 分支只保证可编译与可测。
package launcher

// LaunchOptions 是一次白端启动的输入参数。
type LaunchOptions struct {
	// JavaPath 为空时按 Version 自动探测（javaruntime.go）。
	JavaPath string
	// ClientDir 是 .minecraft 根目录；为空时自动探测（clientdir.go）。
	ClientDir string
	// RoleName 是进服角色名，为空时用 defaultRoleName。
	RoleName string
	// UserID 是网易 entity_id，用于派生角色 UUID 与 -DuserId。
	UserID int64
	// LocalPort 是本机 relay 代理监听端口，客户端连它而不是真实服务器。
	LocalPort int
	// ServerHost 为非空时表示直连目标服务器地址（如官方 IP），覆盖默认的 127.0.0.1 本机 relay。
	ServerHost string
	// ServerPort 为目标服务器端口；大于 0 且 ServerHost 非空时使用。
	ServerPort int
	// AuthPort 是本地 AuthLib 鉴权代答监听端口（internal/authsock）。
	// 非空时作为 JVM 系统属性 -DlauncherControlPort 的首选值传递给客户端。
	AuthPort int
	// JvmArgs 覆盖默认 JVM 调优参数（身份类 -D 参数仍会追加）。
	JvmArgs []string
	// CustomLaunchArgs 是完整的用户自定义参数模板，支持 {{...}} 占位符。
	CustomLaunchArgs []string
	// Version 是目标 Minecraft 版本号，为空时自动探测。
	Version string
	// GameID 是网易服务器 GameID；非空时启用按服隔离的运行沙盒。
	GameID string
	// ControlPort 是 internal/clientcontrol 控制通道的实际监听端口。
	// 真契约是 JVM 系统属性 -DlauncherControlPort（客户端组件
	// com.netease.mc.mod.authlib.authlibWrapper 取 System.getProperty("launcherControlPort")，
	// 官方启动器的 java_args 同样是 -DlauncherControlPort=<port>，见 docs/facts/NETEASE-PROTOCOL.md §10）。
	// --userProperties 的 launcherport 字段一并写入，但它不是自动进服的必要条件。
	// 为 0 时不写这两处：宁可不给端口，也不能给一个没有监听的端口。
	ControlPort int
	// ComponentCacheDir 是 nexus 组件缓存目录：该服按网关权威清单下载下来的组件
	// （internal/install 的产物，见 clientdata 的目录约定）。非空时作为沙盒组件播种的
	// 最后一路叠加来源 —— 排在网易缓存之后，避免本地旧副本盖掉刚下回来的正确版本。
	ComponentCacheDir string
	// AuthToken 是网易账号 token 的明文，用于派生 -DToken。
	// 为空时不写 -DToken（客户端会走无 token 分支）。
	AuthToken string
}

// LaunchSpec 是可直接交给进程启动器执行的命令三元组。
type LaunchSpec struct {
	Exe     string
	Args    []string
	WorkDir string
	// NativeLibDirs 是客户端原生库所在目录，启动时必须加进子进程 PATH。
	//
	// 为什么需要：Windows 的 LoadLibrary 解析被加载 DLL 的**依赖**时，只认
	// 应用程序目录、系统目录与 PATH，不认 java.library.path。实测
	// （2026-09-22，MC 1.12.2）：
	//   - 不加 PATH：org.lwjgl.Sys 报 "no lwjgl64 in java.library.path"，
	//     而 lwjgl64.dll 就在 -Djava.library.path 指向的 natives 目录里
	//     （与官方启动器那份 SHA256 一致，Windows LoadLibrary 直接加载也成功）
	//   - 把 natives 加进 PATH：该错误消失，LWJGL 正常初始化
	// 官方启动器把这些 DLL 放在 JRE bin（即应用目录）里，天然位于搜索路径
	// 首位；nexus 的 JRE 由 CDN 部署、不含它们，故改用 PATH。
	//
	// 两项分别对应：natives（LWJGL/OpenAL/jinput 原生库）与
	// natives/runtime（网易 SDK 的 gray 组件与其依赖 libenvsdk.dll）。
	NativeLibDirs []string
	// Runtime 是本次运行目录准备的同步结果，供调用方记录到日志。
	// 沙盒曾静默丢掉基础核心组件而无人察觉，同步结果必须可核实（规范 I2）。
	Runtime RuntimeSyncReport
	// SdkRuntime 是本次 natives/runtime 权威清单落位结果，与运行目录准备是两个领域
	// （原生库 vs 沙盒），故不并入 RuntimeSyncReport。缺项必须可核实（规范 I2）：
	// 缺件时客户端会在 native 调用处直接 UnsatisfiedLinkError，不是降级。
	SdkRuntime SdkRuntimeReport
}

const (
	// defaultRoleName 是未指定角色名时的占位角色。
	defaultRoleName = "Steve"
	// defaultMainClass 是版本清单缺失 mainClass 时的原版入口类。
	defaultMainClass = "net.minecraft.client.main.Main"
	// defaultMCVersion 是版本探测全部失败时的兜底版本。
	defaultMCVersion = "1.20"
	// relayHost 是客户端要连接的本机 relay 地址（facts §7.1：不直连真实服务器）。
	relayHost = "127.0.0.1"
)
