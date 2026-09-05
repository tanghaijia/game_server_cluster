package biz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"controller-go/internal/client/nodeagent"
	"controller-go/internal/entity"
	"controller-go/internal/repository"
	nodeagentv1 "controller-go/internal/third/nodeagent/v1"

	"google.golang.org/grpc"
	"gorm.io/gorm"
)

// NodeAgentUpdateTarget 单节点更新请求
type NodeAgentUpdateTarget struct {
	AgentID   string
	ReleaseID string
}

// NodeAgentUpdateResult 单节点更新结果（含前置过滤跳过原因）
type NodeAgentUpdateResult struct {
	AgentID     string `json:"agent_id"`
	OK          bool   `json:"ok"`
	Skipped     bool   `json:"skipped,omitempty"`
	Reason      string `json:"reason,omitempty"`
	TargetVer   string `json:"target_version,omitempty"`
	CurrentVer  string `json:"current_version,omitempty"`
}

// NodeAgentUpdateOrchestrator node_agent 一键更新编排（P3，见 docs/node-agent-upgrade-design.md §3.3）。
// 串行执行：前置过滤（失联/有活跃实例/已是最新）→ 状态机落库 → gRPC 下发 →
// 轮询心跳回归新版本确认完成。
type NodeAgentUpdateOrchestrator struct {
	nodeAgentRepo repository.NodeAgentRepository
	nodeRepo      repository.NodeRepository
	instanceRepo  repository.GameInstanceRepository
	releaseRepo   repository.AgentReleaseRepository
	clients       *nodeagent.ClientRegistry
	store         ReleaseStore
	// 下载端点 base：node_agent 可达的 controller 地址（http://host:port）
	downloadBaseURL string
	rpcTimeout      time.Duration
	// 等待心跳回归新版本的轮询参数
	waitTimeout time.Duration
	pollEvery   time.Duration
}

func NewNodeAgentUpdateOrchestrator(
	nodeAgentRepo repository.NodeAgentRepository,
	nodeRepo repository.NodeRepository,
	instanceRepo repository.GameInstanceRepository,
	releaseRepo repository.AgentReleaseRepository,
	clients *nodeagent.ClientRegistry,
	store ReleaseStore,
	downloadBaseURL string,
	rpcTimeout time.Duration,
	waitTimeout time.Duration,
) *NodeAgentUpdateOrchestrator {
	return &NodeAgentUpdateOrchestrator{
		nodeAgentRepo:   nodeAgentRepo,
		nodeRepo:        nodeRepo,
		instanceRepo:    instanceRepo,
		releaseRepo:     releaseRepo,
		clients:         clients,
		store:           store,
		downloadBaseURL: downloadBaseURL,
		rpcTimeout:      rpcTimeout,
		waitTimeout:     waitTimeout,
		pollEvery:       3 * time.Second,
	}
}

// 活跃实例状态集合（更新会重启 agent，节点上有这些实例不允许更新）
func isActiveInstanceStatus(s entity.InstanceStatus) bool {
	switch s {
	case entity.StatusPending, entity.StatusScheduling, entity.StatusPreparingBuild,
		entity.StatusRestoringSnapshot, entity.StatusStarting, entity.StatusRunning,
		entity.StatusStopping, entity.StatusCleaning, entity.StatusQueued,
		entity.StatusCacheWarming:
		return true
	default:
		return false
	}
}

// Update 串行执行一批节点更新。每个节点独立结果；互不阻塞。
func (o *NodeAgentUpdateOrchestrator) Update(ctx context.Context, targets []NodeAgentUpdateTarget) []NodeAgentUpdateResult {
	results := make([]NodeAgentUpdateResult, 0, len(targets))
	for _, t := range targets {
		res := o.updateOne(ctx, t)
		results = append(results, res)
	}
	return results
}

