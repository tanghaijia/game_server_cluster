package entity

import "time"

// PlanBasketItem 套餐篮子中的一项：允许的游戏 + 该游戏默认配置 preset。
// 凭证类配置（如 DST cluster_token）不进 preset（走凭证池注入，见 adapter-framework-design.md §3.6.5）。
type PlanBasketItem struct {
	GameID string            `json:"game_id"`
	Config map[string]string `json:"config,omitempty"`
}

// ServerPlan 管理员套餐（SKU）：资源规格、价格、时长、允许的游戏篮子。
// 编辑只影响新购订阅（购买时 basket 快照进 subscriptions.basket_snapshot）。
type ServerPlan struct {
	ID                  string           `gorm:"column:id;primaryKey"`
	DisplayName         string           `gorm:"column:display_name"`
	Description         string           `gorm:"column:description"`
	PriceCents          int64            `gorm:"column:price_cents"` // 金额：分
	DurationHours       int              `gorm:"column:duration_hours"` // 时长：小时（0 = 永久/手动）
	ResourceCPUMilli    int64            `gorm:"column:resource_cpu_milli"`
	ResourceMemoryBytes int64            `gorm:"column:resource_memory_bytes"`
	ResourceDiskBytes   int64            `gorm:"column:resource_disk_bytes"`
	MaxInstances        int              `gorm:"column:max_instances"` // 订阅内实例数量上限（0 = 不限）
	Basket              []PlanBasketItem `gorm:"column:basket;serializer:json"`
	Enabled             bool             `gorm:"column:enabled"`
	CreateTime          time.Time        `gorm:"column:create_time"`
	UpdateTime          time.Time        `gorm:"column:update_time"`
}

func (ServerPlan) TableName() string {
	return "server_plans"
}
