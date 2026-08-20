package biz

import (
	"context"
	"fmt"
	"time"

	"controller-go/internal/entity"
	"controller-go/internal/repository"
)

// ObserverUseCase 调度观测（管理员视角，S28-S30/F1-F4 落地）：
// 节点资源总览 / 采样历史 / 排队详情 / 事件流 / 调度统计 / 试调度干跑（Preview）。
type ObserverUseCase struct {
	scheduler           *ResourceAwareScheduler
	nodeRepo            repository.NodeRepository
	nodeAgentRepo       repository.NodeAgentRepository
	sampleRepo          repository.NodeResourceSampleRepository
	queueRepo           repository.SchedulingQueueRepository
	instanceRepo        repository.GameInstanceRepository
	gameRepo            repository.GameRepository
	containerConfigRepo repository.GameContainerConfigRepository
	steamBranchRepo     repository.SteamBranchRepository
	eventBus            *SchedulerEventBus
	utilizationTarget   float64
	cacheView           *NodeCacheView
}

func NewObserverUseCase(
	scheduler *ResourceAwareScheduler,
	nodeRepo repository.NodeRepository,
	nodeAgentRepo repository.NodeAgentRepository,
	sampleRepo repository.NodeResourceSampleRepository,
	queueRepo repository.SchedulingQueueRepository,
	instanceRepo repository.GameInstanceRepository,
	gameRepo repository.GameRepository,
	containerConfigRepo repository.GameContainerConfigRepository,
	steamBranchRepo repository.SteamBranchRepository,
	eventBus *SchedulerEventBus,
	utilizationTarget float64,
	cacheView *NodeCacheView,
) *ObserverUseCase {
	return &ObserverUseCase{
		scheduler:           scheduler,
		nodeRepo:            nodeRepo,
		nodeAgentRepo:       nodeAgentRepo,
		sampleRepo:          sampleRepo,
		queueRepo:           queueRepo,
		instanceRepo:        instanceRepo,
		gameRepo:            gameRepo,
		containerConfigRepo: containerConfigRepo,
		steamBranchRepo:     steamBranchRepo,
		eventBus:            eventBus,
		utilizationTarget:   utilizationTarget,
		cacheView:           cacheView,
	}
}

// NodeOverview 节点资源总览（观测用）
type NodeOverview struct {
	NodeID              string  `json:"node_id"`
	NodeAgentID         string  `json:"node_agent_id,omitempty"`
	IP                  string  `json:"ip"`
	Location            string  `json:"location"`
	Enabled             bool    `json:"enabled"`
	Health              string  `json:"health"`  // unknown/healthy/degraded/unhealthy
	Alive               bool    `json:"alive"`
	Pressure            string  `json:"pressure"` // Normal/Warning/Critical
	CPUCapacityMilli    int64   `json:"cpu_capacity_milli"`
	CPUAllocatableMilli int64   `json:"cpu_allocatable_milli"`
	CPUUsedMilli        int64   `json:"cpu_used_milli"`
	CPUReservedMilli    int64   `json:"cpu_reserved_milli"`
	MemCapacityBytes    int64   `json:"mem_capacity_bytes"`
	MemAllocatableBytes int64   `json:"mem_allocatable_bytes"`
	MemUsedBytes        int64   `json:"mem_used_bytes"`
	MemReservedBytes    int64   `json:"mem_reserved_bytes"`
	DiskAllocatableBytes int64  `json:"disk_allocatable_bytes"`
	BandwidthRatio      float64 `json:"bandwidth_ratio"` // 0..1 带宽余量占比
}

