package entity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type InstanceStatus int

const (
	StatusPending InstanceStatus = iota
	StatusScheduling
	StatusPreparingBuild
	StatusRestoringSnapshot
	StatusStarting
	StatusRunning
	StatusStopping
	StatusCleaning
	StatusStopped
	StatusQueued
	Failed
)

func (s InstanceStatus) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusScheduling:
		return "scheduling"
	case StatusPreparingBuild:
		return "preparing_build"
	case StatusRestoringSnapshot:
		return "restoring_snapshot"
	case StatusStarting:
		return "starting"
	case StatusRunning:
		return "running"
	case StatusStopping:
		return "stopping"
	case StatusCleaning:
		return "cleaning"
	case StatusStopped:
		return "stopped"
	case StatusQueued:
		return "queued"
	case Failed:
		return "failed"
	default:
		return "unknown"
	}
}

// MarshalJSON 让 InstanceStatus 在 JSON 中输出可读的字符串（如 "stopped"）而非数字
func (s InstanceStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// ParseInstanceStatus 将状态字符串解析为 InstanceStatus，未知状态返回 false。
// 用于 HTTP 查询参数（如 ?status=running）等外部输入解析。
func ParseInstanceStatus(s string) (InstanceStatus, bool) {
	switch s {
	case "pending":
		return StatusPending, true
	case "scheduling":
		return StatusScheduling, true
	case "preparing_build":
		return StatusPreparingBuild, true
	case "restoring_snapshot":
		return StatusRestoringSnapshot, true
	case "starting":
		return StatusStarting, true
	case "running":
		return StatusRunning, true
	case "stopping":
		return StatusStopping, true
	case "cleaning":
		return StatusCleaning, true
	case "stopped":
		return StatusStopped, true
	case "queued":
		return StatusQueued, true
	case "failed":
		return Failed, true
	default:
		return 0, false
	}
}

// ResourceRequest 实例资源需求（3.1）：request 用于容量判定，limit 不属于调度输入。
// 语义对齐 K8s requests：cpu_milli 为时间片需求（1000m = 独占 1 核）。
type ResourceRequest struct {
	CPUMilli        int64 `json:"cpu_milli"`
	MemoryBytes     int64 `json:"memory_bytes"`
	DiskBytes       int64 `json:"disk_bytes"`
	BandwidthRxMbps int64 `json:"bandwidth_rx_mbps"`
	BandwidthTxMbps int64 `json:"bandwidth_tx_mbps"`
}

type GameInstance struct {
	ID              string         `gorm:"column:id;primaryKey"`
	GameID          string         `gorm:"column:game_id"`
	NodeAgentID     *string        `gorm:"column:node_agent_id"`
	Status          InstanceStatus `gorm:"column:status"`
	LastPendingTime time.Time      `gorm:"column:last_pending_time"`
	CreateTime      time.Time      `gorm:"column:create_time"`
	UpdateTime      time.Time      `gorm:"column:update_time"`
	GameBuildId     string         `gorm:"column:game_build_id"`

	// 调度字段（000014 迁移）
	Region      string           `gorm:"column:region"` // R3：区域偏好；空 = 任意区域（S40）
	Priority    int              `gorm:"column:priority;default:100"` // D7：数值越小越优先
	ResourceReq *ResourceRequest `gorm:"column:resource_request;serializer:json"`
	// 000021 迁移：创建时用户显式指定资源（覆盖 config 默认）；false = 调度写回的快照（仅释放用）
	ResourceOverride bool `gorm:"column:resource_override"`
	// 排队字段（R8，P2）
	QueuedReason string     `gorm:"column:queued_reason"`
	QueuedAt     *time.Time `gorm:"column:queued_at"`
	Cancelled    bool       `gorm:"column:cancelled"` // D10：取消标记

	// 失败原因（000020 迁移）：调度/阶段失败、排队超时、卡死哨兵写入，前端展示
	FailReason string `gorm:"column:fail_reason"`
}

func (GameInstance) TableName() string {
	return "game_instances"
}

/**
* 更新游戏实例的状态
**/
func (g *GameInstance) Advance(ctx context.Context) (status InstanceStatus, err error) {
	switch g.Status {
	case StatusPending:
		g.Status = StatusScheduling
	case StatusScheduling:
		g.Status = StatusPreparingBuild
	case StatusPreparingBuild:
		g.Status = StatusRestoringSnapshot
	case StatusRestoringSnapshot:
		g.Status = StatusStarting
	case StatusStarting:
		g.Status = StatusRunning
	case StatusStopping:
		g.Status = StatusCleaning
	case StatusCleaning:
		g.Status = StatusStopped
	default:
		fmt.Printf("Instance %s is in status %d, cannot advance\n", g.ID, g.Status)
		return g.Status, errors.New("cannot advance from current status")
	}
	return g.Status, nil
}
