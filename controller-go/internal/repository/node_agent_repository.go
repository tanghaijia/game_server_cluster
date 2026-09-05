package repository

import (
	"context"
	"time"

	"controller-go/internal/entity"
)

// NodeAgentRepository 定义 NodeAgent 数据层必须实现的接口
type NodeAgentRepository interface {
	Save(ctx context.Context, agent *entity.NodeAgent) error
	GetByID(ctx context.Context, id string) (*entity.NodeAgent, error)
	// ListEnabledIDs 返回已启用（Enabled）的 node_agent id 列表
	ListEnabledIDs(ctx context.Context) ([]string, error)
	// ListAll 查询全部 node_agent
	ListAll(ctx context.Context) ([]*entity.NodeAgent, error)
	// UpdateHealth 更新存活状态与最近心跳时间（心跳探测用）
	UpdateHealth(ctx context.Context, agentID string, alive bool, at time.Time) error
	// UpdateHealthStatus 更新健康状态机（9.3，S23）
	UpdateHealthStatus(ctx context.Context, agentID string, status entity.NodeAgentHealthStatus) error
	// UpdateAgentVersion 心跳上报 agent 版本（仅在与当前不同时更新，减少写入）
	UpdateAgentVersion(ctx context.Context, agentID, version string) error
	// UpdateUpdateState 更新一键更新状态机（000032）
	UpdateUpdateState(ctx context.Context, agentID, state, targetVersion, errMsg string) error
}
