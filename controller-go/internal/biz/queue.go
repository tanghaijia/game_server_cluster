package biz

import (
	"context"
	"time"

	"controller-go/internal/entity"
	"controller-go/internal/repository"
)

// QueueManager 排队管理（§8.1/§8.2，D4/D9）：
// 封装队列行写入、退避计算、超时判定、取消出队。
type QueueManager struct {
	queueRepo   repository.SchedulingQueueRepository
	backoffBase time.Duration // 退避起点（默认 15s）
	backoffMax  time.Duration // 退避上限（默认 5m）
	timeout     time.Duration // 排队超时（默认 30m）
}

func NewQueueManager(
	queueRepo repository.SchedulingQueueRepository,
	backoffBase time.Duration,
	backoffMax time.Duration,
	timeout time.Duration,
) *QueueManager {
	if backoffBase <= 0 {
		backoffBase = 15 * time.Second
	}
	if backoffMax <= 0 {
		backoffMax = 5 * time.Minute
	}
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	return &QueueManager{queueRepo: queueRepo, backoffBase: backoffBase, backoffMax: backoffMax, timeout: timeout}
}

// Enqueue 首次入队（调度失败且可恢复时调用）。
// attempts=0，wake_at = now + backoffBase（首轮退避）。
func (qm *QueueManager) Enqueue(ctx context.Context, instance *entity.GameInstance, reason string) error {
	now := time.Now()
	priority := instance.Priority
	if priority <= 0 {
		priority = 100
	}
	q := &entity.SchedulingQueue{
		InstanceID: instance.ID,
		Priority:   priority,
		Reason:     reason,
		Attempts:   0,
		WakeAt:     now.Add(qm.backoffBase),
		QueuedAt:   now,
		TimeoutAt:  now.Add(qm.timeout),
	}
	return qm.queueRepo.Enqueue(ctx, q)
}

// OnStillQueued 重试仍资源不足：更新退避（attempts+1）。
// 事件触发（eventWake=true）时重置退避为初始值（S14）。返回是否已超时。
func (qm *QueueManager) OnStillQueued(ctx context.Context, instanceID string, eventWake bool) (bool, error) {
	q, err := qm.queueRepo.Get(ctx, instanceID)
	if err != nil {
		// 队列行丢失（可能被取消/删除）→ 视为超时终止，由调用方处置
		return true, nil
	}
	if time.Now().After(q.TimeoutAt) {
		return true, nil
	}
	attempts := q.Attempts + 1
	if eventWake {
		attempts = 1 // 事件触发重置退避
	}
	wakeAt := time.Now().Add(qm.backoff(attempts))
	return false, qm.queueRepo.UpdateWake(ctx, instanceID, wakeAt, attempts)
}

// Cancel 取消排队：移除队列行（D5；状态更新由调用方完成）
func (qm *QueueManager) Cancel(ctx context.Context, instanceID string) error {
	return qm.queueRepo.Dequeue(ctx, instanceID)
}

// Count 排队总数（QueueStats 用）
func (qm *QueueManager) Count(ctx context.Context) (int64, error) {
	return qm.queueRepo.Count(ctx)
}

// Get 查询排队记录（判断是否已在队列：存在=重试仍不足，不存在=首次入队）
func (qm *QueueManager) Get(ctx context.Context, instanceID string) (*entity.SchedulingQueue, error) {
	return qm.queueRepo.Get(ctx, instanceID)
}

// TimeoutAt 排队超时截止时间（入队时用）
func (qm *QueueManager) TimeoutAt(now time.Time) time.Time {
	return now.Add(qm.timeout)
}

// backoff 退避序列：min(base × 2^(n-1), max)，n>=1（D9）
func (qm *QueueManager) backoff(n int) time.Duration {
	if n <= 1 {
		return qm.backoffBase
	}
	d := qm.backoffBase
	for i := 1; i < n && d < qm.backoffMax; i++ {
		d *= 2
		if d >= qm.backoffMax {
			return qm.backoffMax
		}
	}
	if d > qm.backoffMax {
		return qm.backoffMax
	}
	return d
}
