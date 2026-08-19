package repository

import (
	"context"
	"errors"

	"controller-go/internal/entity"
)

// ErrReservationConflict 预留事务复核失败（并发被抢占/资源不足）：
// 调用方应重试 filter/score（上限 3 次）或转排队（设计 §7.1/§7.3）。
var ErrReservationConflict = errors.New("reservation conflict: node capacity changed")

// ReserveTxRequest 预留事务入参（设计 §7.1）：
// 单事务内完成 锁行复核(H3) → 扣减 reserved → 写端口映射 → 绑定实例(PreparingBuild)。
type ReserveTxRequest struct {
	NodeID         string
	InstanceID     string
	NodeAgentID    string
	Req            entity.ResourceRequest          // 扣减量 = 实例 request
	PortMappings   []entity.ContainerPortMapping   // 端口分配结果（biz 层 MapPort 拆分后的事务外预计算）
	NewStatus      entity.InstanceStatus           // 绑定后状态（StatusPreparingBuild）
	UtilizationTarget float64                      // headroom 计算用（默认 0.8）
}

// ReservationRepository 预留与释放（防超卖，S8/S9/N4）。
type ReservationRepository interface {
	// TryReserve 单事务预留：FOR UPDATE 锁节点行 → 复核 allocatable(H3) → 扣减 reserved →
	// 写入端口映射 → 绑定实例（node_agent_id + status）。复核不通过返回 ErrReservationConflict。
	TryReserve(ctx context.Context, req ReserveTxRequest) error
	// Release 扣回预留（7.2 释放挂点：停止/删除/失败回滚/排队超时/卡死哨兵）。
	Release(ctx context.Context, nodeID string, req entity.ResourceRequest) error
}
