// 版本化 JSON 文件读写：原子写（tmp+rename）+ schemaVersion 迁移 seam（架构文档 §9）。
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// CurrentSchemaVersion 是当前 schema 版本。
const CurrentSchemaVersion = 1

// ErrSchemaTooNew 表示文件版本比程序新（拒绝降级写）。
var ErrSchemaTooNew = errors.New("store: 数据文件版本比程序新，拒绝读取")

// envelope 是落盘外壳：schemaVersion + 数据。
type envelope struct {
	SchemaVersion int             `json:"schemaVersion"`
	Data          json.RawMessage `json:"data"`
}

// Save 原子写一个版本化文件。
func Save[T any](path string, data T) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	full := envelope{SchemaVersion: CurrentSchemaVersion, Data: raw}
	out, err := json.MarshalIndent(full, "", "  ")
	if err != nil {
		return err
	}
	if err := mkdirAll(filepath.Dir(path)); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load 读一个版本化文件；版本不符时返回原始数据与版本供迁移方处理。
func Load[T any](path string) (T, int, error) {
	var zero T
	raw, err := os.ReadFile(path)
	if err != nil {
		return zero, 0, err
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return zero, 0, err
	}
	if env.SchemaVersion > CurrentSchemaVersion {
		return zero, env.SchemaVersion, ErrSchemaTooNew
	}
	var data T
	if len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, &data); err != nil {
			return zero, env.SchemaVersion, err
		}
	}
	return data, env.SchemaVersion, nil
}

// Exists 判断文件是否存在。
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
