package entity

import "time"

// GamePlatformConfig 平台运营方配置（按游戏全局，control=platform 的配置项）。
// 与 game_instances.config（player 级）合并下发：platform 为底、player 覆盖。
// 见 adapter-framework-design.md §3.3.4。
type GamePlatformConfig struct {
	GameID     string            `gorm:"column:game_id;primaryKey"`
	Config     map[string]string `gorm:"column:config;serializer:json"`
	Version    int64             `gorm:"column:version"`
	UpdatedBy  string            `gorm:"column:updated_by"`
	UpdateTime time.Time         `gorm:"column:update_time"`
}

func (GamePlatformConfig) TableName() string {
	return "game_platform_configs"
}
