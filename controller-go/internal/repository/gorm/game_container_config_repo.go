package gorm

import (
	"context"
	"controller-go/internal/entity"

	"gorm.io/gorm"
)

type GameContainerConfigRepo struct {
	db *gorm.DB
}

func NewGameContainerConfigRepo(db *gorm.DB) *GameContainerConfigRepo {
	return &GameContainerConfigRepo{db: db}
}

func (r *GameContainerConfigRepo) Save(ctx context.Context, config *entity.GameContainerConfig) error {
	return r.db.WithContext(ctx).Save(config).Error
}

func (r *GameContainerConfigRepo) GetByID(ctx context.Context, id string) (*entity.GameContainerConfig, error) {
	var config entity.GameContainerConfig
	err := r.db.WithContext(ctx).Preload("PortExcerpt").First(&config, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &config, nil
}
