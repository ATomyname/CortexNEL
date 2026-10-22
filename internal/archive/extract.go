// 解压编排：接收一次解压请求，按魔数分派到具体格式，逐条目交给落位层。
//
// 本文件同时持有整包的完整性策略：能落位的照常落位，但只要有条目被拒绝，
// 整次解压判失败 —— 否则调用方会照常写安装标记，把残缺安装判成完成。
package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/bodgit/sevenzip"
)

const (
	zipSignature = "PK\x03\x04"
	s7zSignature = "7z\xBC\xAF\x27\x1C"
	// extractEmitInterval 是解压进度的最小上报间隔。
	extractEmitInterval = 200 * time.Millisecond
)

// ExtractRequest 描述一次解压。
type ExtractRequest struct {
	// Archive 是归档路径。格式按魔数识别，不看扩展名 —— 实测 ".7z" 名义下
	// 多为 ZIP，靠后缀分派会直接读坏。
	Archive string
	// Dest 是落位根目录（不存在的子目录逐条建出）。
	Dest string
	// Ctx 在条目边界与单条目读取中生效，取消即中断。nil 视为 context.Background()。
	Ctx context.Context
	// Progress 按未压缩字节汇报，可为 nil。
	Progress ProgressFunc
	// OnSkip 在条目被放弃时调用，可为 nil；原因非空即代表该条目不会出现在 Dest。
	// 越界与 Windows 无法表示的名字都走这里 —— 缺件必须留痕，否则只能等启动失败。
	OnSkip func(name, reason string)
}

// contextOrDefault 免去每个内部调用点判空。
func (r ExtractRequest) contextOrDefault() context.Context {
	if r.Ctx == nil {
		return context.Background()
	}
	return r.Ctx
}

// skip 上报一条被放弃的条目（OnSkip 可为 nil）。
func (r ExtractRequest) skip(name, reason string) {
	if r.OnSkip != nil {
		r.OnSkip(name, reason)
	}
}

// skipLog 记录解压中被放弃的条目，并在收尾时判定整包是否落全。
//
// 为什么必须判失败：调用方（internal/install、internal/launcher）都拿"解压成功"
// 当安装标记的写入条件，而标记代表"整包已就位"。
// 少解出一个文件却照常写标记，等于把残缺安装判定为完成，故障会被推迟到启动阶段
// 才爆发，且不留任何线索。
type skipLog struct {
	entries []string
}

// wrap 把本次解压的放弃事件接到本日志上；调用方自己的 OnSkip 仍会被转发。
func (l *skipLog) wrap(req ExtractRequest) ExtractRequest {
	inner := req.OnSkip
	req.OnSkip = func(name, reason string) {
		l.entries = append(l.entries, name+"（"+reason+"）")
		if inner != nil {
			inner(name, reason)
		}
	}
	return req
}

// err 在有条目无法落位时返回可读错误，否则 nil。
func (l *skipLog) err() error {
	if len(l.entries) == 0 {
		return nil
	}
	return fmt.Errorf("archive: 有 %d 个条目无法安全落位: %s",
		len(l.entries), strings.Join(l.entries, "; "))
}

// Extract 按魔数分派解压器：ZIP（标准库）或 7z（bodgit），解压到 req.Dest。
// 无法识别的格式返回错误而不是猜。
//
// 被放弃的条目会让本函数返回错误 —— 但遍历不会中断，能落位的照常落位。
func Extract(req ExtractRequest) error {
	var skips skipLog
	req = skips.wrap(req)
	if err := forEachEntry(req, func(e extractEntry) error {
		ctx := req.contextOrDefault()
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.isDir {
			if err := os.MkdirAll(e.target, 0o755); err != nil {
				return err
			}
			return nil
		}
		n, err := extractOne(ctx, e.target, e.size, e.open)
		if err != nil {
			return err
		}
		e.report(uint64(n), e.size)
		return nil
	}); err != nil {
		return err
	}
	return skips.err()
}

// ExtractMap 按调用方映射落位：namer 对每个条目返回相对 Dest 的目标路径，
// ok=false 跳过该条目。用于归档内部布局与最终布局不一致、需要按规则重定位的包
// （实测：网易核心库包条目在 V_<ver>_libs/ 前缀下，官方最终落位是
// versions/<ver>/ 与 libraries/<坐标>/，见 internal/install）。
//
// namer 返回 ok=false 是调用方的主动取舍，不算缺件；只有映射结果本身无法安全
// 落位（越界、Windows 无法表示）才与 Extract 一样判失败。
func ExtractMap(req ExtractRequest, namer func(name string) (rel string, ok bool)) error {
	var skips skipLog
	req = skips.wrap(req)
	if err := forEachEntry(req, func(e extractEntry) error {
		ctx := req.contextOrDefault()
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.isDir {
			return nil
		}
		rel, ok := namer(e.name)
		if !ok {
			return nil
		}
		target, reason := safeTarget(req.Dest, rel)
		if reason != "" {
			req.skip(e.name, reason)
			return nil
		}
		n, err := extractOne(ctx, target, e.size, e.open)
		if err != nil {
			return err
		}
		e.report(uint64(n), e.size)
		return nil
	}); err != nil {
		return err
	}
	return skips.err()
}

