package repository

import (
	"context"

	"controller-go/internal/entity"
)

// GamePlatformConfigRepository 平台运营方配置数据层接口（按游戏全局）
type GamePlatformConfigRepository interface {
	// GetByGame 查询某游戏的平台配置（不存在返回 nil, nil）
	GetByGame(ctx context.Context, gameID string) (*entity.GamePlatformConfig, error)
	// Save 保存（upsert，version+1）
	Save(ctx context.Context, cfg *entity.GamePlatformConfig) error
}
