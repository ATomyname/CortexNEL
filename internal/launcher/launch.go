// 白端启动编排：解析缺省值 → 准备运行沙盒 → 装配命令。
package launcher

import (
	"path/filepath"
	"runtime"

	"nexus/internal/netease"
)

// BuildMinecraftLaunch 装配一次白端启动命令。
//
// 未显式给出的 JavaPath / ClientDir / Version 会自动探测；GameID 非空时
// 客户端在按服隔离的运行沙盒中启动，工作目录即沙盒目录。
func BuildMinecraftLaunch(opts LaunchOptions) (LaunchSpec, error) {
	resolved := resolveOptions(opts)

	uuid, err := netease.RoleUUID(resolved.RoleName, uint32(resolved.UserID))
	if err != nil {
		return LaunchSpec{}, err
	}

	// 沙盒只准备一次：它带副作用（播种基础组件、叠加服务器组件、写 options.txt），
	// 重复调用只会产生重复 IO。
	var runtimeReport RuntimeSyncReport
	workDir := resolved.ClientDir
	if resolved.GameID != "" {
		if sandbox, report := PrepareGameRuntime(RuntimeSource{
			BaseGameDir:       resolved.ClientDir,
			ComponentCacheDir: resolved.ComponentCacheDir,
			GameID:            resolved.GameID,
			RoleName:          resolved.RoleName,
		}); sandbox != "" {
			workDir = sandbox
			runtimeReport = report
		}
	} else {
		// 无按服沙盒时客户端直接以基础 gameDir 启动，组件目录就是它自己的 mods。
		// 这里同样要撤销旧规则的误停用，否则存量安装会一直缺核心组件。
		runtimeReport.RestoredCoreMods = RestoreDisabledCoreComponents(filepath.Join(resolved.ClientDir, "mods"))
		EnsureOptionsTxt(resolved.ClientDir)
	}

	manifest, hasManifest := readVersionManifest(resolved.ClientDir, resolved.Version)
	nativesDir := resolveNativesDir(resolved.ClientDir, resolved.Version)
	// 按权威清单把 SDK 运行库落位到 natives/runtime（-Druntime_path），否则客户端
	// 的 Library 原生方法绑定不上，任意调用点直接 UnsatisfiedLinkError。
	// 取件来源见 ResolveSdkRuntimeSource，缺件由 CDN 补齐。
	sdkReport := SeedSdkRuntime(ResolveSdkRuntimeSource(resolved.ClientDir), nativesDir)
	// 客户端原生库目录：加进子进程 PATH 后 Windows 才能解析它们的依赖
	// （见 LaunchSpec.NativeLibDirs 的实测说明）。
	nativeLibDirs := []string{nativesDir, filepath.Join(nativesDir, "runtime")}
	ctx := launchContext{
		ClientDir:   resolved.ClientDir,
		WorkDir:     workDir,
		Version:     resolved.Version,
		RoleName:    resolved.RoleName,
		UUID:        uuid,
		UserID:      resolved.UserID,
		GameID:      resolved.GameID,
		LocalPort:   resolved.LocalPort,
		ServerHost:  resolved.ServerHost,
		ServerPort:  resolved.ServerPort,
		ControlPort: resolved.ControlPort,
		AuthPort:    resolved.AuthPort,
		AuthToken:   resolved.AuthToken,
		AssetsDir:   filepath.Join(resolved.ClientDir, "assets"),
		AssetIndex:  manifest.assetIndexID(resolved.Version),
		NativesDir:  nativesDir,
		LibsDir:     filepath.Join(resolved.ClientDir, "libraries"),
		MainClass:   manifest.mainClass(),
		// 握手参数只装配一次：filterkey 是随机值，重复装配会产生两个取值。
		UserProperties:   buildUserProperties(resolved.UserID, resolved.ControlPort),
		UserPropertiesEx: buildUserPropertiesEx(),
	}
	cpList := resolveClassPath(resolved.ClientDir, resolved.Version, manifest, hasManifest)
	ctx.ClassPath = joinClassPath(cpList)
	ctx.ClassPathList = cpList

	// 用户给出完整参数模板时按模板全量覆盖，只做占位符替换。
	if len(resolved.CustomLaunchArgs) > 0 {
		args := make([]string, 0, len(resolved.JvmArgs)+len(resolved.CustomLaunchArgs))
		args = append(args, resolved.JvmArgs...)
		args = append(args, resolved.CustomLaunchArgs...)
		return LaunchSpec{
			Exe:           resolved.JavaPath,
			Args:          substituteUserPlaceholders(ctx, args),
			WorkDir:       workDir,
			NativeLibDirs: nativeLibDirs,
			Runtime:       runtimeReport,
			SdkRuntime:    sdkReport,
		}, nil
	}

	args := buildJVMArgs(ctx, resolved.JvmArgs, manifest.jvmArgumentTemplate())
	args = append(args, buildGameArgs(ctx, manifest.gameArgumentTemplate())...)
	return LaunchSpec{
		Exe:           resolved.JavaPath,
		Args:          args,
		WorkDir:       workDir,
		NativeLibDirs: nativeLibDirs,
		Runtime:       runtimeReport,
		SdkRuntime:    sdkReport,
	}, nil
}

// resolveOptions 补齐 RoleName / ClientDir / Version / JavaPath 的缺省值。
func resolveOptions(opts LaunchOptions) LaunchOptions {
	if opts.RoleName == "" {
		opts.RoleName = defaultRoleName
	}
	if opts.ClientDir == "" || opts.Version == "" {
		dir, _, version := DetectServerClientDir(opts.GameID, opts.Version)
		if opts.ClientDir == "" {
			opts.ClientDir = dir
		}
		if opts.Version == "" {
			opts.Version = version
		}
	}
	if opts.Version == "" {
		opts.Version = defaultMCVersion
	}
	if opts.JavaPath == "" {
		opts.JavaPath = detectJavaOrFallback(opts.Version)
	}
	return opts
}

// detectJavaOrFallback 探测 Java，全部失败时退回让系统 PATH 解析的命令名，
// 使错误暴露在进程启动阶段而不是拿空路径 exec。
func detectJavaOrFallback(version string) string {
	if path, err := DetectJavaForVersion(version); err == nil && path != "" {
		return path
	}
	if runtime.GOOS == "windows" {
		return "javaw.exe"
	}
	return "java"
}

// joinClassPath 用平台分隔符拼接 -cp。
func joinClassPath(paths []string) string {
	sep := classPathSeparator()
	out := ""
	for i, p := range paths {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}
