package gorm

import (
	"context"
	"controller-go/internal/entity"

	"gorm.io/gorm"
)

type NodeAgentRepo struct {
	db *gorm.DB
}

func NewNodeAgentRepo(db *gorm.DB) *NodeAgentRepo {
	return &NodeAgentRepo{db: db}
}

func (r *NodeAgentRepo) Save(ctx context.Context, agent *entity.NodeAgent) error {
	return r.db.WithContext(ctx).Save(agent).Error
}

func (r *NodeAgentRepo) GetByID(ctx context.Context, id string) (*entity.NodeAgent, error) {
	var agent entity.NodeAgent
	err := r.db.WithContext(ctx).First(&agent, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &agent, nil
}
