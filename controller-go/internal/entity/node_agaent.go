package entity

type NodeAgent struct {
	ID     string          `gorm:"column:id;primaryKey"`
	NodeId string          `gorm:"column:node_id"`
	Port   int32           `gorm:"column:port"`
	Status NodeAgentStatus `gorm:"column:status"`
}

func (NodeAgent) TableName() string {
	return "node_agents"
}

type NodeAgentStatus int

const (
	Disabled NodeAgentStatus = iota
	Enabled
)
