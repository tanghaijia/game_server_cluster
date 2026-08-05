package gorm

import (
	"context"
	"controller-go/internal/entity"

	"gorm.io/gorm"
)

type SteamBranchRepo struct {
	db *gorm.DB
}

func NewSteamBranchRepo(db *gorm.DB) *SteamBranchRepo {
	return &SteamBranchRepo{db: db}
}

func (r *SteamBranchRepo) Save(ctx context.Context, branch *entity.SteamBranch) error {
	return r.db.WithContext(ctx).Save(branch).Error
}

func (r *SteamBranchRepo) GetByGameAndBranch(ctx context.Context, gameId, branchName string) (*entity.SteamBranch, error) {
	var branch entity.SteamBranch
	err := r.db.WithContext(ctx).
		Where("game_id = ? AND branch_name = ?", gameId, branchName).
		First(&branch).Error
	if err != nil {
		return nil, err
	}
	return &branch, nil
}

func (r *SteamBranchRepo) ListByGame(ctx context.Context, gameId string) ([]*entity.SteamBranch, error) {
	var branches []*entity.SteamBranch
	err := r.db.WithContext(ctx).
		Where("game_id = ?", gameId).
		Find(&branches).Error
	if err != nil {
		return nil, err
	}
	return branches, nil
}