func (o *NodeAgentUpdateOrchestrator) updateOne(ctx context.Context, t NodeAgentUpdateTarget) NodeAgentUpdateResult {
	agent, err := o.nodeAgentRepo.GetByID(ctx, t.AgentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return NodeAgentUpdateResult{AgentID: t.AgentID, Reason: "node_agent 不存在"}
		}
		return NodeAgentUpdateResult{AgentID: t.AgentID, Reason: "load node_agent: " + err.Error()}
	}
	// 目标 release
	release, err := o.releaseRepo.GetByID(ctx, t.ReleaseID)
	if err != nil {
		return NodeAgentUpdateResult{AgentID: t.AgentID, CurrentVer: agent.AgentVersion,
			Reason: "release 不存在: " + t.ReleaseID}
	}

	// ---- 前置过滤 ----
	if !agent.Alive {
		return NodeAgentUpdateResult{AgentID: t.AgentID, Skipped: true,
			CurrentVer: agent.AgentVersion, TargetVer: release.Version, Reason: "agent 失联（不可更新）"}
	}
	if agent.UpdateState != "" && agent.UpdateState != "idle" && agent.UpdateState != "updated" && agent.UpdateState != "failed" {
		return NodeAgentUpdateResult{AgentID: t.AgentID, Skipped: true,
			CurrentVer: agent.AgentVersion, TargetVer: release.Version,
			Reason: "已有更新进行中（state=" + agent.UpdateState + "）"}
	}
	if agent.AgentVersion == release.Version {
		return NodeAgentUpdateResult{AgentID: t.AgentID, Skipped: true, OK: true,
			CurrentVer: agent.AgentVersion, TargetVer: release.Version, Reason: "已是最新版本"}
	}
	// 活跃实例检查（controller 侧第一道；node_agent 还会自检拒绝）
	active, err := o.activeInstancesOnAgent(ctx, t.AgentID)
	if err != nil {
		return NodeAgentUpdateResult{AgentID: t.AgentID, CurrentVer: agent.AgentVersion,
			TargetVer: release.Version, Reason: "检查活跃实例失败: " + err.Error()}
	}
	if len(active) > 0 {
		return NodeAgentUpdateResult{AgentID: t.AgentID, Skipped: true,
			CurrentVer: agent.AgentVersion, TargetVer: release.Version,
			Reason: fmt.Sprintf("节点仍有 %d 个活跃实例（如 %s），请先停止", len(active), active[0])}
	}

	// ---- 状态机：开始 ----
	if err := o.nodeAgentRepo.UpdateUpdateState(ctx, t.AgentID, "downloading", release.Version, ""); err != nil {
		return NodeAgentUpdateResult{AgentID: t.AgentID, Reason: "更新状态落库失败: " + err.Error()}
	}

	// ---- 定位 node（IP）→ gRPC 下发 ----
	node, err := o.nodeRepo.GetByID(agent.NodeId)
	if err != nil {
		o.fail(ctx, t.AgentID, release.Version, "load node: "+err.Error())
		return NodeAgentUpdateResult{AgentID: t.AgentID, TargetVer: release.Version, Reason: "load node: " + err.Error()}
	}
	client, err := o.clients.Get(ctx, t.AgentID, fmt.Sprintf("%s:%d", node.Ip, agent.Port))
	if err != nil {
		o.fail(ctx, t.AgentID, release.Version, "connect node_agent: "+err.Error())
		return NodeAgentUpdateResult{AgentID: t.AgentID, TargetVer: release.Version, Reason: "connect node_agent: " + err.Error()}
	}

	downloadURL := fmt.Sprintf("%s/api/node-agents/releases/%s/download", o.downloadBaseURL, release.ID)
	rpcCtx, cancel := context.WithTimeout(ctx, o.rpcTimeout)
	defer cancel()
	resp, err := client.UpdateNodeAgent(rpcCtx, &nodeagentv1.UpdateNodeAgentRequest{
		Version:     release.Version,
		Sha256:      release.SHA256,
		SizeBytes:   release.SizeBytes,
		DownloadUrl: downloadURL,
	}, grpc.WaitForReady(true))
	if err != nil {
		o.fail(ctx, t.AgentID, release.Version, "下发更新失败: "+err.Error())
		return NodeAgentUpdateResult{AgentID: t.AgentID, TargetVer: release.Version, Reason: "下发更新失败: " + err.Error()}
	}
	if resp == nil || resp.GetState() == "failed" {
		msg := "更新被 node_agent 拒绝"
		if resp != nil {
			msg = resp.GetMessage()
		}
		o.fail(ctx, t.AgentID, release.Version, msg)
		return NodeAgentUpdateResult{AgentID: t.AgentID, TargetVer: release.Version, Reason: msg}
	}

	// ---- 等心跳回归新版本 ----
	if err := o.nodeAgentRepo.UpdateUpdateState(ctx, t.AgentID, "rebooting", release.Version, ""); err != nil {
		slog.Warn("更新状态 rebooting 落库失败", "agent", t.AgentID, "err", err)
	}
	deadline := time.Now().Add(o.waitTimeout)
	for time.Now().Before(deadline) {
		time.Sleep(o.pollEvery)
		cur, err := o.nodeAgentRepo.GetByID(ctx, t.AgentID)
		if err != nil {
			continue
		}
		if cur.AgentVersion == release.Version {
			_ = o.nodeAgentRepo.UpdateUpdateState(ctx, t.AgentID, "updated", release.Version, "")
			return NodeAgentUpdateResult{AgentID: t.AgentID, OK: true,
				CurrentVer: release.Version, TargetVer: release.Version, Reason: "更新完成"}
		}
	}
	o.fail(ctx, t.AgentID, release.Version, "心跳未在时限内回归新版本（agent 可能未自动重启，请检查部署）")
	return NodeAgentUpdateResult{AgentID: t.AgentID, TargetVer: release.Version,
		Reason: "心跳未在时限内回归新版本（agent 可能未自动重启，请检查部署）"}
}

func (o *NodeAgentUpdateOrchestrator) fail(ctx context.Context, agentID, targetVersion, reason string) {
	if err := o.nodeAgentRepo.UpdateUpdateState(ctx, agentID, "failed", targetVersion, reason); err != nil {
		slog.Error("更新失败状态落库失败", "agent", agentID, "err", err)
	}
}

// activeInstancesOnAgent 该 agent 上的活跃实例 id 列表
func (o *NodeAgentUpdateOrchestrator) activeInstancesOnAgent(ctx context.Context, agentID string) ([]string, error) {
	instances, err := o.instanceRepo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, inst := range instances {
		if inst.NodeAgentID != nil && *inst.NodeAgentID == agentID && isActiveInstanceStatus(inst.Status) {
			out = append(out, inst.ID)
		}
	}
	return out, nil
}
