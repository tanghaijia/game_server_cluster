package entity

import "time"

type OrderStatus int

const (
	OrderStatusCreated OrderStatus = iota
	OrderStatusPaid
	OrderStatusCancelled
	OrderStatusRefunded
)

// Order 游戏服务器购买订单。Amount 单位为分（整数，避免浮点误差）。
type Order struct {
	ID         string      `gorm:"column:id;primaryKey"`
	UserID     string      `gorm:"column:user_id;index"`
	GameID     string      `gorm:"column:game_id"`
	InstanceID string      `gorm:"column:instance_id"` // 下单后由平台层创建实例并回填
	Amount     int64       `gorm:"column:amount"`
	Status     OrderStatus `gorm:"column:status"`
	CreateTime time.Time   `gorm:"column:create_time"`
	UpdateTime time.Time   `gorm:"column:update_time"`
}

func (Order) TableName() string {
	return "orders"
}
