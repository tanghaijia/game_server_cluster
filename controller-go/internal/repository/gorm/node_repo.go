package gorm

import (
	"context"

	"controller-go/internal/entity"

	"gorm.io/gorm"
)

type NodeRepo struct {
	db *gorm.DB
}

func NewNodeRepo(db *gorm.DB) *NodeRepo {
	return &NodeRepo{db: db}
}

func (r *NodeRepo) Save(node *entity.Node) error {
	return r.db.Save(node).Error
}

func (r *NodeRepo) GetByID(id string) (*entity.Node, error) {
	var node entity.Node
	err := r.db.First(&node, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &node, nil
}

func (r *NodeRepo) ListAll(ctx context.Context) ([]*entity.Node, error) {
	var nodes []*entity.Node
	err := r.db.WithContext(ctx).Find(&nodes).Error
	if err != nil {
		return nil, err
	}
	return nodes, nil
}
