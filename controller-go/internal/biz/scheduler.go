package biz

import (
	"context"
	"encoding/json"

	"controller-go/internal/entity"
)

// ScheduleOutcome 一次调度尝试的出口（§2.3）
type ScheduleOutcome int

const (
	OutcomeScheduled ScheduleOutcome = iota
	OutcomeQueued
	OutcomeFailed
)

// String 可读名称（观测/审计输出用）
func (o ScheduleOutcome) String() string {
	switch o {
	case OutcomeScheduled:
		return "scheduled"
	case OutcomeQueued:
		return "queued"
	case OutcomeFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// MarshalJSON 让调度出口在 JSON 中输出字符串（观测接口契约：scheduled/queued/failed）
func (o ScheduleOutcome) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.String())
}

// ScheduleFailCode 失败原因码（F1 结构化原因）
type ScheduleFailCode int

const (
	FailCodeNone ScheduleFailCode = iota
	FailCodeNoNodeAgent          // 无注册 agent（结构性）
	FailCodeNoGameCache          // 无节点有该 game_build 缓存（结构性, D2）
	FailCodeRegionUnreachable    // 区域强制不可达（结构性, D3）
	FailCodeConfigError          // 配置错误（结构性，含单核 request 声明违规）
	FailCodeResourceShortage     // 资源/端口/压力不足（可恢复 → P2 排队）
	FailCodeQueueTimeout         // 排队超时（最终失败）
	FailCodeStaleReservation     // 中间态卡死，预留超时释放（7.4）
)

// NodeExclusion 单个候选节点的排除明细（F1）
type NodeExclusion struct {
	NodeAgentID string
	Reasons     []string
}

// ScheduleResult 调度结果（结构化，F2 审计）
type ScheduleResult struct {
	Outcome     ScheduleOutcome
	NodeAgentID string             // Outcome=Scheduled 时有效
	ReasonCode  ScheduleFailCode
	Reason      string             // 可读原因
	Excluded    []NodeExclusion    // 全部候选的排除明细
	Score       float64
	Scores      map[string]float64 // 选中节点各维度得分
	ResourceReq entity.ResourceRequest // 本次调度解析的资源需求（释放预留时用，7.2）
}

type Scheduler interface {
	// Schedule 执行一次完整调度尝试（filter → score → 预留事务，§2.2）。
	Schedule(ctx context.Context, instance *entity.GameInstance) (*ScheduleResult, error)
	// CancelQueued 取消排队（D5：移除出队，实例保持 stopped）。仅 queued 状态允许。P2 实现。
	CancelQueued(ctx context.Context, instanceID string) error
	// QueueStats 排队统计（调试/指标用）。P2 实现。
	QueueStats() map[string]any
}
