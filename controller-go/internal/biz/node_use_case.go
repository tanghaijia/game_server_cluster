package biz

import (
	"context"
	"errors"
	"fmt"

	"controller-go/internal/entity"
	"controller-go/internal/repository"
)

type NodeUseCase struct {
	repo          repository.NodeRepository
	nodeAgentRepo repository.NodeAgentRepository
}

func NewNodeUseCase(repo repository.NodeRepository, nodeAgentRepo repository.NodeAgentRepository) *NodeUseCase {
	return &NodeUseCase{repo: repo, nodeAgentRepo: nodeAgentRepo}
}

func (uc *NodeUseCase) CreateNode(ip string) (*entity.Node, error) {
	node := &entity.Node{
		Ip: ip,
	}
	err := uc.repo.Save(node)
	if err != nil {
		return nil, err
	}
	return node, nil
}

// ListNodes 列出全部节点
func (uc *NodeUseCase) ListNodes(ctx context.Context) ([]*entity.Node, error) {
	return uc.repo.ListAll(ctx)
}

// GetNode 按 id 查询节点
func (uc *NodeUseCase) GetNode(ctx context.Context, id string) (*entity.Node, error) {
	if id == "" {
		return nil, errors.New("id is required")
	}
	return uc.repo.GetByID(id)
}

// NodeUpdate 节点可编辑字段（指针字段 = 仅更新非 nil 项）
type NodeUpdate struct {
	IP                *string  `json:"ip"`
	CoreNum           *int     `json:"core_num"`
	CoreFrequency     *float64 `json:"core_frequency"`
	MemorySize        *int64   `json:"memory_size"` // MB
	StorageSize       *int64   `json:"storage_size"` // MB
	Location          *string  `json:"location"`
	ServiceProvider   *string  `json:"service_provider"`
	NetRxLimitMbps    *int     `json:"net_rx_limit_mbps"` // 带宽上限（P3 调度评分用）
	NetTxLimitMbps    *int     `json:"net_tx_limit_mbps"`
}

// UpdateNode 更新节点配置（非 nil 字段生效）
func (uc *NodeUseCase) UpdateNode(ctx context.Context, id string, u NodeUpdate) (*entity.Node, error) {
	node, err := uc.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if u.IP != nil {
		node.Ip = *u.IP
	}
	if u.CoreNum != nil {
		node.CoreNum = *u.CoreNum
	}
	if u.CoreFrequency != nil {
		node.CoreFrequency = *u.CoreFrequency
	}
	if u.MemorySize != nil {
		node.MemorySize = *u.MemorySize
	}
	if u.StorageSize != nil {
		node.StorageSize = *u.StorageSize
	}
	if u.Location != nil {
		node.Location = *u.Location
	}
	if u.ServiceProvider != nil {
		node.ServiceProvider = *u.ServiceProvider
	}
	if u.NetRxLimitMbps != nil {
		node.NetRxLimitMbps = *u.NetRxLimitMbps
	}
	if u.NetTxLimitMbps != nil {
		node.NetTxLimitMbps = *u.NetTxLimitMbps
	}
	if err := uc.repo.Save(node); err != nil {
		return nil, err
	}
	return node, nil
}

// DeleteNode 删除节点：被 node_agent 引用时拒绝（须先删除 node_agent）
func (uc *NodeUseCase) DeleteNode(ctx context.Context, id string) error {
	if _, err := uc.repo.GetByID(id); err != nil {
		return err
	}
	agents, err := uc.nodeAgentRepo.ListAll(ctx)
	if err != nil {
		return err
	}
	for _, a := range agents {
		if a.NodeId == id {
			return fmt.Errorf("节点 %s 仍被 node_agent %s 引用，请先删除该 node_agent", id, a.ID)
		}
	}
	return uc.repo.Delete(ctx, id)
}
