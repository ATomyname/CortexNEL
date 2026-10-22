// SDK 运行库包的权威清单。
//
// 事实单源见 docs/facts/NETEASE-PROTOCOL.md §10.6（客户端 natives/runtime 权威清单）。
// 本文件只保留取值与其分组理由，证据与推导过程一律留在 facts 文档，避免两处漂移。
package launcher

// sdkRuntimeClientChecked 是客户端 Common.checkFile 逐项校验并 SafeLoadLibrary
// 的 50 个文件名。实测判定依据：官方跑通的 1.12.2 会话日志里 checkFile True 恰好
// 50 条，且命中集合与本清单逐一对应；1.8.9 会话日志 checkFile True 恰好 2 条，
// 与它 natives/runtime 里恰好只有 2 个命中本清单完全吻合。
//
// 其中 api-ms-win-crt-utility-l1-1-1.dll（约 21 MB）是网易 gray 组件本体，
// JNI 导出 GetToken / reviewWord / reviewName / Skip32 / preInit ——
// com.netease.mc.mod.network.common.Library 的 native 方法全部由它提供。
var sdkRuntimeClientChecked = []string{
	"api-ms-win-core-console-l1-1-0.dll",
	"api-ms-win-core-datetime-l1-1-0.dll",
	"api-ms-win-core-debug-l1-1-0.dll",
	"api-ms-win-core-errorhandling-l1-1-0.dll",
	"api-ms-win-core-file-l1-1-0.dll",
	"api-ms-win-core-file-l1-2-0.dll",
	"api-ms-win-core-file-l2-1-0.dll",
	"api-ms-win-core-handle-l1-1-0.dll",
	"api-ms-win-core-heap-l1-1-0.dll",
	"api-ms-win-core-interlocked-l1-1-0.dll",
	"api-ms-win-core-libraryloader-l1-1-0.dll",
	"api-ms-win-core-localization-l1-2-0.dll",
	"api-ms-win-core-memory-l1-1-0.dll",
	"api-ms-win-core-namedpipe-l1-1-0.dll",
	"api-ms-win-core-processenvironment-l1-1-0.dll",
	"api-ms-win-core-processthreads-l1-1-0.dll",
	"api-ms-win-core-processthreads-l1-1-1.dll",
	"api-ms-win-core-profile-l1-1-0.dll",
	"api-ms-win-core-rtlsupport-l1-1-0.dll",
	"api-ms-win-core-string-l1-1-0.dll",
	"api-ms-win-core-synch-l1-1-0.dll",
	"api-ms-win-core-synch-l1-2-0.dll",
	"api-ms-win-core-sysinfo-l1-1-0.dll",
	"api-ms-win-core-timezone-l1-1-0.dll",
	"api-ms-win-core-util-l1-1-0.dll",
	"api-ms-win-core-xstate-l2-1-0.dll",
	"api-ms-win-crt-conio-l1-1-0.dll",
	"api-ms-win-crt-convert-l1-1-0.dll",
	"api-ms-win-crt-environment-l1-1-0.dll",
	"api-ms-win-crt-filesystem-l1-1-0.dll",
	"api-ms-win-crt-heap-l1-1-0.dll",
	"api-ms-win-crt-locale-l1-1-0.dll",
	"api-ms-win-crt-math-l1-1-0.dll",
	"api-ms-win-crt-multibyte-l1-1-0.dll",
	"api-ms-win-crt-private-l1-1-0.dll",
	"api-ms-win-crt-process-l1-1-0.dll",
	"api-ms-win-crt-runtime-l1-1-0.dll",
	"api-ms-win-crt-stdio-l1-1-0.dll",
	"api-ms-win-crt-string-l1-1-0.dll",
	"api-ms-win-crt-time-l1-1-0.dll",
	"api-ms-win-crt-utility-l1-1-0.dll",
	"api-ms-win-crt-utility-l1-1-1.dll",
	"libcrypto-1_1-x64.dll",
	"libssl-1_1-x64.dll",
	"msvcp140.dll",
	"msvcr120.dll",
	"SecureEngineSDK64.dll",
	"ucrtbase.dll",
	"vcruntime140.dll",
	"WinlicenseSDK64.dll",
}

// sdkRuntimeLoadDeps 是不在客户端 checkFile 清单里、但 gray 组件加载所必需的运行库。
// 目前只有 libenvsdk.dll —— gray 组件的链接依赖（gray DLL 内含 libenvsdk 符号串）。
//
// 有意不含 WinFallbackLookup.dll：它不被 gray 组件引用（DLL 内无该符号串），而且是
// JDK 自带运行库（Temurin/Zulu 17 的 bin\ 下都有），既不该算进 SDK 包，也不该成为
// 新装机器的补齐阻塞项。
var sdkRuntimeLoadDeps = []string{
	"libenvsdk.dll",
}

// sdkRuntimeManifest 是 SDK 运行库包的完整清单（50 + 1）。
//
// 这份清单是版本无关的：1.8.9 / 1.12.2 / 1.20 / 1.21 共用同一套，且每一项都能从
// 网易 CDN 直接下载（见 sdkruntime_fetch.go）。因此任何一份齐全副本都是合法取件
// 来源，见 ResolveSdkRuntimeSource。
var sdkRuntimeManifest = append(append([]string{}, sdkRuntimeClientChecked...), sdkRuntimeLoadDeps...)
