// 单个归档包的安装流程：标记判已装 → 下载 → 校验 → 解压 → 写标记。
//
// 与 internal/archive 的分工：archive 只负责"下载/校验/解压"这三件机械动作，
// 本文件负责顺序、判据（MD5 标记文件）与落位规则。
package install

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"nexus/internal/archive"
	"nexus/internal/netease"
)

// markerDirName 是安装标记目录（位于版本数据目录内，不出现在客户端根的关键路径上）。
const markerDirName = ".install"

// packageOf 归一化一个包描述；包名缺失时退回取 URL 的最后一段
// （标记文件与缓存文件名都依赖它，不能为空）。
func packageOf(name, url, md5 string, size int64) Package {
	if strings.TrimSpace(name) == "" {
		name = path.Base(strings.TrimSuffix(url, "/"))
	}
	return Package{Name: filepath.Base(name), URL: url, MD5: md5, Size: size}
}

// installPackage 把 pkg 解压到 destRoot，用 MD5 标记文件判"已装"。
func (i *Installer) installPackage(ctx context.Context, pkg Package, destRoot, subject string) error {
	if pkg.URL == "" || pkg.MD5 == "" {
		return fmt.Errorf("install: %s 缺少下载地址或 MD5（无法保证装到正确内容）", subject)
	}
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return err
	}
	marker := markerPath(destRoot, pkg.Name)
	if markerHit(marker, pkg.MD5) {
		i.logInfo("包已安装，跳过", "subject", subject, "name", pkg.Name, "md5", pkg.MD5)
		i.report(Progress{Stage: StageDone, Subject: subject, Total: pkg.Size, Done: pkg.Size,
			Message: "已安装，跳过 " + pkg.Name})
		return nil
	}

	archivePath := filepath.Join(destRoot, "cache", pkg.Name)
	i.logInfo("开始下载包", "subject", subject, "name", pkg.Name, "url", pkg.URL, "size", pkg.Size)
	i.report(Progress{Stage: StageDownload, Subject: subject, Total: pkg.Size, Message: "下载 " + pkg.Name})
	if err := archive.Download(ctx, i.HTTP, pkg.URL, archivePath, i.progressReporter(StageDownload, subject, "下载 "+pkg.Name)); err != nil {
		return fmt.Errorf("install: 下载 %s: %w", pkg.Name, err)
	}

	i.report(Progress{Stage: StageVerify, Subject: subject, Total: pkg.Size, Message: "校验 " + pkg.Name})
	if !archive.Matches(archivePath, pkg.MD5) {
		_ = os.Remove(archivePath)
		i.logWarn("包 MD5 校验失败，已删除归档", "subject", subject, "name", pkg.Name, "expect", pkg.MD5)
		return fmt.Errorf("install: %s 校验失败（MD5 不匹配），包可能已更新或下载损坏", pkg.Name)
	}
	i.logInfo("包 MD5 校验通过", "subject", subject, "name", pkg.Name)

	i.report(Progress{Stage: StageExtract, Subject: subject, Total: pkg.Size, Message: "解压 " + pkg.Name})
	if err := archive.Extract(archive.ExtractRequest{
		Archive:  archivePath,
		Dest:     destRoot,
		Ctx:      ctx,
		Progress: i.progressReporter(StageExtract, subject, "解压 "+pkg.Name),
		OnSkip:   i.skipLogger(subject),
	}); err != nil {
		return fmt.Errorf("install: 解压 %s: %w", pkg.Name, err)
	}
	if err := writeMarker(marker, pkg.MD5); err != nil {
		return fmt.Errorf("install: 写安装标记 %s: %w", marker, err)
	}
	i.logInfo("包落位完成", "subject", subject, "name", pkg.Name, "destRoot", destRoot)
	// 解压成功即已落位，归档不再保留（与参考实现一致）：
	// 磁盘占用减半，标记文件保证不会重复下载。
	_ = os.Remove(archivePath)
	i.report(Progress{Stage: StageDone, Subject: subject, Total: pkg.Size, Done: pkg.Size,
		Message: "完成 " + pkg.Name})
	return nil
}

