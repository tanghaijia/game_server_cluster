package biz

import (
	"controller-go/internal/entity"
)

type NodeRepository interface {
	Save(node *entity.Node) error
	GetByID(id string) (*entity.Node, error)
}

type NodeUseCase struct {
	repo NodeRepository
}

func NewNodeUseCase(repo NodeRepository) *NodeUseCase {
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
