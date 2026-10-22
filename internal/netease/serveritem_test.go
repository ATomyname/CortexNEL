package netease

import (
	"encoding/json"
	"testing"
)

// TestServerItemParsesSubModList 服务器 item 的响应必须解析出 sub_mod_list ——
// 该服 mod 套件的 iid 列表全靠它展开。真源样本形状（官方启动器 aov.cs 的
// ComponentDownloadInfoResponse）：entity 顶层带 sub_entities 与 sub_mod_list。
func TestServerItemParsesSubModList(t *testing.T) {
	const raw = `{"entity_id":"4661334467366178884","item_id":"4661334467366178884","mtypeid":2,
	  "sub_mod_list":[4690287328241054167,4690287328189015853,4690372021023046883],
	  "sub_entities":[{"res_name":"2026-09-04-01.7z","res_url":"https://x19.gdl.netease.com/xxx.7z",
	  "res_md5":"aaa","jar_md5":"bbb","res_version":3}]}`

	var info ComponentDownloadInfo
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		t.Fatal(err)
	}
	if len(info.SubModList) != 3 {
		t.Fatalf("sub_mod_list 应解析出 3 个 iid，实得 %v", info.SubModList)
	}
	if info.SubModList[0] != 4690287328241054167 {
		t.Fatalf("iid 解析不符：%v", info.SubModList)
	}
}

// TestServerModIIDs 展开规则：去重、跳过 0 值、保持顺序。
func TestServerModIIDs(t *testing.T) {
	items := []ComponentDownloadInfo{
		{SubModList: []uint64{222, 111, 222, 0}},
		{SubModList: []uint64{111, 333}},
	}
	got := serverModIIDs(items)
	if len(got) != 3 || got[0] != 222 || got[1] != 111 || got[2] != 333 {
		t.Fatalf("展开结果不符：%v", got)
	}
}