// installComponent 下载、校验并落位一个组件。
//
// 落位前用网关给出的 jar_md5 校验解出的 jar：服务端校验的就是这个 md5，
// 差一个字节都会被拒；宁可在这一步失败，也不要往 mods 里塞一个错误的 jar。
func (i *Installer) installComponent(ctx context.Context, c netease.ComponentDownloadInfo, sub netease.ComponentDownloadSub, modsDir string) error {
	subject := "component:" + c.ItemID
	work := filepath.Join(modsDir, ".install-tmp", c.ItemID)
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(filepath.Join(modsDir, ".install-tmp"))

	archivePath := filepath.Join(work, filepath.Base(sub.ResName))
	i.logInfo("开始下载组件归档", "subject", subject, "item", c.ItemID, "url", sub.ResURL, "res_name", sub.ResName)
	i.report(Progress{Stage: StageDownload, Subject: subject, Message: "下载组件 " + sub.ResName})
	if err := archive.Download(ctx, i.HTTP, sub.ResURL, archivePath,
		i.progressReporter(StageDownload, subject, "下载组件 "+sub.ResName)); err != nil {
		return fmt.Errorf("install: 下载组件 %s: %w", c.ItemID, err)
	}
	if sub.ResMD5 != "" && !archive.Matches(archivePath, sub.ResMD5) {
		i.logWarn("组件归档 res_md5 校验失败", "item", c.ItemID, "expect", sub.ResMD5)
		return fmt.Errorf("install: 组件 %s 归档校验失败（res_md5 不匹配）", c.ItemID)
	}

	extracted := filepath.Join(work, "extract")
	i.report(Progress{Stage: StageExtract, Subject: subject, Message: "解压组件 " + sub.ResName})
	if err := archive.Extract(archive.ExtractRequest{
		Archive:  archivePath,
		Dest:     extracted,
		Ctx:      ctx,
		Progress: i.progressReporter(StageExtract, subject, "解压组件 "+sub.ResName),
		OnSkip:   i.skipLogger(subject),
	}); err != nil {
		return fmt.Errorf("install: 解压组件 %s: %w", c.ItemID, err)
	}

	jarPath, err := findComponentJar(extracted)
	if err != nil {
		return fmt.Errorf("install: 组件 %s: %w", c.ItemID, err)
	}
	if sub.JarMD5 != "" && !archive.Matches(jarPath, sub.JarMD5) {
		i.logWarn("组件 jar_md5 与网关不符，放弃落位", "item", c.ItemID, "expect", sub.JarMD5, "jar", jarPath)
		return fmt.Errorf("install: 组件 %s 的 jar MD5 与网关不符（服务端会拒），已放弃落位", c.ItemID)
	}
	target := filepath.Join(modsDir, ComponentJarName(c.ItemID, c.MTypeID, c.EntityID))
	if err := moveFile(jarPath, target); err != nil {
		return fmt.Errorf("install: 组件 %s 落位: %w", c.ItemID, err)
	}
	i.logInfo("组件落位完成", "item", c.ItemID, "target", target, "jar_md5", sub.JarMD5)
	i.report(Progress{Stage: StageDone, Subject: subject, Message: "完成组件 " + c.ItemID})
	return nil
}

// progressReporter 把 archive 的 (done,total) 回调转成事件；label 是展示文案。
func (i *Installer) progressReporter(stage Stage, subject, label string) archive.ProgressFunc {
	return func(done, total int64) {
		i.report(Progress{Stage: stage, Subject: subject, Done: done, Total: total, Message: label})
	}
}

func (i *Installer) report(p Progress) {
	if i.Report != nil {
		i.Report(p)
	}
}

// markerPath 是某个包的安装标记路径（每个包名一个，内容为已装包的 MD5）。
func markerPath(destRoot, pkgName string) string {
	return filepath.Join(destRoot, markerDirName, filepath.Base(pkgName)+".md5")
}

// markerHit 判断标记文件存在且内容等于期望 MD5（包更新后 md5 变化即失效，自动重装）。
func markerHit(marker, wantMD5 string) bool {
	b, err := os.ReadFile(marker)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(string(b)), wantMD5)
}

// writeMarker 原子写标记文件。
func writeMarker(marker, md5 string) error {
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return err
	}
	tmp := marker + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.ToLower(md5)), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, marker)
}

// findComponentJar 在解压产物里找组件 jar（参考实现是把全部 jar 逐个拷出，这里取首个）。
func findComponentJar(dir string) (string, error) {
	var found string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".jar") {
			found = p
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("解压产物里没有 .jar")
	}
	return found, nil
}

// moveFile 同卷改名；跨卷改名失败时退回复制。
func moveFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return err
	}
	return os.Remove(src)
}

// clientRoot 解析归档解压后的实际客户端根。
//
// 实测布局（pkgprobe 对 base-150832 与 1.20-227372 两个包的条目清单）：
// 基础包与版本包条目全部在 .minecraft/ 前缀下，因此客户端根是 root/.minecraft；
// root 下直接存在 versions/ 时（无前缀布局）客户端根就是 root。
func clientRoot(root string) string {
	if isExistingDir(filepath.Join(root, ".minecraft", "versions")) {
		return filepath.Join(root, ".minecraft")
	}
	return root
}

// verifyClientLayout 校验客户端本体确实可用（版本清单必须存在），返回客户端根。
//
// 归档内部布局无法离线预知，所以装完后必须实证：缺关键文件时把根目录的实际顶层
// 条目一并报出来，第一次真机下载即可据此定位，而不是硬往下走。
func verifyClientLayout(root, mcVersion string) (string, error) {
	mc := clientRoot(root)
	manifest := filepath.Join(mc, "versions", mcVersion, mcVersion+".json")
	if _, err := os.Stat(manifest); err == nil {
		return mc, nil
	}
	entries, _ := os.ReadDir(mc)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return "", fmt.Errorf("install: 客户端本体已解压但缺少 %s；%s 下实际顶层条目为 %v"+
		"（归档布局与预期不符，需据实调整落位规则）", manifest, mc, names)
}

