package entity

type GameContainerConfig struct {
	ID                  string                     `gorm:"column:id;primaryKey"`
	ContainerServerPath string                     `gorm:"column:container_server_path"`
	PortMode            GameContainerPortMode      `gorm:"column:port_mode"`
	InjectGamePort      bool                       `gorm:"column:inject_game_port"` // 端口注入：游戏端口 = 分配的宿主端口（identity），通过 env 通告给 adapter
	PortExcerpt         []GameContainerPortExcerpt `gorm:"foreignKey:GameContainerConfigID"`

	// 资源默认值（3.1 来源优先级第二层，D8，000017 迁移）；实例创建时可覆盖
	CPURequestMilli    int64 `gorm:"column:cpu_request_milli"`
	MemoryRequestBytes int64 `gorm:"column:memory_request_bytes"`
	DiskRequestBytes   int64 `gorm:"column:disk_request_bytes"`
	BandwidthRxMbps    int64 `gorm:"column:bandwidth_rx_mbps"`
	BandwidthTxMbps    int64 `gorm:"column:bandwidth_tx_mbps"`
	// 单核应用声明（3.1 声明规范）：调度校验整核（≥1000m 且 %1000==0），启用单核主频评分
	SingleThreaded bool `gorm:"column:single_threaded"`
	// 端口注入 env 变量名（000024 迁移）：adapter.toml port_inject.env 的部署侧配置，
	// 默认 GAME_HOST_PORT；controller 组装实例 env 时读取，消灭 SDTD_SERVER_PORT 类硬编码
	PortInjectEnv string `gorm:"column:port_inject_env;default:GAME_HOST_PORT"`
	// B-04/P1-3：运行时探针（health/players 采集）
	//   probe_mode: "script" | "a2s" | "none"（缺省 script，向后兼容）
	//   query_port_offset: a2s 模式查询端口相对游戏宿主端口的偏移（Valheim=1，多数=0）
	ProbeMode       string `gorm:"column:probe_mode;default:script"`
	QueryPortOffset int    `gorm:"column:query_port_offset;default:0"`
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
