package biz

import (
	"context"
	"errors"

	"controller-go/internal/entity"
	"controller-go/internal/repository"
)

type NodeUseCase struct {
	repo repository.NodeRepository
}

func NewNodeUseCase(repo repository.NodeRepository) *NodeUseCase {
	return &NodeUseCase{repo: repo}
}

func (uc *NodeUseCase) CreateNode(ip string) (*entity.Node, error) {
	node := &entity.Node{
		Ip: ip,
	}
	err := uc.repo.Save(node)
	if err != nil {
		return nil, err
	}
	return node, nil
}

// ListNodes 列出全部节点
func (uc *NodeUseCase) ListNodes(ctx context.Context) ([]*entity.Node, error) {
	return uc.repo.ListAll(ctx)
}

// GetNode 按 id 查询节点
func (uc *NodeUseCase) GetNode(ctx context.Context, id string) (*entity.Node, error) {
	if id == "" {
		return nil, errors.New("id is required")
	}
	return uc.repo.GetByID(id)
}
