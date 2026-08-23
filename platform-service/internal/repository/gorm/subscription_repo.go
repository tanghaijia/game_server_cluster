package gorm

import (
	"context"
	"time"

	"platform-service/internal/entity"

	"gorm.io/gorm"
)

type SubscriptionRepo struct {
	db *gorm.DB
}

func NewSubscriptionRepo(db *gorm.DB) *SubscriptionRepo {
	return &SubscriptionRepo{db: db}
}

func (r *SubscriptionRepo) Save(ctx context.Context, sub *entity.Subscription) error {
	return r.db.WithContext(ctx).Save(sub).Error
}

func (r *SubscriptionRepo) GetByID(ctx context.Context, id string) (*entity.Subscription, error) {
	var sub entity.Subscription
	err := r.db.WithContext(ctx).First(&sub, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

func (r *SubscriptionRepo) ListByUser(ctx context.Context, userID string) ([]*entity.Subscription, error) {
	var subs []*entity.Subscription
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("create_time").Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return subs, nil
}

func (r *SubscriptionRepo) ListByPlan(ctx context.Context, planID string) ([]*entity.Subscription, error) {
	var subs []*entity.Subscription
	err := r.db.WithContext(ctx).Where("plan_id = ?", planID).Order("create_time").Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return subs, nil
}

func (r *SubscriptionRepo) ListAll(ctx context.Context) ([]*entity.Subscription, error) {
	var subs []*entity.Subscription
	err := r.db.WithContext(ctx).Order("create_time").Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return subs, nil
}

// ListOverdue 查询已到期订阅（M12 到期 sweep：active 且 expires_at 已过）
func (r *SubscriptionRepo) ListOverdue(ctx context.Context, now time.Time) ([]*entity.Subscription, error) {
	var subs []*entity.Subscription
	err := r.db.WithContext(ctx).
		Where("status = ?", entity.SubscriptionActive).
		Where("expires_at IS NOT NULL AND expires_at < ?", now).
		Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return subs, nil
}
