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

func (r *GameInstanceRepo) ListByStatuses(ctx context.Context, statuses ...entity.InstanceStatus) ([]*entity.GameInstance, error) {
	var instances []*entity.GameInstance
	err := r.db.WithContext(ctx).
		Where("status IN ?", statuses).
		Find(&instances).Error
	if err != nil {
		return nil, err
	}
	return instances, nil
}

func (r *GameInstanceRepo) ListByGame(ctx context.Context, gameID string) ([]*entity.GameInstance, error) {
	var instances []*entity.GameInstance
	err := r.db.WithContext(ctx).Where("game_id = ?", gameID).Order("create_time").Find(&instances).Error
	if err != nil {
		return nil, err
	}
	return instances, nil
}

func (r *GameInstanceRepo) ListAll(ctx context.Context) ([]*entity.GameInstance, error) {
	var instances []*entity.GameInstance
	err := r.db.WithContext(ctx).
		Order("create_time").
		Find(&instances).Error
	if err != nil {
		return nil, err
	}
	return instances, nil
}

func (r *GameInstanceRepo) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).
		Delete(&entity.GameInstance{}, "id = ?", id).Error
}
