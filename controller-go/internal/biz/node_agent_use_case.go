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

func (uc *NodeAgentUseCase) CreateNodeAgent(ctx context.Context, name string) (*entity.NodeAgent, error) {
	agent := &entity.NodeAgent{
		ID:   name,
		Port: 9090,
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
