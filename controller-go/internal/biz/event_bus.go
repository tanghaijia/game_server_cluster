package biz

import (
	"sync"
	"time"
)

// SchedulerEventType 调度事件类型（S30：事件化观测）
type SchedulerEventType string

const (
	EventInstanceScheduled      SchedulerEventType = "instance_scheduled"        // 调度成功
	EventInstanceQueued         SchedulerEventType = "instance_queued"           // 排队（资源不足）
	EventInstanceScheduleFailed SchedulerEventType = "instance_schedule_failed"  // 调度失败（结构性）
	EventInstanceQueueTimeout   SchedulerEventType = "instance_queue_timeout"    // 排队超时
	EventInstanceQueuedCancelled SchedulerEventType = "instance_queued_cancelled" // 取消排队
	EventInstanceStopped        SchedulerEventType = "instance_stopped"          // 实例停止（释放资源）
	EventInstanceFailed         SchedulerEventType = "instance_failed"           // 实例失败
	EventNodePressureChanged    SchedulerEventType = "node_pressure_changed"     // 节点压力状态变化
	EventNodeHealthChanged      SchedulerEventType = "node_health_changed"       // 节点健康状态变化
	EventReservationReleased    SchedulerEventType = "reservation_released"      // 预留释放
	EventCacheReady             SchedulerEventType = "cache_ready"               // game-cache 转 AVAILABLE
)

// SchedulerEvent 一条调度事件
type SchedulerEvent struct {
	Type        SchedulerEventType `json:"type"`
	OccurredAt  time.Time          `json:"occurred_at"`
	InstanceID  string             `json:"instance_id,omitempty"`
	NodeAgentID string             `json:"node_agent_id,omitempty"`
	Detail      string             `json:"detail,omitempty"` // 可读说明（原因/得分等）
}

// SchedulerEventBus 调度事件总线（进程内环形缓冲）：
// 调度器/队列/压力/健康等发布事件，观测接口与管理员界面消费（S30/F3）。
// 事件写内存（最新 N 条），不阻塞调度主路径。
type SchedulerEventBus struct {
	mu       sync.RWMutex
	events   []SchedulerEvent
	capacity int
}

func NewSchedulerEventBus(capacity int) *SchedulerEventBus {
	if capacity <= 0 {
		capacity = 1000
	}
	return &SchedulerEventBus{capacity: capacity}
}

// Publish 发布事件（非阻塞；超出容量丢弃最旧）
func (b *SchedulerEventBus) Publish(e SchedulerEvent) {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
	if len(b.events) > b.capacity {
		b.events = b.events[len(b.events)-b.capacity:]
	}
}

// Recent 返回最近事件（最新在前）；type 为空返回全部；limit<=0 用容量
func (b *SchedulerEventBus) Recent(limit int, typ SchedulerEventType) []SchedulerEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]SchedulerEvent, 0, len(b.events))
	for i := len(b.events) - 1; i >= 0; i-- {
		e := b.events[i]
		if typ != "" && e.Type != typ {
			continue
		}
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// Len 当前事件数
func (b *SchedulerEventBus) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.events)
}
