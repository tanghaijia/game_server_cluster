package biz

import (
	"context"
	"log/slog"
	"time"

	"controller-go/internal/entity"
	"controller-go/internal/repository"
)

// 一键更新状态机取值（与 NodeAgentUpdateOrchestrator 共用同一套字面量）
const (
	StateIdle        = "idle"
	StateDownloading = "downloading"
	StateRebooting   = "rebooting"
	StateUpdated     = "updated"
	StateFailed      = "failed"
)

// NodeAgentUpdateReconciler 一键更新状态机自动对账（自愈，防「卡在重启中」需手工清状态）。
//
// 背景（docs/agent-release-asset-service-redesign.md 事故记录）：更新状态推进（downloading/
// rebooting → updated/failed）依赖编排器的同步轮询；请求被中断（前端断连/controller 重启）
// 或 agent 异常（替换后起不来/失联）时 fail/updated 落库不会执行 → 状态永久卡死，只能手工
// UPDATE。本组件做后台周期对账，规则与编排器一致且幂等：
//
//   - 状态机 in (downloading, rebooting)：
//     · AgentVersion == TargetVersion（目标非空）→ 收敛为 updated（实际已达成，如编排器中断漏落库）；
//     · 距最近一次状态机写入（last_update_at）超过 waitTimeout 仍未达成 → 收敛为 failed；
//     · 其余保持（窗口内等待）。
type NodeAgentUpdateReconciler struct {
	agentRepo   repository.NodeAgentRepository
	waitTimeout time.Duration
}

func NewNodeAgentUpdateReconciler(
	agentRepo repository.NodeAgentRepository,
	waitTimeout time.Duration,
) *NodeAgentUpdateReconciler {
	return &NodeAgentUpdateReconciler{agentRepo: agentRepo, waitTimeout: waitTimeout}
}

// ReconcileOnce 对账一轮（幂等，任意时刻可跑）。
func (r *NodeAgentUpdateReconciler) ReconcileOnce(ctx context.Context) error {
	agents, err := r.agentRepo.ListAll(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, a := range agents {
		newState, reason, keep := resolveStaleUpdateState(a, now, r.waitTimeout)
		if keep {
			continue
		}
		if newState == StateUpdated {
			// 实际已达成目标版本：收敛为 updated，清空失败原因
			if err := r.agentRepo.UpdateUpdateState(ctx, a.ID, StateUpdated, a.TargetVersion, ""); err != nil {
				slog.Error("更新状态对账 updated 落库失败", "agent", a.ID, "err", err)
				continue
			}
			slog.Info("更新状态对账：agent 已达成目标版本，收敛为 updated",
				"agent", a.ID, "version", a.AgentVersion)
		} else {
			if err := r.agentRepo.UpdateUpdateState(ctx, a.ID, StateFailed, a.TargetVersion, reason); err != nil {
				slog.Error("更新状态对账 failed 落库失败", "agent", a.ID, "err", err)
				continue
			}
			slog.Warn("更新状态对账：超时未完成，收敛为 failed",
				"agent", a.ID, "target", a.TargetVersion, "reason", reason)
		}
	}
	return nil
}

// resolveStaleUpdateState 单条对账决策（纯函数，便于单测）。
// 返回 (newState, reason, keep)；keep=true 表示无需动作。
func resolveStaleUpdateState(a *entity.NodeAgent, now time.Time, waitTimeout time.Duration) (string, string, bool) {
	switch a.UpdateState {
	case StateDownloading, StateRebooting:
	default:
		return "", "", true
	}
	// 1) 实际已达成目标版本（编排器中断漏落库/心跳已回归）→ updated
	if a.TargetVersion != "" && a.AgentVersion == a.TargetVersion {
		return StateUpdated, "", false
	}
	// 2) 距最近一次状态机写入超过 waitTimeout 仍未达成 → failed
	if a.LastUpdateAt != nil && now.Sub(*a.LastUpdateAt) > waitTimeout {
		return StateFailed, "更新在超时窗口内未完成（自动对账收敛：agent 可能未重启或已失联）", false
	}
	// 3) 窗口内 → 保持等待
	return "", "", true
}
