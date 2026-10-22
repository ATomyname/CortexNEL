// Package namegen 实现网易我的世界官方角色随机命名器（提取自网易官方客户端 random_nickname_db），
// 并支持通用中文逻辑谐音生成（声韵同音转换算法）。
package namegen

import (
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

//go:embed words.json
var defaultWordsJSON []byte

// Options 是角色名生成参数。
type Options struct {
	Prefix       string `json:"prefix,omitempty"`
	Suffix       string `json:"suffix,omitempty"`
	Style        string `json:"style,omitempty"`        // netease (网易官方三段式), short (双段式), homophone (中文逻辑谐音)
	BaseWord     string `json:"baseWord,omitempty"`     // 逻辑谐音的基础中文词（例如 "海棠花阔地"）
	Digits       int    `json:"digits,omitempty"`       // 附加数字位数，0~4
	MaxLength    int    `json:"maxLength,omitempty"`    // 默认 16 字符，Minecraft 规范上限
	UseHomophone bool   `json:"useHomophone,omitempty"` // 是否对生成的名字应用中文逻辑谐音转换
	ExactTone    bool   `json:"exactTone,omitempty"`    // 是否严格同声调
	AllowSelf    bool   `json:"allowSelf,omitempty"`    // 是否自然保留部分原字
}

// NetEaseWordBank 是网易官方随机名称词库结构。
type NetEaseWordBank struct {
	Adjectives []string `json:"adjectives"`
	Names      []string `json:"names"`
	Verbs      []string `json:"verbs"`
}

var (
	bankMu sync.RWMutex
	bank   NetEaseWordBank

	// 允许中文、英文字母、数字、下划线及常用标签括号
	validCharRe = regexp.MustCompile(`[^\p{Han}a-zA-Z0-9_\-\[\]]`)
)

func init() {
	if err := json.Unmarshal(defaultWordsJSON, &bank); err != nil {
		bank = NetEaseWordBank{
			Adjectives: []string{"强硬的", "勇敢的", "神秘的", "敏捷的", "冷酷的", "闪耀的", "疾风的", "热情的"},
			Names:      []string{"末影人", "苦力怕", "铁傀儡", "烈焰人", "凋灵", "雪傀儡", "守卫者", "幻术师"},
			Verbs:      []string{"挖矿", "探险", "奔跑", "飞行", "冲锋", "思考", "度假", "潜行"},
		}
	}
}

// GetWordBank 获取当前词库快照。
func GetWordBank() NetEaseWordBank {
	bankMu.RLock()
	defer bankMu.RUnlock()
	return bank
}

func randInt(max int) int {
	if max <= 0 {
		return 0
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0
	}
	return int(n.Int64())
}

// Generate 根据参数生成符合规范的随机角色名。
func Generate(opts Options) string {
	maxLen := opts.MaxLength
	if maxLen <= 0 || maxLen > 16 {
		maxLen = 16
	}

	prefix := cleanIdentifier(opts.Prefix)
	suffix := cleanIdentifier(opts.Suffix)

	digitLen := opts.Digits
	if digitLen < 0 {
		digitLen = 0
	} else if digitLen > 4 {
		digitLen = 4
	}

	numStr := ""
	if digitLen > 0 {
		switch digitLen {
		case 1:
			numStr = fmt.Sprintf("%d", randInt(10))
		case 2:
			numStr = fmt.Sprintf("%02d", randInt(100))
		case 3:
			numStr = fmt.Sprintf("%03d", randInt(1000))
		case 4:
			numStr = fmt.Sprintf("%04d", randInt(10000))
		}
	}

	affixRuneCount := utf8.RuneCountInString(prefix) + utf8.RuneCountInString(suffix) + len(numStr)
	availableForCore := maxLen - affixRuneCount

	// 若前后缀占用过多，合理剪裁前后缀
	if availableForCore < 4 {
		prefixRunes := []rune(prefix)
		if len(prefixRunes) > 3 {
			prefix = string(prefixRunes[:3])
		}
		suffixRunes := []rune(suffix)
		if len(suffixRunes) > 3 {
			suffix = string(suffixRunes[:3])
		}
		affixRuneCount = utf8.RuneCountInString(prefix) + utf8.RuneCountInString(suffix) + len(numStr)
		availableForCore = maxLen - affixRuneCount
		if availableForCore < 4 {
			numStr = ""
			availableForCore = maxLen - utf8.RuneCountInString(prefix) - utf8.RuneCountInString(suffix)
		}
	}

	var core string
	if strings.ToLower(opts.Style) == "homophone" || opts.UseHomophone {
		// 中文逻辑谐音生成模式
		base := strings.TrimSpace(opts.BaseWord)
		if base == "" {
			// 未设置自定义基础词时，从网易词库生成基础词
			base = pickCoreWord("netease", availableForCore)
		}
		core = ConvertHomophone(base, HomophoneOptions{
			ExactTone: opts.ExactTone,
			AllowSelf: opts.AllowSelf,
		})
	} else {
		core = pickCoreWord(opts.Style, availableForCore)
	}

	result := prefix + core + numStr + suffix

	// 严格遵循 16 字符限制（按 Unicode 字符/rune 统计）
	resultRunes := []rune(result)
	if len(resultRunes) > maxLen {
		result = string(resultRunes[:maxLen])
	}
	for utf8.RuneCountInString(result) < 3 {
		result += fmt.Sprintf("%d", randInt(10))
	}

	return result
}

// BatchGenerate 批量生成候选名称。
func BatchGenerate(opts Options, count int) []string {
	if count <= 0 {
		count = 1
	} else if count > 20 {
		count = 20
	}
	seen := make(map[string]bool, count)
	results := make([]string, 0, count)

	for i := 0; i < count*4 && len(results) < count; i++ {
		name := Generate(opts)
		if !seen[name] {
			seen[name] = true
			results = append(results, name)
		}
	}
	return results
}

func pickCoreWord(style string, maxRunes int) string {
	if maxRunes < 3 {
		maxRunes = 3
	}

	bankMu.RLock()
	defer bankMu.RUnlock()

	adjs := bank.Adjectives
	names := bank.Names
	verbs := bank.Verbs

	if len(adjs) == 0 {
		adjs = []string{"强硬的"}
	}
	if len(names) == 0 {
		names = []string{"末影人"}
	}
	if len(verbs) == 0 {
		verbs = []string{"挖矿"}
	}

	// 双段简短模式（形容词 + 名字）
	if strings.ToLower(style) == "short" || strings.ToLower(style) == "minimal" {
		adj := adjs[randInt(len(adjs))]
		name := names[randInt(len(names))]
		combined := adj + name
		if utf8.RuneCountInString(combined) <= maxRunes {
			return combined
		}
		if utf8.RuneCountInString(name) <= maxRunes {
			return name
		}
		return string([]rune(combined)[:maxRunes])
	}

	// 网易官方默认三段式算法：形容词 + 名字 + 动词
	for attempts := 0; attempts < 10; attempts++ {
		adj := adjs[randInt(len(adjs))]
		name := names[randInt(len(names))]
		verb := verbs[randInt(len(verbs))]
		full := adj + name + verb
		if utf8.RuneCountInString(full) <= maxRunes {
			return full
		}
	}

	// 空间不足时降级为 形容词 + 名字 或 名字 + 动词
	adj := adjs[randInt(len(adjs))]
	name := names[randInt(len(names))]
	twoWords := adj + name
	if utf8.RuneCountInString(twoWords) <= maxRunes {
		return twoWords
	}
	verb := verbs[randInt(len(verbs))]
	nameVerb := name + verb
	if utf8.RuneCountInString(nameVerb) <= maxRunes {
		return nameVerb
	}
	return string([]rune(twoWords)[:maxRunes])
}

func cleanIdentifier(s string) string {
	cleaned := validCharRe.ReplaceAllString(s, "")
	return strings.TrimSpace(cleaned)
}
