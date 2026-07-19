package repository

import "controller-go/internal/entity"

// NodeRepository 定义 Node 数据层必须实现的接口
type NodeRepository interface {
	Save(node *entity.Node) error
	GetByID(id string) (*entity.Node, error)
}
