package biz

import (
	"context"
	"controller-go/internal/entity"
	"controller-go/internal/repository"
)

type GameUseCase struct {
	gamerepo repository.GameRepository // 组合接口，不关心底层是 MySQL 还是 PG
}

func NewGameUseCase(gamerepo repository.GameRepository) *GameUseCase {
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
