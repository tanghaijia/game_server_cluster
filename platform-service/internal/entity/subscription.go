package entity

import "time"

type SubscriptionStatus string

const (
	SubscriptionActive    SubscriptionStatus = "active"    // 有效期内
	SubscriptionExpired   SubscriptionStatus = "expired"   // 到期（M12 到期 sweep 写入）
	SubscriptionCancelled SubscriptionStatus = "cancelled" // 用户/管理员取消
	SubscriptionSuspended SubscriptionStatus = "suspended" // 管理员停用（M12：停活跃实例 + 禁 start）
)

// Subscription 用户订阅（购买单元）：绑定一个套餐，可在订阅内创建多个游戏实例
// （实例归属 controller game_instances.subscription_id，单活跃约束 M10 落地）。
type Subscription struct {
	ID             string             `gorm:"column:id;primaryKey"`
	UserID         string             `gorm:"column:user_id;index"`
	PlanID         string             `gorm:"column:plan_id"`
	Status         SubscriptionStatus `gorm:"column:status"`
	ExpiresAt      *time.Time         `gorm:"column:expires_at"` // NULL = 不过期
	BasketSnapshot []PlanBasketItem   `gorm:"column:basket_snapshot;serializer:json"`
	// 购买时从套餐快照的实例数量上限（0 = 不限；快照语义：套餐编辑不追溯）
	MaxInstances int      `gorm:"column:max_instances"`
	CreateTime   time.Time `gorm:"column:create_time"`
	UpdateTime   time.Time `gorm:"column:update_time"`
}

func (Subscription) TableName() string {
	return "subscriptions"
}
