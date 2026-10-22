package netease

import (
	"encoding/json"
	"testing"
)

// TestClientPatchInfoParsesBasePackage 用实测响应（1.20 基础包）固定住字段解析。
//
// 真源样本：{"url":"https://x19.gdl.netease.com/mc_client/base-150832_1642684473.7z",
// "size":150390122,"md5":"46787c7777dc29440c75e64534d2dacc",
// "name":"base-150832_1642684473.7z","version":"3","mc_version":0,
// "core_lib_name":"CoreLibs_0.7z","core_lib_size":0,"core_lib_md5":"","core_lib_url":""}
func TestClientPatchInfoParsesBasePackage(t *testing.T) {
	const raw = `{"entity_id":"0","url":"https://x19.gdl.netease.com/mc_client/base-150832_1642684473.7z",
	  "size":150390122,"md5":"46787c7777dc29440c75e64534d2dacc",
	  "name":"base-150832_1642684473.7z","version":"3","mc_version":0,
	  "refresh_time":1642693561,"core_lib_name":"CoreLibs_0.7z","core_lib_size":0,
	  "core_lib_md5":"","core_lib_url":""}`

	var info ClientPatchInfo
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		t.Fatal(err)
	}
	if info.URL == "" || info.MD5 == "" || info.Name == "" {
		t.Fatalf("基础包关键字段缺失：%+v", info)
	}
	if info.Size != 150390122 {
		t.Fatalf("size = %d，期望 150390122", info.Size)
	}
	if info.McVersion != 0 {
		t.Fatalf("mc_version = %d，期望 0（基础包）", info.McVersion)
	}
	if info.CoreLibURL != "" {
		t.Fatalf("基础包不应带核心库：%+v", info)
	}
}

// TestClientPatchInfoParsesVersionPackage 版本包必须解析出核心库那一组字段 ——
// 缺核心库时 1.20 的 Forge 依赖不完整。
func TestClientPatchInfoParsesVersionPackage(t *testing.T) {
	const raw = `{"entity_id":"1020000","url":"https://x19.gdl.netease.com/mc_client/1.20-227372_1720486851.7z",
	  "size":596963576,"md5":"508fa1b2cfd252ad63c70d0a84f9f0d4",
	  "name":"1.20-227372_1720486851.7z","version":"2","mc_version":1020000,
	  "core_lib_name":"CoreLibs_1020000.7z","core_lib_size":1173565,
	  "core_lib_md5":"1624a948dda5dd67a2b5c5a8277f6ef6","core_lib_url":"https://x19.gdl.netease.com/mc_client/CoreLibs_1020000.7z"}`

	var info ClientPatchInfo
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		t.Fatal(err)
	}
	if info.McVersion != 1020000 || info.Size != 596963576 {
		t.Fatalf("版本包字段不符：%+v", info)
	}
	if info.CoreLibURL == "" || info.CoreLibMD5 == "" || info.CoreLibSize != 1173565 {
		t.Fatalf("核心库字段缺失：%+v", info)
	}
}

// TestComponentDetailsKeepsDownloadFields 组件详情必须保留 res_url / res_md5。
func TestComponentDetailsKeepsDownloadFields(t *testing.T) {
	const raw = `[{"entity_id":"4687456828685462969","item_id":"4687456828685462969","mtypeid":3,
	  "sub_entities":[{"jar_md5":"5c7ec00fae3c64de97bb1a5f3833aea8","res_name":"4687456828685462969.zip",
	  "res_version":3,"res_url":"https://x19.fp.ps.netease.com/file/abc","res_md5":"e5a2e67dba2c2d6bb80817a1c9211036"}]}]`

	var components []ComponentDownloadInfo
	if err := json.Unmarshal([]byte(raw), &components); err != nil {
		t.Fatal(err)
	}
	sub := components[0].SubEntities[0]
	if sub.ResURL == "" || sub.ResMD5 == "" {
		t.Fatalf("下载字段缺失：%+v", sub)
	}
}

// TestIsJSONNull 网关用 null 表示"没有这项"，不能被当成空数组或静默忽略。
func TestIsJSONNull(t *testing.T) {
	if !isJSONNull([]byte(" null\n")) {
		t.Fatal("null 应被识别")
	}
	if isJSONNull([]byte("[]")) {
		t.Fatal("空数组不是 null")
	}
}
