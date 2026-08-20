package biz

import (
	"context"
	"fmt"
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
	recentWindowN int    // 观察窗：取最近 N 个采样做判定（升级与恢复共用，防振荡）
	window        time.Duration // 拉取采样的时间范围（历史窗口，3.4）

	mu     sync.Mutex
	counts map[string]int // nodeID -> 连续超阈轮数（升级计数）
	downs  map[string]int // nodeID -> 连续低于阈值轮数（恢复计数，S14）
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
		recentWindowN:   observePeriods, // 观察窗 = 升级所需轮数（对称，防振荡）
		window:          window,
		counts:          make(map[string]int),
		downs:           make(map[string]int),
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

	// 观察窗：最近 N 个采样（升级与恢复共用同一窗口，天然互斥 → 无 Normal↔Critical 振荡）
	recent := lastNSamples(samples, m.recentWindowN)
	util := latestUtilization(recent, n)
	nodeID := fmtID(n.Id)

	m.mu.Lock()
	status, up, down := pressureStep(util, n.PressureStatus, m.counts[nodeID], m.downs[nodeID],
		m.warningPct, m.criticalPct, m.observePeriods, m.recoverPeriods)
	m.counts[nodeID] = up
	m.downs[nodeID] = down
	m.mu.Unlock()

	if status != n.PressureStatus {
		m.transition(ctx, nodeID, status, util)
	}
}

// pressureStep 一步压力状态判定（纯函数，可单测）：
// 升级/恢复共用同一 util（最近 N 个采样峰值）——util 高则只可能升级、低则只可能恢复，
// 不会出现"同一观察窗内升了又降"的振荡。
func pressureStep(
	util float64,
	status entity.NodePressureStatus,
	up, down int,
	warningPct, criticalPct float64,
	observePeriods, recoverPeriods int,
) (entity.NodePressureStatus, int, int) {
	switch {
	case util >= criticalPct:
		up++
		down = 0
		if up >= observePeriods && status != entity.PressureCritical {
			status = entity.PressureCritical
		}
	case util >= warningPct:
		up++
		down = 0
		if up >= observePeriods && status != entity.PressureWarning {
			status = entity.PressureWarning
		}
	default:
		up = 0
		if status != entity.PressureNormal {
			down++
			if down >= recoverPeriods {
				status = entity.PressureNormal
				down = 0
			}
		}
	}
	return status, up, down
}

func (m *PressureMonitor) transition(ctx context.Context, nodeID string, status entity.NodePressureStatus, util float64) {
	if err := m.nodeRepo.UpdatePressureStatus(ctx, nodeID, status); err != nil {
		slog.Error("PressureMonitor 更新压力状态失败", "nodeId", nodeID, "err", err)
		return
	}
	detail := fmt.Sprintf("压力状态 → %s（最近 %d 个采样峰值 %.0f%%；阈值 Warning=%v%% Critical=%v%%）",
		pressureStatusName(status), m.recentWindowN, util, m.warningPct, m.criticalPct)
	slog.Info("PressureMonitor 节点压力状态变化", "nodeId", nodeID, "status", pressureStatusName(status), "util", util)
	if m.eventBus != nil {
		m.eventBus.Publish(SchedulerEvent{Type: EventNodePressureChanged, OccurredAt: time.Now(),
			NodeAgentID: nodeID, Detail: detail})
	}
}

// lastNSamples 取最近 n 个采样（ListSince 时间升序，末尾为最新）
func lastNSamples(samples []*entity.NodeResourceSample, n int) []*entity.NodeResourceSample {
	if len(samples) <= n {
		return samples
	}
	return samples[len(samples)-n:]
}

// latestUtilization 观察窗内 cpu/mem 占用率峰值（0..100）
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
