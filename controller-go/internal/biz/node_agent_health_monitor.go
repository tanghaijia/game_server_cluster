package biz

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"controller-go/internal/client/nodeagent"
	nodeagentv1 "controller-go/internal/third/nodeagent/v1"
	"controller-go/internal/entity"
	"controller-go/internal/repository"

	"google.golang.org/grpc"
)

// NodeAgentHealthMonitor 周期探测 node_agent 存活与健康状态（§9）：
// 1) 对每个 Enabled node_agent 调用 GetHeartbeat（gRPC，带超时）；
// 2) 成功 → 标记存活、按自检指标（cpu/mem/disk pct）计算健康状态（healthy/degraded）、
//    写入动态资源与采样（3.4）；失败 → 标记失联（unhealthy，防抖阈值）；
// 3) 同步通知 Scheduler 过滤失联节点（调度不再选择）。
type NodeAgentHealthMonitor struct {
	nodeAgentRepo    repository.NodeAgentRepository
	nodeRepo         repository.NodeRepository
	sampleRepo       repository.NodeResourceSampleRepository
	nodeAgentClients *nodeagent.ClientRegistry
	scheduler        *ResourceAwareScheduler
	eventBus         *SchedulerEventBus
	probeTimeout     time.Duration
	failThreshold    int
	degradedPct      float64 // 自检指标达此值 → degraded（9.2）
	failCounts       sync.Map // agentID -> 连续失败次数（防启动/重连瞬态误报）
	// B-04/P1-1：实例运行时统计（健康 + 在线人数）缓存，随心跳刷新
	runtimeStats *RuntimeStatsRegistry
}

func NewNodeAgentHealthMonitor(
	nodeAgentRepo repository.NodeAgentRepository,
	nodeRepo repository.NodeRepository,
	sampleRepo repository.NodeResourceSampleRepository,
	nodeAgentClients *nodeagent.ClientRegistry,
	scheduler *ResourceAwareScheduler,
	eventBus *SchedulerEventBus,
	probeTimeout time.Duration,
	failThreshold int,
	degradedPct float64,
) *NodeAgentHealthMonitor {
	if failThreshold <= 0 {
		failThreshold = 2
	}
	if degradedPct <= 0 || degradedPct > 100 {
		degradedPct = 85
	}
	return &NodeAgentHealthMonitor{
		nodeAgentRepo:    nodeAgentRepo,
		nodeRepo:         nodeRepo,
		sampleRepo:       sampleRepo,
		nodeAgentClients: nodeAgentClients,
		scheduler:        scheduler,
		eventBus:         eventBus,
		probeTimeout:     probeTimeout,
		failThreshold:    failThreshold,
		degradedPct:      degradedPct,
	}
}

// SetRuntimeStatsRegistry 附加实例运行时统计缓存（B-04/P1-1；nil = 不采集）
func (m *NodeAgentHealthMonitor) SetRuntimeStatsRegistry(reg *RuntimeStatsRegistry) {
	m.runtimeStats = reg
}

// Start 启动周期探测（interval 每轮间隔；启动后立即执行一轮）
func (m *NodeAgentHealthMonitor) Start(ctx context.Context, interval time.Duration) {
	slog.Info("NodeAgentHealthMonitor 启动", "interval", interval.String())
	go func() {
		m.probeOnce(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				slog.Info("NodeAgentHealthMonitor 退出"); return
			case <-ticker.C:
				m.probeOnce(ctx)
			}
		}
	}()
}

func (m *NodeAgentHealthMonitor) probeOnce(ctx context.Context) {
	agentIDs, err := m.nodeAgentRepo.ListEnabledIDs(ctx)
	if err != nil {
		slog.Error("NodeAgentHealthMonitor 查询节点失败", "err", err); return
	}
	for _, agentID := range agentIDs {
		m.probeAgent(ctx, agentID)
	}
}

func (m *NodeAgentHealthMonitor) probeAgent(ctx context.Context, agentID string) {
	agent, err := m.nodeAgentRepo.GetByID(ctx, agentID)
	if err != nil {
		slog.Warn("NodeAgentHealthMonitor 加载 agent 失败", "agent", agentID, "err", err); return
	}
	node, err := m.nodeRepo.GetByID(agent.NodeId)
	if err != nil {
		slog.Warn("NodeAgentHealthMonitor 加载 node 失败", "agent", agentID, "err", err); return
	}

	client, err := m.nodeAgentClients.Get(ctx, agentID, fmt.Sprintf("%s:%d", node.Ip, agent.Port))
	if err != nil {
		m.markUnhealthy(ctx, agentID)
		return
	}

	probeCtx, cancel := context.WithTimeout(ctx, m.probeTimeout)
	defer cancel()
	resp, err := client.GetHeartbeat(probeCtx, &nodeagentv1.GetHeartbeatRequest{}, grpc.WaitForReady(true))
	if err != nil {
		// 防抖：连续失败达阈值才标记失联（启动/重连瞬态不误报）
		count, _ := m.failCounts.LoadOrStore(agentID, 0)
		next := count.(int) + 1
		m.failCounts.Store(agentID, next)
		slog.Warn("NodeAgentHealthMonitor 心跳失败", "agent", agentID, "addr", fmt.Sprintf("%s:%d", node.Ip, agent.Port), "fail_count", next, "threshold", m.failThreshold, "err", err)
		if next >= m.failThreshold {
			m.markUnhealthy(ctx, agentID)
		}
		return
	}
	m.failCounts.Store(agentID, 0)
	m.mark(ctx, agentID, true)
	// 自检指标 → 健康状态（healthy/degraded）+ 动态资源/采样落库（3.4）
	if resp != nil {
		m.applyHeartbeat(ctx, agent, node, resp.GetHeartbeat())
		// P2：agent 版本自述（仅变化时更新；更新编排用）
		if v := resp.GetAgentVersion(); v != "" && v != agent.AgentVersion {
			if err := m.nodeAgentRepo.UpdateAgentVersion(ctx, agentID, v); err != nil {
				slog.Error("NodeAgentHealthMonitor 更新 agent 版本失败", "agent", agentID, "version", v, "err", err)
			}
		}
	}
}

