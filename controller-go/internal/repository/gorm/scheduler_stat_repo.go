package gorm

import (
	"context"

	"controller-go/internal/entity"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SchedulerStatRepo struct {
	db *gorm.DB
}

func NewSchedulerStatRepo(db *gorm.DB) *SchedulerStatRepo {
	return &SchedulerStatRepo{db: db}
}

// Incr 原子累加：INSERT ... ON CONFLICT (outcome) DO UPDATE SET count = count + 1
func (r *SchedulerStatRepo) Incr(ctx context.Context, outcome string) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "outcome"}},
		DoUpdates: clause.Assignments(map[string]any{
			"count": gorm.Expr("scheduler_stats.count + 1"),
		}),
	}).Create(&entity.SchedulerStat{Outcome: outcome, Count: 1}).Error
}

func (r *SchedulerStatRepo) All(ctx context.Context) (map[string]int64, error) {
	var rows []entity.SchedulerStat
	if err := r.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		out[row.Outcome] = row.Count
	}
	return out, nil
}

func (r *SchedulerStatRepo) Sum(ctx context.Context) (int64, error) {
	var sum int64
	err := r.db.WithContext(ctx).Model(&entity.SchedulerStat{}).
		Select("COALESCE(SUM(count), 0)").Scan(&sum).Error
	return sum, err
}
