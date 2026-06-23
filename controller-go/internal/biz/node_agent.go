package biz

import (
	"context"
	"controller-go/internal/entity"
)

type NodeAgentRepository interface {
	Save(ctx context.Context, agent *entity.NodeAgent) error
	GetByID(ctx context.Context, id string) (*entity.NodeAgent, error)
}

type NodeAgentUseCase struct {
	repo NodeAgentRepository
}

func NewNodeAgentUseCase(repo NodeAgentRepository) *NodeAgentUseCase {
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
