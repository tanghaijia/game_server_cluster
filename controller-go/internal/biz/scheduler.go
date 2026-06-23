package biz

type Scheduler interface {
	// Schedule schedules a game instance to a node.
	Schedule(gameInstanceID string) (string, error)
}
