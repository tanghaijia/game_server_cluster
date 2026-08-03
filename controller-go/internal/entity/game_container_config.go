package entity

type GameContainerConfig struct {
	ID                  string                     `gorm:"column:id;primaryKey"`
	ContainerServerPath string                     `gorm:"column:container_server_path"`
	PortMode            GameContainerPortMode      `gorm:"column:port_mode"`
	PortExcerpt         []GameContainerPortExcerpt `gorm:"foreignKey:GameContainerConfigID"`
}

func (GameContainerConfig) TableName() string {
	return "game_container_configs"
}

type GameContainerPortMode int

const (
	PORT_MAPPING_MOD_NAT GameContainerPortMode = iota
	PORT_MAPPING_MOD_HOST
)

/*
 * 游戏服务器所需要的连续端口片段
 */
type GameContainerPortExcerpt struct {
	ID                    uint         `gorm:"column:id;primaryKey"`
	GameContainerConfigID string       `gorm:"column:game_container_config_id;index"`
	Protocol              ProtocolType `gorm:"column:protocol"` // 协议
	BeginPort             uint16       `gorm:"column:begin_port"` // 起始端口
	ExcerptLength         uint16       `gorm:"column:excerpt_length"` // 片段长度
}
