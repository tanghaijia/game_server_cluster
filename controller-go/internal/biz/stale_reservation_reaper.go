package biz

import (
	"context"
	"log/slog"
	"time"

	"controller-go/internal/entity"
	"controller-go/internal/repository"
)

// StaleReservationReaper 中间态卡死哨兵（§7.4）：
// 周期扫描卡死在中间态（PreparingBuild/RestoringSnapshot/Starting）超过阈值的实例，
// 释放其预留资源 + 端口并置 Failed（reason=stale_reservation），防止预留永久占用造成容量浪费。
// 节点失联导致的卡死优先走 Uncertain/fencing（S26），哨兵只处理非失联卡死路径。
type StaleReservationReaper struct {
	instanceRepo    repository.GameInstanceRepository
	nodeAgentRepo   repository.NodeAgentRepository
	reservationRepo repository.ReservationRepository
	eventBus        *SchedulerEventBus
	timeout         time.Duration
}

func NewStaleReservationReaper(
	instanceRepo repository.GameInstanceRepository,
	nodeAgentRepo repository.NodeAgentRepository,
	reservationRepo repository.ReservationRepository,
	eventBus *SchedulerEventBus,
	timeout time.Duration,
) *StaleReservationReaper {
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	return &StaleReservationReaper{
		instanceRepo:    instanceRepo,
		nodeAgentRepo:   nodeAgentRepo,
		reservationRepo: reservationRepo,
		eventBus:        eventBus,
		timeout:         timeout,
	}
}

// Start 启动周期巡检（interval 每轮间隔；启动后立即执行一轮）
func (r *StaleReservationReaper) Start(ctx context.Context, interval time.Duration) {
	slog.Info("StaleReservationReaper 启动", "interval", interval.String(), "timeout", r.timeout.String())
	go func() {
		r.reconcileOnce(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				slog.Info("StaleReservationReaper 退出"); return
			case <-ticker.C:
				r.reconcileOnce(ctx)
			}
		}
	}()
}

func (r *StaleReservationReaper) reconcileOnce(ctx context.Context) {
	instances, err := r.instanceRepo.ListByStatuses(ctx,
		entity.StatusPreparingBuild, entity.StatusRestoringSnapshot, entity.StatusStarting)
	if err != nil {
		slog.Error("StaleReservationReaper 查询中间态实例失败", "err", err)
		return
	}
	now := time.Now()
	for _, inst := range instances {
		if now.Sub(inst.UpdateTime) < r.timeout {
			continue
		}
		// 释放预留（7.4）
		if inst.NodeAgentID != nil && inst.ResourceReq != nil {
			if agent, err := r.nodeAgentRepo.GetByID(ctx, *inst.NodeAgentID); err == nil {
				if err := r.reservationRepo.Release(ctx, agent.NodeId, *inst.ResourceReq); err != nil {
					slog.Error("StaleReservationReaper 释放预留失败",
						"instanceId", inst.ID, "nodeAgentId", *inst.NodeAgentID, "err", err)
				} else if r.eventBus != nil {
					r.eventBus.Publish(SchedulerEvent{Type: EventReservationReleased, OccurredAt: time.Now(),
						InstanceID: inst.ID, NodeAgentID: *inst.NodeAgentID, Detail: "中间态卡死，释放预留"})
				}
			}
		}
		inst.Status = entity.Failed
		inst.FailReason = "中间态卡死，预留超时释放（stale_reservation）"
		if err := r.instanceRepo.Save(ctx, inst); err != nil {
			slog.Error("StaleReservationReaper 置失败状态失败", "instanceId", inst.ID, "err", err)
			continue
		}
		slog.Warn("StaleReservationReaper 中间态卡死，已释放预留并置失败",
			"instanceId", inst.ID, "stuckSince", inst.UpdateTime.String())
	}
}
