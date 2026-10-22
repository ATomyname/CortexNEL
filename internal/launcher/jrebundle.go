// 网易 JRE 分发包部署：本机缺匹配大版本的 Java 时，从官方 CDN 拉取已实证的
// JRE 分发包并解压到本应用数据目录（不写用户的网易安装目录，P6）。
//
// 实测证据（2026-09-16 首轮，2026-09-21 补全，2026-09-22 补 MD5，见 facts §10.5）：
//   - 分发 URL：https://x19.gdl.netease.com/<包名>.7z（CDN 直链，无需鉴权）
//   - 存在两代命名，都可能被启动器使用：
//     ① `jre-v64-<构建日期>.7z`：jre-v64-220420.7z（197MB，仅含 jdk17/）
//     ② `<jdk大版本>_v64_v1.7z`：jdk8_v64_v1.7z / jdk17_v64_v1.7z / jdk22_v64_v1.7z
//   - jdk8_v64_v1.7z 实证（2026-09-21）：41,818,948 B，顶层目录 `jre8/`，
//     解压后 javaw -version = 1.8.0_60（Oracle JRE 8u60，64 位）——
//     MC ≤1.16 档位的 Java 8 来源自此确认，不再是"名字未发现"。
//   - jdk22_v64_v1.7z 实证（2026-09-21）：165,584,359 B，顶层目录 `jdk22/`，
//     含 jdk22/bin/javaw.exe —— 覆盖 RequiredJavaMajors 的 1.20.5+ 档位。
//   - 三个包的 MD5 实测（2026-09-22，全量下载后计算，字节数与登记 Size 逐一
//     吻合交叉印证）。官方 CDN 不发布清单，这个登记表就是本应用的 JRE 清单。
//   - jdk21 无分发包（facts §10.5：官方 ext 下只有 jdk17 与 jdk22）。
//     1.20.5+ 的 RequiredJavaMajors 返回 {21,22}，jdk22 满足其一即可。
//   - 旧命名 jre-v32-* 已废弃（404）。
package launcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nexus/internal/archive"
	"nexus/internal/httpx"
	"nexus/internal/netease"
)

// cdnURL 把分发包文件名接到 CDN 下载根。下载根可经 NEXUS_NETEASE_CDN 整体
// 切换到同路径结构的镜像（见 netease.CDNBase）—— JRE、SDK 运行库、基岩整包
// 同一个主机，主机不可达时一处覆盖三处生效。
func cdnURL(name string) string {
	return netease.CDNBase() + "/" + name
}

// jreBundle 是一个已实证的网易 JRE 分发包。
type jreBundle struct {
	// URL 是 CDN 直链。
	URL string
	// Major 是包内 JDK 的大版本。
	Major int
	// TopDir 是归档内的顶层目录名（解压后 javaw 位于 <TopDir>/bin/javaw.exe）。
	TopDir string
	// Size 用于下载进度展示（可 0）。
	Size int64
	// MD5 是整包 7z 的指纹（小写 hex）。下载后校验，不符即删 —— 没有 MD5 的
	// 登记项不允许部署（截断/损坏的包会被解出一棵看似完整的半个树）。
	MD5 string
}

// knownJREBundles 是已实证的分发包登记表（新包发现后在此登记）。
//
// 同一大版本有多个包时保留最先实证可用的那个（jdk17 已有 jre-v64-220420
// 在产，不重复登记同代的 jdk17_v64_v1）。jdk21 官方无包，靠 jdk22 覆盖。
var knownJREBundles = []jreBundle{
	{
		URL:    cdnURL("jdk8_v64_v1.7z"),
		Major:  8,
		TopDir: "jre8",
		Size:   41818948,
		MD5:    "a6d149b133042af5708fe7065a37b241",
	},
	{
		URL:    cdnURL("jre-v64-220420.7z"),
		Major:  17,
		TopDir: "jdk17",
		Size:   197277682,
		MD5:    "16c91500a2187677c358388fb42d7242",
	},
	{
		URL:    cdnURL("jdk22_v64_v1.7z"),
		Major:  22,
		TopDir: "jdk22",
		Size:   165584359,
		MD5:    "35f12cb97ab9b24766275cf6922919c9",
	},
}

// JreProgress 是 JRE 部署进度回调（stage: download/extract/done；done/total 字节）。
type JreProgress func(stage, message string, done, total int64)

// jreLayout 是一个分发包在 destDir 下的一种落位形态：包目录 + 对应的安装标记。
type jreLayout struct {
	// root 是包目录，javaw 位于 <root>/<TopDir>/bin/javaw.exe。
	root string
	// marker 是安装标记路径，内容 = 分发包 MD5。
	marker string
}

// jreLayouts 返回该分发包可能落位的形态。首选 <destDir>/<包名 stem>（本包
// 部署的固定落点）；候选 <destDir>/<TopDir> 是早期版本的扁平落点，仅用于兼容
// 旧机器 —— 旧形态没有标记文件，会被判为未部署并重新部署一次（一次性带宽
// 代价，换来判据统一）。
func jreLayouts(destDir string, b jreBundle) []jreLayout {
	stem := strings.TrimSuffix(filepath.Base(b.URL), ".7z")
	return []jreLayout{
		{root: filepath.Join(destDir, stem), marker: filepath.Join(destDir, stem+".md5")},
		{root: filepath.Join(destDir, b.TopDir), marker: filepath.Join(destDir, b.TopDir+".md5")},
	}
}

