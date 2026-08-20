package biz

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"controller-go/internal/entity"
	"controller-go/internal/repository"
)

// PressureMonitor 节点压力状态机（§3.3，② 实际占用兜底）：
// 实际占用（cpu/mem 取较大者）持续超阈 → Warning → Critical → 停止新调度（H6）；
// 低于阈值连续 recoverPeriods 轮 → 恢复 Normal。
// "持续 N 周期"是防抖核心：CPU 瞬时尖峰不触发迁移，波动被观测窗口吸收。
type PressureMonitor struct {
	nodeRepo   repository.NodeRepository
	sampleRepo repository.NodeResourceSampleRepository
	eventBus   *SchedulerEventBus

	warningPct    float64
	criticalPct   float64
	observePeriods int
	recoverPeriods int
	window        time.Duration // 采样观测窗口（history window，3.4）

	mu     sync.Mutex
	counts map[string]int // nodeID -> 连续超阈轮数
}

func NewPressureMonitor(
	nodeRepo repository.NodeRepository,
	sampleRepo repository.NodeResourceSampleRepository,
	eventBus *SchedulerEventBus,
	warningPct float64,
	criticalPct float64,
	observePeriods int,
	recoverPeriods int,
	window time.Duration,
) *PressureMonitor {
	if warningPct <= 0 || warningPct > 100 {
		warningPct = 85
	}
	if criticalPct <= warningPct || criticalPct > 100 {
		criticalPct = 95
	}
	if observePeriods <= 0 {
		observePeriods = 3
	}
	if recoverPeriods <= 0 {
		recoverPeriods = 3
	}
	if window <= 0 {
		window = 15 * time.Minute
	}
	return &PressureMonitor{
		nodeRepo:        nodeRepo,
		sampleRepo:      sampleRepo,
		eventBus:        eventBus,
		warningPct:      warningPct,
		criticalPct:     criticalPct,
		observePeriods:  observePeriods,
		recoverPeriods:  recoverPeriods,
		window:          window,
		counts:          make(map[string]int),
	}
}

// Start 启动周期巡检（interval 每轮间隔；启动后立即执行一轮）
func (m *PressureMonitor) Start(ctx context.Context, interval time.Duration) {
	slog.Info("PressureMonitor 启动", "interval", interval.String())
	go func() {
		m.reconcileOnce(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				slog.Info("PressureMonitor 退出"); return
			case <-ticker.C:
				m.reconcileOnce(ctx)
			}
		}
	}()
}

func (m *PressureMonitor) reconcileOnce(ctx context.Context) {
	nodes, err := m.nodeRepo.ListAll(ctx)
	if err != nil {
		slog.Error("PressureMonitor 查询节点失败", "err", err)
		return
	}
	since := time.Now().Add(-m.window)
	for _, n := range nodes {
		m.evaluateNode(ctx, n, since)
	}
}

func (m *PressureMonitor) evaluateNode(ctx context.Context, n *entity.Node, since time.Time) {
	samples, err := m.sampleRepo.ListSince(ctx, fmtID(n.Id), since)
	if err != nil {
		slog.Warn("PressureMonitor 查询采样失败", "nodeId", n.Id, "err", err)
		return
	}
	if len(samples) == 0 {
		return // 无采样（节点未上报），保持现状
	}

	// 最近窗口占用率（cpu/mem 取较大者）
	util := latestUtilization(samples, n)
	nodeID := fmtID(n.Id)

	m.mu.Lock()
	defer m.mu.Unlock()

	count := m.counts[nodeID]
	status := n.PressureStatus

	switch {
	case util >= m.criticalPct:
		count++
		if count >= m.observePeriods && status != entity.PressureCritical {
			m.transition(ctx, nodeID, entity.PressureCritical)
		}
	case util >= m.warningPct:
		count++
		if count >= m.observePeriods && status != entity.PressureWarning {
			m.transition(ctx, nodeID, entity.PressureWarning)
		}
	default:
		if count > 0 {
			count--
		}
		if count == 0 && status != entity.PressureNormal {
			m.transition(ctx, nodeID, entity.PressureNormal)
		}
	}
	if count <= 0 {
		count = 0
	}
	m.counts[nodeID] = count
}

func (m *PressureMonitor) transition(ctx context.Context, nodeID string, status entity.NodePressureStatus) {
	if err := m.nodeRepo.UpdatePressureStatus(ctx, nodeID, status); err != nil {
		slog.Error("PressureMonitor 更新压力状态失败", "nodeId", nodeID, "err", err)
		return
	}
	slog.Info("PressureMonitor 节点压力状态变化", "nodeId", nodeID, "status", pressureStatusName(status))
	if m.eventBus != nil {
		m.eventBus.Publish(SchedulerEvent{Type: EventNodePressureChanged, OccurredAt: time.Now(),
			NodeAgentID: nodeID, Detail: "压力状态 → " + pressureStatusName(status)})
	}
}

// latestUtilization 窗口内最近采样的 cpu/mem 占用率较大者（0..1 比例）
func latestUtilization(samples []*entity.NodeResourceSample, node *entity.Node) float64 {
	if len(samples) == 0 {
		return 0
	}
	cpuCap := float64(cpuCapacityMilli(node))
	memCap := float64(memoryCapacityBytes(node))
	var cpuMax, memMax float64
	for _, s := range samples {
		if cpuCap > 0 {
			pct := float64(s.CPUUsedMilli) / cpuCap
			if pct > cpuMax {
				cpuMax = pct
			}
		}
		if memCap > 0 {
			pct := float64(s.MemoryUsedBytes) / memCap
			if pct > memMax {
				memMax = pct
			}
		}
	}
	if cpuMax > memMax {
		return cpuMax * 100
	}
	return memMax * 100
}
