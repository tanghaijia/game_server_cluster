package entity

import "time"

// SchedulerEventRow 调度事件持久化记录（000022 迁移，S30 审计/回溯）
type SchedulerEventRow struct {
	ID          int64     `gorm:"column:id;primaryKey"`
	OccurredAt  time.Time `gorm:"column:occurred_at"`
	Type        string    `gorm:"column:type"`
	InstanceID  string    `gorm:"column:instance_id"`
	NodeAgentID string    `gorm:"column:node_agent_id"`
	Detail      string    `gorm:"column:detail"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

func (SchedulerEventRow) TableName() string {
	return "scheduler_events"
}