// EnsureJre 保证本机存在满足 majors 中任一大版本的 Java，返回 javaw 路径。
//
// 顺序：已部署且标记命中的（直接命中）→ 已登记分发包下载部署（MD5 校验）→
// 全部失败时返回可读错误。progress 可为 nil。
//
// 部署全程铁律：任何一步失败都清干净 —— 半个解压树或残缺归档绝不能留下来，
// 否则下次启动会被"已部署"判据当成可用运行时，从此永远不再重下（实测踩过：
// 存在性判据 + 不回滚的解压 = 永久卡死）。
func EnsureJre(ctx context.Context, majors []int, destDir string, progress JreProgress) (string, error) {
	if len(majors) == 0 {
		return "", fmt.Errorf("launcher: 未指定需要的 Java 大版本")
	}
	report := func(stage, message string, done, total int64) {
		if progress != nil {
			progress(stage, message, done, total)
		}
	}
	// ① 已部署且标记命中的优先：静默命中，不探测、不打告警。
	for _, b := range knownJREBundles {
		if !intIn(b.Major, majors) {
			continue
		}
		if path := deployedJavaw(destDir, b); path != "" {
			return path, nil
		}
	}
	// ② 下载部署：只拉大版本匹配的包。
	for _, b := range knownJREBundles {
		if !intIn(b.Major, majors) {
			continue
		}
		stem := filepath.Base(b.URL)
		bundlePath := filepath.Join(destDir, stem)
		extractDir := filepath.Join(destDir, strings.TrimSuffix(stem, ".7z"))
		if err := os.MkdirAll(filepath.Dir(bundlePath), 0o755); err != nil {
			return "", err
		}
		report("download", "下载 JRE 分发包 "+stem, 0, b.Size)
		if err := archive.Download(ctx, httpx.NewClient(0), b.URL, bundlePath,
			func(done, total int64) { report("download", "下载 JRE 分发包 "+stem, done, total) }); err != nil {
			_ = os.Remove(bundlePath) // 残缺归档不能留着冒充完整包
			return "", fmt.Errorf("launcher: 下载 JRE 分发包失败: %w", err)
		}
		if !archive.Matches(bundlePath, b.MD5) {
			_ = os.Remove(bundlePath)
			return "", fmt.Errorf("launcher: JRE 分发包 %s 校验失败（MD5 不匹配），包可能已更新或下载损坏", stem)
		}
		report("extract", "解压 JRE 分发包 "+stem, 0, b.Size)
		if err := archive.Extract(archive.ExtractRequest{
			Archive: bundlePath,
			Dest:    extractDir,
			Ctx:     ctx,
		}); err != nil {
			_ = os.RemoveAll(extractDir) // 半个树不能留给存在性判据
			_ = os.Remove(bundlePath)
			return "", fmt.Errorf("launcher: 解压 JRE 分发包: %w", err)
		}
		javaw := filepath.Join(extractDir, b.TopDir, "bin", "javaw.exe")
		if !isExistingFile(javaw) {
			_ = os.RemoveAll(extractDir)
			_ = os.Remove(bundlePath)
			return "", fmt.Errorf("launcher: JRE 分发包解压后未找到 %s/bin/javaw.exe", b.TopDir)
		}
		marker := filepath.Join(destDir, strings.TrimSuffix(stem, ".7z")+".md5")
		if !writeFileAtomic(marker, []byte(strings.ToLower(b.MD5))) {
			_ = os.RemoveAll(extractDir)
			_ = os.Remove(bundlePath)
			return "", fmt.Errorf("launcher: 写 JRE 安装标记失败: %s", marker)
		}
		_ = os.Remove(bundlePath)
		report("done", "JRE 部署完成", b.Size, b.Size)
		return javaw, nil
	}
	return "", fmt.Errorf("launcher: CDN 上没有匹配 Java %s 的已实证分发包", intList(majors))
}

// FindDeployedJavaw 返回 destDir 下已部署且标记命中的 javaw 路径（没有则空串）。
// 只查盘，不下载。
//
// 与 EnsureJre 的分工：EnsureJre 是"保证可用"（缺了就下载），本函数是"是否已有"。
// 调用方（ensureMirrorJava）需要先问一遍：已有就直接用，省掉一次 java -version
// 子进程，也不会打出误导性的"大版本不匹配"告警。
func FindDeployedJavaw(majors []int, destDir string) string {
	for _, b := range knownJREBundles {
		if !intIn(b.Major, majors) {
			continue
		}
		if path := deployedJavaw(destDir, b); path != "" {
			return path
		}
	}
	return ""
}

// deployedJavaw 返回某个分发包已部署且标记命中的 javaw 路径（未部署返回空）。
//
// 判据是「javaw 存在 且 标记内容 = 分发包 MD5」，缺一不可：只有存在性判据时，
// 一次中断的解压留下的半个树会被当成已部署，永远不再重下。标记由部署成功后
// 原子写入，所以它同时证明"整包解压完整落位过"。
// 没有 MD5 的登记项永远不会命中（登记表不允许无指纹条目）。
func deployedJavaw(destDir string, b jreBundle) string {
	if b.MD5 == "" {
		return ""
	}
	for _, l := range jreLayouts(destDir, b) {
		javaw := filepath.Join(l.root, b.TopDir, "bin", "javaw.exe")
		if !isExistingFile(javaw) {
			continue
		}
		data, err := os.ReadFile(l.marker)
		if err != nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(string(data)), b.MD5) {
			return javaw
		}
	}
	return ""
}

// intIn 判断值是否在集合中。
func intIn(v int, list []int) bool {
	for _, a := range list {
		if a == v {
			return true
		}
	}
	return false
}

// intList 把版本集合转成可读串。
func intList(list []int) string {
	parts := make([]string, 0, len(list))
	for _, v := range list {
		parts = append(parts, fmt.Sprintf("%d", v))
	}
	return strings.Join(parts, "/")
}
