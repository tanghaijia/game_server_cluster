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
	// ListByStatuses 按状态批量查询实例
	ListByStatuses(ctx context.Context, statuses ...entity.InstanceStatus) ([]*entity.GameInstance, error)
}
