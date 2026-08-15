package repository

import (
	"context"
	"controller-go/internal/entity"
)

// GameContainerConfigRepository 定义 GameContainerConfig 数据层必须实现的接口
type GameContainerConfigRepository interface {
	Save(ctx context.Context, config *entity.GameContainerConfig) error
	GetByID(ctx context.Context, id string) (*entity.GameContainerConfig, error)
	// Delete 删除容器配置及其端口片段
	Delete(ctx context.Context, id string) error
}