// extractEntry 是一个已通过路径校验、准备落位的归档条目。
type extractEntry struct {
	// name 是条目在归档内的原始路径。
	name string
	// target 是安全映射后的落位绝对路径。
	target string
	// size 是归档声明的未压缩字节数（0 表示未知）。
	size uint64
	// isDir 区分目录条目。
	isDir bool
	// open 打开条目内容流。
	open func() (io.ReadCloser, error)
	// report 把本条目解出的字节计入进度累计。
	report func(written uint64, declared uint64)
}

// forEachEntry 按魔数分派归档读取器（ZIP 标准库或 7z bodgit），逐条目回调。
// 无法识别的格式返回错误而不是猜。
func forEachEntry(req ExtractRequest, fn func(e extractEntry) error) error {
	head := make([]byte, 8)
	f, err := os.Open(req.Archive)
	if err != nil {
		return err
	}
	if _, err := io.ReadFull(f, head); err != nil {
		f.Close()
		return err
	}
	f.Close()

	switch {
	case bytes.HasPrefix(head, []byte(zipSignature)):
		return forEachZipEntry(req, fn)
	case bytes.HasPrefix(head, []byte(s7zSignature)):
		return forEach7zEntry(req, fn)
	default:
		return fmt.Errorf("archive: 无法识别的归档格式（魔数 %X）", head)
	}
}

// forEachZipEntry 遍历 ZIP 中央目录，逐条目交给 fn。
//
// 进度累计器在遍历前按中央目录的未压缩大小算出总量：Extract/ExtractMap 共用它，
// 避免两个分派器各写一遍而漏掉终值。
func forEachZipEntry(req ExtractRequest, fn func(extractEntry) error) error {
	zr, err := zip.OpenReader(req.Archive)
	if err != nil {
		return err
	}
	defer zr.Close()

	var total uint64
	for _, f := range zr.File {
		total += f.UncompressedSize64
	}
	progress := newExtractProgress(req.Progress, total, extractEmitInterval)
	for _, f := range zr.File {
		target, reason := safeTarget(req.Dest, f.Name)
		if reason != "" {
			req.skip(f.Name, reason)
			continue
		}
		inner := f
		if err := fn(extractEntry{
			name:   f.Name,
			target: target,
			size:   f.UncompressedSize64,
			isDir:  f.FileInfo().IsDir(),
			open:   func() (io.ReadCloser, error) { return inner.Open() },
			report: progress.add,
		}); err != nil {
			return err
		}
	}
	progress.flush()
	return nil
}

// forEach7zEntry 遍历 7z 归档，逐条目交给 fn，语义与 forEachZipEntry 一致。
func forEach7zEntry(req ExtractRequest, fn func(extractEntry) error) error {
	r, err := sevenzip.OpenReader(req.Archive)
	if err != nil {
		return err
	}
	defer r.Close()

	var total uint64
	for _, f := range r.File {
		total += uint64(f.UncompressedSize)
	}
	progress := newExtractProgress(req.Progress, total, extractEmitInterval)
	for _, f := range r.File {
		target, reason := safeTarget(req.Dest, f.Name)
		if reason != "" {
			req.skip(f.Name, reason)
			continue
		}
		inner := f
		if err := fn(extractEntry{
			name:   f.Name,
			target: target,
			size:   uint64(f.UncompressedSize),
			isDir:  f.FileInfo().IsDir(),
			open:   func() (io.ReadCloser, error) { return inner.Open() },
			report: progress.add,
		}); err != nil {
			return err
		}
	}
	progress.flush()
	return nil
}

// extractProgress 把逐条目回调整形为带节流的进度回调，并保证结束时补一次终值。
//
// total 取归档声明的未压缩总量（遍历前算好），done 只累计真正解出的条目；
// 终值强制报到 total —— 有条目被放弃时 done 会小于 total，把进度停在 99% 看起来
// 像卡死，而落位是否完整由完整性判定决定，不靠进度条判断。
type extractProgress struct {
	report   ProgressFunc
	interval time.Duration
	total    uint64
	done     uint64
	lastEmit time.Time
}

func newExtractProgress(report ProgressFunc, total uint64, interval time.Duration) *extractProgress {
	p := &extractProgress{report: report, total: total, interval: interval, lastEmit: time.Now()}
	if report != nil {
		report(0, int64(total))
	}
	return p
}

// add 累计一个条目的进度。进度按归档声明的未压缩大小计；声明缺失（0）时按实际
// 写入计 —— 两者都为 0 就没有可汇报的量。
func (p *extractProgress) add(written, declared uint64) {
	if p.report == nil {
		return
	}
	if declared > 0 {
		p.done += declared
	} else {
		p.done += written
	}
	if time.Since(p.lastEmit) < p.interval {
		return
	}
	p.report(int64(p.done), int64(p.total))
	p.lastEmit = time.Now()
}

// flush 结束时补发一次终值，让进度条走到头而不是停在 99%。
func (p *extractProgress) flush() {
	if p.report == nil {
		return
	}
	p.report(int64(p.total), int64(p.total))
}
