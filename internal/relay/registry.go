// 有序注册表与匹配（架构文档 §6.3）：处理器顺序会话启动时冻结（P3）。
package relay

import "slices"

// Registry 是包处理器注册表：注册顺序即派发顺序。
type Registry struct {
	entries []Registered
}

// Register 追加处理器（进程启动期调用，主 goroutine 串行）。
func (rg *Registry) Register(entries ...Registered) {
	rg.entries = append(rg.entries, entries...)
}

// Snapshot 返回冻结副本，供单会话使用（P3：相同输入 → 相同输出）。
func (rg *Registry) Snapshot() []Registered {
	out := make([]Registered, len(rg.entries))
	copy(out, rg.entries)
	return out
}

// Match 返回按冻结顺序命中的处理器工厂。
// 匹配规则：State ∧ Dir ∧ (PacketID 相等 ∨ 通配) ∧ (GameID 相等 ∨ 通配) ∧ Version。
func (rg *Registry) Match(state State, dir Direction, id int32, gameID string, version int32) []Registered {
	var hits []Registered
	for _, e := range rg.entries {
		k := e.Key
		if k.State != state || k.Dir != dir {
			continue
		}
		if k.PacketID != -1 && k.PacketID != id {
			continue
		}
		if k.GameID != "" && k.GameID != gameID {
			continue
		}
		if !versionMatches(k.Versions, version) {
			continue
		}
		hits = append(hits, e)
	}
	return hits
}

func versionMatches(versions []int32, version int32) bool {
	if len(versions) == 0 {
		return true
	}
	return slices.Contains(versions, version)
}
