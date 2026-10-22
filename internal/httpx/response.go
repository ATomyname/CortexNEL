package httpx

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ReadRespBody 读取响应体（最多 limit 字节），Content-Encoding: gzip 时先解压。
// 必须经此读取手动设置 Accept-Encoding 的请求：net/http 仅对自身附加该头的
// 请求做透明解压，自行设置后响应可能是 gzip 原始字节。
func ReadRespBody(resp *http.Response, limit int64) ([]byte, error) {
	var r io.Reader = resp.Body
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("httpx: gzip 解压: %w", err)
		}
		defer gz.Close()
		r = gz
	}
	return io.ReadAll(io.LimitReader(r, limit))
}
