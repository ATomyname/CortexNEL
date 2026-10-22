package netease

import (
	"encoding/json"
	"testing"
)

// TestComponentDownloadSubParsesResURL 下载地址与归档校验值必须能从网关响应解析出来。
//
// 真源：Fantnel EntityComponentDownloadInfoResponseSub 的 [JsonPropertyName("res_url")] /
// ("res_md5")。缺这两项时组件无法补齐，客户端会带着缺失的组件启动。
func TestComponentDownloadSubParsesResURL(t *testing.T) {
	const raw = `[{
	  "entity_id": "4687456828685462969",
	  "item_id": "4687456828685462969",
	  "mtypeid": 3,
	  "sub_entities": [{
	    "jar_md5": "5c7ec00fae3c64de97bb1a5f3833aea8",
	    "res_name": "4687456828685462969.zip",
	    "res_version": 3,
	    "res_url": "https://x19.fp.ps.netease.com/file/5c9780522786fdf93abcc6d9mSQm0AT002",
	    "res_md5": "e5a2e67dba2c2d6bb80817a1c9211036"
	  }]
	}]`

	var components []ComponentDownloadInfo
	if err := json.Unmarshal([]byte(raw), &components); err != nil {
		t.Fatal(err)
	}
	if len(components) != 1 || len(components[0].SubEntities) != 1 {
		t.Fatalf("解析出的组件数不符：%#v", components)
	}
	sub := components[0].SubEntities[0]
	if sub.ResURL == "" {
		t.Fatal("res_url 未解析（下载地址丢失，组件无法补齐）")
	}
	if sub.ResMD5 == "" {
		t.Fatal("res_md5 未解析（下载后无法校验归档）")
	}
	// 同源的 modInfo 映射不受影响。
	mods := componentMods(components)
	if len(mods) != 1 || mods[0].IID != "4687456828685462969" || mods[0].ModPath != "4687456828685462969@3@0.jar" {
		t.Fatalf("componentMods 结果不符：%#v", mods)
	}
}
