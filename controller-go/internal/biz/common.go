package biz

const (
	OPERATION_POLLING_MINITE = 3
	// 单实例连续自动重试上限:超过后标记 Failed,避免对同一实例无限重试
	OPERATION_RETRY_MAX = 3
	TCP_PORT_BEGIN      = 50100
	TCP_PORT_END        = 52000
	UDP_PORT_BEGIN      = 50100
	UDP_PORT_END        = 52000
)
