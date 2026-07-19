package repository

import (
	"context"
	"controller-go/internal/entity"
)

// GameInstanceRepository 定义 GameInstance 数据层必须实现的接口
type GameInstanceRepository interface {
	Save(ctx context.Context, instance *entity.GameInstance) error
	GetByID(ctx context.Context, id string) (*entity.GameInstance, error)
	UpdateStatus(ctx context.Context, instance *entity.GameInstance) error
}
