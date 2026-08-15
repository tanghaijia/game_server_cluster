package biz

import (
	"errors"
	"sync"

	"controller-go/internal/entity"
)

// SimpleScheduler 简单的调度器实现，按次序选择节点，且跳过已知失联（不可达）的节点。
// 存活状态由 NodeAgentHealthMonitor 周期探测后通过 SetAlive 维护。
type SimpleScheduler struct {
	nodeIDs []string
	counter int

	aliveMu sync.RWMutex
	alive   map[string]bool // node_agent id -> 存活（默认 true，等待首次探测）
}

func NewSimpleScheduler(nodeIDs []string) *SimpleScheduler {
	alive := make(map[string]bool, len(nodeIDs))
	for _, id := range nodeIDs {
		alive[id] = true
	}
	return &SimpleScheduler{nodeIDs: nodeIDs, alive: alive}
}

// SetAlive 更新节点存活状态（NodeAgentHealthMonitor 调用）
func (s *SimpleScheduler) SetAlive(nodeID string, alive bool) {
	s.aliveMu.Lock()
	defer s.aliveMu.Unlock()
	s.alive[nodeID] = alive
}

// AliveNodes 返回当前存活节点列表（供调试接口展示）
func (s *SimpleScheduler) AliveNodes() map[string]bool {
	s.aliveMu.RLock()
	defer s.aliveMu.RUnlock()
	out := make(map[string]bool, len(s.alive))
	for k, v := range s.alive {
		out[k] = v
	}
	return out
}

// Schedule 轮询选择第一个存活节点；全部失联时返回错误（实例将被标记 failed）。
func (s *SimpleScheduler) Schedule(gameInstance *entity.GameInstance) (string, error) {
	s.aliveMu.RLock()
	defer s.aliveMu.RUnlock()

	if len(s.nodeIDs) == 0 {
		return "", errors.New("no node agents registered")
	}

	for i := 0; i < len(s.nodeIDs); i++ {
		id := s.nodeIDs[s.counter%len(s.nodeIDs)]
		s.counter++
		if s.alive[id] {
			return id, nil
		}
	}
	return "", errors.New("no alive node agent available")
}
