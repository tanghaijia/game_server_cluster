package biz

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"controller-go/internal/entity"
	"controller-go/internal/repository"

	nodeagentv1 "controller-go/internal/third/nodeagent/v1"
)

// CacheSnapshotFetcher 拉取单节点 (game, branch) 缓存状态的接口（GameCacheManager 实现 GetNodeCache）
type CacheSnapshotFetcher interface {
	GetNodeCache(ctx context.Context, nodeAgentID, gameID, branchName string) (*nodeagentv1.GameCache, error)
}

// CacheEntry 单个 (game, branch) 在节点上的缓存状态（快照条目）
type CacheEntry struct {
	GameID           string  `json:"game_id"`
	Branch           string  `json:"branch"`
	Available        bool    `json:"available"` // status == AVAILABLE（H5 判定）
	Status           string  `json:"status"`    // available/downloading/removed/unavailable/missing
	BuildID          string  `json:"build_id"`
	DownloadProgress float32 `json:"download_progress"`
	// P2-B：缓存内容实测字节数（node 下载完成后上报；0 = 未知）
	SizeBytes uint64 `json:"size_bytes"`
	// P4（下载可观测性）：最近一次失败原因（node 落库透传；空 = 无失败/成功）
	LastError string `json:"last_error,omitempty"`
}

// NodeCacheView game-cache 视图（§10）：进程内快照，周期刷新；
// 调度 H5 判定与管理员观测（节点缓存状态）共用。
// 与 GameCacheManager 职责分离：它负责"把缓存推送到节点"，本视图只负责"知道谁有缓存、什么状态"。
type NodeCacheView struct {
	mu       sync.RWMutex
	snapshot map[string]map[string]*CacheEntry // agentID -> "gameID:branchName" -> entry

	fetcher    CacheSnapshotFetcher
	agentRepo  repository.NodeAgentRepository
	branchRepo repository.SteamBranchRepository
	gameRepo   repository.GameRepository
	eventBus   *SchedulerEventBus

	// 缓存状态变化（转 AVAILABLE）回调：唤醒排队（S14）
	onCacheReady func()
}

func NewNodeCacheView(
	fetcher CacheSnapshotFetcher,
	agentRepo repository.NodeAgentRepository,
	branchRepo repository.SteamBranchRepository,
	gameRepo repository.GameRepository,
	eventBus *SchedulerEventBus,
) *NodeCacheView {
	return &NodeCacheView{
		snapshot:   make(map[string]map[string]*CacheEntry),
		fetcher:    fetcher,
		agentRepo:  agentRepo,
		branchRepo: branchRepo,
		gameRepo:   gameRepo,
		eventBus:   eventBus,
	}
}

// SetOnCacheReady 注册缓存就绪回调（main.go 注入 queueWaker.Wake）
func (v *NodeCacheView) SetOnCacheReady(fn func()) {
	v.onCacheReady = fn
}

// Start 启动周期刷新（interval 每轮间隔；启动后立即执行一轮）
func (v *NodeCacheView) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	slog.Info("NodeCacheView 启动", "refresh_interval", interval.String())
	go func() {
		v.refreshOnce(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				slog.Info("NodeCacheView 退出")
				return
			case <-ticker.C:
				v.refreshOnce(ctx)
			}
		}
	}()
}

// refreshOnce 刷新快照：对每个 enabled node_agent × Enable 分支查询缓存状态（§10）
func (v *NodeCacheView) refreshOnce(ctx context.Context) {
	agentIDs, err := v.agentRepo.ListEnabledIDs(ctx)
	if err != nil {
		slog.Error("NodeCacheView 查询 enabled 节点失败", "err", err)
		return
	}
	// Enable 分支：gameID -> []branchName
	enabledBranches := make(map[string][]string)
	games, err := v.gameRepo.ListAll(ctx)
	if err != nil {
		slog.Error("NodeCacheView 查询游戏失败", "err", err)
		return
	}
	for _, g := range games {
		branches, err := v.branchRepo.ListByGame(ctx, g.ID)
		if err != nil {
			continue
		}
		for _, b := range branches {
			if b.Status == entity.Enable {
				enabledBranches[g.ID] = append(enabledBranches[g.ID], b.BranchName)
			}
		}
	}

	snapshot := make(map[string]map[string]*CacheEntry, len(agentIDs))
	for _, agentID := range agentIDs {
		m := make(map[string]*CacheEntry)
		for gameID, branches := range enabledBranches {
			for _, branch := range branches {
				m[gameID+":"+branch] = v.fetchEntry(ctx, agentID, gameID, branch)
			}
		}
		snapshot[agentID] = m
	}

	v.mu.Lock()
	changed := v.diff(snapshot)
	v.snapshot = snapshot
	v.mu.Unlock()

	if changed && v.onCacheReady != nil {
		slog.Info("NodeCacheView 检测到缓存状态变化，唤醒排队")
		v.onCacheReady()
	}
	if changed && v.eventBus != nil {
		v.eventBus.Publish(SchedulerEvent{Type: EventCacheReady, OccurredAt: time.Now(),
			Detail: "game-cache 状态变化（部分转 AVAILABLE）"})
	}
}

