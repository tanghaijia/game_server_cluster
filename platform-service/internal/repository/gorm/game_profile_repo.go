package gorm

import (
	"context"

	"platform-service/internal/entity"

	"gorm.io/gorm"
)

type GameProfileRepo struct {
	db *gorm.DB
}

func NewGameProfileRepo(db *gorm.DB) *GameProfileRepo {
	return &GameProfileRepo{db: db}
}

func (r *GameProfileRepo) Save(ctx context.Context, p *entity.GameProfile) error {
	return r.db.WithContext(ctx).Save(p).Error
}

func (r *GameProfileRepo) GetByID(ctx context.Context, gameID string) (*entity.GameProfile, error) {
	var p entity.GameProfile
	err := r.db.WithContext(ctx).First(&p, "game_id = ?", gameID).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *GameProfileRepo) ListAll(ctx context.Context) ([]*entity.GameProfile, error) {
	var ps []*entity.GameProfile
	err := r.db.WithContext(ctx).Order("sort_order").Find(&ps).Error
	if err != nil {
		return nil, err
	}
	return ps, nil
}

func (r *GameProfileRepo) ListEnabled(ctx context.Context) ([]*entity.GameProfile, error) {
	var ps []*entity.GameProfile
	err := r.db.WithContext(ctx).Where("enabled = ?", true).Order("sort_order").Find(&ps).Error
	if err != nil {
		return nil, err
	}
	return ps, nil
}

func (r *GameProfileRepo) Delete(ctx context.Context, gameID string) error {
	return r.db.WithContext(ctx).Delete(&entity.GameProfile{}, "game_id = ?", gameID).Error
}
