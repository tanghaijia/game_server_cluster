package biz

import (
	"context"
	"testing"
	"time"

	"controller-go/internal/entity"
)

func ts(d time.Duration) *time.Time {
	t := time.Now().Add(-d)
	return &t
}

// 对账决策纯函数分支
func TestResolveStaleUpdateState(t *testing.T) {
	wait := 120 * time.Second
	now := time.Now()

	// 非进行中状态 → keep
	if _, _, keep := resolveStaleUpdateState(&entity.NodeAgent{UpdateState: StateIdle}, now, wait); !keep {
		t.Error("idle 应 keep")
	}
	if _, _, keep := resolveStaleUpdateState(&entity.NodeAgent{UpdateState: StateFailed}, now, wait); !keep {
		t.Error("failed 应 keep")
	}
	// downloading 且心跳已回归目标版本 → updated（编排器中断漏落库场景）
	s, _, keep := resolveStaleUpdateState(&entity.NodeAgent{
		UpdateState: StateDownloading, AgentVersion: "v0.1.3", TargetVersion: "v0.1.3",
		LastUpdateAt: ts(10 * time.Minute),
	}, now, wait)
	if keep || s != StateUpdated {
		t.Errorf("已达成目标应收敛 updated, got keep=%v state=%q", keep, s)
	}
	// rebooting 超过 waitTimeout 未达成 → failed
	s, reason, keep := resolveStaleUpdateState(&entity.NodeAgent{
		UpdateState: StateRebooting, AgentVersion: "v0.1.2", TargetVersion: "v0.1.3",
		LastUpdateAt: ts(wait + time.Minute),
	}, now, wait)
	if keep || s != StateFailed || reason == "" {
		t.Errorf("超时应收敛 failed, got keep=%v state=%q", keep, s)
	}
	// rebooting 在窗口内 → keep（agent 正在重启，不误杀）
	if _, _, keep := resolveStaleUpdateState(&entity.NodeAgent{
		UpdateState: StateRebooting, AgentVersion: "v0.1.2", TargetVersion: "v0.1.3",
		LastUpdateAt: ts(30 * time.Second),
	}, now, wait); !keep {
		t.Error("窗口内 rebooting 应 keep（不误杀正常重启）")
	}
}

// 整轮对账：超时残留 → failed；已达成 → updated
func TestReconcileOnceConverges(t *testing.T) {
	repo := newFakeUpdateAgentRepo(
		&entity.NodeAgent{ID: "a-timeout", NodeId: "n1", UpdateState: StateRebooting,
			AgentVersion: "v0.1.2", TargetVersion: "v0.1.3", LastUpdateAt: ts(5 * time.Minute)},
		&entity.NodeAgent{ID: "a-reached", NodeId: "n2", UpdateState: StateDownloading,
			AgentVersion: "v0.1.3", TargetVersion: "v0.1.3", LastUpdateAt: ts(1 * time.Minute)},
		&entity.NodeAgent{ID: "a-inflight", NodeId: "n3", UpdateState: StateRebooting,
			AgentVersion: "v0.1.1", TargetVersion: "v0.1.3", LastUpdateAt: ts(10 * time.Second)},
		&entity.NodeAgent{ID: "a-idle", NodeId: "n4", UpdateState: StateIdle},
	)
	r := NewNodeAgentUpdateReconciler(repo, 120*time.Second)
	if err := r.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := repo.agents["a-timeout"].UpdateState; got != StateFailed {
		t.Errorf("a-timeout: want failed, got %q", got)
	}
	if got := repo.agents["a-reached"].UpdateState; got != StateUpdated {
		t.Errorf("a-reached: want updated, got %q", got)
	}
	if got := repo.agents["a-reached"].LastUpdateErr; got != "" {
		t.Errorf("a-reached: 收敛 updated 应清空错误, got %q", got)
	}
	if got := repo.agents["a-inflight"].UpdateState; got != StateRebooting {
		t.Errorf("a-inflight: 窗口内应保持 rebooting, got %q", got)
	}
	if got := repo.agents["a-idle"].UpdateState; got != StateIdle {
		t.Errorf("a-idle: 应保持 idle, got %q", got)
	}
}
