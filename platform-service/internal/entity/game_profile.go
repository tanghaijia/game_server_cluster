package entity

import "time"

// GameProfile 游戏产品/呈现目录（见 docs/multi-game-platform-design.md）
// game_id 关联 controller 的 games.ID（跨库，无外键约束）
type GameProfile struct {
	GameID      string    `gorm:"column:game_id;primaryKey" json:"game_id"`
	DisplayName string    `gorm:"column:display_name" json:"display_name"`
	IconURL     string    `gorm:"column:icon_url" json:"icon_url"`
	AccentColor string    `gorm:"column:accent_color" json:"accent_color"`
	Description string    `gorm:"column:description" json:"description"`
	Enabled     bool      `gorm:"column:enabled" json:"enabled"`
	SortOrder   int       `gorm:"column:sort_order" json:"sort_order"`
	UpdateTime  time.Time `gorm:"column:update_time" json:"update_time"`
}

func (GameProfile) TableName() string {
	return "game_profiles"
}
