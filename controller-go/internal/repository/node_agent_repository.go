package repository

import (
	"context"
	"controller-go/internal/entity"
)

// NodeAgentRepository 定义 NodeAgent 数据层必须实现的接口
type NodeAgentRepository interface {
	Save(ctx context.Context, agent *entity.NodeAgent) error
	GetByID(ctx context.Context, id string) (*entity.NodeAgent, error)
	// ListEnabledIDs 返回已启用（Enabled）的 node_agent id 列表
	ListEnabledIDs(ctx context.Context) ([]string, error)
	// ListAll 查询全部 node_agent
	ListAll(ctx context.Context) ([]*entity.NodeAgent, error)
}
