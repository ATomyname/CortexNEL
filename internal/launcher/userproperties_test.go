package launcher

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestUserPropertiesCarriesControlPort launcherport 必须落在 --userProperties 里，
// 否则客户端不知道回连何处，也就不会自动进服。
func TestUserPropertiesCarriesControlPort(t *testing.T) {
	raw := buildUserProperties(933874987, 11413)

	var props map[string][]any
	if err := json.Unmarshal([]byte(raw), &props); err != nil {
		t.Fatalf("--userProperties 必须是合法 JSON：%v（%s）", err, raw)
	}

	port, ok := props["launcherport"]
	if !ok {
		t.Fatalf("缺 launcherport 字段：%s", raw)
	}
	if got, ok := port[0].(float64); !ok || int(got) != 11413 {
		t.Fatalf("launcherport = %v，期望 11413", port[0])
	}
	if uid, ok := props["uid"][0].(float64); !ok || int64(uid) != 933874987 {
		t.Fatalf("uid = %v，期望 933874987", props["uid"][0])
	}
	// gameid 恒为 0：服务器知识不经启动参数下发（隔离门禁 P1/P2）。
	if gid, ok := props["gameid"][0].(float64); !ok || int(gid) != 0 {
		t.Fatalf("gameid = %v，期望 0", props["gameid"][0])
	}
	if key, ok := props["filterkey"][0].(string); !ok || len(key) != filterKeyLength {
		t.Fatalf("filterkey 长度 = %d，期望 %d", len(key), filterKeyLength)
	}
}

// TestUserPropertiesOmitsUnboundControlPort 控制通道没开时不得写出端口 0，
// 让客户端去连一个没有监听的端口比不给端口更糟。
func TestUserPropertiesOmitsUnboundControlPort(t *testing.T) {
	raw := buildUserProperties(1, 0)
	if strings.Contains(raw, "launcherport") {
		t.Fatalf("控制端口为 0 时不应写 launcherport：%s", raw)
	}
	var props map[string][]any
	if err := json.Unmarshal([]byte(raw), &props); err != nil {
		t.Fatalf("仍应是合法 JSON：%v", err)
	}
}

// TestUserPropertiesExFieldNames 字段名大小写按客户端约定，不可按 Go 习惯改写。
func TestUserPropertiesExFieldNames(t *testing.T) {
	raw := buildUserPropertiesEx()
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("--userPropertiesEx 必须是合法 JSON：%v（%s）", err, raw)
	}
	for _, key := range []string{"GameType", "isFilter", "channel", "timedelta", "launcherVersion"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("缺字段 %s：%s", key, raw)
		}
	}
	if got["GameType"].(float64) != netGameType {
		t.Fatalf("GameType = %v，期望 %d（EnumGType.NetGame）", got["GameType"], netGameType)
	}
	if got["channel"] != "netease" {
		t.Fatalf("channel = %v，期望 netease", got["channel"])
	}
}

// TestTokenArgumentSkipsEmptyToken 无 token 时不写 -DToken。
func TestTokenArgumentSkipsEmptyToken(t *testing.T) {
	if got := tokenArgument(""); got != "" {
		t.Fatalf("空 token 应不写参数，实得 %q", got)
	}
	got := tokenArgument("abc123")
	if !strings.HasPrefix(got, "-DToken=") {
		t.Fatalf("参数前缀不符：%q", got)
	}
	if len(got) <= len("-DToken=") {
		t.Fatalf("加密后的 token 不应为空：%q", got)
	}
}

// TestFilterKeyIsRandom 两次生成不应相同（否则 rand 没接上）。
func TestFilterKeyIsRandom(t *testing.T) {
	if randomFilterKey() == randomFilterKey() {
		t.Fatal("filterkey 两次相同，随机源未生效")
	}
}
