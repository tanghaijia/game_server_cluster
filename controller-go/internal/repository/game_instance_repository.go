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
	// ListByGame 查询某游戏的全部实例
	ListByGame(ctx context.Context, gameID string) ([]*entity.GameInstance, error)
	// ListActiveBySubscription 查询某订阅下所有活跃（占用单活跃槽位）实例
	// （M10：subscription_id = ? 且 status NOT IN (stopped, failed)）
	ListActiveBySubscription(ctx context.Context, subscriptionID string) ([]*entity.GameInstance, error)
	// ListBySubscription 查询某订阅下全部实例（M11：订阅内实例列表）
	ListBySubscription(ctx context.Context, subscriptionID string) ([]*entity.GameInstance, error)
	// ListAll 查询全部实例（按创建时间排序）
	ListAll(ctx context.Context) ([]*entity.GameInstance, error)
	// Delete 按主键删除实例
	Delete(ctx context.Context, id string) error
}
