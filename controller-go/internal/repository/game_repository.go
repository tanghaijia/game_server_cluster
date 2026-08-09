package repository

import (
	"context"
	"controller-go/internal/entity"
)

// GameRepository 定义数据层必须实现的接口（解耦的关键）
type GameRepository interface {
	Save(ctx context.Context, instance *entity.Game) error
	GetByID(ctx context.Context, id string) (*entity.Game, error)
	ListAll(ctx context.Context) ([]*entity.Game, error)
	Delete(ctx context.Context, id string) error
}
