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

// NodeCacheView game-cache 视图（§10）：进程内快照，周期刷新；
// 调度 H5 判定从快照读取（避免每次调度实时 gRPC 查询 node_agent）。
// 与 GameCacheManager 职责分离：它负责"把缓存推送到节点"，本视图只负责"知道谁有缓存"。
type NodeCacheView struct {
	mu        sync.RWMutex
	available map[string]map[string]bool // agentID -> "gameID:branchName" -> AVAILABLE

	fetcher    CacheSnapshotFetcher
	agentRepo  repository.NodeAgentRepository
	branchRepo repository.SteamBranchRepository
	gameRepo   repository.GameRepository

	// 缓存状态变化（转 AVAILABLE）回调：唤醒排队（S14）
	onCacheReady func()
}

func NewNodeCacheView(
	fetcher CacheSnapshotFetcher,
	agentRepo repository.NodeAgentRepository,
	branchRepo repository.SteamBranchRepository,
	gameRepo repository.GameRepository,
) *NodeCacheView {
	return &NodeCacheView{
		available:  make(map[string]map[string]bool),
		fetcher:    fetcher,
		agentRepo:  agentRepo,
		branchRepo: branchRepo,
		gameRepo:   gameRepo,
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

	snapshot := make(map[string]map[string]bool, len(agentIDs))
	for _, agentID := range agentIDs {
		m := make(map[string]bool)
		for gameID, branches := range enabledBranches {
			for _, branch := range branches {
				m[gameID+":"+branch] = v.fetchAvailable(ctx, agentID, gameID, branch)
			}
		}
		snapshot[agentID] = m
	}

	v.mu.Lock()
	changed := v.diff(snapshot)
	v.available = snapshot
	v.mu.Unlock()

	if changed && v.onCacheReady != nil {
		slog.Info("NodeCacheView 检测到缓存状态变化，唤醒排队")
		v.onCacheReady()
	}
}

func (v *NodeCacheView) fetchAvailable(ctx context.Context, agentID, gameID, branch string) bool {
	gc, err := v.fetcher.GetNodeCache(ctx, agentID, gameID, branch)
	if err != nil {
		return false // 查询失败视为无缓存（保守）
	}
	return gc != nil && gc.GetStatus() == nodeagentv1.GameCacheStatus_AVAILABLE
}

// diff 比较新旧快照，返回是否有任何节点缓存转为可用
func (v *NodeCacheView) diff(next map[string]map[string]bool) bool {
	for agentID, m := range next {
		old, ok := v.available[agentID]
		if !ok {
			for _, avail := range m {
				if avail {
					return true
				}
			}
			continue
		}
		for key, avail := range m {
			if avail && !old[key] {
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
	m, ok := v.available[agentID]
	if !ok {
		return false, nil
	}
	return m[gameID+":"+branchName], nil
}

var _ CacheStatusProvider = (*NodeCacheView)(nil)
