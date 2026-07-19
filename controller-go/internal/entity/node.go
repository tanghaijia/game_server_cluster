package entity

/**
* 服务器节点信息
**/
type Node struct {
	Id              int64   `gorm:"column:id;primaryKey;autoIncrement"`
	Ip              string  `gorm:"column:ip"`
	CoreNum         int     `gorm:"column:core_num"`
	CoreFrequency   float64 `gorm:"column:core_frequency"`
	MemorySize      int64   `gorm:"column:memory_size"`
	StorageSize     int64   `gorm:"column:storage_size"`
	Location        string  `gorm:"column:location"`
	ServiceProvider string  `gorm:"column:service_provider"`
}

func (Node) TableName() string {
	return "nodes"
}
