package biz

import (
	"context"
	"fmt"
	"time"

	"controller-go/internal/entity"
	"controller-go/internal/repository"
)

type NodeAgentUseCase struct {
	repo     repository.NodeAgentRepository
	nodeRepo repository.NodeRepository
}

func NewNodeAgentUseCase(repo repository.NodeAgentRepository, nodeRepo repository.NodeRepository) *NodeAgentUseCase {
	return &NodeAgentUseCase{repo: repo, nodeRepo: nodeRepo}
}

func (uc *NodeAgentUseCase) CreateNodeAgent(ctx context.Context, name, nodeID string, port int32) (*entity.NodeAgent, error) {
	if port <= 0 {
		port = 9090
	}
	agent := &entity.NodeAgent{
		ID:     name,
		NodeId: nodeID,
		Port:   port,
		Status: entity.Enabled,
	}
	err := uc.repo.Save(ctx, agent)
	if err != nil {
		return nil, err
	}
	return agent, nil
}

func (uc *NodeAgentUseCase) GetNode(ctx context.Context, nodeagent *entity.NodeAgent) (*entity.Node, error) {
	node, err := uc.nodeRepo.GetByID(nodeagent.NodeId)
	return node, err
}

// ListNodeAgents 列出全部 node_agent
func (uc *NodeAgentUseCase) ListNodeAgents(ctx context.Context) ([]*entity.NodeAgent, error) {
	return uc.repo.ListAll(ctx)
}

// SetEnabled 启用/停用 node_agent（调度与缓存循环只认 Enabled 节点）
func (uc *NodeAgentUseCase) SetEnabled(ctx context.Context, id string, enabled bool) (*entity.NodeAgent, error) {
	agent, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if enabled {
		agent.Status = entity.Enabled
	} else {
		agent.Status = entity.Disabled
	}
	if err := uc.repo.Save(ctx, agent); err != nil {
		return nil, err
	}
	return agent, nil
}

// NodeAgentHealth 单个 node_agent 的存活视图（供管理员查看）
type NodeAgentHealth struct {
	ID              string     `json:"id"`
	NodeId          string     `json:"node_id"`
	Port            int32      `json:"port"`
	Addr            string     `json:"addr"`
	Status          string     `json:"status"`
	Alive           bool       `json:"alive"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at"`
}

// ListNodeAgentHealth 列出全部 node_agent 的存活视图（含连接地址）
func (uc *NodeAgentUseCase) ListNodeAgentHealth(ctx context.Context) ([]NodeAgentHealth, error) {
	agents, err := uc.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]NodeAgentHealth, 0, len(agents))
	for _, a := range agents {
		h := NodeAgentHealth{
			ID:              a.ID,
			NodeId:          a.NodeId,
			Port:            a.Port,
			Alive:           a.Alive,
			LastHeartbeatAt: a.LastHeartbeatAt,
		}
		if a.Status == entity.Enabled {
			h.Status = "enabled"
		} else {
			h.Status = "disabled"
		}
		if a.NodeId != "" {
			if node, err := uc.nodeRepo.GetByID(a.NodeId); err == nil {
				h.Addr = fmt.Sprintf("%s:%d", node.Ip, a.Port)
			}
		}
		out = append(out, h)
	}
	return out, nil
}
