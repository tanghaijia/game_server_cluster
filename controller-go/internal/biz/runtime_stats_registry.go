package biz

import "sync"

// InstanceRuntimeStat controller 侧缓存的单实例运行时统计（B-04/P1-1）。
// 来源：node_agent 运行时探针（RuntimeProbeService）→ NodeHeartbeat.instance_runtime → 健康监控消费。
type InstanceRuntimeStat struct {
	InstanceID  string `json:"instance_id"`
	PlayerCount uint32 `json:"player_count"`
	MaxPlayers  uint32 `json:"max_players"`
	Healthy     bool   `json:"healthy"`
	ProbeMode   string `json:"probe_mode"` // "a2s" | "script"
	ProbeError  string `json:"probe_error"`
	SampledAt   string `json:"sampled_at"`
}

// RuntimeStatsRegistry 内存缓存的实例运行时统计。
// 每个 node_agent 心跳到达时以 UpdateForNode 整体替换该节点上报的实例集；
// 不再上报的实例（已停止/失败/节点失联）自动清除。
// 内存语义（非持久化）：属于瞬态遥测，重启丢失可接受。
type RuntimeStatsRegistry struct {
	mu    sync.RWMutex
	stats map[string]InstanceRuntimeStat // instanceID -> stat
	nodes map[string]map[string]struct{} // nodeAgentID -> instanceIDs
}

func NewRuntimeStatsRegistry() *RuntimeStatsRegistry {
	return &RuntimeStatsRegistry{
		stats: make(map[string]InstanceRuntimeStat),
		nodes: make(map[string]map[string]struct{}),
	}
}

// UpdateForNode 用某节点本次心跳携带的实例统计整体替换该节点集合。
// 按节点作用域：节点 A 上报空列表不会误删节点 B 的实例。
func (r *RuntimeStatsRegistry) UpdateForNode(nodeAgentID string, stats []InstanceRuntimeStat) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make(map[string]struct{}, len(stats))
	for _, s := range stats {
		r.stats[s.InstanceID] = s
		ids[s.InstanceID] = struct{}{}
	}
	if prev, ok := r.nodes[nodeAgentID]; ok {
		for id := range prev {
			if _, still := ids[id]; !still {
				delete(r.stats, id)
			}
		}
	}
	r.nodes[nodeAgentID] = ids
}

// Get 查询单实例运行时统计（ok=false = 尚无数据，前端按"未知"展示）。
func (r *RuntimeStatsRegistry) Get(instanceID string) (InstanceRuntimeStat, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.stats[instanceID]
	return s, ok
}
