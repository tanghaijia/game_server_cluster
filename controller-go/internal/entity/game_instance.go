package entity

import (
	"context"
	"errors"
	"fmt"
)

type InstanceStatus int

const (
	StatusPending InstanceStatus = iota
	StatusScheduling
	StatusPreparingBuild
	StatusRestoringSnapshot
	StatusRunning
	StatusStopping
	StatusStopped
	Failed
)

type GameInstance struct {
	ID              string
	Game            *Game
	NodeAgent       *NodeAgent
	Status          InstanceStatus
	LastPendingTime int64
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
		g.Status = StatusRunning
	case StatusStopping:
		g.Status = StatusStopped
	default:
		fmt.Printf("Instance %s is in status %d, cannot advance\n", g.ID, g.Status)
		return g.Status, errors.New("cannot advance from current status")
	}
	return g.Status, nil
}
