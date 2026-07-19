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

func NewNodeAgentUseCase(repo repository.NodeAgentRepository) *NodeAgentUseCase {
	return &NodeAgentUseCase{repo: repo}
}

func (uc *NodeAgentUseCase) CreateNodeAgent(ctx context.Context, name string) (*entity.NodeAgent, error) {
	agent := &entity.NodeAgent{
		Node: nil,
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