// NodesOverview 全部节点资源总览
func (uc *ObserverUseCase) NodesOverview(ctx context.Context) ([]*NodeOverview, error) {
	nodes, err := uc.nodeRepo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	agents, err := uc.nodeAgentRepo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	agentByNode := make(map[string]*entity.NodeAgent)
	for _, a := range agents {
		agentByNode[a.NodeId] = a
	}
	out := make([]*NodeOverview, 0, len(nodes))
	for _, n := range nodes {
		ov := &NodeOverview{
			NodeID:              fmtID(n.Id),
			IP:                  n.Ip,
			Location:            n.Location,
			Pressure:            pressureStatusName(n.PressureStatus),
			CPUCapacityMilli:    cpuCapacityMilli(n),
			CPUUsedMilli:        n.CPUUsedMilli,
			CPUReservedMilli:    n.CPUReservedMilli,
			MemCapacityBytes:    memoryCapacityBytes(n),
			MemUsedBytes:        n.MemoryUsedBytes,
			MemReservedBytes:    n.MemoryReservedBytes,
			BandwidthRatio:      bandwidthRatio(n, 0, 0), // 观测页用预留视图（评分侧用 P95，见 candidate.go）
		}
		cap := ComputeCapacity(n, uc.utilizationTarget)
		ov.CPUAllocatableMilli = cap.CPUAllocatableMilli
		ov.MemAllocatableBytes = cap.MemAllocatableBytes
		ov.DiskAllocatableBytes = cap.DiskAllocatableBytes
		if a, ok := agentByNode[fmtID(n.Id)]; ok {
			ov.NodeAgentID = a.ID
			ov.Enabled = a.Status == entity.Enabled
			ov.Health = healthStatusName(a.HealthStatus)
			ov.Alive = a.Alive
		} else {
			ov.Health = "no_agent"
		}
		out = append(out, ov)
	}
	return out, nil
}

// NodeHistory 节点资源采样历史（曲线数据，时间降序）
func (uc *ObserverUseCase) NodeHistory(ctx context.Context, nodeID string, window time.Duration) ([]*entity.NodeResourceSample, error) {
	if window <= 0 {
		window = time.Hour
	}
	samples, err := uc.sampleRepo.ListSince(ctx, nodeID, time.Now().Add(-window))
	if err != nil {
		return nil, err
	}
	// 时间降序（最新在前）
	for i, j := 0, len(samples)-1; i < j; i, j = i+1, j-1 {
		samples[i], samples[j] = samples[j], samples[i]
	}
	return samples, nil
}

// QueueItemOverview 排队项（观测用）
type QueueItemOverview struct {
	InstanceID      string `json:"instance_id"`
	GameID          string `json:"game_id"`
	Priority        int    `json:"priority"`
	Reason          string `json:"reason"`
	Attempts        int    `json:"attempts"`
	QueuedAt        string `json:"queued_at"`
	WakeAt          string `json:"wake_at"`
	WaitSeconds     int64  `json:"wait_seconds"`
	RemainingSeconds int64 `json:"remaining_seconds"` // 到排队超时的剩余秒数（<0 = 已超时待清理）
}

// QueueOverview 排队详情列表
func (uc *ObserverUseCase) QueueOverview(ctx context.Context) ([]*QueueItemOverview, error) {
	qs, err := uc.queueRepo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	instByID := make(map[string]*entity.GameInstance)
	instances, err := uc.instanceRepo.ListAll(ctx)
	if err == nil {
		for _, inst := range instances {
			instByID[inst.ID] = inst
		}
	}
	now := time.Now()
	out := make([]*QueueItemOverview, 0, len(qs))
	for _, q := range qs {
		item := &QueueItemOverview{
			InstanceID:  q.InstanceID,
			Priority:    q.Priority,
			Reason:      q.Reason,
			Attempts:    q.Attempts,
			QueuedAt:    q.QueuedAt.Format(time.RFC3339),
			WakeAt:      q.WakeAt.Format(time.RFC3339),
			WaitSeconds: int64(now.Sub(q.QueuedAt).Seconds()),
			RemainingSeconds: int64(q.TimeoutAt.Sub(now).Seconds()),
		}
		if inst, ok := instByID[q.InstanceID]; ok {
			item.GameID = inst.GameID
		}
		out = append(out, item)
	}
	return out, nil
}

