package entity

import "time"

type NodeAgent struct {
	ID              string          `gorm:"column:id;primaryKey"`
	NodeId          string          `gorm:"column:node_id"`
	Port            int32           `gorm:"column:port"`
	Status          NodeAgentStatus `gorm:"column:status"`
	Alive           bool            `gorm:"column:alive"`             // 存活检测结果（controller 心跳探测）
	LastHeartbeatAt *time.Time      `gorm:"column:last_heartbeat_at"` // 最近一次心跳时间
}

func (NodeAgent) TableName() string {
	return "node_agents"
}

type NodeAgentStatus int

const (
	Disabled NodeAgentStatus = iota
	Enabled
)
