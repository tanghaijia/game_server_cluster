package repository

import (
	"context"
	"time"

	"platform-service/internal/entity"
)

// SubscriptionRepository 订阅数据层接口
type SubscriptionRepository interface {
	Save(ctx context.Context, sub *entity.Subscription) error
	GetByID(ctx context.Context, id string) (*entity.Subscription, error)
	ListByUser(ctx context.Context, userID string) ([]*entity.Subscription, error)
	ListByPlan(ctx context.Context, planID string) ([]*entity.Subscription, error)
	ListAll(ctx context.Context) ([]*entity.Subscription, error)
	// ListOverdue 查询已到期（status=active 且 expires_at < now）的订阅（M12 到期 sweep）
	ListOverdue(ctx context.Context, now time.Time) ([]*entity.Subscription, error)
}
