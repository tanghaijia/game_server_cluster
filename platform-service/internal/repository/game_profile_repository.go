package repository

import (
	"context"

	"platform-service/internal/entity"
)

// GameProfileRepository 游戏资料数据层接口
type GameProfileRepository interface {
	Save(ctx context.Context, p *entity.GameProfile) error
	GetByID(ctx context.Context, gameID string) (*entity.GameProfile, error)
	ListAll(ctx context.Context) ([]*entity.GameProfile, error)
	ListEnabled(ctx context.Context) ([]*entity.GameProfile, error)
}
