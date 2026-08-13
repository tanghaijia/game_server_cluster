package biz

import (
	"context"
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
