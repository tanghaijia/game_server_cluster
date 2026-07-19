package biz

import "controller-go/internal/entity"

// SimpleScheduler 简单的调度器实现，按次序选择节点
type SimpleScheduler struct {
	// 可扩展：后续接入负载、地域等策略
	nodeIDs []string
	counter int
}

func NewSimpleScheduler(nodeIDs []string) *SimpleScheduler {
	return &SimpleScheduler{nodeIDs: nodeIDs}
}

func (s *SimpleScheduler) Schedule(gameInstance *entity.GameInstance) (string, error) {
	if len(s.nodeIDs) == 0 {
		return "", nil // 无可用节点时返回空
	}
	id := s.nodeIDs[s.counter%len(s.nodeIDs)]
	s.counter++
	return id, nil
}
