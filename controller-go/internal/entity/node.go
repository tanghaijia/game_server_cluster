package entity

import "time"

/**
* 服务器节点信息
*
* 容量语义（3.2）：core_num/memory_size/storage_size = 平台可分配上限（非物理总量），
* 系统开销由运维配置时扣除；memory_size/storage_size 单位为 MB，内部统一换算 bytes。
**/
type Node struct {
	Id              int64   `gorm:"column:id;primaryKey;autoIncrement"`
	Ip              string  `gorm:"column:ip"`
	CoreNum         int     `gorm:"column:core_num"`
	CoreFrequency   float64 `gorm:"column:core_frequency"` // GHz；不参与容量计算，仅评分参考（单核主频偏好）
	MemorySize      int64   `gorm:"column:memory_size"`    // MB
	StorageSize     int64   `gorm:"column:storage_size"`   // MB
	Location        string  `gorm:"column:location"`
	ServiceProvider string  `gorm:"column:service_provider"`

	// 动态资源（node_agent 心跳上报，000012 迁移）
	CPUUsedMilli    int64      `gorm:"column:cpu_used_milli"`
	MemoryUsedBytes int64      `gorm:"column:memory_used_bytes"`
	DiskUsedBytes   int64      `gorm:"column:disk_used_bytes"`
	NetRxBps        int64      `gorm:"column:net_rx_bps"`
	NetTxBps        int64      `gorm:"column:net_tx_bps"`
	UsageReportedAt *time.Time `gorm:"column:usage_reported_at"`

	// 预留（调度事务维护，S8；实时占用不参与 allocatable，防双重扣减 S4）
	CPUReservedMilli    int64 `gorm:"column:cpu_reserved_milli"`
	MemoryReservedBytes int64 `gorm:"column:memory_reserved_bytes"`
	DiskReservedBytes   int64 `gorm:"column:disk_reserved_bytes"`

	// 压力状态（3.3，落库供重启恢复）
	PressureStatus NodePressureStatus `gorm:"column:pressure_status"`

	// 带宽（§3.5，D6 软约束；000018 迁移）
	NetRxLimitMbps          int `gorm:"column:net_rx_limit_mbps"` // 平台可分配带宽上限（Mbps）
	NetTxLimitMbps          int `gorm:"column:net_tx_limit_mbps"`
	BandwidthRxReservedMbps int `gorm:"column:bandwidth_rx_reserved_mbps"` // 已预留（调度事务维护）
	BandwidthTxReservedMbps int `gorm:"column:bandwidth_tx_reserved_mbps"`
}

func (Node) TableName() string {
	return "nodes"
}

type NodePressureStatus int

const (
	PressureNormal NodePressureStatus = iota
	PressureWarning
	PressureCritical
)

// NodeDynamicUsage node_agent 心跳上报的动态资源（ResourceSampler 写入 nodes 动态列）
type NodeDynamicUsage struct {
	CPUUsedMilli    int64
	MemoryUsedBytes int64
	DiskUsedBytes   int64
	NetRxBps        int64
	NetTxBps        int64
}

// NodeResourceSample 节点资源采样（000013 表，历史视图数据源）
type NodeResourceSample struct {
	ID              int64     `gorm:"column:id;primaryKey"`
	NodeID          string    `gorm:"column:node_id"`
	SampledAt       time.Time `gorm:"column:sampled_at"`
	CPUUsedMilli    int64     `gorm:"column:cpu_used_milli"`
	MemoryUsedBytes int64     `gorm:"column:memory_used_bytes"`
	DiskUsedBytes   int64     `gorm:"column:disk_used_bytes"`
	NetRxBps        int64     `gorm:"column:net_rx_bps"`
	NetTxBps        int64     `gorm:"column:net_tx_bps"`
}

func (NodeResourceSample) TableName() string {
	return "node_resource_samples"
}