// installCoreLib 把核心库包按官方最终布局落位（真源 Fantnel
// InstallerService.InstallCoreLibs，官方安装实测 1.20 一致）。
//
// 实测 CoreLibs_<ver>.zip 条目在 V_<ver>_libs/ 前缀下，与最终布局不一致：
//   - .jar → 在客户端根 libraries/ 里按 basename 匹配覆盖（官方实测
//     modlauncher-10.0.9.jar → libraries/cpw/mods/modlauncher/10.0.9/）；
//     匹配不到按真源跳过（该库不被版本清单引用，落哪都无效），报一条进度消息；
//   - 非 .jar（如 <ver>.dat）→ versions/<ver>/<basename>（官方实测 1.20.dat）。
func (i *Installer) installCoreLib(ctx context.Context, pkg Package, root, mcVersion string) error {
	if pkg.URL == "" || pkg.MD5 == "" {
		return fmt.Errorf("install: 核心库缺少下载地址或 MD5（无法保证装到正确内容）")
	}
	marker := markerPath(root, pkg.Name)
	if markerHit(marker, pkg.MD5) {
		i.report(Progress{Stage: StageDone, Subject: "client:" + mcVersion + ":corelib",
			Total: pkg.Size, Done: pkg.Size, Message: "已安装，跳过 " + pkg.Name})
		return nil
	}

	mc := clientRoot(root)
	archivePath := filepath.Join(root, "cache", pkg.Name)
	i.report(Progress{Stage: StageDownload, Subject: "client:" + mcVersion + ":corelib", Total: pkg.Size, Message: "下载 " + pkg.Name})
	if err := archive.Download(ctx, i.HTTP, pkg.URL, archivePath, i.progressReporter(StageDownload, "client:"+mcVersion+":corelib", "下载 "+pkg.Name)); err != nil {
		return fmt.Errorf("install: 下载 %s: %w", pkg.Name, err)
	}
	i.report(Progress{Stage: StageVerify, Subject: "client:" + mcVersion + ":corelib", Total: pkg.Size, Message: "校验 " + pkg.Name})
	if !archive.Matches(archivePath, pkg.MD5) {
		_ = os.Remove(archivePath)
		return fmt.Errorf("install: %s 校验失败（MD5 不匹配），包可能已更新或下载损坏", pkg.Name)
	}

	libIndex := libraryBasenameIndex(filepath.Join(mc, "libraries"))
	subject := "client:" + mcVersion + ":corelib"
	err := archive.ExtractMap(archive.ExtractRequest{
		Archive:  archivePath,
		Dest:     mc,
		Ctx:      ctx,
		Progress: i.progressReporter(StageExtract, subject, "解压 "+pkg.Name),
		OnSkip:   i.skipLogger(subject),
	}, func(name string) (string, bool) {
		base := filepath.Base(name)
		if strings.EqualFold(filepath.Ext(base), ".jar") {
			relInLibraries, ok := libIndex[base]
			if !ok {
				i.report(Progress{Stage: StageExtract, Subject: subject,
					Message: fmt.Sprintf("核心库 %s 在 libraries 中无同名文件，已跳过", base)})
				return "", false
			}
			// 索引相对 libraries 目录；落位目标根是客户端根，补回前缀。
			return filepath.Join("libraries", relInLibraries), true
		}
		return filepath.Join("versions", mcVersion, base), true
	})
	if err != nil {
		return fmt.Errorf("install: 核心库落位 %s: %w", pkg.Name, err)
	}
	if err := writeMarker(marker, pkg.MD5); err != nil {
		return fmt.Errorf("install: 写安装标记 %s: %w", marker, err)
	}
	_ = os.Remove(archivePath)
	i.report(Progress{Stage: StageDone, Subject: subject, Total: pkg.Size, Done: pkg.Size, Message: "完成 " + pkg.Name})
	return nil
}

// libraryBasenameIndex 递归扫描 libraries 目录，按文件名索引相对路径。
// 同名文件取先见者（与真源 Directory.GetFiles 的首个匹配一致）。
func libraryBasenameIndex(librariesDir string) map[string]string {
	index := make(map[string]string)
	prefix := librariesDir + string(filepath.Separator)
	_ = filepath.WalkDir(librariesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(d.Name()) != ".jar" {
			return nil
		}
		rel, relErr := filepath.Rel(prefix, path)
		if relErr != nil {
			return nil
		}
		name := strings.ToLower(d.Name())
		if _, exists := index[name]; !exists {
			index[name] = rel
		}
		return nil
	})
	return index
}

// isExistingDir 判断路径存在且是目录。
func isExistingDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// parseItemID 把 iid 字符串转成数字 id（组件详情查询用的是数字列表）。
func parseItemID(s string) (uint64, bool) {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return v, err == nil
}
