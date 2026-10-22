// 单个条目落位：把归档条目里的字节原子地写到目标路径。
//
// 这一层是"半份文件绝不留下"这条承诺的实现点 —— 解压失败、磁盘满、CRC 不符、
// 文件被运行中的客户端占用，四种情况下目标路径都只能是"完整的"或"根本不存在"。
package archive

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// partSuffix 是落位前的临时文件后缀。
const partSuffix = ".part"

// ctxReader 在每次 Read 前检查取消，让单个大条目（基岩整包里有几百 MB 的
// 单文件）也能被 ctx 打断，而不是只能等条目边界。
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// extractOne 打开一个归档条目并原子落位到 target，返回写入字节数。
//
// 三件事缺一不可：
//  1. 先写 target+".part"，成功才改名 —— 半份文件绝不顶着最终文件名留下。
//     直接 O_TRUNC 写 target 会让解压失败时留下截断的、或 CRC 不符但长度完整的
//     文件，而后者恰好能通过"文件存在"这类判据（JRE 就是这样被毒化的）；
//  2. 按声明大小核对实际写入 —— 解压流提前结束而不报错时 io.Copy 返回 (n, nil)，
//     只有这个比较能发现"少解了一半"；
//  3. 临时文件与目标都随错误清理/保留原样，绝不留下半份。
func extractOne(ctx context.Context, target string, expected uint64, open func() (io.ReadCloser, error)) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return 0, err
	}
	rc, err := open()
	if err != nil {
		return 0, err
	}
	tmp := target + partSuffix
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		_ = rc.Close()
		return 0, err
	}

	n, err := io.Copy(out, ctxReader{ctx: ctx, r: rc})
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && expected > 0 && uint64(n) != expected {
		err = fmt.Errorf("archive: %s 解出 %d 字节，归档声明 %d 字节（归档损坏或截断）",
			filepath.Base(target), n, expected)
	}
	// 归档的完整性校验（ZIP 的 CRC32、部分 7z 实现的校验和）可能由 Close 抛出，
	// 不能用 defer 吞掉。
	if cerr := rc.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return n, err
	}
	if err := placeFile(tmp, target); err != nil {
		return n, err
	}
	return n, nil
}

// placeFile 把写好的临时文件改名到 target，失败时清理临时文件。
//
// target 已存在时改名可能因只读属性失败：去掉只读、删掉旧文件、再改名一次。
// 若删除也失败（文件正被运行中的客户端占用），就清掉临时文件并如实报错 ——
// 旧文件原样保留。任何路径下都不会出现半份 target。
// 解压落位（extractOne）与下载落位（Download）共用这一条铁律。
func placeFile(tmp, target string) error {
	renameErr := os.Rename(tmp, target)
	if renameErr == nil {
		return nil
	}
	if os.Chmod(target, 0o666) == nil && os.Remove(target) == nil {
		if err := os.Rename(tmp, target); err == nil {
			return nil
		} else {
			renameErr = err
		}
	}
	_ = os.Remove(tmp)
	return fmt.Errorf("archive: 落位 %s 失败: %w", filepath.Base(target), renameErr)
}
