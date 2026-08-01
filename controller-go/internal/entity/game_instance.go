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

type GameInstance struct {
	ID              string          `gorm:"column:id;primaryKey"`
	GameID          string          `gorm:"column:game_id"`
	NodeAgentID     *string         `gorm:"column:node_agent_id"`
	Status          InstanceStatus  `gorm:"column:status"`
	LastPendingTime time.Time       `gorm:"column:last_pending_time"`
	CreateTime      time.Time       `gorm:"column:create_time"`
	UpdateTime      time.Time       `gorm:"column:update_time"`
	GameBuildId     string          `gorm:"column:game_build_id"`
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
