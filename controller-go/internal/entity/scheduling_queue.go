package entity

import "time"

// SchedulingQueue 排队队列（§8.1，000015 迁移）：
// 一行 = 一个排队实例；game_instances.status=Queued 与队列行一一对应。
// 排序：priority ASC, queued_at ASC（数值越小越优先，默认 100，D7）。
type SchedulingQueue struct {
	InstanceID string    `gorm:"column:instance_id;primaryKey"`
	Priority   int       `gorm:"column:priority"`
	Reason     string    `gorm:"column:reason"` // 排队原因（ScheduleResult.Reason）
	Attempts   int       `gorm:"column:attempts"` // 已重试次数（退避依据，D9）
	WakeAt     time.Time `gorm:"column:wake_at"` // 下次唤醒时间
	QueuedAt   time.Time `gorm:"column:queued_at"`
	TimeoutAt  time.Time `gorm:"column:timeout_at"` // 排队超时截止（S16）
}

func (SchedulingQueue) TableName() string {
	return "scheduling_queue"
}
