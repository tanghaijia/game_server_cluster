package repository

import (
	"context"
	"time"

	"controller-go/internal/entity"
)

// NodeRepository 定义 Node 数据层必须实现的接口
type NodeRepository interface {
	Save(node *entity.Node) error
	GetByID(id string) (*entity.Node, error)
	// ListAll 查询全部节点
	ListAll(ctx context.Context) ([]*entity.Node, error)
	// UpdateDynamicUsage 更新节点动态资源（ResourceSampler 心跳上报写入）
	UpdateDynamicUsage(ctx context.Context, nodeID string, u entity.NodeDynamicUsage, reportedAt time.Time) error
}
