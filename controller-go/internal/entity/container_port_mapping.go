package entity

type ContainerPortMapping struct {
	ID            string       `gorm:"column:id;primaryKey"`
	InstanceId    string       `gorm:"column:instance_id;index"`
	NodeAgentId   string       `gorm:"column:node_agent_id;index"`
	HostPort      uint16       `gorm:"column:host_port"`
	ContainerPort uint16       `gorm:"column:container_port"`
	Protocol      ProtocolType `gorm:"column:protocol"`
	IsGamePort    bool         `gorm:"column:is_game_port"` // 游戏主端口对应的宿主端口（connect 地址/端口注入用）
}

func (ContainerPortMapping) TableName() string {
	return "game_container_port_mappings"
}

type ProtocolType int

const (
	TCP ProtocolType = iota
	UDP
)