func (v *NodeCacheView) fetchEntry(ctx context.Context, agentID, gameID, branch string) *CacheEntry {
	entry := &CacheEntry{GameID: gameID, Branch: branch, Status: "missing"}
	gc, err := v.fetcher.GetNodeCache(ctx, agentID, gameID, branch)
	if err != nil {
		// 查询失败视为无缓存（保守），状态标记 missing
		return entry
	}
	if gc == nil {
		return entry
	}
	entry.BuildID = gc.GetBuildId()
	entry.DownloadProgress = gc.GetDownloadProgress()
	// P2-B：缓存内容实测大小（磁盘记账输入）
	entry.SizeBytes = gc.GetSizeBytes()
	// P4：失败原因透传（快照 30s 刷新，admin 观测可见）
	entry.LastError = gc.GetLastError()
	switch gc.GetStatus() {
	case nodeagentv1.GameCacheStatus_AVAILABLE:
		entry.Available = true
		entry.Status = "available"
	case nodeagentv1.GameCacheStatus_DOWNLOADING:
		entry.Status = "downloading"
	case nodeagentv1.GameCacheStatus_REMOVED:
		entry.Status = "removed"
	default:
		entry.Status = "unavailable"
	}
	return entry
}

// diff 比较新旧快照，返回是否有任何节点缓存转为可用
func (v *NodeCacheView) diff(next map[string]map[string]*CacheEntry) bool {
	for agentID, m := range next {
		old, ok := v.snapshot[agentID]
		if !ok {
			for _, e := range m {
				if e.Available {
					return true
				}
			}
			continue
		}
		for key, e := range m {
			if e.Available && (old[key] == nil || !old[key].Available) {
				return true
			}
		}
	}
	return false
}

// CacheAvailable 实现 CacheStatusProvider（H5 判定）：从快照读取；
// 快照缺失的节点/分支视为无缓存（保守，S10/D2）。
func (v *NodeCacheView) CacheAvailable(ctx context.Context, agentID, gameID, branchName string) (bool, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	m, ok := v.snapshot[agentID]
	if !ok {
		return false, nil
	}
	e := m[gameID+":"+branchName]
	return e != nil && e.Available, nil
}

// ListSnapshot 返回全量快照（管理员观测：某节点上所有 game/branch 的缓存状态）
func (v *NodeCacheView) ListSnapshot() map[string][]*CacheEntry {
	v.mu.RLock()
	defer v.mu.RUnlock()
	out := make(map[string][]*CacheEntry, len(v.snapshot))
	for agentID, m := range v.snapshot {
		entries := make([]*CacheEntry, 0, len(m))
		for _, e := range m {
			entries = append(entries, e)
		}
		out[agentID] = entries
	}
	return out
}

// CacheDiskUsageBytes 返回某节点缓存的磁盘占用（P2-B 记账，§8.4）：
// availableBytes = Σ AVAILABLE 版本 size；downloadingBytes = Σ DOWNLOADING 版本 size。
// 调度/placer 用它做"可用缓存预算 = cache_budget − reserved_cache − 更新缓冲"的输入。
// 注：快照只覆盖 Enable 分支（Disabled/Abandoned 分支的缓存由 P1 删除循环收敛，未计入）。
func (v *NodeCacheView) CacheDiskUsageBytes(agentID string) (availableBytes, downloadingBytes uint64) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	m, ok := v.snapshot[agentID]
	if !ok {
		return 0, 0
	}
	for _, e := range m {
		if e.Status == "downloading" {
			downloadingBytes += e.SizeBytes
		} else if e.Available {
			availableBytes += e.SizeBytes
		}
	}
	return availableBytes, downloadingBytes
}

// CacheState 实现 CacheStatusProvider：从快照读取 (game, branch) 缓存状态（P2-C 调度用）。
func (v *NodeCacheView) CacheState(ctx context.Context, agentID, gameID, branchName string) (CacheState, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	m, ok := v.snapshot[agentID]
	if !ok {
		return CacheState{Status: "missing"}, nil
	}
	e := m[gameID+":"+branchName]
	if e == nil {
		return CacheState{Status: "missing"}, nil
	}
	return CacheState{Status: e.Status, Available: e.Available, SizeBytes: e.SizeBytes}, nil
}

// BranchSizeBytes 实现 CacheStatusProvider：返回某 (game, branch) 的已知缓存大小
// （集群内任意节点实测最大者；P2-C 冷节点磁盘硬约束的 needs 值）。
// ok=false 表示全集群未知（从未下载过）→ 调度放行，下载期 ENOSPC 由 Unavailable 兜底。
func (v *NodeCacheView) BranchSizeBytes(ctx context.Context, gameID, branchName string) (uint64, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	key := gameID + ":" + branchName
	var maxSize uint64
	for _, m := range v.snapshot {
		if e, ok := m[key]; ok && e.SizeBytes > maxSize {
			maxSize = e.SizeBytes
		}
	}
	return maxSize, maxSize > 0
}

var _ CacheStatusProvider = (*NodeCacheView)(nil)
