package repository

import "context"

// SchedulerStatRepository 调度统计持久化（000023 表，S29 指标）：
// 调度尝试计数（scheduled/queued/failed）落库，重启后 Stats 可恢复，不归零。
type SchedulerStatRepository interface {
	// Incr 原子累加某 outcome 的计数（不存在则初始化为 1）
	Incr(ctx context.Context, outcome string) error
	// All 返回全部 outcome → 计数
	All(ctx context.Context) (map[string]int64, error)
	// Sum 返回全部计数总和
	Sum(ctx context.Context) (int64, error)
}