// Events 调度事件流（S30）：hours>0 时从 DB 查持久化历史（重启后可回溯），否则读内存实时缓冲。
func (uc *ObserverUseCase) Events(ctx context.Context, limit int, typ SchedulerEventType, hours int) []SchedulerEvent {
	if uc.eventBus == nil {
		return nil
	}
	if hours > 0 {
		return uc.eventBus.History(ctx, time.Now().Add(-time.Duration(hours)*time.Hour), string(typ), limit)
	}
	return uc.eventBus.Recent(limit, typ)
}

// SchedulerStats 调度统计（指标 S29）
type SchedulerStats struct {
	Attempts   map[string]int64 `json:"attempts"` // scheduled/queued/failed
	QueueLen   int64            `json:"queue_len"`
	EventCount int              `json:"event_count"`
}

func (uc *ObserverUseCase) SchedulerStats(ctx context.Context) SchedulerStats {
	st := SchedulerStats{Attempts: map[string]int64{}}
	if uc.scheduler != nil {
		for k, v := range uc.scheduler.Stats() {
			st.Attempts[k] = v
		}
	}
	if n, err := uc.queueRepo.Count(ctx); err == nil {
		st.QueueLen = n
	}
	if uc.eventBus != nil {
		st.EventCount = uc.eventBus.Len()
	}
	return st
}

// PreviewRequest 试调度入参（观测/调试）
type PreviewRequest struct {
	GameID      string                 `json:"game_id"`
	GameBuildID string                 `json:"game_build_id,omitempty"`
	Region      string                 `json:"region,omitempty"`
	Resources   *entity.ResourceRequest `json:"resources,omitempty"`
}

// PreviewSchedule 试调度干跑：返回每个候选节点的约束判定与得分（不预留、不落库）
func (uc *ObserverUseCase) PreviewSchedule(ctx context.Context, req PreviewRequest) (*PreviewResult, error) {
	buildID := req.GameBuildID
	if buildID == "" {
		branches, err := uc.steamBranchRepo.ListByGame(ctx, req.GameID)
		if err == nil {
			for _, b := range branches {
				if b.Status == entity.Enable {
					buildID = fmt.Sprintf("%d", b.LastBuildId)
					break
				}
			}
		}
	}
	inst := &entity.GameInstance{
		GameID:           req.GameID,
		GameBuildId:      buildID,
		Region:           req.Region,
		ResourceReq:      req.Resources,
		ResourceOverride: req.Resources != nil, // 试调度传入的资源视为显式覆盖
	}
	return uc.scheduler.Preview(ctx, inst)
}

// NodeCacheOverview 节点缓存条目（管理员观测：某节点上各 game/branch 的缓存状态）
type NodeCacheOverview struct {
	NodeAgentID      string  `json:"node_agent_id"`
	NodeID           string  `json:"node_id,omitempty"`
	GameID           string  `json:"game_id"`
	Branch           string  `json:"branch"`
	Status           string  `json:"status"` // available/downloading/removed/unavailable/missing
	BuildID          string  `json:"build_id"`
	DownloadProgress float32 `json:"download_progress"`
}

// CacheOverview 全部 enabled 节点的 game-cache 状态（来自 NodeCacheView 快照，§10）
func (uc *ObserverUseCase) CacheOverview(ctx context.Context) ([]*NodeCacheOverview, error) {
	if uc.cacheView == nil {
		return nil, nil
	}
	snapshot := uc.cacheView.ListSnapshot()
	agents, err := uc.nodeAgentRepo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	agentByID := make(map[string]*entity.NodeAgent, len(agents))
	for _, a := range agents {
		agentByID[a.ID] = a
	}
	out := make([]*NodeCacheOverview, 0)
	for agentID, entries := range snapshot {
		base := NodeCacheOverview{NodeAgentID: agentID}
		if a, ok := agentByID[agentID]; ok {
			base.NodeID = a.NodeId
		}
		for _, e := range entries {
			row := base
			row.GameID = e.GameID
			row.Branch = e.Branch
			row.Status = e.Status
			row.BuildID = e.BuildID
			row.DownloadProgress = e.DownloadProgress
			out = append(out, &row)
		}
	}
	return out, nil
}
