package biz

import (
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
