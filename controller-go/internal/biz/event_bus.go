package biz

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"controller-go/internal/entity"
	"controller-go/internal/repository"
)

// SchedulerEventType 调度事件类型（S30：事件化观测）
type SchedulerEventType string

const (
	EventInstanceScheduled       SchedulerEventType = "instance_scheduled"         // 调度成功
	EventInstanceQueued          SchedulerEventType = "instance_queued"            // 排队（资源不足）
	EventInstanceScheduleFailed  SchedulerEventType = "instance_schedule_failed"   // 调度失败（结构性）
	EventInstanceQueueTimeout    SchedulerEventType = "instance_queue_timeout"     // 排队超时
	EventInstanceQueuedCancelled SchedulerEventType = "instance_queued_cancelled"  // 取消排队
	EventInstanceStopped         SchedulerEventType = "instance_stopped"           // 实例停止（释放资源）
	EventInstanceFailed          SchedulerEventType = "instance_failed"            // 实例失败
	EventNodePressureChanged     SchedulerEventType = "node_pressure_changed"      // 节点压力状态变化
	EventNodeHealthChanged       SchedulerEventType = "node_health_changed"        // 节点健康状态变化
	EventReservationReleased     SchedulerEventType = "reservation_released"       // 预留释放
	EventCacheReady              SchedulerEventType = "cache_ready"                // game-cache 转 AVAILABLE
)

// SchedulerEvent 一条调度事件
type SchedulerEvent struct {
	Type        SchedulerEventType `json:"type"`
	OccurredAt  time.Time          `json:"occurred_at"`
	InstanceID  string             `json:"instance_id,omitempty"`
	NodeAgentID string             `json:"node_agent_id,omitempty"`
	Detail      string             `json:"detail,omitempty"` // 可读说明（原因/得分等）
}

// SchedulerEventBus 调度事件总线（S30）：
// ① 内存环形缓冲（最新 N 条，实时展示）；
// ② 双写持久化（scheduler_events 表）——Publish 非阻塞进 persistCh，后台批量 flush，
//    重启后观测/审计历史仍可回溯。repo 为 nil 时仅内存（测试/兼容）。
type SchedulerEventBus struct {
	mu       sync.RWMutex
	events   []SchedulerEvent
	capacity int

	repo       repository.SchedulerEventRepository
	persistCh  chan SchedulerEvent
	flushEvery time.Duration
	pruneAfter time.Duration // 事件保留时长（默认 7 天，定期清理）
}

func NewSchedulerEventBus(capacity int, repo repository.SchedulerEventRepository) *SchedulerEventBus {
	if capacity <= 0 {
		capacity = 1000
	}
	return &SchedulerEventBus{
		capacity:   capacity,
		repo:       repo,
		persistCh:  make(chan SchedulerEvent, 2048),
		flushEvery: time.Second,
		pruneAfter: 7 * 24 * time.Hour,
	}
}

// Start 启动持久化消费者（批量 flush + 定期清理旧事件）。ctx 取消时 flush 剩余。
func (b *SchedulerEventBus) Start(ctx context.Context) {
	if b.repo == nil {
		return // 无持久化后端，仅内存
	}
	go func() {
		pending := make([]*entity.SchedulerEventRow, 0, 64)
		flush := func() {
			if len(pending) == 0 {
				return
			}
			// 不阻塞：写失败仅记日志（事件可丢，不拖垮调度路径）
			if err := b.repo.AppendBatch(context.Background(), pending); err != nil {
				slog.Error("SchedulerEventBus 持久化事件失败", "count", len(pending), "err", err)
			}
			pending = pending[:0]
		}
		ticker := time.NewTicker(b.flushEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				flush() // 退出前落库剩余
				return
			case e := <-b.persistCh:
				pending = append(pending, &entity.SchedulerEventRow{
					OccurredAt:  e.OccurredAt,
					Type:        string(e.Type),
					InstanceID:  e.InstanceID,
					NodeAgentID: e.NodeAgentID,
					Detail:      e.Detail,
					CreatedAt:   time.Now(),
				})
				if len(pending) >= 64 {
					flush()
				}
			case <-ticker.C:
				flush()
				// 定期清理旧事件（保留策略）
				if _, err := b.repo.PruneBefore(context.Background(), time.Now().Add(-b.pruneAfter)); err != nil {
					slog.Debug("SchedulerEventBus 清理旧事件失败", "err", err)
				}
			}
		}
	}()
}

// Publish 发布事件（非阻塞：内存追加 + 入持久化队列；超出容量丢弃最旧）
func (b *SchedulerEventBus) Publish(e SchedulerEvent) {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now()
	}
	b.mu.Lock()
	b.events = append(b.events, e)
	if len(b.events) > b.capacity {
		b.events = b.events[len(b.events)-b.capacity:]
	}
	b.mu.Unlock()

	if b.repo != nil {
		select {
		case b.persistCh <- e:
		default: // 队列满：丢弃（事件可丢，不阻塞）
		}
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

// History 从 DB 查询持久化事件（时间降序）；typ 非空按类型过滤。repo 为 nil 时回退内存。
func (b *SchedulerEventBus) History(ctx context.Context, since time.Time, typ string, limit int) []SchedulerEvent {
	if b.repo == nil {
		events := b.Recent(limit, SchedulerEventType(typ))
		return events
	}
	rows, err := b.repo.ListSince(ctx, since, typ, limit)
	if err != nil {
		slog.Error("SchedulerEventBus 查询历史事件失败", "err", err)
		return nil
	}
	out := make([]SchedulerEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, SchedulerEvent{
			Type:        SchedulerEventType(r.Type),
			OccurredAt:  r.OccurredAt,
			InstanceID:  r.InstanceID,
			NodeAgentID: r.NodeAgentID,
			Detail:      r.Detail,
		})
	}
	return out
}

// Len 当前事件数（内存缓冲）
func (b *SchedulerEventBus) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.events)
}
