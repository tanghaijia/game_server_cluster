package entity

import "time"

type NodeAgent struct {
	ID              string                `gorm:"column:id;primaryKey"`
	NodeId          string                `gorm:"column:node_id"`
	Port            int32                 `gorm:"column:port"`
	Status          NodeAgentStatus       `gorm:"column:status"`
	Alive           bool                  `gorm:"column:alive"`             // 存活检测结果（controller 心跳探测，兼容/调试用）
	LastHeartbeatAt *time.Time            `gorm:"column:last_heartbeat_at"` // 最近一次心跳时间
	HealthStatus    NodeAgentHealthStatus `gorm:"column:health_status"`     // 9.1 健康状态（调度 H2 判定依据）

	// 000032：一键更新（docs/node-agent-upgrade-design.md §3.3）
	AgentVersion  string     `gorm:"column:agent_version"`   // 心跳上报的当前版本
	UpdateState   string     `gorm:"column:update_state"`    // idle/downloading/verifying/rebooting/updated/failed
	TargetVersion string     `gorm:"column:target_version"`  // 目标发布版本（更新中）
	LastUpdateAt  *time.Time `gorm:"column:last_update_at"`  // 最近一次更新完成/失败时间
	LastUpdateErr string     `gorm:"column:last_update_err"` // 最近失败原因
}

func (NodeAgent) TableName() string {
	return "node_agents"
}

type NodeAgentStatus int

const (
	Disabled NodeAgentStatus = iota
	Enabled
)

// NodeAgentHealthStatus 健康状态（9.1，000016 迁移）：
// unknown 未探测（首次探测前不可调度）；healthy/degraded 可调度（degraded 评分惩罚）；
// unhealthy 排除（连通性失败/心跳过期/自检严重超标）。
type NodeAgentHealthStatus int

const (
	HealthUnknown NodeAgentHealthStatus = iota
	HealthHealthy
	HealthDegraded
	HealthUnhealthy
)
