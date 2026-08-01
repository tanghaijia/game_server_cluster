package entity

type GameContainerConfig struct {
	ID                  string                     `gorm:"column:id;primaryKey"`
	ContainerServerPath string                     `gorm:"column:container_server_path"`
	PortMode            GameContainerPortMode      `gorm:"column:port_mode"`
	PortMapping         []GameContainerPortMapping `gorm:"foreignKey:GameContainerConfigID"`
}

func (GameContainerConfig) TableName() string {
	return "game_container_configs"
}

type GameContainerPortMapping struct {
	ID                    uint         `gorm:"column:id;primaryKey"`
	GameContainerConfigID string       `gorm:"column:game_container_config_id;index"`
	HostPort              uint16       `gorm:"column:host_port"`
	ContainerPort         uint16       `gorm:"column:container_port"`
	Protocol              ProtocolType `gorm:"column:protocol"`
}

func (GameContainerPortMapping) TableName() string {
	return "game_container_port_mappings"
}

type ProtocolType int

const (
	TCP ProtocolType = iota
	UDP
)

type GameContainerPortMode int

const (
	PORT_MAPPING_MOD_NAT GameContainerPortMode = iota
	PORT_MAPPING_MOD_HOST
)
