package gorm

import (
	"context"
	"controller-go/internal/entity"

	"gorm.io/gorm"
)

type GameInstanceRepo struct {
	db *gorm.DB
}

func NewGameInstanceRepo(db *gorm.DB) *GameInstanceRepo {
	return &GameInstanceRepo{db: db}
}

func (r *GameInstanceRepo) Save(ctx context.Context, instance *entity.GameInstance) error {
	return r.db.WithContext(ctx).Save(instance).Error
}

func (r *GameInstanceRepo) GetByID(ctx context.Context, id string) (*entity.GameInstance, error) {
	var instance entity.GameInstance
	err := r.db.WithContext(ctx).First(&instance, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &instance, nil
}

func (r *GameInstanceRepo) UpdateStatus(ctx context.Context, instance *entity.GameInstance) error {
	return r.db.WithContext(ctx).Model(&entity.GameInstance{}).
		Where("id = ?", instance.ID).
		Update("status", instance.Status).Error
}
