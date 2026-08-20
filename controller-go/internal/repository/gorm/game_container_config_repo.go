package gorm

import (
	"context"
	"controller-go/internal/entity"

	"gorm.io/gorm"
)

type GameContainerConfigRepo struct {
	db *gorm.DB
}

func NewGameContainerConfigRepo(db *gorm.DB) *GameContainerConfigRepo {
	return &GameContainerConfigRepo{db: db}
}

func (r *GameContainerConfigRepo) Save(ctx context.Context, config *entity.GameContainerConfig) error {
	return r.db.WithContext(ctx).Save(config).Error
}

func (r *GameContainerConfigRepo) GetByID(ctx context.Context, id string) (*entity.GameContainerConfig, error) {
	var config entity.GameContainerConfig
	err := r.db.WithContext(ctx).Preload("PortExcerpt").First(&config, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (r *GameContainerConfigRepo) Delete(ctx context.Context, id string) error {
	// 先删端口片段，再删配置
	if err := r.db.WithContext(ctx).
		Where("game_container_config_id = ?", id).
		Delete(&entity.GameContainerPortExcerpt{}).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Delete(&entity.GameContainerConfig{}, "id = ?", id).Error
}

// ReplacePortExcerpts 整体替换端口片段（删旧插新，事务内）
func (r *GameContainerConfigRepo) ReplacePortExcerpts(ctx context.Context, configID string, excerpts []entity.GameContainerPortExcerpt) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("game_container_config_id = ?", configID).
			Delete(&entity.GameContainerPortExcerpt{}).Error; err != nil {
			return err
		}
		for i := range excerpts {
			excerpts[i].ID = 0 // 新插入，重置自增主键
			excerpts[i].GameContainerConfigID = configID
			if err := tx.Create(&excerpts[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
