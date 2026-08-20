package biz

import (
	"context"
	"log/slog"
	"time"

	"controller-go/internal/entity"
	"controller-go/internal/repository"
)

// DispatchRequester 排队唤醒器向 dispatcher 提交实例的接口（dispatcher 实现）
type DispatchRequester interface {
	// RequestDispatch 将实例压入 dispatcher 串行消费队列
	RequestDispatch(ctx context.Context, instance *entity.GameInstance) error
}

// QueueWaker 排队唤醒器（§8.3，S14）：
// ① 定时扫描：每 queue_scan_interval 查 ListDue(now)，按 priority 顺序唤醒；
// ② 事件驱动：资源释放/健康恢复等通过 Wake() 立即触发一轮（并重置退避）；
// 防风暴：每轮最多 maxPerRound 个 + 同实例同轮去重；唤醒后调度走 dispatcher 单消费队列串行化。
type QueueWaker struct {
	queueRepo    repository.SchedulingQueueRepository
	instanceRepo repository.GameInstanceRepository
	dispatcher   DispatchRequester
	maxPerRound  int
	wakeCh       chan struct{}
}

func NewQueueWaker(
	queueRepo repository.SchedulingQueueRepository,
	instanceRepo repository.GameInstanceRepository,
	dispatcher DispatchRequester,
	maxPerRound int,
) *QueueWaker {
	if maxPerRound <= 0 {
		maxPerRound = 50
	}
	return &QueueWaker{
		queueRepo:    queueRepo,
		instanceRepo: instanceRepo,
		dispatcher:   dispatcher,
		maxPerRound:  maxPerRound,
		wakeCh:       make(chan struct{}, 1), // 容量 1：合并并发事件
	}
}

// Start 启动唤醒器（定时扫描 + 事件通道）
func (w *QueueWaker) Start(ctx context.Context, scanInterval time.Duration) {
	if scanInterval <= 0 {
		scanInterval = 5 * time.Second
	}
	slog.Info("QueueWaker 启动", "scan_interval", scanInterval.String(), "max_per_round", w.maxPerRound)
	go func() {
		w.wakeOnce(ctx) // 启动立即追平
		ticker := time.NewTicker(scanInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				slog.Info("QueueWaker 退出")
				return
			case <-ticker.C:
				w.wakeOnce(ctx)
			case <-w.wakeCh:
				w.wakeOnce(ctx) // 事件唤醒：立即扫一轮（退避重置由 OnStillQueued 处理）
			}
		}
	}()
}

// Wake 事件触发唤醒（非阻塞；资源释放/健康恢复/cache 就绪时调用）
func (w *QueueWaker) Wake() {
	select {
	case w.wakeCh <- struct{}{}:
	default: // 已有待处理事件，合并
	}
}

func (w *QueueWaker) wakeOnce(ctx context.Context) {
	due, err := w.queueRepo.ListDue(ctx, time.Now())
	if err != nil {
		slog.Error("QueueWaker 查询到期队列失败", "err", err)
		return
	}
	if len(due) == 0 {
		return
	}
	seen := make(map[string]bool, len(due))
	woke := 0
	for _, q := range due {
		if woke >= w.maxPerRound || seen[q.InstanceID] {
			continue
		}
		seen[q.InstanceID] = true

		inst, err := w.instanceRepo.GetByID(ctx, q.InstanceID)
		if err != nil {
			// 实例不存在（已删除）→ 清队
			_ = w.queueRepo.Dequeue(ctx, q.InstanceID)
			continue
		}
		if inst.Status != entity.StatusQueued {
			// 状态不一致（被取消/已调度）→ 清队
			_ = w.queueRepo.Dequeue(ctx, q.InstanceID)
			continue
		}
		woke++
		if err := w.dispatcher.RequestDispatch(ctx, inst); err != nil {
			slog.Warn("QueueWaker 唤醒派发失败", "instanceId", q.InstanceID, "err", err)
		}
	}
	if woke > 0 {
		slog.Info("QueueWaker 唤醒完成", "woke", woke, "due", len(due))
	}
}
