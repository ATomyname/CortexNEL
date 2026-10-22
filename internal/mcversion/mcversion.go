// Package mcversion 只做一件事：比较 Minecraft 版本号的先后。
//
// 独立成包是因为启动参数装配（internal/launcher）与客户端控制协议
// （internal/gamerpc）都要按版本分叉，而两处都不应各自实现一份比较逻辑。
// 本包不含任何业务判断，只有版本号序。
package mcversion

import (
	"strconv"
	"strings"
)

// AtLeast 判断 version 是否不低于 floor（按点分段逐段比较数值）。
// 例：AtLeast("1.20.1", "1.17") == true，AtLeast("1.20", "1.20.5") == false。
// 前缀相同时段数多的更新（1.20.1 > 1.20），段数不足则更旧（1.20 < 1.20.5）。
func AtLeast(version, floor string) bool {
	got := Segments(version)
	want := Segments(floor)
	for i := 0; i < len(got) && i < len(want); i++ {
		if got[i] != want[i] {
			return got[i] > want[i]
		}
	}
	return len(got) >= len(want)
}

// Segments 把版本号切成数值段，只取每段的前导数字。
// 取前导数字而不是整段解析，是为了让带后缀的版本号仍落在正确的档位：
// "1.20.1-forge" 与 "1.20-pre1" 都应视作 1.20 一档，若整段解析失败按 0 计，
// 1.20 的预览版会被误判成 1.0 并分到 Java 8。无前导数字的段按 0 计。
func Segments(version string) []int {
	raw := strings.Split(strings.TrimPrefix(strings.TrimSpace(version), "v"), ".")
	out := make([]int, 0, len(raw))
	for _, part := range raw {
		out = append(out, leadingNumber(part))
	}
	return out
}

// leadingNumber 取字符串的前导十进制数字，无前导数字时返回 0。
func leadingNumber(part string) int {
	part = strings.TrimSpace(part)
	end := 0
	for end < len(part) && part[end] >= '0' && part[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	n, err := strconv.Atoi(part[:end])
	if err != nil {
		return 0
	}
	return n
}
