package repository

import (
	"context"
	"controller-go/internal/entity"
)

// NodeAgentRepository 定义 NodeAgent 数据层必须实现的接口
type NodeAgentRepository interface {
	Save(ctx context.Context, agent *entity.NodeAgent) error
	GetByID(ctx context.Context, id string) (*entity.NodeAgent, error)
	ListIDs(ctx context.Context) ([]string, error)
}
