package gorm

import (
	"context"
	"controller-go/internal/entity"

	"gorm.io/gorm"
)

type GamePlatformConfigRepo struct {
	db *gorm.DB
}

func NewGamePlatformConfigRepo(db *gorm.DB) *GamePlatformConfigRepo {
	return &GamePlatformConfigRepo{db: db}
}

func (r *GamePlatformConfigRepo) GetByGame(ctx context.Context, gameID string) (*entity.GamePlatformConfig, error) {
	var cfg entity.GamePlatformConfig
	err := r.db.WithContext(ctx).First(&cfg, "game_id = ?", gameID).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (r *GamePlatformConfigRepo) Save(ctx context.Context, cfg *entity.GamePlatformConfig) error {
	return r.db.WithContext(ctx).Save(cfg).Error
}
