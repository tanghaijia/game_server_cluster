package gorm

import (
	"context"

	"platform-service/internal/entity"

	"gorm.io/gorm"
)

type ServerPlanRepo struct {
	db *gorm.DB
}

func NewServerPlanRepo(db *gorm.DB) *ServerPlanRepo {
	return &ServerPlanRepo{db: db}
}

func (r *ServerPlanRepo) Save(ctx context.Context, plan *entity.ServerPlan) error {
	return r.db.WithContext(ctx).Save(plan).Error
}

func (r *ServerPlanRepo) GetByID(ctx context.Context, id string) (*entity.ServerPlan, error) {
	var plan entity.ServerPlan
	err := r.db.WithContext(ctx).First(&plan, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

func (r *ServerPlanRepo) ListAll(ctx context.Context) ([]*entity.ServerPlan, error) {
	var plans []*entity.ServerPlan
	err := r.db.WithContext(ctx).Order("create_time").Find(&plans).Error
	if err != nil {
		return nil, err
	}
	return plans, nil
}

func (r *ServerPlanRepo) ListEnabled(ctx context.Context) ([]*entity.ServerPlan, error) {
	var plans []*entity.ServerPlan
	err := r.db.WithContext(ctx).Where("enabled = ?", true).Order("create_time").Find(&plans).Error
	if err != nil {
		return nil, err
	}
	return plans, nil
}

func (r *ServerPlanRepo) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&entity.ServerPlan{}, "id = ?", id).Error
}
