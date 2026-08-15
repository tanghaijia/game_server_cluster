package entity

type GameContainerConfig struct {
	ID                  string                     `gorm:"column:id;primaryKey"`
	ContainerServerPath string                     `gorm:"column:container_server_path"`
	PortMode            GameContainerPortMode      `gorm:"column:port_mode"`
	InjectGamePort      bool                       `gorm:"column:inject_game_port"` // 端口注入：游戏端口 = 分配的宿主端口（identity），通过 env 通告给 adapter
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
	IsGamePort            bool         `gorm:"column:is_game_port"` // 游戏主端口（对客户端公开的连接端口）
}
