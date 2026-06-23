package biz

import (
	"context"
	"controller-go/internal/entity"
)

// GameRepository 定义数据层必须实现的接口（解耦的关键）
type GameRepository interface {
	Save(ctx context.Context, instance *entity.Game) error
	GetByID(ctx context.Context, id string) (*entity.Game, error)
}

type GameUseCase struct {
	gamerepo GameRepository // 组合接口，不关心底层是 MySQL 还是 PG
}

func NewGameUseCase(gamerepo GameRepository) *GameUseCase {
	return &GameUseCase{gamerepo: gamerepo}
}

func (uc *GameUseCase) CreateGame(ctx context.Context, name string) (*entity.Game, error) {
	game := &entity.Game{
		Name: name,
	}
	err := uc.gamerepo.Save(ctx, game)
	if err != nil {
		return nil, err
	}
	return game, nil
}
