package entity

// SchedulerStat 调度统计持久化行（000023 迁移，S29 指标）：
// 调度尝试计数按 outcome（scheduled/queued/failed）累加，重启后可恢复，不归零。
type SchedulerStat struct {
	Outcome string `gorm:"column:outcome;primaryKey"` // scheduled / queued / failed
	Count   int64  `gorm:"column:count"`
}

func (SchedulerStat) TableName() string {
	return "scheduler_stats"
}
