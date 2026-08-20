package biz

import (
	"context"
	"fmt"

	"controller-go/internal/repository"
)

// DebugUseCase 提供调试 / 运维视角的聚合查询与手动触发能力。
// 与业务 UseCase 不同，它横跨多个领域对象，仅服务于 /debug/* 与诊断接口。
type DebugUseCase struct {
	dispatcher      *ReconcileDispatcher
	instanceRepo    repository.GameInstanceRepository
	portMappingRepo repository.ContainerPortMappingRepository
	nodeAgentRepo   repository.NodeAgentRepository
	nodeRepo        repository.NodeRepository
	scheduler       Scheduler
}

func NewDebugUseCase(
	dispatcher *ReconcileDispatcher,
	instanceRepo repository.GameInstanceRepository,
	portMappingRepo repository.ContainerPortMappingRepository,
	nodeAgentRepo repository.NodeAgentRepository,
	nodeRepo repository.NodeRepository,
	scheduler Scheduler,
) *DebugUseCase {
	return &DebugUseCase{
		dispatcher:      dispatcher,
		instanceRepo:    instanceRepo,
		portMappingRepo: portMappingRepo,
		nodeAgentRepo:   nodeAgentRepo,
		nodeRepo:        nodeRepo,
		scheduler:       scheduler,
	}
}

// ReconcileStatus 返回调度器当前运行状态：队列长度、自动重试计数、
// 处于中间态（待调度）的实例列表、调度器自身状态。
func (uc *DebugUseCase) ReconcileStatus(ctx context.Context) (map[string]any, error) {
	instances, err := uc.instanceRepo.ListByStatuses(ctx, dispatchableStatuses...)
	if err != nil {
		return nil, fmt.Errorf("list dispatchable instances: %w", err)
	}
	return map[string]any{
		"queue_len":              uc.dispatcher.QueueLen(),
		"retry_counts":           uc.dispatcher.RetryCounts(),
		"dispatchable_instances": instances,
		"scheduler":              uc.schedulerState(),
	}, nil
}

// Recover 手动触发调度恢复：把中间态实例重新入队（等价于重启后的 Recover 流程）。
func (uc *DebugUseCase) Recover(ctx context.Context) (int, error) {
	if err := uc.dispatcher.Recover(ctx); err != nil {
		return 0, err
	}
	return uc.dispatcher.QueueLen(), nil
}

// InstanceOverview 返回全部实例的聚合视图：实例 + 端口映射 + 所在 node_agent 地址。
// 便于一次看清 DB 状态与真实调度结果的对应关系。
func (uc *DebugUseCase) InstanceOverview(ctx context.Context) ([]map[string]any, error) {
	instances, err := uc.instanceRepo.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}

	out := make([]map[string]any, 0, len(instances))
	for _, inst := range instances {
		ports, err := uc.portMappingRepo.ListByInstanceId(ctx, inst.ID)
		if err != nil {
			return nil, fmt.Errorf("list port mappings of %s: %w", inst.ID, err)
		}

		var nodeAgentInfo map[string]any
		if inst.NodeAgentID != nil {
			nodeAgentInfo = uc.nodeAgentInfo(ctx, *inst.NodeAgentID)
		}

		out = append(out, map[string]any{
			"instance":   inst,
			"ports":      ports,
			"node_agent": nodeAgentInfo,
		})
	}
	return out, nil
}

// nodeAgentInfo 解析 node_agent 的 id / node_id / 连接地址（失败返回 error 字段）
func (uc *DebugUseCase) nodeAgentInfo(ctx context.Context, nodeAgentID string) map[string]any {
	agent, err := uc.nodeAgentRepo.GetByID(ctx, nodeAgentID)
	if err != nil {
		return map[string]any{"id": nodeAgentID, "error": err.Error()}
	}
	info := map[string]any{
		"id":      agent.ID,
		"node_id": agent.NodeId,
		"port":    agent.Port,
		"status":  agent.Status,
	}
	if node, err := uc.nodeRepo.GetByID(agent.NodeId); err == nil {
		info["addr"] = fmt.Sprintf("%s:%d", node.Ip, agent.Port)
	} else {
		info["addr_error"] = err.Error()
	}
	return info
}

// schedulerState 读取调度器内部状态（ResourceAwareScheduler 暴露统计，其他实现返回类型名）
func (uc *DebugUseCase) schedulerState() map[string]any {
	if s, ok := uc.scheduler.(*ResourceAwareScheduler); ok {
		return map[string]any{
			"type":     "resource_aware",
			"stats":    s.Stats(),
			"queue":    s.QueueStats(),
			"weights":  s.weights,
		}
	}
	return map[string]any{"type": fmt.Sprintf("%T", uc.scheduler)}
}
