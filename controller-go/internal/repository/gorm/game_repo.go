package gorm

import (
	"context"
	"controller-go/internal/entity"

	"gorm.io/gorm"
)

type GameRepo struct {
	db *gorm.DB
}

func NewGameRepo(db *gorm.DB) *GameRepo {
	return &GameRepo{db: db}
}

func (r *GameRepo) Save(ctx context.Context, game *entity.Game) error {
	return r.db.WithContext(ctx).Save(game).Error
}

func (r *GameRepo) GetByID(ctx context.Context, id string) (*entity.Game, error) {
	var game entity.Game
	err := r.db.WithContext(ctx).First(&game, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &game, nil
}

func (r *GameRepo) ListAll(ctx context.Context) ([]*entity.Game, error) {
	var games []*entity.Game
	err := r.db.WithContext(ctx).Find(&games).Error
	if err != nil {
		return nil, err
	}
	return games, nil
}
