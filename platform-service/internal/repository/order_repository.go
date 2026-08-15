package repository

import (
	"context"

	"platform-service/internal/entity"
)

// OrderRepository 定义订单数据层必须实现的接口
type OrderRepository interface {
	Save(ctx context.Context, order *entity.Order) error
	GetByID(ctx context.Context, id string) (*entity.Order, error)
	ListByUser(ctx context.Context, userID string) ([]*entity.Order, error)
	ListByGame(ctx context.Context, gameID string) ([]*entity.Order, error)
	ListByUserAndGame(ctx context.Context, userID, gameID string) ([]*entity.Order, error)
	ListAll(ctx context.Context) ([]*entity.Order, error)
	// MarkGameRemoved 把某游戏未终结的订单标记为已下架（删除游戏时调用）
	MarkGameRemoved(ctx context.Context, gameID string) error
}
