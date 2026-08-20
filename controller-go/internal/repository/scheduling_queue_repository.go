package repository

import (
	"context"
	"time"

	"controller-go/internal/entity"
)

// SchedulingQueueRepository 排队队列数据层（§8）。
type SchedulingQueueRepository interface {
	// Enqueue 写入排队记录（首次入队；主键冲突应视为幂等失败由调用方处理）
	Enqueue(ctx context.Context, q *entity.SchedulingQueue) error
	// Dequeue 移除排队记录（唤醒成功 / 取消 / 删除 / 超时）
	Dequeue(ctx context.Context, instanceID string) error
	// Get 查询排队记录
	Get(ctx context.Context, instanceID string) (*entity.SchedulingQueue, error)
	// UpdateWake 更新退避后的唤醒时间与重试次数
	UpdateWake(ctx context.Context, instanceID string, wakeAt time.Time, attempts int) error
	// ListDue 查询已到期（wake_at <= now）的排队记录，按 priority, queued_at 排序
	ListDue(ctx context.Context, now time.Time) ([]*entity.SchedulingQueue, error)
	// ListAll 查询全部排队记录
	ListAll(ctx context.Context) ([]*entity.SchedulingQueue, error)
	// Count 排队总数
	Count(ctx context.Context) (int64, error)
}
