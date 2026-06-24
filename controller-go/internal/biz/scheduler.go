package biz

import "controller-go/internal/entity"

type Scheduler interface {
	// Schedule schedules a game instance to a node.
	Schedule(gameInstance *entity.GameInstance) (string, error)
}
