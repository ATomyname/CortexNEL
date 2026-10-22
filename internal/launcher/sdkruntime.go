// SDK 运行库落位：保证 <natives>/runtime（即 -Druntime_path）里有权威清单的全部文件。
//
// 客户端的 com.netease.mc.mod.network.common.Library 从这里 System.load 网易 gray
// 组件，从而绑定 GetToken / reviewWord / reviewName / Skip32 / preInit 等 native。
// 缺件的后果是致命的（不是降级）：native 未绑定，客户端会在任意调用点抛
// UnsatisfiedLinkError —— 已实测三个炸点：启动期 GetToken（1.8.9）、物品注册期
// reviewWord（1.12.2，FilterHelper.doSdkFilter）、以及 Library.LoadLibrary 处
// Can't load library（1.21）。
//
// 词汇约定（四份文件各一名一责）：清单（有哪些文件）见 sdkruntime_manifest.go，
// 来源（从哪儿拿）见 sdkruntime_source.go，补件（把缺失的从 CDN 下载回来）见
// sdkruntime_fetch.go，落位（本文件，铺进 natives/runtime）。
//
// 这些 dll 不随任何 JRE 分发，也不在游戏包里，但可从网易 CDN 直接下载（证据见
// docs/facts/NETEASE-PROTOCOL.md §10.6），所以下载是补件的职责，本文件只负责
// 「按清单落位」。
//
// 缺件时尽力补齐、照常启动（不阻断），但缺项必须出现在报告里，供调用方留痕
// （规范 I2）—— 1.8.9 那次就是静默缺了 48 项，日志上只看得见「启动后崩了」。
package launcher

import (
	"bytes"
	"os"
	"path/filepath"
)

// SdkRuntimeReport 是一次 SDK 运行库落位的结果。
type SdkRuntimeReport struct {
	// SourceDir 是实际取件的来源目录；为空表示没找到任何齐全副本，全部走缺失。
	SourceDir string
	// Seeded 是本次新落位（含替换内容不一致的旧副本）的文件数。
	Seeded int
	// Missing 是落位后 natives/runtime 仍缺失的清单项，按清单序。
	Missing []string
}

// SeedSdkRuntime 按权威清单把 SDK 运行库落位到 nativesDir/runtime。
//
// 幂等：内容已一致的跳过，重复调用无重复 IO。落位只按清单取件，不搬运来源目录里的
// 其他内容 —— 旧规则把「JRE bin 里非 JDK 自带的 dll」当候选，结果给 1.8.9 灌了 40 个
// JDK8 自带库（deny list 是按 Temurin 17 标定的，对 JDK8 不生效）却一个清单项都没
// 补上，既污染又掩盖了真缺件。
func SeedSdkRuntime(sourceDir, nativesDir string) SdkRuntimeReport {
	report := SdkRuntimeReport{SourceDir: sourceDir}
	if nativesDir == "" {
		return report
	}
	dstDir := filepath.Join(nativesDir, "runtime")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		report.Missing = append([]string{}, sdkRuntimeManifest...)
		return report
	}

	if sourceDir != "" {
		for _, name := range sdkRuntimeManifest {
			if placeSdkRuntimeFile(filepath.Join(sourceDir, name), filepath.Join(dstDir, name)) {
				report.Seeded++
			}
		}
	}

	// 缺项以落位后的实际状态为准，而不是以「这次没写成」倒推：目标本就齐全时
	// 一次都不写，但缺项必须是空。
	for _, name := range sdkRuntimeManifest {
		if info, err := os.Stat(filepath.Join(dstDir, name)); err != nil || info.Size() == 0 {
			report.Missing = append(report.Missing, name)
		}
	}
	return report
}

// placeSdkRuntimeFile 按内容落位一个原生库：目标与源逐字节一致才跳过，返回本次是否写入。
//
// 判定必须逐字节而不是只比大小 —— 同尺寸的坏副本会被 System.load 留下，Library 的
// native 就绑定不上，而目录里「文件都在」，看不出缺什么。组件同步（modsync.go）
// 只比大小就够：条目在同步前已由上游按 iid+md5 对账（facts §3.7）。
func placeSdkRuntimeFile(src, dst string) (wrote bool) {
	data, err := os.ReadFile(src)
	if err != nil {
		return false
	}
	if existing, err := os.ReadFile(dst); err == nil && bytes.Equal(existing, data) {
		return false
	}
	return writeFileAtomic(dst, data)
}
