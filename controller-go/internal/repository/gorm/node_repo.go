package gorm

import (
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
