package repository

import (
	"context"

	"controller-go/internal/entity"
)

// NodeRepository 定义 Node 数据层必须实现的接口
type NodeRepository interface {
	Save(node *entity.Node) error
	GetByID(id string) (*entity.Node, error)
	// ListAll 查询全部节点
	ListAll(ctx context.Context) ([]*entity.Node, error)
}
