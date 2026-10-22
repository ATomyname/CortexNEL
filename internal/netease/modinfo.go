// modInfo 构造与校验语义（facts §3.7）：服务端校验的是 iid+md5 集合，
// 空列表必须输出 {"mods":[]}（不是 "" 或 null）。
package netease

import (
	"encoding/json"
	"errors"
	"strings"
)

// Mod 是 modInfo JSON 条目（字段序固定：modPath,name,id,iid,md5,version）。
type Mod struct {
	ModPath string `json:"modPath"`
	Name    string `json:"name"`
	ID      string `json:"id"`
	IID     string `json:"iid"`
	MD5     string `json:"md5"`
	Version string `json:"version"`
}

// ModList 是 modInfo 根对象。
type ModList struct {
	Mods []Mod `json:"mods"`
}

// BuildModInfoJSON 序列化 modInfo；对齐 Fantnel GameProfile.GetModInfo()：无组件或空列表时输出 ""。
func BuildModInfoJSON(mods []Mod) string {
	if len(mods) == 0 {
		return ""
	}
	data, err := json.Marshal(ModList{Mods: mods})
	if err != nil {
		return ""
	}
	return string(data)
}

// ErrBadJarName 表示组件文件名不符合 {ItemId}@{MTypeId}@{EntityId}.jar 形状。
var ErrBadJarName = errors.New("netease: 组件文件名形状非法")

// ParseJarName 解析本地组件 jar 名（facts §3.7 / Fantnel InstallerService）：
// iid = ItemId；核心模组(MTypeId=3) modPath = id = "{ItemId}@{MTypeId}@0.jar"；
// 资产模组(MTypeId=2) 可用实际文件名。
func ParseJarName(fileName string) (Mod, error) {
	base := strings.TrimSuffix(fileName, ".jar")
	parts := strings.Split(base, "@")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return Mod{}, ErrBadJarName
	}
	itemID, typeID, entityID := parts[0], parts[1], parts[2]
	modPath := itemID + "@" + typeID + "@" + entityID + ".jar"
	id := modPath
	if typeID == "3" {
		modPath = itemID + "@3@0.jar"
		id = itemID + "@3@0.jar"
	}
	return Mod{
		ModPath: modPath,
		ID:      id,
		IID:     itemID,
	}, nil
}
