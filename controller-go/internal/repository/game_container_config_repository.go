package repository

import (
	"context"
	"controller-go/internal/entity"
)

// GameContainerConfigRepository 定义 GameContainerConfig 数据层必须实现的接口
type GameContainerConfigRepository interface {
	Save(ctx context.Context, config *entity.GameContainerConfig) error
	GetByID(ctx context.Context, id string) (*entity.GameContainerConfig, error)
}
