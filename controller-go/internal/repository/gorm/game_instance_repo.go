package gorm

import (
	"context"
	"time"

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
	instance.UpdateTime = time.Now()
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
		Updates(map[string]any{
			"status":      instance.Status,
			"update_time": time.Now(),
		}).Error
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

// ListActiveBySubscription 查询订阅下所有活跃实例（status NOT IN (stopped, failed)）。
// 谓词与迁移 000027 部分唯一索引保持一致（防漂移测试钉死）。
func (r *GameInstanceRepo) ListActiveBySubscription(ctx context.Context, subscriptionID string) ([]*entity.GameInstance, error) {
	var instances []*entity.GameInstance
	err := r.db.WithContext(ctx).
		Where("subscription_id = ?", subscriptionID).
		Where("status NOT IN ?", []entity.InstanceStatus{entity.StatusStopped, entity.Failed}).
		Order("create_time").
		Find(&instances).Error
	if err != nil {
		return nil, err
	}
	return instances, nil
}

// ListBySubscription 查询订阅下全部实例（含 stopped/failed，M11）
func (r *GameInstanceRepo) ListBySubscription(ctx context.Context, subscriptionID string) ([]*entity.GameInstance, error) {
	var instances []*entity.GameInstance
	err := r.db.WithContext(ctx).
		Where("subscription_id = ?", subscriptionID).
		Order("create_time").
		Find(&instances).Error
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
