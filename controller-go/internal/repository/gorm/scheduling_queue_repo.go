package gorm

import (
	"context"
	"time"

	"controller-go/internal/entity"

	"gorm.io/gorm"
)

type SchedulingQueueRepo struct {
	db *gorm.DB
}

func NewSchedulingQueueRepo(db *gorm.DB) *SchedulingQueueRepo {
	return &SchedulingQueueRepo{db: db}
}

func (r *SchedulingQueueRepo) Enqueue(ctx context.Context, q *entity.SchedulingQueue) error {
	return r.db.WithContext(ctx).Create(q).Error
}

func (r *SchedulingQueueRepo) Dequeue(ctx context.Context, instanceID string) error {
	return r.db.WithContext(ctx).
		Where("instance_id = ?", instanceID).
		Delete(&entity.SchedulingQueue{}).Error
}

func (r *SchedulingQueueRepo) Get(ctx context.Context, instanceID string) (*entity.SchedulingQueue, error) {
	var q entity.SchedulingQueue
	err := r.db.WithContext(ctx).First(&q, "instance_id = ?", instanceID).Error
	if err != nil {
		return nil, err
	}
	return &q, nil
}

func (r *SchedulingQueueRepo) UpdateWake(ctx context.Context, instanceID string, wakeAt time.Time, attempts int) error {
	return r.db.WithContext(ctx).Model(&entity.SchedulingQueue{}).
		Where("instance_id = ?", instanceID).
		Updates(map[string]any{"wake_at": wakeAt, "attempts": attempts}).Error
}

func (r *SchedulingQueueRepo) ListDue(ctx context.Context, now time.Time) ([]*entity.SchedulingQueue, error) {
	var out []*entity.SchedulingQueue
	err := r.db.WithContext(ctx).
		Where("wake_at <= ?", now).
		Order("priority ASC, queued_at ASC").
		Find(&out).Error
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *SchedulingQueueRepo) ListAll(ctx context.Context) ([]*entity.SchedulingQueue, error) {
	var out []*entity.SchedulingQueue
	err := r.db.WithContext(ctx).
		Order("priority ASC, queued_at ASC").
		Find(&out).Error
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *SchedulingQueueRepo) Count(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&entity.SchedulingQueue{}).Count(&n).Error
	return n, err
}
