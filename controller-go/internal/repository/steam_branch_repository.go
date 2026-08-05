package repository

import (
	"context"
	"controller-go/internal/entity"
)

// SteamBranchRepository 定义 SteamBranch 数据层必须实现的接口
type SteamBranchRepository interface {
	// Save 插入或更新分支（按主键）
	Save(ctx context.Context, branch *entity.SteamBranch) error
	// GetByGameAndBranch 按 game_id + branch_name 查询，未找到返回 gorm.ErrRecordNotFound
	GetByGameAndBranch(ctx context.Context, gameId, branchName string) (*entity.SteamBranch, error)
	// ListByGame 查询某 game 的全部分支
	ListByGame(ctx context.Context, gameId string) ([]*entity.SteamBranch, error)
}
