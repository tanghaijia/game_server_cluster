package repository

import (
	"context"

	"controller-go/internal/entity"
)

// AgentReleaseRepository node_agent 发布清单（000031，见 docs/node-agent-upgrade-design.md）
type AgentReleaseRepository interface {
	Save(ctx context.Context, release *entity.AgentRelease) error
	GetByID(ctx context.Context, id string) (*entity.AgentRelease, error)
	ListAll(ctx context.Context) ([]*entity.AgentRelease, error)
	// GetByVersionOSArch 精确查（同版本同平台重复上传时用于去重/覆盖提示）
	GetByVersionOSArch(ctx context.Context, version, osName, arch string) (*entity.AgentRelease, error)
}
