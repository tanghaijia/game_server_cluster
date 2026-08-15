package biz

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"controller-go/internal/client/nodeagent"
	nodeagentv1 "controller-go/internal/third/nodeagent/v1"
	"controller-go/internal/repository"

	"google.golang.org/grpc"
)

// NodeAgentHealthMonitor 周期探测 node_agent 存活状态：
// 1) 对每个 Enabled node_agent 调用 GetHeartbeat（gRPC，带超时）；
// 2) 成功 → 标记存活并落库（alive + last_heartbeat_at）；失败 → 标记失联；
// 3) 同步通知 SimpleScheduler 过滤失联节点（调度不再选择）。
type NodeAgentHealthMonitor struct {
	nodeAgentRepo    repository.NodeAgentRepository
	nodeRepo         repository.NodeRepository
	nodeAgentClients *nodeagent.ClientRegistry
	scheduler        *SimpleScheduler
	probeTimeout     time.Duration
	failThreshold    int
	failCounts       sync.Map // agentID -> 连续失败次数（防启动/重连瞬态误报）
}

func NewNodeAgentHealthMonitor(
	nodeAgentRepo repository.NodeAgentRepository,
	nodeRepo repository.NodeRepository,
	nodeAgentClients *nodeagent.ClientRegistry,
	scheduler *SimpleScheduler,
	probeTimeout time.Duration,
	failThreshold int,
) *NodeAgentHealthMonitor {
	if failThreshold <= 0 {
		failThreshold = 2
	}
	return &NodeAgentHealthMonitor{
		nodeAgentRepo:    nodeAgentRepo,
		nodeRepo:         nodeRepo,
		nodeAgentClients: nodeAgentClients,
		scheduler:        scheduler,
		probeTimeout:     probeTimeout,
		failThreshold:    failThreshold,
	}
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
		m.mark(ctx, agentID, false); return
	}

	probeCtx, cancel := context.WithTimeout(ctx, m.probeTimeout)
	defer cancel()
	_, err = client.GetHeartbeat(probeCtx, &nodeagentv1.GetHeartbeatRequest{}, grpc.WaitForReady(true))
	if err != nil {
		// 防抖：连续失败达阈值才标记失联（启动/重连瞬态不误报）
		count, _ := m.failCounts.LoadOrStore(agentID, 0)
		next := count.(int) + 1
		m.failCounts.Store(agentID, next)
		slog.Warn("NodeAgentHealthMonitor 心跳失败", "agent", agentID, "addr", fmt.Sprintf("%s:%d", node.Ip, agent.Port), "fail_count", next, "threshold", m.failThreshold, "err", err)
		if next >= m.failThreshold {
			m.mark(ctx, agentID, false)
		}
		return
	}
	m.failCounts.Store(agentID, 0)
	m.mark(ctx, agentID, true)
}

func (m *NodeAgentHealthMonitor) mark(ctx context.Context, agentID string, alive bool) {
	now := time.Now()
	m.scheduler.SetAlive(agentID, alive)
	if err := m.nodeAgentRepo.UpdateHealth(ctx, agentID, alive, now); err != nil {
		slog.Error("NodeAgentHealthMonitor 更新状态失败", "agent", agentID, "err", err)
	}
}
