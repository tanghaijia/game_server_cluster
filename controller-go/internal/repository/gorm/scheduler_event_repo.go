package gorm

import (
	"context"
	"time"

	"controller-go/internal/entity"

	"gorm.io/gorm"
)

type SchedulerEventRepo struct {
	db *gorm.DB
}

func NewSchedulerEventRepo(db *gorm.DB) *SchedulerEventRepo {
	return &SchedulerEventRepo{db: db}
}

func (r *SchedulerEventRepo) AppendBatch(ctx context.Context, events []*entity.SchedulerEventRow) error {
	if len(events) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&events).Error
}

func (r *SchedulerEventRepo) ListSince(ctx context.Context, since time.Time, typ string, limit int) ([]*entity.SchedulerEventRow, error) {
	q := r.db.WithContext(ctx).Where("occurred_at >= ?", since)
	if typ != "" {
		q = q.Where("type = ?", typ)
	}
	q = q.Order("occurred_at DESC, id DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var out []*entity.SchedulerEventRow
	if err := q.Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *SchedulerEventRepo) PruneBefore(ctx context.Context, before time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Where("occurred_at < ?", before).Delete(&entity.SchedulerEventRow{})
	return res.RowsAffected, res.Error
}
