package entity

/**
* 服务器节点信息
**/
type Node struct {
	Id              int64
	Ip              string
	CoreNum         int
	CoreFrequency   float64
	MemorySize      int64
	StorageSize     int64
	Location        string
	ServiceProvider string
}
