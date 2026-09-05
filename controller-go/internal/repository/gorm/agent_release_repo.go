package gorm

import (
	"context"

	"controller-go/internal/entity"

	"gorm.io/gorm"
)

type AgentReleaseRepo struct {
	db *gorm.DB
}

func NewAgentReleaseRepo(db *gorm.DB) *AgentReleaseRepo {
	return &AgentReleaseRepo{db: db}
}

func (r *AgentReleaseRepo) Save(ctx context.Context, release *entity.AgentRelease) error {
	return r.db.WithContext(ctx).Save(release).Error
}

func (r *AgentReleaseRepo) GetByID(ctx context.Context, id string) (*entity.AgentRelease, error) {
	var release entity.AgentRelease
	err := r.db.WithContext(ctx).First(&release, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &release, nil
}

func (r *AgentReleaseRepo) ListAll(ctx context.Context) ([]*entity.AgentRelease, error) {
	var releases []*entity.AgentRelease
	err := r.db.WithContext(ctx).Order("created_at DESC").Find(&releases).Error
	if err != nil {
		return nil, err
	}
	return releases, nil
}

func (r *AgentReleaseRepo) GetByVersionOSArch(ctx context.Context, version, osName, arch string) (*entity.AgentRelease, error) {
	var release entity.AgentRelease
	err := r.db.WithContext(ctx).
		Where("version = ? AND os = ? AND arch = ?", version, osName, arch).
		First(&release).Error
	if err != nil {
		return nil, err
	}
	return &release, nil
}
