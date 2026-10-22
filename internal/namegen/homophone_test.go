package namegen

import (
	"testing"
	"unicode/utf8"
)

func TestConvertHomophone_UserExample(t *testing.T) {
	input := "海棠花阔地"
	for i := 0; i < 20; i++ {
		output := ConvertHomophone(input, HomophoneOptions{ExactTone: false})
		if utf8.RuneCountInString(output) != utf8.RuneCountInString(input) {
			t.Fatalf("长度不一致: input=%s, output=%s", input, output)
		}
		t.Logf("转换结果 %d: %s -> %s", i+1, input, output)
	}
}

func TestConvertHomophone_ExactTone(t *testing.T) {
	input := "海棠花阔地"
	output := ConvertHomophone(input, HomophoneOptions{ExactTone: true})
	if utf8.RuneCountInString(output) != utf8.RuneCountInString(input) {
		t.Fatalf("长度不一致: input=%s, output=%s", input, output)
	}
	t.Logf("严格同调结果: %s -> %s", input, output)
}

func TestBatchConvertHomophone(t *testing.T) {
	input := "海棠花阔地"
	list := BatchConvertHomophone(input, 5, HomophoneOptions{ExactTone: false})
	if len(list) == 0 {
		t.Fatalf("预期生成多个候选结果")
	}
	for i, res := range list {
		t.Logf("候选 %d: %s", i+1, res)
	}
}
