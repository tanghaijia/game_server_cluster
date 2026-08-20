package repository

import (
	"context"
	"time"

	"controller-go/internal/entity"
)

// SchedulerEventRepository 调度事件持久化（000022 表，审计/回溯）
type SchedulerEventRepository interface {
	// AppendBatch 批量写入事件（异步 flush 用）
	AppendBatch(ctx context.Context, events []*entity.SchedulerEventRow) error
	// ListSince 查询自 since 起的事件（时间降序）；typ 非空按类型过滤；limit<=0 不限制
	ListSince(ctx context.Context, since time.Time, typ string, limit int) ([]*entity.SchedulerEventRow, error)
	// PruneBefore 清理早于保留时间的事件（保留策略，默认 7 天）
	PruneBefore(ctx context.Context, before time.Time) (int64, error)
	// Count 统计持久化事件总数（观测看板"事件数"，重启后不归零）
	Count(ctx context.Context) (int64, error)
}
