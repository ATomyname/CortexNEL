// Package namegen 中文通用逻辑谐音生成器。
// 支持输入任意中文词组/句子（如 "海棠花阔地"），通过拼音映射与同音字库逻辑转换为谐音（如 "海塘话扩第"）。
package namegen

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"unicode"
)

//go:embed pinyin_table.json
var pinyinTableJSON []byte

// PinyinTable 拼音与汉字映射库。
type PinyinTable struct {
	CharToPinyin map[string]string   `json:"char_to_pinyin"`
	Exact        map[string][]string `json:"exact"` // 带声调，如 "tang2": ["塘", "堂", "唐", "糖", "棠", "螳"]
	Fuzzy        map[string][]string `json:"fuzzy"` // 不带声调，如 "tang": ["塘", "堂", "唐", "糖", "趟", "烫", "淌"]
}

var (
	homophoneMu    sync.RWMutex
	pinyinTable    PinyinTable
	reTrailingTone = regexp.MustCompile(`[0-9]$`)
)

func init() {
	_ = json.Unmarshal(pinyinTableJSON, &pinyinTable)
}

// HomophoneOptions 控制谐音转换逻辑选项。
type HomophoneOptions struct {
	ExactTone bool `json:"exactTone"` // 是否严格声调匹配（true 为同调同音，false 允许四声所有同音字）
	AllowSelf bool `json:"allowSelf"` // 是否允许部分字符保留原字（如“海棠花阔地”中保留“海”生成“海塘话扩第”）
}

// ConvertHomophone 对任意输入的中文文本按音节进行通用逻辑谐音转换。
// 例如输入 "海棠花阔地"，可生成 "海塘话扩第"、"海塘华括第" 等。
func ConvertHomophone(text string, opts HomophoneOptions) string {
	homophoneMu.RLock()
	defer homophoneMu.RUnlock()

	var sb strings.Builder
	for _, r := range text {
		// 非汉字（英文字母、数字、符号等）原样输出
		if !unicode.Is(unicode.Han, r) {
			sb.WriteRune(r)
			continue
		}

		ch := string(r)
		pyWithTone, exists := pinyinTable.CharToPinyin[ch]
		if !exists {
			sb.WriteString(ch)
			continue
		}

		// 如果开启了自然保留原字，有 25% 几率保持原字以增加谐音自然度
		if opts.AllowSelf && randInt(100) < 25 {
			sb.WriteString(ch)
			continue
		}

		purePy := reTrailingTone.ReplaceAllString(pyWithTone, "")

		var candidates []string
		if opts.ExactTone {
			// 严格同声调同音字
			for _, c := range pinyinTable.Exact[pyWithTone] {
				if c != ch {
					candidates = append(candidates, c)
				}
			}
			// 如果严格同声调字库较少，平滑回退到四声同音字中
			if len(candidates) == 0 {
				for _, c := range pinyinTable.Fuzzy[purePy] {
					if c != ch {
						candidates = append(candidates, c)
					}
				}
			}
		} else {
			// 允许四声所有同音字（如 花 hua1 -> 话/化/画 hua4）
			for _, c := range pinyinTable.Fuzzy[purePy] {
				if c != ch {
					candidates = append(candidates, c)
				}
			}
		}

		if len(candidates) > 0 {
			chosen := candidates[randInt(len(candidates))]
			sb.WriteString(chosen)
		} else {
			sb.WriteString(ch)
		}
	}

	return sb.String()
}

// BatchConvertHomophone 批量生成多个候选谐音结果。
func BatchConvertHomophone(text string, count int, opts HomophoneOptions) []string {
	if count <= 0 {
		count = 1
	} else if count > 20 {
		count = 20
	}

	seen := make(map[string]bool, count)
	results := make([]string, 0, count)

	for i := 0; i < count*5 && len(results) < count; i++ {
		res := ConvertHomophone(text, opts)
		if !seen[res] {
			seen[res] = true
			results = append(results, res)
		}
	}

	if len(results) == 0 {
		results = append(results, text)
	}

	return results
}