// markUnhealthy 标记失联：alive=false + health_status=unhealthy
func (m *NodeAgentHealthMonitor) markUnhealthy(ctx context.Context, agentID string) {
	m.mark(ctx, agentID, false)
	if err := m.nodeAgentRepo.UpdateHealthStatus(ctx, agentID, entity.HealthUnhealthy); err != nil {
		slog.Error("NodeAgentHealthMonitor 更新健康状态失败", "agent", agentID, "err", err)
	}
	m.publishHealthChange(agentID, entity.HealthUnhealthy)
}

// publishHealthChange 健康状态变化事件（S30）
func (m *NodeAgentHealthMonitor) publishHealthChange(agentID string, status entity.NodeAgentHealthStatus) {
	if m.eventBus == nil {
		return
	}
	m.eventBus.Publish(SchedulerEvent{Type: EventNodeHealthChanged, OccurredAt: time.Now(),
		NodeAgentID: agentID, Detail: "健康状态 → " + healthStatusName(status)})
}

func healthStatusName(s entity.NodeAgentHealthStatus) string {
	switch s {
	case entity.HealthHealthy:
		return "healthy"
	case entity.HealthDegraded:
		return "degraded"
	case entity.HealthUnhealthy:
		return "unhealthy"
	default:
		return "unknown"
	}
}

func (m *NodeAgentHealthMonitor) mark(ctx context.Context, agentID string, alive bool) {
	now := time.Now()
	if err := m.nodeAgentRepo.UpdateHealth(ctx, agentID, alive, now); err != nil {
		slog.Error("NodeAgentHealthMonitor 更新状态失败", "agent", agentID, "err", err)
	}
}

// applyHeartbeat 按心跳自检指标计算健康状态并落库动态资源与采样（§9.2/§3.4）。
// node_agent 已上报 cpu/mem/disk 使用率 pct；绝对值按节点容量换算。
func (m *NodeAgentHealthMonitor) applyHeartbeat(ctx context.Context, agent *entity.NodeAgent, node *entity.Node, hb *nodeagentv1.NodeHeartbeat) {
	status := entity.HealthHealthy
	if hb != nil {
		if float64(hb.GetCpuUsagePct()) >= m.degradedPct ||
			float64(hb.GetMemoryUsagePct()) >= m.degradedPct ||
			float64(hb.GetDiskUsagePct()) >= m.degradedPct {
			status = entity.HealthDegraded
		}
	}
	if err := m.nodeAgentRepo.UpdateHealthStatus(ctx, agent.ID, status); err != nil {
		slog.Error("NodeAgentHealthMonitor 更新健康状态失败", "agent", agent.ID, "err", err)
	} else if status != agent.HealthStatus {
		// 健康状态变化（healthy ↔ degraded）→ 事件（S30）
		m.publishHealthChange(agent.ID, status)
	}
	if hb == nil {
		return
	}

	// B-04/P1-1：实例运行时统计（健康 + 在线人数）→ 缓存（按节点作用域替换）
	if m.runtimeStats != nil {
		stats := make([]InstanceRuntimeStat, 0, len(hb.GetInstanceRuntime()))
		for _, s := range hb.GetInstanceRuntime() {
			stats = append(stats, InstanceRuntimeStat{
				InstanceID:  s.GetInstanceId(),
				PlayerCount: s.GetPlayerCount(),
				MaxPlayers:  s.GetMaxPlayers(),
				Healthy:     s.GetHealthy(),
				ProbeMode:   s.GetProbeMode(),
				ProbeError:  s.GetProbeError(),
				SampledAt:   s.GetSampledAt(),
			})
		}
		m.runtimeStats.UpdateForNode(agent.ID, stats)
	}

	usage := entity.NodeDynamicUsage{
		CPUUsedMilli:    int64(float64(cpuCapacityMilli(node)) * float64(hb.GetCpuUsagePct()) / 100),
		MemoryUsedBytes: int64(float64(memoryCapacityBytes(node)) * float64(hb.GetMemoryUsagePct()) / 100),
		DiskUsedBytes:   int64(float64(diskCapacityBytes(node)) * float64(hb.GetDiskUsagePct()) / 100),
		NetRxBps:        int64(hb.GetNetRxBps()),
		NetTxBps:        int64(hb.GetNetTxBps()),
	}
	if err := m.nodeRepo.UpdateDynamicUsage(ctx, agent.NodeId, usage, time.Now()); err != nil {
		slog.Error("NodeAgentHealthMonitor 更新动态资源失败", "agent", agent.ID, "err", err)
	}
	if err := m.sampleRepo.Append(ctx, &entity.NodeResourceSample{
		NodeID:          agent.NodeId,
		SampledAt:       time.Now(),
		CPUUsedMilli:    usage.CPUUsedMilli,
		MemoryUsedBytes: usage.MemoryUsedBytes,
		DiskUsedBytes:   usage.DiskUsedBytes,
		NetRxBps:        usage.NetRxBps,
		NetTxBps:        usage.NetTxBps,
	}); err != nil {
		slog.Error("NodeAgentHealthMonitor 追加采样失败", "agent", agent.ID, "err", err)
	}
}
